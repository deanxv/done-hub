package xAI

import (
	"done-hub/common/model_utils"
	"done-hub/common/requester"
	"done-hub/model"
	"done-hub/providers/base"
	"done-hub/providers/openai"
	"done-hub/types"
	"fmt"
	"io"
	"net/http"

	"github.com/tidwall/sjson"
)

// 定义供应商工厂
type XAIProviderFactory struct{}

// 创建 XAIProvider
func (f XAIProviderFactory) Create(channel *model.Channel) base.ProviderInterface {
	fmt.Println("Creating XAIProvider for channel:")
	return &XAIProvider{
		OpenAIProvider: openai.OpenAIProvider{
			BaseProvider: base.BaseProvider{
				Config:          getConfig(),
				Channel:         channel,
				Requester:       requester.NewHTTPRequester(channel.GetProxy(), RequestErrorHandle),
				SupportResponse: true,
			},
			SupportStreamOptions: true,
			UsageHandler:         usageHandler,
			RequestHandleBefore:  requestHandler,
			ResponsesBodyPatch:   responsesBodyPatch,
		},
	}
}

func getConfig() base.ProviderConfig {
	return base.ProviderConfig{
		BaseURL:           "https://api.x.ai",
		ChatCompletions:   "/v1/chat/completions",
		ImagesGenerations: "/v1/images/generations",
		ImagesEdit:        "/v1/images/edits",
		ModelList:         "/v1/models",
		Responses:         "/v1/responses",
	}
}

type XAIProvider struct {
	openai.OpenAIProvider
}

func RequestErrorHandle(resp *http.Response) *types.OpenAIError {
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	msg := string(bodyBytes)
	if msg == "" {
		return nil
	}

	return openai.ErrorHandle(&types.OpenAIErrorResponse{
		Error: types.OpenAIError{
			Message: msg,
			Code:    "xAI_error",
		},
	})
}

func usageHandler(usage *types.Usage) (ForcedFormatting bool) {
	usage.CompletionTokens = usage.TotalTokens - usage.PromptTokens

	return true
}

func requestHandler(request *types.ChatCompletionRequest) (errWithCode *types.OpenAIErrorWithStatusCode) {

	if model_utils.HasPrefixCaseInsensitive(request.Model, "grok-4") || model_utils.HasPrefixCaseInsensitive(request.Model, "grok-3-mini") || model_utils.HasPrefixCaseInsensitive(request.Model, "grok-3-mini-fast") {
		request.Stop = nil
		request.FrequencyPenalty = nil
		request.PresencePenalty = nil
	}

	if !supportsReasoningEffort(request.Model) {
		request.ReasoningEffort = nil
	}

	return nil
}

// responsesBodyPatch 在 /v1/responses 字节透传出去前做 xAI 专属清洗：
//   - stream_options：Responses 端点不接受此字段（chat 端点接受，因此仅在 Responses 透传时删）。
//   - reasoning.effort：仅 grok-3-mini / grok-4.20-multi-agent / grok-4.3 支持，其余删掉避免上游 400。
func responsesBodyPatch(modelName string, body []byte) []byte {
	body, _ = sjson.DeleteBytes(body, "stream_options")
	if !supportsReasoningEffort(modelName) {
		body, _ = sjson.DeleteBytes(body, "reasoning.effort")
	}
	return body
}

// supportsReasoningEffort 与 xAI 官方文档保持一致的白名单。
func supportsReasoningEffort(modelName string) bool {
	return model_utils.HasPrefixCaseInsensitive(modelName, "grok-3-mini") ||
		model_utils.HasPrefixCaseInsensitive(modelName, "grok-4.20-multi-agent") ||
		model_utils.HasPrefixCaseInsensitive(modelName, "grok-4.3")
}
