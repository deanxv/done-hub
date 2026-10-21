package ali

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/types"
	"net/http"
)

func (p *AliProvider) CreateEmbeddings(request *types.EmbeddingRequest) (*types.EmbeddingResponse, *types.OpenAIErrorWithStatusCode) {
	url, errWithCode := p.GetSupportedAPIUri(config.RelayModeEmbeddings)
	if errWithCode != nil {
		return nil, errWithCode
	}
	// 获取请求地址
	fullRequestURL := p.GetFullRequestURL(url, request.Model)

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

	aliResponse := &AliEmbeddingResponse{}

	// 发送请求
	_, errWithCode = p.Requester.SendRequest(req, aliResponse, false)
	if errWithCode != nil {
		return nil, errWithCode
	}

	return p.convertToEmbeddingOpenai(aliResponse, request)
}

func convertFromEmbeddingOpenai(request *types.EmbeddingRequest) (*AliEmbeddingRequest, *types.OpenAIErrorWithStatusCode) {
	texts, err := request.ParseInput()
	if err != nil {
		return nil, common.StringErrorWrapperLocal(err.Error(), "invalid_request_error", http.StatusBadRequest)
	}
	return &AliEmbeddingRequest{
		Model: "text-embedding-v1",
		Input: struct {
			Texts []string `json:"texts"`
		}{
			Texts: texts,
		},
	}, nil
}

func (p *AliProvider) convertToEmbeddingOpenai(response *AliEmbeddingResponse, request *types.EmbeddingRequest) (openaiResponse *types.EmbeddingResponse, errWithCode *types.OpenAIErrorWithStatusCode) {
	aiError := errorHandle(&response.AliError)
	if aiError != nil {
		errWithCode = &types.OpenAIErrorWithStatusCode{
			OpenAIError: *aiError,
			StatusCode:  http.StatusBadRequest,
		}
		return
	}

	openaiResponse = &types.EmbeddingResponse{
		Object: "list",
		Data:   make([]types.Embedding, 0, len(response.Output.Embeddings)),
		Model:  request.Model,
		Usage: &types.Usage{
			PromptTokens:     response.Usage.TotalTokens,
			CompletionTokens: response.Usage.OutputTokens,
			TotalTokens:      response.Usage.TotalTokens,
		},
	}

	for _, item := range response.Output.Embeddings {
		openaiResponse.Data = append(openaiResponse.Data, types.Embedding{
			Object:    `embedding`,
			Index:     item.TextIndex,
			Embedding: item.Embedding,
		})
	}

	*p.Usage = *openaiResponse.Usage

	return
}
