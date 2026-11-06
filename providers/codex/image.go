package codex

import (
	"bufio"
	"bytes"
	"done-hub/common"
	"done-hub/common/requester"
	"done-hub/providers/base"
	"done-hub/providers/openai"
	"done-hub/types"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

// Codex 内部 Responses + image_generation 工具的编排模型默认值，
// 专门承担"决定调用 image_generation 工具并选参"的轻量模型；真正的图像模型在 tools[0].model 指定。
// 上游会按 ChatGPT 账号套餐下线旧编排模型（如 gpt-5.4-mini），可在渠道插件参数
// images_main_model 里覆盖，留空使用此默认值。
const (
	imagesResponsesMainModel = "gpt-5.6-luna"
	imageToolActionGenerate  = "generate"
	imageToolActionEdit      = "edit"

	// codexDirectImagesGenerations 是 ChatGPT 后端暴露的原生 images 协议端点（相对 BaseURL）。
	// 固定路径，不进 getConfig()/ProviderConfig：其它端点可被渠道 BaseURL 覆盖，但这条只对
	// chatgpt.com 的 backend 有效，代理到只实现 /backend-api/codex/responses 的中转时本就该 404。
	// 对 gpt-image-* 走这里，而不是绕 Responses 编排：编排模型（images_main_model）驱动托管
	// image_generation 工具时会自己往 tool call 里填 complexity，而渲染侧对部分模型拒绝该参数，
	// 报 "Complexity 'medium' is not supported for this image model"。工具层删字段治不了根，
	// 只能绕开编排。CLIProxyAPI / sub2api 对同一批模型也是这么直连的。
	codexDirectImagesGenerations = "/backend-api/codex/images/generations"
	codexDirectImagesEdits       = "/backend-api/codex/images/edits"
)

type imagesRequestBody struct {
	Instructions      string              `json:"instructions"`
	Stream            bool                `json:"stream"`
	Reasoning         imagesReasoning     `json:"reasoning"`
	ParallelToolCalls bool                `json:"parallel_tool_calls"`
	Include           []string            `json:"include"`
	Model             string              `json:"model"`
	Store             bool                `json:"store"`
	ToolChoice        imagesToolChoice    `json:"tool_choice"`
	Input             []imagesInputItem   `json:"input"`
	Tools             []imageGenerateTool `json:"tools"`
}

type imagesReasoning struct {
	Effort  string `json:"effort"`
	Summary string `json:"summary"`
}

type imagesToolChoice struct {
	Type string `json:"type"`
}

type imagesInputItem struct {
	Type    string               `json:"type"`
	Role    string               `json:"role"`
	Content []imagesInputContent `json:"content"`
}

type imagesInputContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

type imageGenerateTool struct {
	Type              string              `json:"type"`
	Action            string              `json:"action"`
	Model             string              `json:"model"`
	N                 *int                `json:"n,omitempty"`
	Size              string              `json:"size,omitempty"`
	Quality           string              `json:"quality,omitempty"`
	Background        string              `json:"background,omitempty"`
	OutputFormat      string              `json:"output_format,omitempty"`
	Moderation        string              `json:"moderation,omitempty"`
	Style             string              `json:"style,omitempty"`
	OutputCompression *int                `json:"output_compression,omitempty"`
	InputImageMask    *imagesMaskImageURL `json:"input_image_mask,omitempty"`
	// 以下参数官方 Responses image_generation 工具同样接受（与 /v1/images/generations 同源），
	// 不映射就会在 chat→codex 转协议时静默丢参：SVG 走 output_format=svg + svg.model，
	// GIF 走 frame_count/frame_duration_ms。
	//
	// 注意：故意不含 complexity。Codex /responses 的托管 image_generation 绑定 gpt-image-2-codex，
	// 会以 "Complexity 'medium' is not supported for this image model" 拒绝该参数（error_type
	// image_generation_user_error），与 tools[].model 写的是 2.5 还是 2.6 无关。客户端传来时
	// 直接丢弃。白名单模型（codexDirectImageModels）不走这条路径，由直连函数直接下发复杂度。
	FrameCount      *int            `json:"frame_count,omitempty"`
	FrameDurationMs *int            `json:"frame_duration_ms,omitempty"`
	SVG             json.RawMessage `json:"svg,omitempty"`
}

type imagesMaskImageURL struct {
	ImageURL string `json:"image_url"`
}

// CreateImageGenerationsStream / CreateImageEditsStream：codex 图像走内部 Responses 工具协议，
// 嵌入的 OpenAIProvider 流式方法会把 images 格式请求体发到 /backend-api/codex/responses，
// 覆写返回哨兵让 relay 层降级为非流式 + 合成 SSE。
func (p *CodexProvider) CreateImageGenerationsStream(request *types.ImageRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	return nil, base.ImageStreamNotSupportedError()
}

func (p *CodexProvider) CreateImageEditsStream(request *types.ImageEditRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	return nil, base.ImageStreamNotSupportedError()
}

// CreateImageGenerations 走 /backend-api/codex/responses + image_generation 工具实现 /v1/images/generations。
func (p *CodexProvider) CreateImageGenerations(request *types.ImageRequest) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode) {
	prompt := strings.TrimSpace(request.Prompt)
	if prompt == "" {
		return nil, common.StringErrorWrapperLocal("prompt is required", "invalid_request_error", http.StatusBadRequest)
	}

	if isCodexDirectImageModel(request.Model) {
		return p.createImageGenerationsDirect(request)
	}

	body, err := json.Marshal(buildImagesRequestBody(imageToolActionGenerate, prompt, buildToolFromImageRequest(request), nil, "", p.ImagesMainModel))
	if err != nil {
		return nil, common.ErrorWrapperLocal(err, "build_request_failed", http.StatusBadRequest)
	}

	return p.executeImagesResponses(body, request.ResponseFormat, imagesUsageFallback{
		Model:   request.Model,
		Quality: request.Quality,
		Size:    request.Size,
	})
}

// codexDirectImageModels 是走直连原生 images 端点的模型白名单（与 CLIProxyAPI 一致）。
// 显式列举而非前缀匹配：未来新增的 gpt-image-* 未必在直连端点可用，自动送过去会 404，
// 而它们原本靠编排路径可能仍能工作；同理 gpt-image-1（上游已下线）也留在编排侧。
// chatgpt-image-* 不在白名单：它是别名入口，行为由上游决定，不走直连。
var codexDirectImageModels = map[string]struct{}{
	"gpt-image-1.5":                     {},
	"gpt-image-2":                       {},
	"gpt-image-2.5":                     {},
	"gpt-image-2.5-flare":               {},
	"gpt-image-2.5-sunburst":            {},
	"gpt-image-2.5-flare-2026-09-08":    {},
	"gpt-image-2.5-sunburst-2026-09-08": {},
	"gpt-image-2.6":                     {},
	"gpt-image-2.6-flare":               {},
	"gpt-image-2.6-sunburst":            {},
}

// isCodexDirectImageModel 判断图像模型是否该直连原生 images 端点，而不是绕 Responses 编排。
func isCodexDirectImageModel(model string) bool {
	_, ok := codexDirectImageModels[strings.ToLower(strings.TrimSpace(model))]
	return ok
}

// createImageGenerationsDirect 直连 /backend-api/codex/images/generations（非流式）。
// 请求体与 openai 渠道同构：整结构体序列化（含 extra_params 等非官方字段原样下发，
// 与 openai 渠道一致），本地方对参数完全可控，不存在编排模型二次选参。
func (p *CodexProvider) createImageGenerationsDirect(request *types.ImageRequest) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode) {
	body, err := buildDirectImagesRequestBody(request)
	if err != nil {
		return nil, common.ErrorWrapperLocal(err, "build_request_failed", http.StatusBadRequest)
	}

	return p.postDirectImages(codexDirectImagesGenerations, body, request.Model, request.Quality, request.Size)
}

// buildDirectImagesRequestBody 序列化直连 images 端点的请求体。
// 清 stream / partial_images 只是防御：relay 的流式降级路径（image-generations.go）在调用前
// 已把两者置 nil，正常不会带进来；但直连端点按 JSON 解析，一旦有调用方（如未来新增的测速入口）
// 直接塞 Stream=true，上游会回 SSE 导致 decode 失败，代价不值得省。
func buildDirectImagesRequestBody(request *types.ImageRequest) ([]byte, error) {
	sanitized := *request
	sanitized.Stream = nil
	sanitized.PartialImages = nil
	// response_format 不下发：gpt-image 直连端点固定返回 b64_json，url 形态在本地按
	// 客户端语义转换（与 sub2api 直连一致），原样发过去可能被上游判非法而 400。
	sanitized.ResponseFormat = ""
	return json.Marshal(&sanitized)
}

// directImageHeaders 构造直连 images 端点的请求头：复用 codex 的凭证与伪装头
// （Authorization / chatgpt-account-id / version / originator），Accept 改为 JSON。
func (p *CodexProvider) directImageHeaders() (map[string]string, *types.OpenAIErrorWithStatusCode) {
	headers, err := p.getRequestHeadersInternal()
	if err != nil {
		return nil, p.handleTokenError(err)
	}
	p.applyDefaultHeaders(headers)
	headers["Accept"] = "application/json"
	return headers, nil
}

// CreateImageEdits 走 /backend-api/codex/responses + image_generation（action=edit）实现 /v1/images/edits。
// multipart 上传的图片/mask 会被转成 base64 data URL 拼进 Responses 的 input/tools 字段。
func (p *CodexProvider) CreateImageEdits(request *types.ImageEditRequest) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode) {
	prompt := strings.TrimSpace(request.Prompt)
	if prompt == "" {
		return nil, common.StringErrorWrapperLocal("prompt is required", "invalid_request_error", http.StatusBadRequest)
	}

	if isCodexDirectImageModel(request.Model) {
		return p.createImageEditsDirect(request)
	}

	inputImages, errCode := collectEditInputImages(request)
	if errCode != nil {
		return nil, errCode
	}
	if len(inputImages) == 0 {
		return nil, common.StringErrorWrapperLocal("image is required", "invalid_request_error", http.StatusBadRequest)
	}

	maskURL := ""
	if request.Mask != nil {
		dataURL, err := fileHeaderToDataURL(request.Mask)
		if err != nil {
			return nil, common.ErrorWrapperLocal(err, "read_mask_failed", http.StatusBadRequest)
		}
		maskURL = dataURL
	}

	tool := buildToolFromImageEditRequest(request)

	body, err := json.Marshal(buildImagesRequestBody(imageToolActionEdit, prompt, tool, inputImages, maskURL, p.ImagesMainModel))
	if err != nil {
		return nil, common.ErrorWrapperLocal(err, "build_request_failed", http.StatusBadRequest)
	}

	return p.executeImagesResponses(body, request.ResponseFormat, imagesUsageFallback{
		Model:   request.Model,
		Quality: request.Quality,
		Size:    request.Size,
	})
}

// createImageEditsDirect 直连 /backend-api/codex/images/edits（非流式）。
// 与 generations 直连同理：绕开编排模型，参数由本地完全决定。上传文件转成 data URL，
// 与官方 JSON 形态的 edits（images[].image_url）一致；不做 multipart 原样转发，
// 因为需要改写 model 且本地已解析过文件句柄。
func (p *CodexProvider) createImageEditsDirect(request *types.ImageEditRequest) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode) {
	inputImages, errCode := collectEditInputImages(request)
	if errCode != nil {
		return nil, errCode
	}
	if len(inputImages) == 0 {
		return nil, common.StringErrorWrapperLocal("image is required", "invalid_request_error", http.StatusBadRequest)
	}

	maskURL := ""
	if request.Mask != nil {
		dataURL, err := fileHeaderToDataURL(request.Mask)
		if err != nil {
			return nil, common.ErrorWrapperLocal(err, "read_mask_failed", http.StatusBadRequest)
		}
		maskURL = dataURL
	}

	body, err := buildDirectImageEditsRequestBody(request, inputImages, maskURL)
	if err != nil {
		return nil, common.ErrorWrapperLocal(err, "build_request_failed", http.StatusBadRequest)
	}

	return p.postDirectImages(codexDirectImagesEdits, body, request.Model, request.Quality, request.Size)
}

// buildDirectImageEditsRequestBody 构造直连 edits 端点的 JSON 请求体。
// 只下发已建模的官方字段：multipart 原始表单里的未知键不在此转发，避免把客户端
// 任意字段塞给上游（与 openai 渠道的原样透传语义不同，这里是有意收窄）。
// 与 generations 直连的差异：那边整结构体序列化会带上 user / response_format，这里不带。
// gpt-image-* 固定返回 b64_json，response_format 无影响；user 是已弃用字段，故未补。
func buildDirectImageEditsRequestBody(request *types.ImageEditRequest, imageURLs []string, maskURL string) ([]byte, error) {
	type imageRef struct {
		ImageURL string `json:"image_url"`
	}
	payload := struct {
		Model             string          `json:"model"`
		Prompt            string          `json:"prompt"`
		N                 int             `json:"n,omitempty"`
		Size              string          `json:"size,omitempty"`
		Quality           string          `json:"quality,omitempty"`
		Background        *string         `json:"background,omitempty"`
		Moderation        *string         `json:"moderation,omitempty"`
		OutputFormat      *string         `json:"output_format,omitempty"`
		OutputCompression *int            `json:"output_compression,omitempty"`
		Complexity        *string         `json:"complexity,omitempty"`
		FrameCount        *int            `json:"frame_count,omitempty"`
		FrameDurationMs   *int            `json:"frame_duration_ms,omitempty"`
		SVG               json.RawMessage `json:"svg,omitempty"`
		Images            []imageRef      `json:"images"`
		Mask              *imageRef       `json:"mask,omitempty"`
	}{
		Model:             strings.TrimSpace(request.Model),
		Prompt:            strings.TrimSpace(request.Prompt),
		Size:              strings.TrimSpace(request.Size),
		Quality:           strings.TrimSpace(request.Quality),
		Background:        request.Background,
		Moderation:        request.Moderation,
		OutputFormat:      request.OutputFormat,
		OutputCompression: request.OutputCompression,
		Complexity:        request.Complexity,
		FrameCount:        request.FrameCount,
		FrameDurationMs:   request.FrameDurationMs,
		SVG:               normalizeSVGRawMessage(rawMessageFromFormValue(request.SVG)),
	}
	if shouldPassImagesN(request.Model, request.N) {
		payload.N = request.N
	}
	for _, url := range imageURLs {
		payload.Images = append(payload.Images, imageRef{ImageURL: url})
	}
	if maskURL != "" {
		payload.Mask = &imageRef{ImageURL: maskURL}
	}
	return json.Marshal(&payload)
}

// postDirectImages 汇总直连图像端点共用的传输样板：拼 URL、取头、建请求、发送。
// generations 与 edits 共用，避免两条直连路径各自维护一份而漂移。
func (p *CodexProvider) postDirectImages(path string, body []byte, model, quality, size string) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode) {
	fullRequestURL := p.GetFullRequestURL(path, "")
	headers, errCode := p.directImageHeaders()
	if errCode != nil {
		return nil, errCode
	}

	req, reqErr := p.Requester.NewRequest(http.MethodPost, fullRequestURL,
		p.Requester.WithBody(body),
		p.Requester.WithHeader(headers))
	if reqErr != nil {
		return nil, common.ErrorWrapper(reqErr, "new_request_failed", http.StatusInternalServerError)
	}
	defer req.Body.Close()

	return p.sendDirectImagesRequest(req, model, quality, size)
}

// sendDirectImagesRequest 发送直连图像请求并统一处理 usage 落库与漏返兜底。
// generations 与 edits 共用，避免两条直连路径的计费口径各写一份而漂移。
func (p *CodexProvider) sendDirectImagesRequest(req *http.Request, model, quality, size string) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode) {
	response := &openai.OpenAIProviderImageResponse{}
	_, errWithCode := p.Requester.SendRequest(req, response, false)
	// 直连端点的 usage 未必带 total_tokens（可能只给 input_tokens/output_tokens，
	// 见 sub2api 的同端夹具），而 ToOpenAIUsage 只搬运 total_tokens、不做推导。
	// 若不补齐，下面的兜底会以为上游没给 usage，按图估算覆盖掉真实值。
	if response.Usage != nil && response.Usage.TotalTokens == 0 {
		response.Usage.TotalTokens = response.Usage.InputTokens + response.Usage.OutputTokens
	}
	// 与 openai 渠道同条件：上游 200 + body 带 error 且含 usage 时也先落 usage，避免白嫖。
	if response.Usage != nil && response.Usage.TotalTokens > 0 {
		*p.Usage = *response.Usage.ToOpenAIUsage()
	}
	if errWithCode != nil {
		return nil, errWithCode
	}
	if openaiErr := openai.ErrorHandle(&response.OpenAIErrorResponse); openaiErr != nil {
		return nil, &types.OpenAIErrorWithStatusCode{
			OpenAIError: *openaiErr,
			StatusCode:  http.StatusBadRequest,
		}
	}

	if p.Usage.TotalTokens == 0 {
		imageCount := len(response.Data)
		resolvedQuality, resolvedSize := quality, size
		if v := openai.RawMessageToString(response.Quality); v != "" {
			resolvedQuality = v
		}
		if v := openai.RawMessageToString(response.Size); v != "" {
			resolvedSize = v
		}
		perImage := openai.ImageFallbackOutputTokens(model, resolvedQuality, resolvedSize)
		p.Usage.CompletionTokens = imageCount * perImage
		p.Usage.TotalTokens = p.Usage.PromptTokens + p.Usage.CompletionTokens
	}

	return &response.ImageResponse, nil
}

// buildToolFromImageEditRequest 把 /v1/images/edits 的字段平铺到 image_generation tool 参数上。
// 与 buildToolFromImageRequest 对称：两者入口协议不同（multipart / JSON），可选参数集合一致，
// 因此共用 applyImageOptionFields；入口差异仅在 svg 的承载形式（表单文本需转成 RawMessage）。
func buildToolFromImageEditRequest(request *types.ImageEditRequest) imageGenerateTool {
	tool := imageGenerateTool{Model: strings.TrimSpace(request.Model)}
	if shouldPassImagesN(request.Model, request.N) {
		n := request.N
		tool.N = &n
	}
	tool.Size = strings.TrimSpace(request.Size)
	tool.Quality = strings.TrimSpace(request.Quality)
	applyImageOptionFields(&tool, imageOptionFields{
		Background:        request.Background,
		OutputFormat:      request.OutputFormat,
		Moderation:        request.Moderation,
		OutputCompression: request.OutputCompression,
		FrameCount:        request.FrameCount,
		FrameDurationMs:   request.FrameDurationMs,
		SVG:               rawMessageFromFormValue(request.SVG),
	})
	return tool
}

// buildToolFromImageRequest 把 /v1/images/generations 的字段平铺到 image_generation tool 参数上。
func buildToolFromImageRequest(request *types.ImageRequest) imageGenerateTool {
	tool := imageGenerateTool{Model: strings.TrimSpace(request.Model)}
	if shouldPassImagesN(request.Model, request.N) {
		n := request.N
		tool.N = &n
	}
	tool.Size = strings.TrimSpace(request.Size)
	tool.Quality = strings.TrimSpace(request.Quality)
	tool.Style = strings.TrimSpace(request.Style)
	applyImageOptionFields(&tool, imageOptionFields{
		Background:        request.Background,
		OutputFormat:      request.OutputFormat,
		Moderation:        request.Moderation,
		OutputCompression: request.OutputCompression,
		FrameCount:        request.FrameCount,
		FrameDurationMs:   request.FrameDurationMs,
		SVG:               request.SVG,
	})
	return tool
}

// rawMessageFromFormValue 把 edits 表单里接住的 svg 文本转成 RawMessage；nil 保持 nil，
// 让结构体编码时的 omitempty 生效（空对象 {} 的语义仍由非空文本保留）。
func rawMessageFromFormValue(v *string) json.RawMessage {
	if v == nil {
		return nil
	}
	return json.RawMessage(*v)
}

// imageOptionFields 是 generations 与 edits 两条入口共有的可选参数集合。
// 官方 images 接口对两者暴露同样的字段，只有入口协议（JSON / multipart）不同；
// 抽成同一份映射避免两条路径各自维护导致其中一条静默丢参。
type imageOptionFields struct {
	Background        *string
	OutputFormat      *string
	Moderation        *string
	OutputCompression *int
	FrameCount        *int
	FrameDurationMs   *int
	SVG               json.RawMessage
}

func applyImageOptionFields(tool *imageGenerateTool, fields imageOptionFields) {
	if fields.Background != nil {
		tool.Background = strings.TrimSpace(*fields.Background)
	}
	if fields.OutputFormat != nil {
		tool.OutputFormat = strings.TrimSpace(*fields.OutputFormat)
	}
	if fields.Moderation != nil {
		tool.Moderation = strings.TrimSpace(*fields.Moderation)
	}
	if fields.OutputCompression != nil {
		tool.OutputCompression = fields.OutputCompression
	}
	tool.FrameCount = fields.FrameCount
	tool.FrameDurationMs = fields.FrameDurationMs
	tool.SVG = normalizeSVGRawMessage(fields.SVG)
}

// normalizeSVGRawMessage 统一 generations（JSON 请求体，svg 已是原始 JSON）与 edits
// （multipart 表单，svg 的值是带引号的 JSON 文本或直接是对象）两种来源的 svg 表示。
// 表单里传字符串时必须先解掉外层引号，否则上游会按 JSON 字符串而非对象解析并报缺 svg.model。
func normalizeSVGRawMessage(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] != '"' {
		return trimmed
	}
	var text string
	if err := json.Unmarshal(trimmed, &text); err != nil {
		return nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return json.RawMessage(text)
}

func buildImagesRequestBody(
	action string,
	prompt string,
	tool imageGenerateTool,
	inputImages []string,
	maskURL string,
	mainModel string,
) imagesRequestBody {
	tool.Type = "image_generation"
	tool.Action = action
	if maskURL != "" {
		tool.InputImageMask = &imagesMaskImageURL{ImageURL: maskURL}
	}

	content := make([]imagesInputContent, 0, 1+len(inputImages))
	content = append(content, imagesInputContent{Type: "input_text", Text: prompt})
	for _, imageURL := range inputImages {
		content = append(content, imagesInputContent{Type: "input_image", ImageURL: imageURL})
	}

	return imagesRequestBody{
		Instructions:      "",
		Stream:            true,
		Reasoning:         imagesReasoning{Effort: "medium", Summary: "auto"},
		ParallelToolCalls: true,
		Include:           []string{"reasoning.encrypted_content"},
		Model:             mainModel,
		Store:             false,
		ToolChoice:        imagesToolChoice{Type: "image_generation"},
		Input: []imagesInputItem{{
			Type:    "message",
			Role:    "user",
			Content: content,
		}},
		Tools: []imageGenerateTool{tool},
	}
}

// imagesUsageFallback 记录"上游漏返 usage 时按模型/quality/size 估算 output tokens"所需的入参。
// 完全对齐 providers/openai/image_generations.go 的兜底口径，让 codex 渠道与 openai 渠道的计费精度一致。
type imagesUsageFallback struct {
	Model   string
	Quality string
	Size    string
}

// 发送请求并解析 SSE 流，把 image_generation_call 的 base64 结果汇总为标准 OpenAI ImageResponse。
func (p *CodexProvider) executeImagesResponses(body []byte, responseFormat string, fallback imagesUsageFallback) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode) {
	fullRequestURL := p.GetFullRequestURL(p.Config.ImagesGenerations, "")

	headers, err := p.getRequestHeadersInternal()
	if err != nil {
		return nil, p.handleTokenError(err)
	}
	p.applyDefaultHeaders(headers)
	headers["Accept"] = "text/event-stream"

	req, reqErr := p.Requester.NewRequest(http.MethodPost, fullRequestURL,
		p.Requester.WithBody(body),
		p.Requester.WithHeader(headers))
	if reqErr != nil {
		return nil, common.ErrorWrapper(reqErr, "new_request_failed", http.StatusInternalServerError)
	}
	defer req.Body.Close()

	resp, errWithCode := p.Requester.SendRequestRaw(req)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer resp.Body.Close()

	return p.parseImagesStream(resp.Body, responseFormat, fallback)
}

// currentAccessToken 返回当前凭证里的 access token，用于 SSE 错误事件脱敏。
// 没有凭证（纯文本 key 模式）时返回空串；调用方必须配合 accessToken != "" 守门再做 Replace，
// 因为 strings.Replace(s, "", repl, n) 会在每个字符间插入 repl，并不是 no-op。
func (p *CodexProvider) currentAccessToken() string {
	if p.Credentials == nil {
		return ""
	}
	return p.Credentials.AccessToken
}

// 逐行读 SSE 数据行，识别 image_generation_call 结果和 usage，容忍乱序 / 分包。
func (p *CodexProvider) parseImagesStream(body io.Reader, responseFormat string, fallback imagesUsageFallback) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode) {
	reader := bufio.NewReader(body)

	var (
		items     []types.ImageResponseDataInner
		seen      = make(map[string]struct{})
		usage     *types.ResponsesUsage
		createdAt int64
	)

	format := strings.ToLower(strings.TrimSpace(responseFormat))
	if format == "" {
		format = "b64_json"
	}
	accessToken := p.currentAccessToken()

	for {
		line, readErr := reader.ReadBytes('\n')
		if data := strings.TrimSpace(string(line)); strings.HasPrefix(data, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(data, "data:"))
			if payload != "" && payload != "[DONE]" {
				if errCode := handleImagesStreamEvent(payload, format, accessToken, &items, seen, &usage, &createdAt); errCode != nil {
					return nil, errCode
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, common.ErrorWrapper(readErr, "stream_read_failed", http.StatusInternalServerError)
		}
	}

	if len(items) == 0 {
		// 上游 200 但没回 image_generation_call，属于上游异常，走非 Local 让 relay 重试/熔断链路能识别。
		return nil, common.StringErrorWrapper("upstream did not return any image output", "no_image_output", http.StatusBadGateway)
	}

	// 仅当上游真给了非空 usage 时才覆盖，避免把 RelayHandler 预填的本地 PromptTokens 零化。
	// 与 providers/openai/image_generations.go 同条件，保持渠道间口径一致。
	if usage != nil && usage.TotalTokens > 0 {
		*p.Usage = *usage.ToOpenAIUsage()
	}
	if p.Usage.TotalTokens == 0 {
		// 与 openai/image_generations.go 共用同一兜底口径，避免三处拷贝靠注释维系而漂移。
		perImage := openai.ImageFallbackOutputTokens(fallback.Model, fallback.Quality, fallback.Size)
		p.Usage.CompletionTokens = len(items) * perImage
		p.Usage.TotalTokens = p.Usage.PromptTokens + p.Usage.CompletionTokens
	}

	response := &types.ImageResponse{
		Data:  items,
		Usage: usage,
	}
	if createdAt > 0 {
		response.Created = createdAt
	}
	return response, nil
}

// 解析单个 SSE data 事件；命中 image_generation_call 时累计结果，命中 error / response.failed 直接返回错误。
// accessToken 用于在错误 message 里把可能回带的明文 token 替换成 xxxxx，与 base.go:RequestErrorHandle 行为对齐。
func handleImagesStreamEvent(
	payload string,
	format string,
	accessToken string,
	items *[]types.ImageResponseDataInner,
	seen map[string]struct{},
	usage **types.ResponsesUsage,
	createdAt *int64,
) *types.OpenAIErrorWithStatusCode {
	var event struct {
		Type     string `json:"type"`
		Response *struct {
			CreatedAt int64                 `json:"created_at"`
			Usage     *types.ResponsesUsage `json:"usage"`
			Output    []imageOutputItem     `json:"output"`
			Error     *imageStreamError     `json:"error"`
		} `json:"response"`
		Item  *imageOutputItem  `json:"item"`
		Error *imageStreamError `json:"error"`
	}
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return nil
	}

	switch event.Type {
	case "error":
		return imageStreamErrorToOpenAI(event.Error, accessToken)
	case "response.failed":
		if event.Response != nil {
			return imageStreamErrorToOpenAI(event.Response.Error, accessToken)
		}
		return common.StringErrorWrapper("upstream response failed", "upstream_error", http.StatusBadGateway)
	case "response.output_item.done":
		if event.Item != nil {
			appendImageResultItem(items, seen, *event.Item, format)
		}
	case "response.completed":
		if event.Response == nil {
			return nil
		}
		if event.Response.CreatedAt > 0 {
			*createdAt = event.Response.CreatedAt
		}
		if event.Response.Usage != nil {
			*usage = event.Response.Usage
		}
		for _, item := range event.Response.Output {
			appendImageResultItem(items, seen, item, format)
		}
	}
	return nil
}

type imageOutputItem struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	Result        string `json:"result"`
	RevisedPrompt string `json:"revised_prompt"`
	OutputFormat  string `json:"output_format"`
}

type imageStreamError struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Param   string `json:"param"`
}

// 把单条 image_generation_call 结果按 response_format 转成 OpenAI ImageResponse.data 项。
// 同一张图会被 response.output_item.done 与 response.completed 各回一次，按 output_format|result 去重
// （空 result 在前面已经被滤掉，不需要 id 兜底）。
func appendImageResultItem(items *[]types.ImageResponseDataInner, seen map[string]struct{}, item imageOutputItem, format string) {
	if item.Type != "image_generation_call" {
		return
	}
	result := strings.TrimSpace(item.Result)
	if result == "" {
		return
	}
	key := strings.TrimSpace(item.OutputFormat) + "|" + result
	if _, exists := seen[key]; exists {
		return
	}
	seen[key] = struct{}{}

	entry := types.ImageResponseDataInner{RevisedPrompt: strings.TrimSpace(item.RevisedPrompt)}
	if format == "url" {
		entry.URL = "data:" + imageMIMEType(item.OutputFormat) + ";base64," + result
	} else {
		entry.B64JSON = result
	}
	*items = append(*items, entry)
}

func imageStreamErrorToOpenAI(streamErr *imageStreamError, accessToken string) *types.OpenAIErrorWithStatusCode {
	if streamErr == nil {
		return common.StringErrorWrapper("upstream image generation failed", "upstream_error", http.StatusBadGateway)
	}
	status := http.StatusBadGateway
	if strings.EqualFold(streamErr.Code, "moderation_blocked") || strings.EqualFold(streamErr.Type, "image_generation_user_error") {
		status = http.StatusBadRequest
	}
	message := strings.TrimSpace(streamErr.Message)
	if message == "" {
		message = "upstream image generation failed"
	}
	if accessToken != "" {
		message = strings.ReplaceAll(message, accessToken, "xxxxx")
	}
	return &types.OpenAIErrorWithStatusCode{
		StatusCode: status,
		OpenAIError: types.OpenAIError{
			Message: message,
			Type:    strings.TrimSpace(streamErr.Type),
			Code:    strings.TrimSpace(streamErr.Code),
			Param:   strings.TrimSpace(streamErr.Param),
		},
	}
}

// imageMIMEType 把请求的 output_format 映射成 data URL 的 MIME。
// svg / gif 是 images 接口的正式枚举（SVG 与动图输出），漏映射会让客户端把
// SVG/GIF 结果按 image/png 解码从而渲染失败。
func imageMIMEType(outputFormat string) string {
	switch strings.ToLower(strings.TrimSpace(outputFormat)) {
	case "", "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	case "gif":
		return "image/gif"
	case "svg":
		return "image/svg+xml"
	default:
		return "image/png"
	}
}

// dall-e-3 不支持 n>1，其它模型在用户显式 n>1 时传给上游。
func shouldPassImagesN(model string, n int) bool {
	if n <= 1 {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(model), "dall-e-3")
}

func collectEditInputImages(request *types.ImageEditRequest) ([]string, *types.OpenAIErrorWithStatusCode) {
	headers := make([]*multipart.FileHeader, 0, 1+len(request.Images))
	if request.Image != nil {
		headers = append(headers, request.Image)
	}
	headers = append(headers, request.Images...)

	dataURLs := make([]string, 0, len(headers))
	for _, header := range headers {
		dataURL, err := fileHeaderToDataURL(header)
		if err != nil {
			return nil, common.ErrorWrapperLocal(err, "read_image_failed", http.StatusBadRequest)
		}
		dataURLs = append(dataURLs, dataURL)
	}
	return dataURLs, nil
}

func fileHeaderToDataURL(header *multipart.FileHeader) (string, error) {
	if header == nil {
		return "", fmt.Errorf("file header is nil")
	}
	file, err := header.Open()
	if err != nil {
		return "", fmt.Errorf("open upload %q: %w", header.Filename, err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("read upload %q: %w", header.Filename, err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("upload %q is empty", header.Filename)
	}

	contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		return "", fmt.Errorf("upload %q is not an image (content-type %q)", header.Filename, contentType)
	}

	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}
