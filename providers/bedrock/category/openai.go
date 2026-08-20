package category

import (
	"done-hub/common"
	"done-hub/common/requester"
	"done-hub/providers/base"
	"done-hub/providers/openai"
	"done-hub/types"
	"encoding/json"
	"net/http"
)

func init() {
	CategoryMap["openai"] = Category{
		ChatComplete:              ConvertOpenaiFromChatOpenai,
		ResponseChatComplete:      ConvertOpenaiToChatOpenai,
		ResponseChatCompleteStrem: OpenaiChatCompleteStrem,
	}
}

// bedrockOpenaiChatRequest 用外层同名字段遮蔽嵌入结构体的 model/stream，把两个
// 无 omitempty 的字段从序列化结果中真正省略（nil + omitempty → 不输出）。
// types.ChatCompletionRequest 是全局共享类型，不能直接加 omitempty。
type bedrockOpenaiChatRequest struct {
	types.ChatCompletionRequest
	Model  any `json:"model,omitempty"`
	Stream any `json:"stream,omitempty"`
}

// ConvertOpenaiFromChatOpenai Bedrock 上 openai.* 模型（gpt-oss / gpt-5.6）的 InvokeModel
// 原生 payload 就是 OpenAI Chat Completions 请求格式，因此只需浅拷贝做少量清理：
//   - Model/Stream 由 URL 中的 modelId 与调用的 API（InvokeModel vs WithResponseStream）
//     决定，body 里必须省略：gpt-5.6 对 "model":"" 与流式 API 下的 "stream":false 均
//     直接 400（2026-08 实测；gpt-oss 容忍零值，但统一省略最安全）；
//   - StreamOptions 非 Bedrock openai schema 字段，一并清掉；
//   - gpt-oss schema 用 max_completion_tokens，把 max_tokens 迁移过去
//     （与 providers/openai/chat.go otherProcessing 对 o1/gpt-5 的处理同理）。
func ConvertOpenaiFromChatOpenai(request *types.ChatCompletionRequest) (any, *types.OpenAIErrorWithStatusCode) {
	openaiRequest := *request
	openaiRequest.StreamOptions = nil

	if openaiRequest.MaxTokens > 0 && openaiRequest.MaxCompletionTokens == 0 {
		openaiRequest.MaxCompletionTokens = openaiRequest.MaxTokens
	}
	openaiRequest.MaxTokens = 0

	return &bedrockOpenaiChatRequest{ChatCompletionRequest: openaiRequest}, nil
}

func ConvertOpenaiToChatOpenai(provider base.ProviderInterface, response *http.Response, request *types.ChatCompletionRequest) (*types.ChatCompletionResponse, *types.OpenAIErrorWithStatusCode) {
	openaiResponse := &openai.OpenAIProviderChatResponse{}
	err := json.NewDecoder(response.Body).Decode(openaiResponse)
	if err != nil {
		return nil, common.ErrorWrapper(err, "decode_response_failed", http.StatusInternalServerError)
	}

	if aiError := openai.ErrorHandle(&openaiResponse.OpenAIErrorResponse); aiError != nil {
		return nil, &types.OpenAIErrorWithStatusCode{
			OpenAIError: *aiError,
			StatusCode:  http.StatusBadRequest,
		}
	}

	usage := provider.GetUsage()
	// usage 缺失或没有产出 token 时用文本估算兜底（对齐 providers/openai/chat.go 非流路径）。
	if openaiResponse.Usage == nil || openaiResponse.Usage.CompletionTokens == 0 {
		openaiResponse.Usage = &types.Usage{
			PromptTokens: usage.PromptTokens,
		}
		openaiResponse.Usage.CompletionTokens = common.CountTokenText(openaiResponse.GetContent(), request.Model)
		openaiResponse.Usage.TotalTokens = openaiResponse.Usage.PromptTokens + openaiResponse.Usage.CompletionTokens
	}
	*usage = *openaiResponse.Usage

	// 把响应中的 bedrock model id 改回用户请求的模型名（含渠道别名映射）。
	openaiResponse.Model = provider.GetResponseModelName(request.Model)

	return &openaiResponse.ChatCompletionResponse, nil
}

// OpenaiChatCompleteStrem openai.* 模型的流式 chunk 就是 OpenAI chunk JSON，
// 但经 eventstream 解码后是裸 JSON（无 "data:" 前缀、无 [DONE] 结尾）。
// 补上前缀后完整复用 openai.OpenAIStreamHandler 的处理（usage 提取/兜底、
// TextBuilder 累积、model 名改写）；流尾由 awsStreamReader 自动补发 io.EOF，
// 不依赖 [DONE]。
func OpenaiChatCompleteStrem(provider base.ProviderInterface, request *types.ChatCompletionRequest) requester.HandlerPrefix[string] {
	chatHandler := &openai.OpenAIStreamHandler{
		Usage:     provider.GetUsage(),
		ModelName: request.Model,
		Context:   provider.GetContext(),
	}

	return func(rawLine *[]byte, dataChan chan string, errChan chan error) {
		if len(*rawLine) == 0 {
			*rawLine = nil
			return
		}
		withPrefix := make([]byte, 0, len(*rawLine)+6)
		withPrefix = append(withPrefix, "data: "...)
		withPrefix = append(withPrefix, *rawLine...)
		*rawLine = withPrefix

		chatHandler.HandlerChatStream(rawLine, dataChan, errChan)
	}
}
