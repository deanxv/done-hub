package category

import (
	"done-hub/common"
	"done-hub/common/requester"
	"done-hub/providers/base"
	"done-hub/providers/gemini"
	"done-hub/types"
	"encoding/json"
	"net/http"
)

func init() {
	CategoryMap["gemini"] = &Category{
		Category:                  "gemini",
		Publisher:                 PublisherGoogle,
		ChatComplete:              ConvertGeminiFromChatOpenai,
		ResponseChatComplete:      ConvertGeminiToChatOpenai,
		ResponseChatCompleteStrem: GeminiChatCompleteStrem,
		ErrorHandler:              gemini.RequestErrorHandle(""),
		GetModelName:              GetGeminiModelName,
		GetOtherUrl:               getGeminiOtherUrl,
	}
}

func ConvertGeminiFromChatOpenai(request *types.ChatCompletionRequest) (any, *types.OpenAIErrorWithStatusCode) {
	geminiRequest, err := gemini.ConvertFromChatOpenai(request)
	if err != nil {
		return nil, err
	}

	return geminiRequest, nil
}

func ConvertGeminiToChatOpenai(provider base.ProviderInterface, response *http.Response, request *types.ChatCompletionRequest) (*types.ChatCompletionResponse, *types.OpenAIErrorWithStatusCode) {
	geminiResponse := &gemini.GeminiChatResponse{}
	err := json.NewDecoder(response.Body).Decode(geminiResponse)
	if err != nil {
		return nil, common.ErrorWrapper(err, "decode_response_failed", http.StatusInternalServerError)
	}

	return gemini.ConvertToChatOpenai(provider, geminiResponse, request)
}

func GeminiChatCompleteStrem(provider base.ProviderInterface, request *types.ChatCompletionRequest) requester.HandlerPrefix[string] {
	chatHandler := &gemini.GeminiStreamHandler{
		Usage:   provider.GetUsage(),
		Request: request,
		Context: provider.GetContext(), // 传递 Context
	}

	return chatHandler.HandlerStream
}

func GetGeminiModelName(modelName string) string {
	return modelName
}

func getGeminiOtherUrl(stream bool) string {
	if stream {
		return "streamGenerateContent?alt=sse"
	}
	return "generateContent"
}
