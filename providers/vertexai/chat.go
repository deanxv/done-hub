package vertexai

import (
	"done-hub/common"
	"done-hub/common/requester"
	"done-hub/providers/gemini"
	"done-hub/providers/vertexai/category"
	"done-hub/types"
	"encoding/json"
	"net/http"
)

func (p *VertexAIProvider) CreateChatCompletion(request *types.ChatCompletionRequest) (*types.ChatCompletionResponse, *types.OpenAIErrorWithStatusCode) {
	request.OneOtherArg = p.GetOtherArg()
	// 发送请求
	response, errWithCode := p.Send(request)
	if errWithCode != nil {
		return nil, errWithCode
	}

	defer response.Body.Close()

	return p.Category.ResponseChatComplete(p, response, request)
}

func (p *VertexAIProvider) CreateChatCompletionStream(request *types.ChatCompletionRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	request.OneOtherArg = p.GetOtherArg()
	// 发送请求
	response, errWithCode := p.Send(request)
	if errWithCode != nil {
		return nil, errWithCode
	}

	return requester.RequestStream(p.Requester, response, p.Category.ResponseChatCompleteStrem(p, request))
}

func (p *VertexAIProvider) Send(request *types.ChatCompletionRequest) (*http.Response, *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getChatRequest(request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	// 发送请求
	return p.Requester.SendRequestRaw(req)
}

func (p *VertexAIProvider) getChatRequest(request *types.ChatCompletionRequest) (*http.Request, *types.OpenAIErrorWithStatusCode) {
	var err error
	p.Category, err = category.GetCategory(request.Model)
	if err != nil || p.Category.ChatComplete == nil || p.Category.ResponseChatComplete == nil {
		return nil, common.StringErrorWrapperLocal("vertexAI provider not found", "vertexAI_err", http.StatusInternalServerError)
	}

	otherUrl := p.Category.GetOtherUrl(request.Stream)
	modelName := p.Category.GetModelName(request.Model)

	// 获取请求地址
	fullRequestURL := p.GetFullRequestURL(modelName, otherUrl)
	if fullRequestURL == "" {
		return nil, common.ErrorWrapperLocal(nil, "invalid_vertexai_config", http.StatusInternalServerError)
	}

	headers, err := p.getRequestHeadersInternal()
	if err != nil {
		return nil, p.handleTokenError(err)
	}

	vertexaiRequest, errWithCode := p.Category.ChatComplete(request)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 对于 Gemini 模型，需要清理请求数据
	var finalRequest any = vertexaiRequest
	if p.Category.Category == "gemini" {
		rawData, err := json.Marshal(vertexaiRequest)
		if err != nil {
			return nil, common.ErrorWrapper(err, "marshal_vertexai_request_failed", http.StatusInternalServerError)
		}
		var dataMap map[string]interface{}
		if err := json.Unmarshal(rawData, &dataMap); err != nil {
			return nil, common.ErrorWrapper(err, "unmarshal_vertexai_request_failed", http.StatusInternalServerError)
		}
		gemini.CleanGeminiRequestMap(dataMap, true)
		finalRequest = dataMap
	}

	// 错误处理
	p.Requester.ErrorHandler = RequestErrorHandle(p.Category.ErrorHandler, p.ProjectID)

	// 使用BaseProvider的统一方法创建请求，支持额外参数处理
	req, errWithCode := p.NewRequestWithCustomParams(http.MethodPost, fullRequestURL, finalRequest, headers, request.Model)
	if errWithCode != nil {
		return nil, errWithCode
	}
	return req, nil
}
