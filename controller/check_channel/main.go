package check_channel

import (
	"done-hub/common/logger"
	"done-hub/common/utils"
	"done-hub/model"
	"done-hub/providers"
	providers_base "done-hub/providers/base"
	"done-hub/types"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	CheckStatusFailed  = 0
	CheckStatusSuccess = 1
	// CheckStatusWarning 表示结果仅供参考：既不能证明有问题，也不能证明没问题
	CheckStatusWarning = 2
	// CheckStatusSkipped 表示这一项对当前渠道不适用（如请求经 Claude 转换后不再包含被测参数），不参与汇总
	CheckStatusSkipped = 3
)

// 主请求成功但耗时超过该阈值视为响应慢
const slowThreshold = 6 * time.Second

type ModelResult struct {
	Model  string                `json:"model"`
	Status int                   `json:"status"`
	Items  []*CheckProcessResult `json:"items"`
}

type CheckProcessResult struct {
	Key       string                        `json:"key"`
	Status    int                           `json:"status"`
	LatencyMs int64                         `json:"latency_ms"`
	Category  string                        `json:"category,omitempty"`
	Results   []*CheckResult                `json:"results"`
	Response  *types.ChatCompletionResponse `json:"response,omitempty"`
}

type CheckResult struct {
	Name   string `json:"name"`
	Status int    `json:"status"`
	Remark string `json:"remark"`
}

// CheckContext 是检测项判定时可用的渠道信息，用于区分「上游不支持」和「请求在 done-hub 转换时就被丢掉了」
type CheckContext struct {
	// ClaudeConversion 为 true 表示请求经过了 OpenAI→Claude 转换：response_format 会被忽略
	ClaudeConversion bool
	// RewritesUnknownRole 为 true 表示请求经过了 Claude 或 Gemini 转换：非法角色会被改写为 user
	RewritesUnknownRole bool
}

type CheckProcess interface {
	GetKey() string
	GetRequest() *types.ChatCompletionRequest
	Check(ctx CheckContext, req *types.ChatCompletionRequest, resp *types.ChatCompletionResponse, openaiErr *types.OpenAIError) []*CheckResult
}

// CheckModel 先执行基础检测，再并行执行其余检测项。
// 基础检测单独先跑：Codex / Claude Code 等 OAuth 渠道 token 过期时，每个 provider 会各自用同一个
// refresh_token 刷新，并行会刷新多次；基础检测跑完后 token 已入缓存。同时耗时不受其他检测项挤占。
// 每个检测项各自创建 provider：provider 内部持有 usage 与 gin.Context，不能跨 goroutine 共享。
func CheckModel(channelId int, modelName string) (*ModelResult, error) {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return nil, errors.New("model is empty")
	}

	channel, err := model.GetChannelById(channelId)
	if err != nil {
		return nil, err
	}
	channel.SetProxy()

	processes := getProcess(modelName)
	result := &ModelResult{
		Model: modelName,
		Items: make([]*CheckProcessResult, len(processes)),
	}

	// getProcess 保证第一项是基础检测
	result.Items[0] = runProcess(channel, modelName, processes[0])

	var wg sync.WaitGroup
	for i := 1; i < len(processes); i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result.Items[i] = runProcess(channel, modelName, processes[i])
		}(i)
	}
	wg.Wait()

	result.Status = aggregateStatus(result.Items)
	return result, nil
}

func runProcess(channel *model.Channel, modelName string, p CheckProcess) (item *CheckProcessResult) {
	// 后台协程不在 gin Recovery 保护内，panic 会拖垮整个进程
	defer func() {
		if r := recover(); r != nil {
			logger.SysError(fmt.Sprintf("check channel panic: %v\n%s", r, debug.Stack()))
			item = &CheckProcessResult{
				Key:     p.GetKey(),
				Status:  CheckStatusFailed,
				Results: []*CheckResult{{Name: "响应", Status: CheckStatusFailed, Remark: "检测过程异常"}},
			}
		}
	}()

	item = &CheckProcessResult{Key: p.GetKey()}

	chat, upstreamModel, err := newChatInterface(channel, modelName)
	if err != nil {
		item.Results = []*CheckResult{{Name: "响应", Status: CheckStatusFailed, Remark: utils.MaskSensitiveInfo(err.Error())}}
		item.Status = CheckStatusFailed
		return item
	}

	req := p.GetRequest()
	req.Model = upstreamModel

	ctx := CheckContext{
		ClaudeConversion:    usesClaudeConversion(channel.Type, upstreamModel),
		RewritesUnknownRole: rewritesUnknownRole(channel.Type, upstreamModel, geminiUsesOpenAIAPI(channel)),
	}
	if ctx.RewritesUnknownRole && p.GetKey() == errorProbeKey {
		// 转换后非法角色变成 user，请求必然合法，结果固定为不适用，发给上游只会白白消耗额度
		item.Results = p.Check(ctx, req, nil, nil)
		item.Status = itemStatus(item.Results)
		return item
	}

	start := time.Now()
	resp, apiErr := chat.CreateChatCompletion(req)
	latency := time.Since(start)
	item.LatencyMs = latency.Milliseconds()

	var openaiErr *types.OpenAIError
	if apiErr != nil {
		openaiErr = &apiErr.OpenAIError
		resp = nil
		// 中转层数检测本来就要报错，不需要错误分类
		if p.GetKey() != errorProbeKey {
			item.Category = classifyError(apiErr.StatusCode, openaiErr.Type, openaiErr.Message)
		}
	}

	item.Results = p.Check(ctx, req, resp, openaiErr)
	if p.GetKey() == baseProbeKey && openaiErr == nil {
		item.Results = append(item.Results, checkLatency(latency))
	}
	item.Response = resp
	item.Status = itemStatus(item.Results)
	return item
}

// newChatInterface 与 testChannel 保持一致：解析 # 后缀参数并应用渠道的模型映射，
// 否则配置了映射的渠道会把别名直接发给上游。
func newChatInterface(channel *model.Channel, modelName string) (providers_base.ChatInterface, string, error) {
	var otherArg string
	if parts := strings.SplitN(modelName, "#", 2); len(parts) == 2 {
		modelName, otherArg = parts[0], parts[1]
	}

	req, err := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req

	provider := providers.GetProvider(channel, c)
	if provider == nil {
		return nil, "", errors.New("channel not implemented")
	}
	if otherArg != "" {
		provider.SetOtherArg(otherArg)
	}
	upstreamModel, err := provider.ModelMappingHandler(modelName)
	if err != nil {
		return nil, "", err
	}
	provider.SetUsage(&types.Usage{})

	chat, ok := provider.(providers_base.ChatInterface)
	if !ok {
		return nil, "", errors.New("channel not implemented")
	}
	return chat, strings.TrimPrefix(upstreamModel, "+"), nil
}

// itemStatus：任一项失败即失败；否则有提示则为提示；全部不适用则为不适用；其余为通过
func itemStatus(results []*CheckResult) int {
	status := CheckStatusSkipped
	for _, r := range results {
		switch r.Status {
		case CheckStatusFailed:
			return CheckStatusFailed
		case CheckStatusWarning:
			status = CheckStatusWarning
		case CheckStatusSuccess:
			if status == CheckStatusSkipped {
				status = CheckStatusSuccess
			}
		}
	}
	return status
}

// aggregateStatus 汇总一个模型的总体结论。
// 只有基础检测失败才判模型失败：其他项失败只说明不支持该能力，算作提示。
// 中转层数只是线索（可被改写），不适用的项与渠道质量无关，两者都不参与汇总。
func aggregateStatus(items []*CheckProcessResult) int {
	status := CheckStatusSuccess
	for _, item := range items {
		if item.Key == baseProbeKey && item.Status == CheckStatusFailed {
			return CheckStatusFailed
		}
		if item.Key == errorProbeKey || item.Status == CheckStatusSkipped {
			continue
		}
		if item.Status != CheckStatusSuccess {
			status = CheckStatusWarning
		}
	}
	return status
}

func getProcess(modelName string) []CheckProcess {
	return []CheckProcess{
		CreateCheckBaseProcess(modelName),
		CreateCheckImgProcess(modelName),
		CreateCheckJsonFormatProcess(modelName),
		CreateCheckToolProcess(modelName),
		CreateCheckErrorProcess(modelName),
	}
}

// failedResponse 与手动测速一致对错误信息脱敏；其余 remark 是模型名、回答等，脱敏会误伤 us.anthropic.xxx 这类模型名
func failedResponse(openaiErr *types.OpenAIError) []*CheckResult {
	return []*CheckResult{{Name: "响应", Status: CheckStatusFailed, Remark: utils.MaskSensitiveInfo(openaiErr.Message)}}
}

func firstChoiceContent(resp *types.ChatCompletionResponse) (string, bool) {
	if resp == nil || len(resp.Choices) == 0 {
		return "", false
	}
	return resp.Choices[0].Message.StringContent(), true
}
