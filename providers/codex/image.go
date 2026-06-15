package codex

import (
	"bufio"
	"done-hub/common"
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

// Codex 内部 Responses + image_generation 工具的编排模型，硬编码为 ChatGPT 内部
// 专门承担"决定调用 image_generation 工具并选参"的轻量模型；真正的图像模型在 tools[0].model 指定。
const (
	imagesResponsesMainModel = "gpt-5.4-mini"
	imageToolActionGenerate  = "generate"
	imageToolActionEdit      = "edit"
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
}

type imagesMaskImageURL struct {
	ImageURL string `json:"image_url"`
}

// CreateImageGenerations 走 /backend-api/codex/responses + image_generation 工具实现 /v1/images/generations。
func (p *CodexProvider) CreateImageGenerations(request *types.ImageRequest) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode) {
	prompt := strings.TrimSpace(request.Prompt)
	if prompt == "" {
		return nil, common.StringErrorWrapperLocal("prompt is required", "invalid_request_error", http.StatusBadRequest)
	}

	body, err := json.Marshal(buildImagesRequestBody(imageToolActionGenerate, prompt, buildToolFromImageRequest(request), nil, ""))
	if err != nil {
		return nil, common.ErrorWrapperLocal(err, "build_request_failed", http.StatusBadRequest)
	}

	return p.executeImagesResponses(body, request.ResponseFormat, imagesUsageFallback{
		Model:   request.Model,
		Quality: request.Quality,
		Size:    request.Size,
	})
}

// CreateImageEdits 走 /backend-api/codex/responses + image_generation（action=edit）实现 /v1/images/edits。
// multipart 上传的图片/mask 会被转成 base64 data URL 拼进 Responses 的 input/tools 字段。
func (p *CodexProvider) CreateImageEdits(request *types.ImageEditRequest) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode) {
	prompt := strings.TrimSpace(request.Prompt)
	if prompt == "" {
		return nil, common.StringErrorWrapperLocal("prompt is required", "invalid_request_error", http.StatusBadRequest)
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

	tool := imageGenerateTool{Model: strings.TrimSpace(request.Model)}
	if shouldPassImagesN(request.Model, request.N) {
		n := request.N
		tool.N = &n
	}
	tool.Size = strings.TrimSpace(request.Size)

	body, err := json.Marshal(buildImagesRequestBody(imageToolActionEdit, prompt, tool, inputImages, maskURL))
	if err != nil {
		return nil, common.ErrorWrapperLocal(err, "build_request_failed", http.StatusBadRequest)
	}

	return p.executeImagesResponses(body, request.ResponseFormat, imagesUsageFallback{
		Model: request.Model,
		Size:  request.Size,
	})
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
	if request.Background != nil {
		tool.Background = strings.TrimSpace(*request.Background)
	}
	if request.OutputFormat != nil {
		tool.OutputFormat = strings.TrimSpace(*request.OutputFormat)
	}
	if request.Moderation != nil {
		tool.Moderation = strings.TrimSpace(*request.Moderation)
	}
	tool.Style = strings.TrimSpace(request.Style)
	if request.OutputCompression != nil {
		tool.OutputCompression = request.OutputCompression
	}
	return tool
}

func buildImagesRequestBody(
	action string,
	prompt string,
	tool imageGenerateTool,
	inputImages []string,
	maskURL string,
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
		Model:             imagesResponsesMainModel,
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
		perImage := 258
		if openai.IsGPTImageModel(fallback.Model) {
			perImage = openai.GPTImageOutputTokens(fallback.Quality, fallback.Size)
		}
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

func imageMIMEType(outputFormat string) string {
	switch strings.ToLower(strings.TrimSpace(outputFormat)) {
	case "", "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
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
