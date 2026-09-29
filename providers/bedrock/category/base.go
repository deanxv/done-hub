package category

import (
	"done-hub/common/model_utils"
	"done-hub/common/requester"
	"done-hub/providers/base"
	"done-hub/types"
	"errors"
	"net/http"
	"strings"
)

// 基础模型映射关系
var bedrockMap = map[string]string{
	//base model id
	"claude-3-7-sonnet-20250219": "anthropic.claude-3-7-sonnet-20250219-v1:0",
	"claude-3-5-sonnet-20240620": "anthropic.claude-3-5-sonnet-20240620-v1:0",
	"claude-3-5-sonnet-20241022": "anthropic.claude-3-5-sonnet-20241022-v2:0",
	"claude-3-opus-20240229":     "anthropic.claude-3-opus-20240229-v1:0",
	"claude-3-sonnet-20240229":   "anthropic.claude-3-sonnet-20240229-v1:0",
	"claude-3-haiku-20240307":    "anthropic.claude-3-haiku-20240307-v1:0",
	"claude-3-5-haiku-20241022":  "anthropic.claude-3-5-haiku-20241022-v1:0",
	"claude-2.1":                 "anthropic.claude-v2:1",
	"claude-2.0":                 "anthropic.claude-v2",
	"claude-instant-1.2":         "anthropic.claude-instant-v1",
	"claude-sonnet-4-20250514":   "anthropic.claude-sonnet-4-20250514-v1:0",
	"claude-opus-4-20250514":     "anthropic.claude-opus-4-20250514-v1:0",
	"claude-opus-4-1-20250805":   "anthropic.claude-opus-4-1-20250805-v1:0",
	"claude-sonnet-4-5-20250929": "anthropic.claude-sonnet-4-5-20250929-v1:0",
	"claude-sonnet-4-6":          "anthropic.claude-sonnet-4-6",
	"claude-haiku-4-5-20251001":  "anthropic.claude-haiku-4-5-20251001-v1:0",
	"claude-opus-4-5-20251101":   "anthropic.claude-opus-4-5-20251101-v1:0",
	"claude-opus-4-6":            "anthropic.claude-opus-4-6-v1",
	"claude-opus-4-7":            "anthropic.claude-opus-4-7",
	"claude-opus-4-8":            "anthropic.claude-opus-4-8",
	"claude-sonnet-5":            "anthropic.claude-sonnet-5",
	"claude-sonnet-5-5":          "anthropic.claude-sonnet-5-5",
	"claude-fable-5":             "anthropic.claude-fable-5",
	"claude-fable-5-1":           "anthropic.claude-fable-5-1",
	"claude-opus-5":              "anthropic.claude-opus-5",
	"claude-opus-5-5":            "anthropic.claude-opus-5-5",
	"gpt-oss-120b":               "openai.gpt-oss-120b-1:0",
	"gpt-oss-20b":                "openai.gpt-oss-20b-1:0",
	"gpt-5.6-sol":                "openai.gpt-5.6-sol",
	"gpt-5.6-terra":              "openai.gpt-5.6-terra",
	"gpt-5.6-luna":               "openai.gpt-5.6-luna",
}

// 用户显式书写的区域前缀（手动覆盖优先）
var regionPrefixes = []string{"global.", "us.", "eu.", "apac."}

// 各 bedrock 模型支持的跨区 inference profile：region 根（aws region 第一段，如
// us-east-1 -> us）映射到该模型在此 region 实际可用的 profile 前缀。未列出的模型/region
// 按裸 id 直连，不加前缀。
//
// 注意 AP 区分两代：claude-3.x / sonnet-4 / opus-4-1 等旧世代有 apac. profile；
// 4.5+ 新世代没有 apac.，改为更细的 au.（ap-southeast-2/4/6）与 jp.（ap-northeast-1/3），
// 其余 ap 区仅 global.。本表的 key 只到 region 根（ap），表达不了 au./jp. 的区分，
// 故 AP 统一映射到 global.（全 ap 区可用且无区域溢价）；需要 au./jp. 数据驻留的
// 部署可在模型名上显式写前缀覆盖。例外：opus-4-5 连 au./jp. 都没有，只有 us./eu./global.。
//
// "*" 为 region 根通配兜底。缺少它时，ca/me/sa/il/af/mx 等根查不到映射会返回空前缀、
// 发出裸 model id；而部分模型（如 haiku-4-5）AWS 明确不支持裸 id on-demand 调用，
// 会直接 400。故凡 global. 全区可用的模型都应带 "*"。
//
// "ca"：AWS 把 ca-central-1 / ca-west-1 归入 **US geo** 的 source region（us. profile 的
// 目标区含加拿大本地），所以支持 us. geo 的模型这里应映射到 "us"，否则会退到 "*"→global.
// 虽能用但丢失数据驻留。注意并非所有模型的 ca 都支持 geo——sonnet-4-5 / haiku-4-5 /
// opus-4-5 的 ca-west-1 是 Geo 不支持，这些模型不要加 ca 键，留给 "*" 兜底。
var awsModelCanCrossRegionMap = map[string]map[string]string{
	"anthropic.claude-3-sonnet-20240229-v1:0": {"us": "us", "eu": "eu", "ap": "apac"},
	"anthropic.claude-3-opus-20240229-v1:0":   {"us": "us"},
	// 3-haiku：Geo inference ID 只有 us./eu.（无 apac.，已核 model card）。
	// ap 各区 In-Region 支持，故不设 ap 键、也不加 "*"，让它发裸 id 走单区调用。
	"anthropic.claude-3-haiku-20240307-v1:0":    {"us": "us", "eu": "eu"},
	"anthropic.claude-3-5-sonnet-20240620-v1:0": {"us": "us", "eu": "eu", "ap": "apac"},
	"anthropic.claude-3-5-sonnet-20241022-v2:0": {"us": "us", "ap": "apac"},
	"anthropic.claude-3-5-haiku-20241022-v1:0":  {"us": "us"},
	"anthropic.claude-3-7-sonnet-20250219-v1:0": {"us": "us", "eu": "eu", "ap": "apac"},
	"anthropic.claude-sonnet-4-20250514-v1:0":   {"us": "us", "eu": "eu", "ap": "apac"},
	"anthropic.claude-opus-4-20250514-v1:0":     {"us": "us"},
	"anthropic.claude-opus-4-1-20250805-v1:0":   {"us": "us"},
	// 4.5 世代及以后：global. 在全部商业区可用，故一律带 "*" 兜底。
	// 没有 "*" 时，ca/me/sa/il/af/mx 等 region 根查不到映射会退化成裸 id 直发，
	// 对「强制要求 profile」的模型（如 haiku-4-5）必然 400。
	"anthropic.claude-sonnet-4-5-20250929-v1:0": {"us": "us", "eu": "eu", "ap": "global", "*": "global"},
	"anthropic.claude-sonnet-4-6":               {"us": "us", "eu": "eu", "ap": "global", "*": "global"},
	"anthropic.claude-haiku-4-5-20251001-v1:0":  {"us": "us", "eu": "eu", "ap": "global", "*": "global"},
	"anthropic.claude-opus-4-5-20251101-v1:0":   {"us": "us", "eu": "eu", "ap": "global", "*": "global"},
	"anthropic.claude-opus-4-6-v1":              {"us": "us", "ca": "us", "eu": "eu", "ap": "global", "*": "global"},
	"anthropic.claude-opus-4-7":                 {"us": "us", "ca": "us", "eu": "eu", "ap": "global", "*": "global"},
	"anthropic.claude-opus-4-8":                 {"us": "us", "ca": "us", "eu": "eu", "ap": "global", "*": "global"},
	"anthropic.claude-opus-5":                   {"us": "us", "eu": "eu", "ap": "global", "*": "global"},
	// opus-5-5：geo 最全（us/eu/au/jp 均有）。本表的 region 根只区分到 us/eu/ap，
	// au./jp. 表达不出来，故 AP 区统一用 global（可用且无区域溢价）；
	// 需要 au./jp. 数据驻留的部署可在模型名上显式写前缀覆盖。
	"anthropic.claude-opus-5-5": {"us": "us", "eu": "eu", "ap": "global", "*": "global"},
	// sonnet-5：EU 无 geo profile，仅 Global（AWS model card 2026-06-30）
	"anthropic.claude-sonnet-5": {"us": "us", "eu": "global", "ap": "global", "*": "global"},
	// sonnet-5-5：完全没有 geo profile（model card 的 Geo inference ID 为 N/A），
	// 所有区域一律走 global.
	"anthropic.claude-sonnet-5-5": {"*": "global"},
	"anthropic.claude-fable-5":    {"us": "us", "eu": "global", "ap": "global", "*": "global"},
	// fable-5-1：geo 仅 us.，EU/AP 无 geo profile，回落 global.
	"anthropic.claude-fable-5-1": {"us": "us", "eu": "global", "ap": "global", "*": "global"},
	// GPT-5.6 闭源系仅支持 inference profile 调用（裸 openai.xxx 会被 on-demand 400），
	// 任意 region 统一走 global. profile（2026-08 实测 InvokeModel / chat-completions /
	// responses 三端点均可用）。"*" 为 region 根通配，见 autoCrossRegionPrefix。
	"openai.gpt-5.6-sol":   {"*": "global"},
	"openai.gpt-5.6-terra": {"*": "global"},
	"openai.gpt-5.6-luna":  {"*": "global"},
}

var CategoryMap = map[string]Category{}

type Category struct {
	ModelName                 string
	ChatComplete              ChatCompletionConvert
	ResponseChatComplete      ChatCompletionResponse
	ResponseChatCompleteStrem ChatCompletionStreamResponse
}

func GetCategory(modelName, region string) (*Category, error) {
	modelName = GetModelName(modelName, region)
	// 获取provider
	provider := ""

	if model_utils.ContainsCaseInsensitive(modelName, "anthropic") {
		provider = "anthropic"
	} else if model_utils.ContainsCaseInsensitive(modelName, "openai.") {
		provider = "openai"
	}

	if category, exists := CategoryMap[provider]; exists {
		category.ModelName = modelName
		return &category, nil
	}

	return nil, errors.New("category_not_found")
}

func GetModelName(modelName, region string) string {
	// 提取用户显式书写的区域前缀
	regionPrefix := ""
	for _, prefix := range regionPrefixes {
		if strings.HasPrefix(modelName, prefix) {
			regionPrefix = prefix
			modelName = modelName[len(prefix):]
			break
		}
	}

	// 查找基础模型映射
	if mappedName, exists := bedrockMap[modelName]; exists {
		modelName = mappedName
	}

	// 用户未显式写前缀时，按 region 自动推断跨区前缀
	if regionPrefix == "" {
		regionPrefix = autoCrossRegionPrefix(modelName, region)
	}

	// 如果有区域前缀，添加回去
	if regionPrefix != "" {
		modelName = regionPrefix + modelName
	}

	return modelName
}

// 按 region 与模型跨区可用性返回需拼接的前缀（如 "us."），不可跨区时返回空串
func autoCrossRegionPrefix(awsModelID, region string) string {
	regionRoot := region
	if i := strings.Index(region, "-"); i > 0 {
		regionRoot = region[:i]
	}

	profileMap, ok := awsModelCanCrossRegionMap[awsModelID]
	if !ok {
		return ""
	}

	profilePrefix, ok := profileMap[regionRoot]
	if !ok {
		// "*"：不区分 region 根的通配 profile（如 GPT-5.6 全区走 global.）
		profilePrefix, ok = profileMap["*"]
		if !ok {
			return ""
		}
	}

	return profilePrefix + "."
}

type ChatCompletionConvert func(*types.ChatCompletionRequest) (any, *types.OpenAIErrorWithStatusCode)
type ChatCompletionResponse func(base.ProviderInterface, *http.Response, *types.ChatCompletionRequest) (*types.ChatCompletionResponse, *types.OpenAIErrorWithStatusCode)

type ChatCompletionStreamResponse func(base.ProviderInterface, *types.ChatCompletionRequest) requester.HandlerPrefix[string]
