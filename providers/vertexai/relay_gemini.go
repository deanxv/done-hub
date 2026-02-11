package vertexai

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/requester"
	"done-hub/providers/gemini"
	"done-hub/providers/vertexai/category"
	"done-hub/types"
	"encoding/json"
	"net/http"
	"strings"
)

func (p *VertexAIProvider) CreateGeminiChat(request *gemini.GeminiChatRequest) (*gemini.GeminiChatResponse, *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getGeminiRequest(request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	geminiResponse := &gemini.GeminiChatResponse{}
	// 发送请求
	_, openaiErr := p.Requester.SendRequest(req, geminiResponse, false)
	if openaiErr != nil {
		return nil, openaiErr
	}

	// 检查是否是 countTokens 请求（Vertex AI 版本）
	isCountTokens := len(geminiResponse.Candidates) == 0 &&
		(geminiResponse.UsageMetadata != nil || geminiResponse.TotalTokens > 0)

	if !isCountTokens && len(geminiResponse.Candidates) == 0 {
		return nil, common.StringErrorWrapper("no candidates", "no_candidates", http.StatusInternalServerError)
	}

	usage := p.GetUsage()
	*usage = gemini.ConvertOpenAIUsageWithFallback(geminiResponse.UsageMetadata, geminiResponse)

	return geminiResponse, nil
}

func (p *VertexAIProvider) CreateGeminiChatStream(request *gemini.GeminiChatRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getGeminiRequest(request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	channel := p.GetChannel()

	chatHandler := &gemini.GeminiRelayStreamHandler{
		Usage:     p.Usage,
		ModelName: request.Model,
		Prefix:    `data: `,

		Key: channel.Key,
	}

	// 发送请求
	resp, openaiErr := p.Requester.SendRequestRaw(req)
	if openaiErr != nil {
		return nil, openaiErr
	}

	stream, openaiErr := requester.RequestNoTrimStream(p.Requester, resp, chatHandler.HandlerStream)
	if openaiErr != nil {
		return nil, openaiErr
	}

	return stream, nil
}

func (p *VertexAIProvider) getGeminiRequest(request *gemini.GeminiChatRequest) (*http.Request, *types.OpenAIErrorWithStatusCode) {
	var err error
	p.Category, err = category.GetCategory(request.Model)
	if err != nil || p.Category.ChatComplete == nil || p.Category.ResponseChatComplete == nil {
		return nil, common.StringErrorWrapperLocal("vertexAI gemini provider not found", "vertexAI_err", http.StatusInternalServerError)
	}

	// 根据 Action 确定正确的 URL
	otherUrl := getVertexAIGeminiURL(request.Action, request.Stream)
	modelName := p.Category.GetModelName(request.Model)

	// 获取请求地址
	fullRequestURL := p.GetFullRequestURL(modelName, otherUrl)
	if fullRequestURL == "" {
		return nil, common.StringErrorWrapperLocal("vertexAI config error", "invalid_vertexai_config", http.StatusInternalServerError)
	}

	headers, err := p.getRequestHeadersInternal()
	if err != nil {
		return nil, p.handleTokenError(err)
	}

	if request.Stream {
		headers["Accept"] = "text/event-stream"
	}

	// 错误处理
	p.Requester.ErrorHandler = RequestErrorHandle(p.Category.ErrorHandler)

	dataMap, wasVertexAI, exists := p.GetProcessedBody()
	if !exists || !wasVertexAI {
		if rawMap, ok := p.GetRawMapBody(); ok {
			dataMap = rawMap
			gemini.CleanGeminiRequestMap(dataMap, true)
		} else if rawData, rawExists := p.GetRawBody(); rawExists {
			dataMap = make(map[string]interface{})
			if err := json.Unmarshal(rawData, &dataMap); err != nil {
				return nil, common.ErrorWrapper(err, "unmarshal_vertexai_gemini_data_failed", http.StatusInternalServerError)
			}
			gemini.CleanGeminiRequestMap(dataMap, true)
		} else if exists {
			gemini.CleanGeminiRequestMap(dataMap, true)
		} else {
			return nil, common.StringErrorWrapperLocal("request body not found", "request_body_not_found", http.StatusInternalServerError)
		}
		p.SetProcessedBody(dataMap, true)
		p.Context.Set(config.GinRawMapBodyKey, nil)
	}

	// 使用BaseProvider的统一方法创建请求，支持额外参数处理
	req, errWithCode := p.NewRequestWithCustomParams(http.MethodPost, fullRequestURL, dataMap, headers, request.Model)
	if errWithCode != nil {
		return nil, errWithCode
	}
	return req, nil
}

// getVertexAIGeminiURL 根据 Action 和 Stream 返回正确的 Vertex AI URL
func getVertexAIGeminiURL(action string, stream bool) string {
	switch action {
	case "countTokens":
		return "countTokens"
	case "streamGenerateContent":
		return "streamGenerateContent?alt=sse"
	case "generateContent":
		if stream {
			return "streamGenerateContent?alt=sse"
		}
		return "generateContent"
	default:
		// 对于其他 action，直接使用原始值
		if stream && !strings.Contains(action, "stream") {
			return "stream" + strings.Title(action) + "?alt=sse"
		}
		return action
	}
}

func convertOpenAIUsage(geminiUsage *gemini.GeminiUsageMetadata) types.Usage {
	if geminiUsage == nil {
		return types.Usage{}
	}
	return types.Usage{
		PromptTokens:     geminiUsage.PromptTokenCount,
		CompletionTokens: geminiUsage.CandidatesTokenCount + geminiUsage.ThoughtsTokenCount,
		TotalTokens:      geminiUsage.TotalTokenCount,

		CompletionTokensDetails: types.CompletionTokensDetails{
			ReasoningTokens: geminiUsage.ThoughtsTokenCount,
		},
	}
}
