package zhipu

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/types"
	"net/http"
)

func (p *ZhipuProvider) CreateEmbeddings(request *types.EmbeddingRequest) (*types.EmbeddingResponse, *types.OpenAIErrorWithStatusCode) {
	url, errWithCode := p.GetSupportedAPIUri(config.RelayModeEmbeddings)
	if errWithCode != nil {
		return nil, errWithCode
	}
	// 获取请求地址
	fullRequestURL := p.GetFullRequestURL(url)
	if fullRequestURL == "" {
		return nil, common.ErrorWrapper(nil, "invalid_zhipu_config", http.StatusInternalServerError)
	}

	// 获取请求头
	headers := p.GetRequestHeaders()

	aliRequest, errWithCode := convertFromEmbeddingOpenai(request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	// 创建请求
	req, err := p.Requester.NewRequest(http.MethodPost, fullRequestURL, p.Requester.WithBody(aliRequest), p.Requester.WithHeader(headers))
	if err != nil {
		return nil, common.ErrorWrapper(err, "new_request_failed", http.StatusInternalServerError)
	}
	defer req.Body.Close()

	zhipuResponse := &ZhipuEmbeddingResponse{}

	// 发送请求
	_, errWithCode = p.Requester.SendRequest(req, zhipuResponse, false)
	if errWithCode != nil {
		return nil, errWithCode
	}

	return p.convertToEmbeddingOpenai(zhipuResponse, request)
}

func convertFromEmbeddingOpenai(request *types.EmbeddingRequest) (*ZhipuEmbeddingRequest, *types.OpenAIErrorWithStatusCode) {
	input, err := request.ParseInputString()
	if err != nil {
		return nil, common.StringErrorWrapperLocal(err.Error(), "invalid_request_error", http.StatusBadRequest)
	}
	return &ZhipuEmbeddingRequest{
		Model: request.Model,
		Input: input,
	}, nil
}

func (p *ZhipuProvider) convertToEmbeddingOpenai(response *ZhipuEmbeddingResponse, request *types.EmbeddingRequest) (openaiResponse *types.EmbeddingResponse, errWithCode *types.OpenAIErrorWithStatusCode) {
	aiError := errorHandle(&response.Error)
	if aiError != nil {
		errWithCode = &types.OpenAIErrorWithStatusCode{
			OpenAIError: *aiError,
			StatusCode:  http.StatusBadRequest,
		}
		return
	}

	openAIEmbeddingResponse := &types.EmbeddingResponse{
		Object: "list",
		Data:   response.Data,
		Model:  request.Model,
		Usage:  response.Usage,
	}

	*p.Usage = *response.Usage

	return openAIEmbeddingResponse, nil
}
