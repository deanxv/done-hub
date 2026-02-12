package vertexai_express

import (
	"done-hub/common"
	"done-hub/common/requester"
	"done-hub/providers/gemini"
	"done-hub/types"
	"encoding/json"
	"net/http"
	"strings"
)

func (p *VertexAIExpressProvider) CreateChatCompletion(request *types.ChatCompletionRequest) (*types.ChatCompletionResponse, *types.OpenAIErrorWithStatusCode) {
	if p.UseOpenaiAPI {
		return p.OpenAIProvider.CreateChatCompletion(request)
	}

	geminiRequest, errWithCode := gemini.ConvertFromChatOpenai(request)
	if errWithCode != nil {
		return nil, errWithCode
	}

	req, errWithCode := p.getChatRequest(geminiRequest, false)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	geminiChatResponse := &gemini.GeminiChatResponse{}
	_, errWithCode = p.Requester.SendRequest(req, geminiChatResponse, false)
	if errWithCode != nil {
		return nil, errWithCode
	}

	return gemini.ConvertToChatOpenai(p, geminiChatResponse, request)
}

func (p *VertexAIExpressProvider) CreateChatCompletionStream(request *types.ChatCompletionRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	channel := p.GetChannel()
	if p.UseOpenaiAPI {
		return p.OpenAIProvider.CreateChatCompletionStream(request)
	}

	geminiRequest, errWithCode := gemini.ConvertFromChatOpenai(request)
	if errWithCode != nil {
		return nil, errWithCode
	}

	req, errWithCode := p.getChatRequest(geminiRequest, false)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	// 发送请求
	resp, errWithCode := p.Requester.SendRequestRaw(req)
	if errWithCode != nil {
		return nil, errWithCode
	}

	chatHandler := &gemini.GeminiStreamHandler{
		Usage:   p.Usage,
		Request: request,

		Key:     channel.Key,
		Context: p.Context,
	}

	return requester.RequestStream(p.Requester, resp, chatHandler.HandlerStream)
}

// CreateGeminiChat 实现 GeminiChatInterface 接口
func (p *VertexAIExpressProvider) CreateGeminiChat(request *gemini.GeminiChatRequest) (*gemini.GeminiChatResponse, *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getChatRequest(request, true)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	geminiResponse := &gemini.GeminiChatResponse{}
	_, errWithCode = p.Requester.SendRequest(req, geminiResponse, false)
	if errWithCode != nil {
		return nil, errWithCode
	}

	if request.Action != "countTokens" && len(geminiResponse.Candidates) == 0 {
		return nil, common.StringErrorWrapper("no candidates", "no_candidates", http.StatusInternalServerError)
	}

	usage := p.GetUsage()
	*usage = gemini.ConvertOpenAIUsage(geminiResponse.UsageMetadata)

	return geminiResponse, nil
}

// CreateGeminiChatStream 实现 GeminiChatInterface 接口
func (p *VertexAIExpressProvider) CreateGeminiChatStream(request *gemini.GeminiChatRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getChatRequest(request, true)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	channel := p.GetChannel()

	chatHandler := &gemini.GeminiRelayStreamHandler{
		Usage:     p.Usage,
		ModelName: request.Model,
		Prefix:    `data: `,
		Key:       channel.Key,
	}

	resp, errWithCode := p.Requester.SendRequestRaw(req)
	if errWithCode != nil {
		return nil, errWithCode
	}

	stream, errWithCode := requester.RequestNoTrimStream(p.Requester, resp, chatHandler.HandlerStream)
	if errWithCode != nil {
		return nil, errWithCode
	}

	return stream, nil
}

func (p *VertexAIExpressProvider) getChatRequest(geminiRequest *gemini.GeminiChatRequest, isRelay bool) (*http.Request, *types.OpenAIErrorWithStatusCode) {
	// 根据 Action 确定正确的 URL
	url := getActionURL(geminiRequest)
	// 获取请求地址
	fullRequestURL := p.GetFullRequestURL(url, geminiRequest.Model)

	// 获取请求头
	headers := p.GetRequestHeaders()
	if geminiRequest.Stream {
		headers["Accept"] = "text/event-stream"
	}

	if isRelay {
		// 字节级路径：优先使用已清理的字节缓存，避免对含 base64 的大请求做 json.Unmarshal/Marshal
		bodyBytes, wasVertexAI, exists := p.GetProcessedBodyBytes()
		if exists && wasVertexAI {
			// 缓存命中（VertexAI → VertexAI Express 重试）
			req, errWithCode := p.NewRequestWithCustomParamsBytes(http.MethodPost, fullRequestURL, bodyBytes, headers, geminiRequest.Model)
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
			req, errWithCode := p.NewRequestWithCustomParamsBytes(http.MethodPost, fullRequestURL, cleaned, headers, geminiRequest.Model)
			if errWithCode != nil {
				return nil, errWithCode
			}
			return req, nil
		}

		// map 回退（跨 provider 重试）
		dataMap, _, mapExists := p.GetProcessedBody()
		if mapExists {
			gemini.CleanGeminiRequestMap(dataMap, true)
			req, errWithCode := p.NewRequestWithCustomParams(http.MethodPost, fullRequestURL, dataMap, headers, geminiRequest.Model)
			if errWithCode != nil {
				return nil, errWithCode
			}
			return req, nil
		}

		return nil, common.StringErrorWrapperLocal("request body not found", "request_body_not_found", http.StatusInternalServerError)
	}

	// 非 relay 路径（OpenAI → Gemini 转换）
	p.pluginHandle(geminiRequest)

	jsonBytes, err := json.Marshal(geminiRequest)
	if err != nil {
		return nil, common.ErrorWrapper(err, "marshal_failed", http.StatusInternalServerError)
	}
	var dataMap map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &dataMap); err != nil {
		return nil, common.ErrorWrapper(err, "unmarshal_data_failed", http.StatusInternalServerError)
	}
	gemini.CleanGeminiRequestMap(dataMap, true)

	req, errWithCode := p.NewRequestWithCustomParams(http.MethodPost, fullRequestURL, dataMap, headers, geminiRequest.Model)
	if errWithCode != nil {
		return nil, errWithCode
	}

	return req, nil
}

func (p *VertexAIExpressProvider) pluginHandle(request *gemini.GeminiChatRequest) {
	if !p.UseCodeExecution {
		return
	}

	if len(request.Tools) > 0 {
		return
	}

	if p.Channel.Plugin == nil {
		return
	}

	request.Tools = append(request.Tools, gemini.GeminiChatTools{
		CodeExecution: &gemini.GeminiCodeExecution{},
	})
}

func getActionURL(geminiRequest *gemini.GeminiChatRequest) string {
	action := geminiRequest.Action
	if action == "" {
		action = "generateContent"
	}

	switch action {
	case "countTokens":
		return "countTokens"
	case "streamGenerateContent":
		return "streamGenerateContent?alt=sse"
	case "generateContent":
		if geminiRequest.Stream {
			return "streamGenerateContent?alt=sse"
		}
		return "generateContent"
	case "predictLongRunning":
		return "predictLongRunning"
	default:
		if geminiRequest.Stream && !strings.Contains(action, "stream") {
			return "stream" + strings.Title(action) + "?alt=sse"
		}
		return action
	}
}
