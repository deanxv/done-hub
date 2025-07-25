package openai

import (
	"done-hub/common/config"
	"done-hub/types"
	"net/http"
)

func (p *OpenAIProvider) CreateEmbeddings(request *types.EmbeddingRequest) (*types.EmbeddingResponse, *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.GetRequestTextBody(config.RelayModeEmbeddings, request.Model, request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	response := &OpenAIProviderEmbeddingsResponse{}
	// 发送请求
	_, errWithCode = p.Requester.SendRequest(req, response, false)
	if errWithCode != nil {
		return nil, errWithCode
	}

	openaiErr := ErrorHandle(&response.OpenAIErrorResponse)
	if openaiErr != nil {
		errWithCode = &types.OpenAIErrorWithStatusCode{
			OpenAIError: *openaiErr,
			StatusCode:  http.StatusBadRequest,
		}
		return nil, errWithCode
	}

	if response.Usage != nil {
		*p.Usage = *response.Usage
	}

	return &response.EmbeddingResponse, nil
}
