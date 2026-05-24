package openai

import (
	"done-hub/common/config"
	"done-hub/types"
	"net/http"
)

func (p *OpenAIProvider) CreateImageVariations(request *types.ImageEditRequest) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getRequestImageBody(config.RelayModeImagesVariations, request.Model, request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	response := &OpenAIProviderImageResponse{}
	// 发送请求
	_, errWithCode = p.Requester.SendRequest(req, response, false)

	// 上游 usage 优先（旧代码无条件用 imageCount*258 覆盖，丢失了上游真实计费数据）；
	// 顺带覆盖"HTTP 200 + body 带 error + 含 usage"的聚合上游场景。
	if response.Usage != nil && response.Usage.TotalTokens > 0 {
		*p.Usage = *response.Usage.ToOpenAIUsage()
	}

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

	if p.Usage.TotalTokens == 0 {
		// 上游未返回 usage，按生成图像数量兜底，避免空回复计费
		imageCount := len(response.Data)
		p.Usage.CompletionTokens = imageCount * 258
		p.Usage.TotalTokens = p.Usage.PromptTokens + p.Usage.CompletionTokens
	}

	return &response.ImageResponse, nil
}
