package vertexai

import (
	"done-hub/common"
	"done-hub/common/requester"
	"done-hub/providers/claude"
	"done-hub/providers/vertexai/category"
	"done-hub/types"
	"net/http"
)

func (p *VertexAIProvider) CreateClaudeChat(request *claude.ClaudeRequest) (*claude.ClaudeResponse, *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getClaudeRequest(request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	claudeResponse := &claude.ClaudeResponse{}
	// // 发送请求
	_, openaiErr := p.Requester.SendRequest(req, claudeResponse, false)
	if openaiErr != nil {
		return nil, openaiErr
	}

	claude.ClaudeUsageToOpenaiUsage(&claudeResponse.Usage, p.GetUsage())

	return claudeResponse, nil
}

func (p *VertexAIProvider) CreateClaudeChatStream(request *claude.ClaudeRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getClaudeRequest(request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	chatHandler := &claude.ClaudeRelayStreamHandler{
		Usage:     p.Usage,
		ModelName: request.Model,
		Prefix:    `data: {"type"`,
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

func (p *VertexAIProvider) getClaudeRequest(request *claude.ClaudeRequest) (*http.Request, *types.OpenAIErrorWithStatusCode) {
	var err error
	p.Category, err = category.GetCategory(request.Model)
	if err != nil || p.Category.Category != "claude" {
		return nil, common.StringErrorWrapperLocal("vertexAI provider not found", "vertexAI_err", http.StatusInternalServerError)
	}

	otherUrl := p.Category.GetOtherUrl(request.Stream)
	modelName := p.Category.GetModelName(request.Model)

	// 获取请求地址
	fullRequestURL := p.GetFullRequestURL(modelName, otherUrl)
	if fullRequestURL == "" {
		return nil, common.StringErrorWrapperLocal("vertexAI config error", "invalid_vertexai_config", http.StatusInternalServerError)
	}

	headers := p.GetRequestHeaders()

	if headers == nil {
		return nil, common.StringErrorWrapperLocal("vertexAI config error", "invalid_vertexai_config", http.StatusInternalServerError)
	}

	if request.Stream {
		headers["Accept"] = "text/event-stream"
	}

	copyRequest := *request

	vertexaiRequest := &category.ClaudeRequest{
		ClaudeRequest:    &copyRequest,
		AnthropicVersion: category.AnthropicVersion,
	}
	vertexaiRequest.Model = ""

	// 错误处理
	p.Requester.ErrorHandler = RequestErrorHandle(p.Category.ErrorHandler)

	// 使用BaseProvider的统一方法创建请求，支持额外参数处理
	req, errWithCode := p.NewRequestWithCustomParams(http.MethodPost, fullRequestURL, vertexaiRequest, headers, request.Model)
	if errWithCode != nil {
		return nil, errWithCode
	}
	return req, nil
}
