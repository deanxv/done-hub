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
	Prompt         string `json:"prompt,omitempty" binding:"required"`
	Model          string `json:"model,omitempty"`
	N              int    `json:"n,omitempty"`
	Quality        string `json:"quality,omitempty"`
	Size           string `json:"size,omitempty"`
	Style          string `json:"style,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
	User           string `json:"user,omitempty"`
	// OpenAI 终端用户稳定标识，已取代 user（user 仍兼容）。非空时优先于 user，用于安全监控与滥用检测。
	// 结构体编码路径下不显式接住会在重新编码时静默丢参，客户端传了等于没传。
	SafetyIdentifier string  `json:"safety_identifier,omitempty"`
	AspectRatio      *string `json:"aspect_ratio,omitempty"`
	OutputQuality    *int    `json:"output_quality,omitempty"`
	SafetyTolerance  *string `json:"safety_tolerance,omitempty"`
	PromptUpsampling *string `json:"prompt_upsampling,omitempty"`

	Background        *string `json:"background,omitempty"`
	Moderation        *string `json:"moderation,omitempty"`
	OutputCompression *int    `json:"output_compression,omitempty"`
	OutputFormat      *string `json:"output_format,omitempty"`
	// Complexity 是规划算力档位（low/medium/high），与渲染 quality 相互独立。SVG 输出时
	// 它控制参考图的复杂度；不传则由模型默认值决定。同样走结构体编码，不接住会静默丢参。
	// openai 渠道与 codex 直连原生端点（codexDirectImageModels 白名单）都原样透传；只有走
	// Responses 编排的模型会在工具层丢弃——托管 image_generation 不接受该参数（会报
	// Complexity is not supported for this image model）。
	Complexity *string `json:"complexity,omitempty"`
	// GIF 输出专用（output_format=gif）：帧数与帧时长。与 svg 同属会被结构体编码静默
	// 丢弃的字段，且上游对取值有硬约束（2-16 帧、单帧 20ms-655350ms），交由上游校验。
	FrameCount      *int `json:"frame_count,omitempty"`
	FrameDurationMs *int `json:"frame_duration_ms,omitempty"`
	// SVG 输出配置：output_format=svg 时，启用 SVG model selection 就必须带 svg.model，
	// 未启用则应整个省略。官方当前字段为 model 与 reasoning.effort，但用 RawMessage 原样承载
	// 而非固定结构体：结构体编码路径（未开 PassThroughBody / AllowExtraBody）下，上游将来
	// 增补任何 svg 子字段都会被重新编码静默丢弃——这正是本字段当初被吃掉的原因。
	// nil 表示未传（omitempty 不输出），空对象 {} 与「传了空 svg」的语义也自然保留。
	SVG json.RawMessage `json:"svg,omitempty"`

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
	// SVG 回显。同样用 RawMessage：上游可能返回对象或字符串，本地只做原样回显不读取值。
	SVG json.RawMessage `json:"svg,omitempty"`
	// SVGUsage 是 SVG 作者模型（svg.model）的 token 用量，官方与渲染侧 usage 分开上报：
	// input_tokens / model / output_tokens / reasoning_tokens / total_tokens。
	// 本地只回显、不参与计费推算——计费口径仍取顶层 usage，避免同一份 token 双算。
	SVGUsage json.RawMessage `json:"svg_usage,omitempty"`
	Quality  json.RawMessage `json:"quality,omitempty"`
	Size     json.RawMessage `json:"size,omitempty"`
	// 官方回显本次实际生效的 safety_identifier（请求 safety_identifier 非空取之，否则回落 user）。
	// 同上用 RawMessage 原样回显，不读取其值。
	SafetyIdentifier json.RawMessage `json:"safety_identifier,omitempty"`
	Usage            *ResponsesUsage `json:"usage,omitempty"`
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
	Quality        string                  `form:"quality"`
	Size           string                  `form:"size"`
	ResponseFormat string                  `form:"response_format"`
	User           string                  `form:"user"`
	Stream         *bool                   `form:"stream"`
	PartialImages  *int                    `form:"partial_images"`
	// 以下为官方 /v1/images/edits 已支持、但原先未建模的字段。OpenAI 渠道走原始 body 直发
	// 不受影响；codex 渠道会把 edits 转成 Responses 的 image_generation 工具，不接住就会在
	// 转协议时静默丢参（gif/svg 依赖 output_format/svg 等）。
	// complexity 只在直连 images 协议的渠道上有意义（codex 已对 gpt-image-* 直连原生端点）；
	// 走 Responses 编排的模型仍会在工具层丢弃它。
	Background        *string `form:"background"`
	Complexity        *string `form:"complexity"`
	Moderation        *string `form:"moderation"`
	OutputCompression *int    `form:"output_compression"`
	OutputFormat      *string `form:"output_format"`
	FrameCount        *int    `form:"frame_count"`
	FrameDurationMs   *int    `form:"frame_duration_ms"`
	// SVG 走 multipart 表单，gin 的表单绑定无法直接写入 []byte/json.RawMessage，
	// 因此用 *string 接住 JSON 文本，再由 codex 侧解码成对象。
	// json:"-" 是必需的：encoding/json 无 tag 时按字段名大小写不敏感匹配，JSON 形态的
	// edits 请求（聚合上游契约）里 "svg": {...} 会命中这个 *string 字段并解码失败，
	// 把原本被忽略的键变成 400。multipart 绑定走 form tag，不受影响。
	// 已知取舍：加了 json:"-" 后，JSON 形态的 edits 在 codex 渠道上 svg 不会透传（保持
	// 「未知键被忽略」的旧行为）。官方 edits 只定义 multipart，openai 渠道又走原始字节直发，
	// 因此实际只影响「聚合上游用 JSON 提交 svg」这一非官方场景；需要支持时再单独建模。
	SVG *string `json:"-" form:"svg"`
}

// StreamEnabled 判断请求是否要求流式返回。edits 走 multipart 表单，stream 以字符串
// "true" 传入，gin 绑定到 *bool 已做转换，两个协议共用指针判空语义。
func (r *ImageRequest) StreamEnabled() bool {
	return r.Stream != nil && *r.Stream
}

func (r *ImageEditRequest) StreamEnabled() bool {
	return r.Stream != nil && *r.Stream
}
