package gemini

import (
	"done-hub/common"
	"done-hub/common/model_utils"
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
	// Gemini 原生生图模型（gemini-*-image、nano-banana 等）走 generateContent 端点，
	// 不支持 :predict，必须走 /v1/chat/completions 或原生 /gemini/.../generateContent。
	// 只有 imagen-* 系列才走 :predict。
	if !model_utils.HasPrefixCaseInsensitive(request.Model, "imagen") {
		return nil, common.StringErrorWrapperLocal(
			"this model only supports image generation via chat completions or native Gemini API, not /v1/images/generations",
			"unsupported_image_generation_path",
			http.StatusBadRequest,
		)
	}

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

	// 内容策略拦截了所有预测时，返回明确错误而非空成功响应，避免计费归零且符合 OpenAI 规范。
	if len(openaiResponse.Data) == 0 {
		return nil, common.StringErrorWrapper("all generated images were blocked by content policy", "content_policy_violation", http.StatusBadRequest)
	}

	usage := p.GetUsage()
	// PromptTokens 保持之前根据 prompt 内容计算的值。
	// Imagen predict 端点按每张图 258 token 计费（来源：Google cookbook），
	// 与 Gemini 原生生图（generateContent 路径）的 1290 token/张是不同端点的不同标准，见 chat.go。
	// 使用过滤后的实际图片数而非原始 Predictions 数，避免内容策略拦截部分预测时多计费。
	const imagenTokensPerImage = 258
	imageTokens := len(openaiResponse.Data) * imagenTokensPerImage
	usage.CompletionTokens = imageTokens
	usage.CompletionTokensDetails.ImageTokens = imageTokens
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	return openaiResponse, nil
}
