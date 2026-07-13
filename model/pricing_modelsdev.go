package model

import (
	"done-hub/common/config"
	"done-hub/common/logger"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"time"

	"gorm.io/datatypes"
)

// models.dev/api.json 的价格同步支持。
//
// models.dev 的数据形态与 done-hub 的 Price 完全不同,需要换算:
//   - 结构:provider -> models -> cost{}（嵌套），done-hub 是扁平的 []*Price
//   - 单位:cost.* 是「美元 / 百万 token」的绝对价，done-hub 的 Input/Output 是倍率
//           （基准 1 倍率 = $0.002/1K = $2/百万token，见 DollarRate）
//   - provider:字符串（"xai"/"openai"...），done-hub 用整数 ChannelType
//
// 换算公式（done-hub 的 Input/Output 都是各自独立的绝对倍率，计费见
// relay/relay_util/quota.go: quota = prompt*GetInput() + completion*GetOutput()）:
//   Input  = cost.input  / modelsDevCostPerMillionBase   // 绝对倍率
//   Output = cost.output / modelsDevCostPerMillionBase   // 绝对倍率（直接除，不是 output/input）
//   ExtraRatios[cached_read]  = cost.cache_read  / cost.input   // 相对 input 的倍率
//   ExtraRatios[cached_write] = cost.cache_write / cost.input   // 相对 input 的倍率
//   LongContext = { Threshold: tiers[0].tier.size,
//                   InputRatio:  tiers[0].input  / cost.input,
//                   OutputRatio: tiers[0].output / cost.output }
//
// 换算思路参考 new-api 的 convertModelsDevToRatioData，但公式按 done-hub 的计费模型调整
// （done-hub 没有 completion_ratio 概念，Output 是绝对倍率）。

const (
	// modelsDevURL 是 models.dev 的定价数据源。
	modelsDevURL = "https://models.dev/api.json"

	// modelsDevCostPerMillionBase：models.dev 的价是「美元/百万token」，
	// done-hub 的 1 倍率 = $2/百万token（DollarRate=0.002 → $0.002/1K → $2/1M），
	// 所以绝对价除以 2 即得倍率。用显式常量表达 $2 这个基准。
	modelsDevCostPerMillionBase = 2.0
)

// firstPartyModelsDevProviders 是「模型发布方官方源」白名单。
// 只收发布该模型的实验室官方源，不收多厂商托管/中转（bedrock/azure/nvidia/groq...），
// 因为后者常以 0/促销价托管他厂模型，会污染第一方真实价（如 unorouter 把 claude-opus 报成 1/12 官方价）。
var firstPartyModelsDevProviders = map[string]bool{
	"anthropic": true, "openai": true, "google": true, "google-vertex": true,
	"xai": true, "deepseek": true, "mistral": true, "cohere": true,
	"meta": true, "llama": true, "moonshotai": true, "zhipuai": true, "zai": true,
	"minimax": true, "alibaba": true, "stepfun": true, "perplexity": true,
}

// isFirstPartyModelsDevProvider 判断 provider 是否为模型发布方官方源。
func isFirstPartyModelsDevProvider(provider string) bool {
	return firstPartyModelsDevProviders[provider]
}

// modelsDevProvider / modelsDevModel / modelsDevCost 对应 models.dev/api.json 的结构。
// cost.* 用指针以区分「字段缺失」与「值为 0」（免费模型 input=0 是合法的）。
type modelsDevProvider struct {
	Models map[string]modelsDevModel `json:"models"`
}

type modelsDevModel struct {
	Cost modelsDevCost `json:"cost"`
}

type modelsDevCost struct {
	Input      *float64        `json:"input"`
	Output     *float64        `json:"output"`
	CacheRead  *float64        `json:"cache_read"`
	CacheWrite *float64        `json:"cache_write"`
	Tiers      []modelsDevTier `json:"tiers"`
}

// modelsDevTier 是分档价（大上下文模型超阈值后单价跳涨）。
// 只处理 tier.type=="context" 的档位（核实 models.dev 现状：tier.type 仅有 "context"）。
// models.dev 的 tier 还带 cache_read/cache_write，但 done-hub 的 LongContextTier 只有
// Threshold/InputRatio/OutputRatio 三字段，无处承载分档 cache 倍率，故此处有意不解析 tier 内 cache_*。
type modelsDevTier struct {
	Input  *float64 `json:"input"`
	Output *float64 `json:"output"`
	Tier   struct {
		Type string `json:"type"`
		Size int    `json:"size"`
	} `json:"tier"`
}

// modelsDevCandidate 是某模型名在冲突消解后最终入选的一份价格。
type modelsDevCandidate struct {
	Provider   string
	Input      float64
	Output     *float64
	CacheRead  *float64
	CacheWrite *float64
	Tiers      []modelsDevTier
}

// GetPriceByModelsDev 从 models.dev 拉取并转换成 done-hub 的 []*Price（不落库）。
// 落库仍走现有的 SyncPricing（由调用方按 updateMode 决定新增/更新/覆盖）。
func GetPriceByModelsDev() ([]*Price, error) {
	logger.SysLog("Start fetch prices from models.dev: " + modelsDevURL)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(modelsDevURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models.dev: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models.dev returned status %d", resp.StatusCode)
	}

	prices, err := ConvertModelsDevToPrices(resp.Body)
	if err != nil {
		return nil, err
	}
	logger.SysLog(fmt.Sprintf("models.dev 转换完成，共 %d 个价格配置", len(prices)))
	return prices, nil
}

// ConvertModelsDevToPrices 解析 models.dev/api.json 并转成 []*Price。
// 同名模型跨 provider 冲突时，优先第一方官方源、再优先非零 input、再取最便宜、
// provider 名做稳定 tie-break，保证结果确定性可复现（详见 shouldReplaceModelsDevCandidate）。
func ConvertModelsDevToPrices(reader io.Reader) ([]*Price, error) {
	var upstream map[string]modelsDevProvider
	if err := json.NewDecoder(reader).Decode(&upstream); err != nil {
		return nil, fmt.Errorf("failed to decode models.dev response: %v", err)
	}
	if len(upstream) == 0 {
		return nil, errors.New("empty models.dev response")
	}

	// provider 名排序，保证遍历顺序确定（冲突 tie-break 依赖它）。
	providers := make([]string, 0, len(upstream))
	for provider := range upstream {
		providers = append(providers, provider)
	}
	sort.Strings(providers)

	selected := make(map[string]modelsDevCandidate)
	for _, provider := range providers {
		models := upstream[provider].Models
		if len(models) == 0 {
			continue
		}
		modelNames := make([]string, 0, len(models))
		for name := range models {
			modelNames = append(modelNames, name)
		}
		sort.Strings(modelNames)

		for _, name := range modelNames {
			candidate, ok := buildModelsDevCandidate(provider, models[name].Cost)
			if !ok {
				continue
			}
			if current, exists := selected[name]; !exists || shouldReplaceModelsDevCandidate(current, candidate) {
				selected[name] = candidate
			}
		}
	}

	if len(selected) == 0 {
		return nil, errors.New("no valid models.dev pricing entries found")
	}

	// 模型名排序输出，结果稳定。
	names := make([]string, 0, len(selected))
	for name := range selected {
		names = append(names, name)
	}
	sort.Strings(names)

	prices := make([]*Price, 0, len(names))
	for _, name := range names {
		prices = append(prices, candidateToPrice(name, selected[name]))
	}
	return prices, nil
}

// buildModelsDevCandidate 从一份 cost 构造候选，非法/无法换算的返回 false 跳过。
func buildModelsDevCandidate(provider string, cost modelsDevCost) (modelsDevCandidate, bool) {
	if cost.Input == nil || !isValidNonNegativeCost(*cost.Input) {
		return modelsDevCandidate{}, false
	}
	input := *cost.Input

	var output *float64
	if cost.Output != nil {
		if !isValidNonNegativeCost(*cost.Output) {
			return modelsDevCandidate{}, false
		}
		output = cost.Output
	}

	// input=0 且 output>0 无法换算成 done-hub 的倍率（会得到 output/0），跳过。
	if input == 0 && output != nil && *output > 0 {
		return modelsDevCandidate{}, false
	}

	candidate := modelsDevCandidate{Provider: provider, Input: input, Output: output}
	if cost.CacheRead != nil && isValidNonNegativeCost(*cost.CacheRead) {
		candidate.CacheRead = cost.CacheRead
	}
	if cost.CacheWrite != nil && isValidNonNegativeCost(*cost.CacheWrite) {
		candidate.CacheWrite = cost.CacheWrite
	}
	candidate.Tiers = cost.Tiers
	return candidate, true
}

// shouldReplaceModelsDevCandidate 冲突消解（同名模型跨 provider）：
//  0. 第一方（官方发布方）优先（最高维度）——官方价是权威基准，无条件盖过中转/托管商，
//     否则「取最便宜」会被中转商异常低价/促销价系统性污染（如 unorouter 报 claude-opus 为官方价的 1/12）。
//     置于非零判断之上：第一方的官方免费档（如 meta 的 llama input=0）也不应被中转付费价盖掉。
//  1. 优先非零 input（免费聚合项不应盖掉真实价）——仅在双方同为/同非第一方时生效
//  2. input 明显更便宜的优先（主原则，复刻 new-api）
//  3. input 近似相等时，优先带 context 分档的候选——否则纯比价会系统性丢弃
//     官方的分档信息（如 grok-4.5 在多个 provider 同价，只有 xai 带 tiers）
//  4. 仍打平时按 provider 名做稳定 tie-break，保证结果确定
func shouldReplaceModelsDevCandidate(current, next modelsDevCandidate) bool {
	currentFP := isFirstPartyModelsDevProvider(current.Provider)
	nextFP := isFirstPartyModelsDevProvider(next.Provider)
	if currentFP != nextFP {
		return nextFP // 第一方无条件胜出，无关价格
	}

	currentNonZero := current.Input > 0
	nextNonZero := next.Input > 0
	if currentNonZero != nextNonZero {
		return nextNonZero
	}
	if nextNonZero && !nearlyEqualCost(next.Input, current.Input) {
		return next.Input < current.Input
	}
	// input 近似相等：偏好带分档信息的一份。
	currentHasTier := hasContextTier(current.Tiers)
	nextHasTier := hasContextTier(next.Tiers)
	if currentHasTier != nextHasTier {
		return nextHasTier
	}
	return next.Provider < current.Provider
}

// hasContextTier 判断候选是否带有效的 context 分档。
func hasContextTier(tiers []modelsDevTier) bool {
	_, ok := firstContextTier(tiers)
	return ok
}

// candidateToPrice 把入选候选换算成一个 done-hub *Price。
func candidateToPrice(name string, c modelsDevCandidate) *Price {
	price := &Price{
		Model:       name,
		Type:        TokensPriceType,
		ChannelType: 0, // models.dev 无渠道类型语义，且 ChannelType 不参与计费，填 0（Unknown）。
		Input:       roundRatioValue(c.Input / modelsDevCostPerMillionBase),
	}
	if c.Output != nil {
		price.Output = roundRatioValue(*c.Output / modelsDevCostPerMillionBase)
	}

	// cache 倍率是相对 input 的倍率（done-hub ExtraRatios 约定）；input=0 时无法换算，跳过。
	if c.Input > 0 {
		extra := make(map[string]float64)
		if c.CacheRead != nil {
			extra[config.UsageExtraCachedRead] = roundRatioValue(*c.CacheRead / c.Input)
		}
		if c.CacheWrite != nil {
			extra[config.UsageExtraCachedWrite] = roundRatioValue(*c.CacheWrite / c.Input)
		}
		if len(extra) > 0 {
			jsonExtra := datatypes.NewJSONType(extra)
			price.ExtraRatios = &jsonExtra
		}
	}

	// 分档价 → LongContext。只取第一个 context 型档位（models.dev 每模型至多一个 context tier）。
	if tier, ok := firstContextTier(c.Tiers); ok && c.Input > 0 {
		lc := LongContextTier{Threshold: tier.Tier.Size}
		if tier.Input != nil {
			lc.InputRatio = roundRatioValue(*tier.Input / c.Input)
		}
		if tier.Output != nil && c.Output != nil && *c.Output > 0 {
			lc.OutputRatio = roundRatioValue(*tier.Output / *c.Output)
		}
		// 阈值有效且至少有一侧倍率才写入，避免落一份空档。
		if lc.Threshold > 0 && (lc.InputRatio > 0 || lc.OutputRatio > 0) {
			jsonLC := datatypes.NewJSONType(lc)
			price.LongContext = &jsonLC
		}
	}

	return price
}

// firstContextTier 返回第一个 type=="context" 且带正阈值的档位。
func firstContextTier(tiers []modelsDevTier) (modelsDevTier, bool) {
	for _, t := range tiers {
		if t.Tier.Type == "context" && t.Tier.Size > 0 {
			return t, true
		}
	}
	return modelsDevTier{}, false
}

// isValidNonNegativeCost 挡掉 NaN/Inf/负数。
func isValidNonNegativeCost(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
}

// nearlyEqualCost 浮点近似相等，用于冲突比价。
func nearlyEqualCost(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

// roundRatioValue 统一保留 6 位小数，避免浮点尾差。
func roundRatioValue(v float64) float64 {
	return math.Round(v*1e6) / 1e6
}
