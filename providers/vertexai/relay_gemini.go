package vertexai

import (
	"done-hub/common"
	"done-hub/common/requester"
	"done-hub/providers/gemini"
	"done-hub/providers/vertexai/category"
	"done-hub/types"
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

	// 字节级路径：优先使用已清理的字节缓存，避免对含 base64 的大请求做 json.Unmarshal/Marshal
	bodyBytes, wasVertexAI, exists := p.GetProcessedBodyBytes()
	if exists && wasVertexAI {
		// 缓存命中（VertexAI → VertexAI 重试）
		req, errWithCode := p.NewRequestWithCustomParamsBytes(http.MethodPost, fullRequestURL, bodyBytes, headers, request.Model)
		if errWithCode != nil {
			return nil, errWithCode
		}
		return req, nil
	}

	// 从原始字节清理
	if rawData, rawExists := p.GetRawBody(); rawExists {
		cleaned, err := gemini.CleanGeminiRequestBytes(rawData, true)
		if err != nil {
			return nil, common.ErrorWrapper(err, "clean_gemini_request_bytes_failed", http.StatusInternalServerError)
		}
		p.SetProcessedBodyBytes(cleaned, true)
		req, errWithCode := p.NewRequestWithCustomParamsBytes(http.MethodPost, fullRequestURL, cleaned, headers, request.Model)
		if errWithCode != nil {
			return nil, errWithCode
		}
		return req, nil
	}

	// map 回退（跨 provider 重试）
	dataMap, _, mapExists := p.GetProcessedBody()
	if mapExists {
		gemini.CleanGeminiRequestMap(dataMap, true)
		req, errWithCode := p.NewRequestWithCustomParams(http.MethodPost, fullRequestURL, dataMap, headers, request.Model)
		if errWithCode != nil {
			return nil, errWithCode
		}
		return req, nil
	}

	return nil, common.StringErrorWrapperLocal("request body not found", "request_body_not_found", http.StatusInternalServerError)
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
