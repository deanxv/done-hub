package types

import (
	"mime/multipart"
	"strings"
)

// 走 image generations 协议处理的模型清单。命中时 chat completions 入口会把请求降级到
// /v1/images/generations，避免上游用 chat 协议返回 base64 时被本地 tokenize 当文本反算。
// 拆成 exact / prefix 两组以避免误判（例如 dall-e-2 vs dall-e-2-something）。
var (
	imageGenerationModelExact = []string{
		"dall-e-2",
		"dall-e-3",
	}
	imageGenerationModelPrefixes = []string{
		"gpt-image-",
		"chatgpt-image-",
	}
)

func IsImageGenerationModel(modelName string) bool {
	m := strings.ToLower(modelName)
	for _, name := range imageGenerationModelExact {
		if m == name {
			return true
		}
	}
	for _, p := range imageGenerationModelPrefixes {
		if strings.HasPrefix(m, p) {
			return true
		}
	}
	return false
}

type ImageRequest struct {
	Prompt           string  `json:"prompt,omitempty" binding:"required"`
	Model            string  `json:"model,omitempty"`
	N                int     `json:"n,omitempty"`
	Quality          string  `json:"quality,omitempty"`
	Size             string  `json:"size,omitempty"`
	Style            string  `json:"style,omitempty"`
	ResponseFormat   string  `json:"response_format,omitempty"`
	User             string  `json:"user,omitempty"`
	AspectRatio      *string `json:"aspect_ratio,omitempty"`
	OutputQuality    *int    `json:"output_quality,omitempty"`
	SafetyTolerance  *string `json:"safety_tolerance,omitempty"`
	PromptUpsampling *string `json:"prompt_upsampling,omitempty"`

	Background        *string `json:"background,omitempty"`
	Moderation        *string `json:"moderation,omitempty"`
	OutputCompression *int    `json:"output_compression,omitempty"`
	OutputFormat      *string `json:"output_format,omitempty"`

	// 透传参数，用于支持特定provider的额外参数
	ExtraParams map[string]interface{} `json:"extra_params,omitempty"`
}

type ImageResponse struct {
	Created any                      `json:"created,omitempty"`
	Data    []ImageResponseDataInner `json:"data,omitempty"`
	Usage   *ResponsesUsage          `json:"usage,omitempty"`
}

type ImageResponseDataInner struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

type ImageEditRequest struct {
	Image          *multipart.FileHeader   `form:"image"`
	Images         []*multipart.FileHeader `form:"image[]"`
	Mask           *multipart.FileHeader   `form:"mask"`
	Model          string                  `form:"model"`
	Prompt         string                  `form:"prompt"`
	N              int                     `form:"n"`
	Size           string                  `form:"size"`
	ResponseFormat string                  `form:"response_format"`
	User           string                  `form:"user"`
}
