package category

import (
	"done-hub/common"
	"done-hub/common/requester"
	"done-hub/providers/base"
	"done-hub/providers/claude"
	"done-hub/types"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

const AnthropicVersion = "vertex-2023-10-16"

type ClaudeRequest struct {
	*claude.ClaudeRequest
	AnthropicVersion string `json:"anthropic_version"`
}

// claudeModelOverrides 是无法用通用规则推导的模型名映射（Vertex 侧 ID 与 Anthropic 官方 ID 不一致）。
// 常规的 "-YYYYMMDD" 日期后缀由 NormalizeClaudeModelName 统一转成 "@YYYYMMDD"，无需逐个登记。
var claudeModelOverrides = map[string]string{
	"claude-3-5-sonnet-20241022": "claude-3-5-sonnet-v2@20241022",
}

var claudeDatedModelPattern = regexp.MustCompile(`^(.+)-(\d{8})$`)

func init() {
	CategoryMap["claude"] = &Category{
		Category:                  "claude",
		Publisher:                 PublisherAnthropic,
		ChatComplete:              ConvertClaudeFromChatOpenai,
		ResponseChatComplete:      ConvertClaudeToChatOpenai,
		ResponseChatCompleteStrem: ClaudeChatCompleteStrem,
		ErrorHandler:              claude.RequestErrorHandle,
		GetModelName:              NormalizeClaudeModelName,
		GetOtherUrl:               getClaudeOtherUrl,
	}
}

func ConvertClaudeFromChatOpenai(request *types.ChatCompletionRequest) (any, *types.OpenAIErrorWithStatusCode) {
	rawRequest, err := claude.ConvertFromChatOpenai(request)
	if err != nil {
		return nil, err
	}

	claudeRequest := &ClaudeRequest{}
	claudeRequest.ClaudeRequest = rawRequest
	claudeRequest.AnthropicVersion = AnthropicVersion

	// 删除model字段
	claudeRequest.Model = ""

	return claudeRequest, nil
}

func ConvertClaudeToChatOpenai(provider base.ProviderInterface, response *http.Response, request *types.ChatCompletionRequest) (*types.ChatCompletionResponse, *types.OpenAIErrorWithStatusCode) {
	claudeResponse := &claude.ClaudeResponse{}
	err := json.NewDecoder(response.Body).Decode(claudeResponse)
	if err != nil {
		return nil, common.ErrorWrapper(err, "decode_response_failed", http.StatusInternalServerError)
	}

	return claude.ConvertToChatOpenai(provider, claudeResponse, request)
}

func ClaudeChatCompleteStrem(provider base.ProviderInterface, request *types.ChatCompletionRequest) requester.HandlerPrefix[string] {
	chatHandler := &claude.ClaudeStreamHandler{
		Usage:   provider.GetUsage(),
		Request: request,
		Prefix:  "data:",
	}

	return chatHandler.HandlerStream
}

// NormalizeClaudeModelName 把 Anthropic 官方模型 ID 转成 Vertex AI 的模型 ID：
//   - 显式映射表优先（处理 v2 之类的不规则命名）
//   - 已是 "@YYYYMMDD" 形式的原样返回
//   - "claude-xxx-YYYYMMDD" 转为 "claude-xxx@YYYYMMDD"
//   - 其余（如当前代无日期的 claude-opus-5-5）原样返回
func NormalizeClaudeModelName(modelName string) string {
	if mapped, ok := claudeModelOverrides[modelName]; ok {
		return mapped
	}
	if strings.Contains(modelName, "@") {
		return modelName
	}
	if m := claudeDatedModelPattern.FindStringSubmatch(modelName); len(m) == 3 {
		return m[1] + "@" + m[2]
	}
	return modelName
}

// getClaudeOtherUrl 返回 Claude 模型在 Vertex 上的调用动作：流式 streamRawPredict，非流式 rawPredict。
func getClaudeOtherUrl(stream bool) string {
	if stream {
		return "streamRawPredict"
	}
	return "rawPredict"
}
