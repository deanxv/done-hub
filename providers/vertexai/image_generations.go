package vertexai

import (
	"done-hub/common"
	"done-hub/types"
	"net/http"
	"time"
)

type VertexAIImageRequest struct {
	Instances  []VertexAIImageInstance `json:"instances"`
	Parameters VertexAIImageParameters `json:"parameters"`
}

type VertexAIImageInstance struct {
	Prompt string `json:"prompt"`
}

type VertexAIImageParameters struct {
	SampleCount      int                    `json:"sampleCount"`
	AspectRatio      string                 `json:"aspectRatio,omitempty"`
	NegativePrompt   string                 `json:"negativePrompt,omitempty"`
	Seed             *uint32                `json:"seed,omitempty"`
	EnhancePrompt    *bool                  `json:"enhancePrompt,omitempty"`
	PersonGeneration string                 `json:"personGeneration,omitempty"`
	SafetySetting    string                 `json:"safetySetting,omitempty"`
	AddWatermark     *bool                  `json:"addWatermark,omitempty"`
	OutputOptions    *VertexAIOutputOptions `json:"outputOptions,omitempty"`
}

type VertexAIOutputOptions struct {
	MimeType           string `json:"mimeType,omitempty"`
	CompressionQuality int    `json:"compressionQuality,omitempty"`
}

type VertexAIImageResponse struct {
	Predictions []VertexAIImagePrediction `json:"predictions"`
}

type VertexAIImagePrediction struct {
	BytesBase64Encoded string `json:"bytesBase64Encoded"`
	MimeType           string `json:"mimeType,omitempty"`
}

func (p *VertexAIProvider) CreateImageGenerations(request *types.ImageRequest) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode) {
	// 构建请求体
	vertexRequest := &VertexAIImageRequest{
		Instances: []VertexAIImageInstance{
			{
				Prompt: request.Prompt,
			},
		},
		Parameters: VertexAIImageParameters{
			SampleCount: request.N,
			// 默认设置为允许成人
			PersonGeneration: "allow_adult",
		},
	}

	// 设置图片比例
	if request.AspectRatio != nil {
		vertexRequest.Parameters.AspectRatio = *request.AspectRatio
	} else if request.Size != "" {
		// 从尺寸转换为比例
		vertexRequest.Parameters.AspectRatio = sizeToAspectRatio(request.Size)
	} else {
		// 默认比例
		vertexRequest.Parameters.AspectRatio = "1:1"
	}
	// 设置输出选项
	vertexRequest.Parameters.OutputOptions = &VertexAIOutputOptions{
		MimeType: "image/png",
	}

	// 获取请求地址
	fullRequestURL := p.GetFullRequestURL(request.Model, "predict")
	if fullRequestURL == "" {
		return nil, common.ErrorWrapper(nil, "invalid_vertex_ai_config", http.StatusInternalServerError)
	}

	// 获取请求头
	headers := p.GetRequestHeaders()

	// 使用BaseProvider的统一方法创建请求，支持额外参数处理
	req, errWithCode := p.NewRequestWithCustomParams(http.MethodPost, fullRequestURL, vertexRequest, headers, request.Model)
	if errWithCode != nil {
		return nil, errWithCode
	}

	vertexResponse := &VertexAIImageResponse{}
	_, errWithCode = p.Requester.SendRequest(req, vertexResponse, false)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 检查是否有图像生成
	imageCount := len(vertexResponse.Predictions)
	if imageCount == 0 {
		return nil, common.StringErrorWrapper("no image generated", "no_image_generated", http.StatusInternalServerError)
	}

	// 转换响应格式
	openaiResponse := &types.ImageResponse{
		Created: time.Now().Unix(),
		Data:    make([]types.ImageResponseDataInner, 0, imageCount),
	}

	for _, prediction := range vertexResponse.Predictions {
		if prediction.BytesBase64Encoded == "" {
			continue
		}

		openaiResponse.Data = append(openaiResponse.Data, types.ImageResponseDataInner{
			B64JSON: prediction.BytesBase64Encoded,
		})
	}

	// 设置使用量 - 生图模型的token计算
	// PromptTokens保持之前根据prompt内容计算的值
	// CompletionTokens根据生成的图像数量计算，避免空回复计费问题
	p.Usage.CompletionTokens = imageCount * 258
	p.Usage.TotalTokens = p.Usage.PromptTokens + p.Usage.CompletionTokens

	return openaiResponse, nil
}

// 将尺寸转换为比例
func sizeToAspectRatio(size string) string {
	switch size {
	case "1024x1024":
		return "1:1"
	case "1024x1792":
		return "9:16"
	case "1792x1024":
		return "16:9"
	case "768x1024":
		return "3:4"
	case "1024x768":
		return "4:3"
	default:
		return "1:1"
	}
}
