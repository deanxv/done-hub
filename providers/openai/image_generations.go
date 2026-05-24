package openai

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/types"
	"net/http"
)

func (p *OpenAIProvider) CreateImageGenerations(request *types.ImageRequest) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.GetRequestTextBody(config.RelayModeImagesGenerations, request.Model, request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	response := &OpenAIProviderImageResponse{}
	// 发送请求
	_, errWithCode = p.Requester.SendRequest(req, response, false)

	// 即便后续判错也先落 usage：覆盖"HTTP 200 + body 带 error 字段 + 仍含 usage"这种聚合上游场景。
	if response.Usage != nil && response.Usage.TotalTokens > 0 {
		*p.Usage = *response.Usage.ToOpenAIUsage()
	}

	if errWithCode != nil {
		return nil, errWithCode
	}

	// 检测是否错误
	openaiErr := ErrorHandle(&response.OpenAIErrorResponse)
	if openaiErr != nil {
		errWithCode = &types.OpenAIErrorWithStatusCode{
			OpenAIError: *openaiErr,
			StatusCode:  http.StatusBadRequest,
		}
		return nil, errWithCode
	}

	if p.Usage.TotalTokens == 0 {
		// 上游未返回 usage，按生成图像数量兜底，避免空回复计费
		imageCount := len(response.Data)
		p.Usage.CompletionTokens = imageCount * 258
		p.Usage.TotalTokens = p.Usage.PromptTokens + p.Usage.CompletionTokens
	}

	return &response.ImageResponse, nil

}

func IsWithinRange(element string, value int) bool {
	if _, ok := common.DalleGenerationImageAmounts[element]; !ok {
		return true
	}
	minCount := common.DalleGenerationImageAmounts[element][0]
	maxCount := common.DalleGenerationImageAmounts[element][1]

	return value >= minCount && value <= maxCount
}
