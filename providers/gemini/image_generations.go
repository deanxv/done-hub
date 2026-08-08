package gemini

import (
	"done-hub/common"
	"done-hub/common/requester"
	"done-hub/common/utils"
	"done-hub/providers/base"
	"done-hub/types"
	"net/http"
)

// CreateImageGenerationsStream Imagen 只有 predict 非流式端点；嵌入的 OpenAIProvider 流式方法
// 会按 config.ImagesGenerations（哨兵值 "1"）拼出无效 URL，覆写返回哨兵让 relay 层降级合成 SSE。
// geminicli / antigravity / vertexai_express 经嵌入继承此覆写。
func (p *GeminiProvider) CreateImageGenerationsStream(request *types.ImageRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	return nil, base.ImageStreamNotSupportedError()
}

func (p *GeminiProvider) CreateImageGenerations(request *types.ImageRequest) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode) {
	// 创建动态参数map
	parameters := make(GeminiImageParametersDynamic)
	parameters["sampleCount"] = request.N

	// 设置默认的personGeneration
	parameters["personGeneration"] = "allow_adult"

	// 处理AspectRatio
	if request.AspectRatio != nil {
		parameters["aspectRatio"] = *request.AspectRatio
	} else {
		switch request.Size {
		case "1024x1792":
			parameters["aspectRatio"] = "9:16"
		case "1792x1024":
			parameters["aspectRatio"] = "16:9"
		default:
			parameters["aspectRatio"] = "1:1"
		}
	}

	// 透传所有额外参数
	if request.ExtraParams != nil {
		for key, value := range request.ExtraParams {
			parameters[key] = value
		}
	}

	geminiRequest := &GeminiImageRequest{
		Instances: []GeminiImageInstance{
			{
				Prompt: request.Prompt,
			},
		},
		Parameters: parameters,
	}

	fullRequestURL := p.GetFullRequestURL("predict", request.Model)
	headers := p.GetRequestHeaders()

	req, err := p.Requester.NewRequest(http.MethodPost, fullRequestURL, p.Requester.WithBody(geminiRequest), p.Requester.WithHeader(headers))
	if err != nil {
		return nil, common.ErrorWrapper(err, "new_request_failed", http.StatusInternalServerError)
	}

	defer req.Body.Close()

	geminiImageResponse := &GeminiImageResponse{}
	_, errWithCode := p.Requester.SendRequest(req, geminiImageResponse, false)
	if errWithCode != nil {
		return nil, errWithCode
	}

	imageCount := len(geminiImageResponse.Predictions)

	// 如果imageCount为0，则返回错误
	if imageCount == 0 {
		return nil, common.StringErrorWrapper("no image generated", "no_image_generated", http.StatusInternalServerError)
	}

	openaiResponse := &types.ImageResponse{
		Created: utils.GetTimestamp(),
		Data:    make([]types.ImageResponseDataInner, 0, imageCount),
	}

	for _, prediction := range geminiImageResponse.Predictions {
		if prediction.BytesBase64Encoded == "" {
			continue
		}

		openaiResponse.Data = append(openaiResponse.Data, types.ImageResponseDataInner{
			B64JSON: prediction.BytesBase64Encoded,
		})
	}

	usage := p.GetUsage()
	// PromptTokens保持之前根据prompt内容计算的值
	// CompletionTokens根据生成的图像数量计算，避免空回复计费问题
	usage.CompletionTokens = imageCount * 258
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	return openaiResponse, nil
}
