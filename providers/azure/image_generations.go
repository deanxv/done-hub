package azure

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/requester"
	"done-hub/providers/base"
	"done-hub/providers/openai"
	"done-hub/types"
	"errors"
	"net/http"
	"time"
)

// CreateImageGenerationsStream dall-e-2 走 Azure 异步 :submit + operation-location 轮询，
// 上游返回 202 + 任务状态 JSON，继承的流式方法会把它当空响应合成零事件流；返回哨兵让
// relay 层降级到下方带轮询的非流式实现。其余模型（gpt-image 等）URL 与协议同 OpenAI，
// 委托嵌入实现走真实流式。
func (p *AzureProvider) CreateImageGenerationsStream(request *types.ImageRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	if request.Model == "dall-e-2" {
		return nil, base.ImageStreamNotSupportedError()
	}
	return p.OpenAIProvider.CreateImageGenerationsStream(request)
}

func (p *AzureProvider) CreateImageGenerations(request *types.ImageRequest) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode) {

	req, errWithCode := p.GetRequestTextBody(config.RelayModeImagesGenerations, request.Model, request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	var response *types.ImageResponse
	var resp *http.Response
	if request.Model == "dall-e-2" {
		imageAzureResponse := &ImageAzureResponse{}
		resp, errWithCode = p.Requester.SendRequest(req, imageAzureResponse, false)
		if errWithCode != nil {
			return nil, errWithCode
		}
		response, errWithCode = p.ResponseAzureImageHandler(resp, imageAzureResponse)
		if errWithCode != nil {
			return nil, errWithCode
		}
	} else {
		var openaiResponse openai.OpenAIProviderImageResponse
		_, errWithCode = p.Requester.SendRequest(req, &openaiResponse, false)
		if errWithCode != nil {
			return nil, errWithCode
		}
		// 检测是否错误
		openaiErr := openai.ErrorHandle(&openaiResponse.OpenAIErrorResponse)
		if openaiErr != nil {
			errWithCode = &types.OpenAIErrorWithStatusCode{
				OpenAIError: *openaiErr,
				StatusCode:  http.StatusBadRequest,
			}
			return nil, errWithCode
		}
		response = &openaiResponse.ImageResponse
	}

	if response.Usage != nil && response.Usage.TotalTokens > 0 {
		*p.Usage = *response.Usage.ToOpenAIUsage()
	} else {
		// 如果没有返回usage信息，计算Azure生图的CompletionTokens
		imageCount := len(response.Data)
		// PromptTokens保持之前根据prompt内容计算的值
		// CompletionTokens根据生成的图像数量计算，避免空回复计费问题
		p.Usage.CompletionTokens = imageCount * 258
		p.Usage.TotalTokens = p.Usage.PromptTokens + p.Usage.CompletionTokens
	}

	return response, nil

}

func (p *AzureProvider) ResponseAzureImageHandler(resp *http.Response, azure *ImageAzureResponse) (OpenAIResponse *types.ImageResponse, errWithCode *types.OpenAIErrorWithStatusCode) {
	if azure.Status == "canceled" || azure.Status == "failed" {
		errWithCode = &types.OpenAIErrorWithStatusCode{
			OpenAIError: types.OpenAIError{
				Message: azure.Error.Message,
				Type:    "one_api_error",
				Code:    azure.Error.Code,
			},
			StatusCode: resp.StatusCode,
		}
		return
	}

	operationLocation := resp.Header.Get("operation-location")
	if operationLocation == "" {
		return nil, common.ErrorWrapper(errors.New("image url is empty"), "get_images_url_failed", http.StatusInternalServerError)
	}

	req, err := p.Requester.NewRequest("GET", operationLocation, p.Requester.WithHeader(p.GetRequestHeaders()))
	if err != nil {
		return nil, common.ErrorWrapper(err, "get_images_request_failed", http.StatusInternalServerError)
	}

	getImageAzureResponse := ImageAzureResponse{}
	for i := 0; i < 3; i++ {
		// 休眠 2 秒
		time.Sleep(2 * time.Second)
		_, errWithCode = p.Requester.SendRequest(req, &getImageAzureResponse, false)
		if errWithCode != nil {
			return
		}

		if getImageAzureResponse.Status == "canceled" || getImageAzureResponse.Status == "failed" {
			return nil, &types.OpenAIErrorWithStatusCode{
				OpenAIError: types.OpenAIError{
					Message: getImageAzureResponse.Error.Message,
					Type:    "get_images_request_failed",
					Code:    getImageAzureResponse.Error.Code,
				},
				StatusCode: resp.StatusCode,
			}
		}
		if getImageAzureResponse.Status == "succeeded" {
			return &getImageAzureResponse.Result, nil
		}
	}

	return nil, common.ErrorWrapper(errors.New("get image Timeout"), "get_images_url_failed", http.StatusInternalServerError)
}
