package types

import (
	"encoding/json"
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
		"imagen-", // imagen 只支持 predict，chat 请求需降级到 image 协议（Gemini/Vertex 的 CreateImageGenerations）
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

	Stream        *bool `json:"stream,omitempty"`
	PartialImages *int  `json:"partial_images,omitempty"`

	// 透传参数，用于支持特定provider的额外参数
	ExtraParams map[string]interface{} `json:"extra_params,omitempty"`
}

type ImageResponse struct {
	Created any                      `json:"created,omitempty"`
	Data    []ImageResponseDataInner `json:"data,omitempty"`
	// gpt-image-* 系列顶层附带 background/output_format/quality/size 等参数回显。
	// 结构体编码路径（未开 PassThroughBody）下若不显式接住会被丢弃，导致返回体与官方不一致。
	//
	// 用 json.RawMessage 而非 string：这几个字段仅做原样回显、本地从不读取其值，用 string 会
	// 在聚合上游把 quality/size 返成数字（如 size:1024）时触发 json 类型不匹配 →
	// decode_response_failed(500)。而该 500 非 LocalError、shouldRetry 对 5xx 返 true，叠加
	// image_generations.go「先落 usage 再判错」，会导致本次已 Consume、重试成功再 Consume 一次
	// 的重复扣费。RawMessage 接受任意 JSON 值（字符串/数字/对象）不报错，且比 string 更保真
	// （原样透传上游写法）。全部 omitempty：nil（上游未返回，如 dall-e）时不输出零值。
	Background   json.RawMessage `json:"background,omitempty"`
	OutputFormat json.RawMessage `json:"output_format,omitempty"`
	Quality      json.RawMessage `json:"quality,omitempty"`
	Size         json.RawMessage `json:"size,omitempty"`
	Usage        *ResponsesUsage `json:"usage,omitempty"`
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
	Stream         *bool                   `form:"stream"`
	PartialImages  *int                    `form:"partial_images"`
}

// StreamEnabled 判断请求是否要求流式返回。edits 走 multipart 表单，stream 以字符串
// "true" 传入，gin 绑定到 *bool 已做转换，两个协议共用指针判空语义。
func (r *ImageRequest) StreamEnabled() bool {
	return r.Stream != nil && *r.Stream
}

func (r *ImageEditRequest) StreamEnabled() bool {
	return r.Stream != nil && *r.Stream
}
