package xAI

import (
	"done-hub/common"
	"done-hub/common/config"
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

// xAI 的 /v1/images/edits 只接受 application/json，且不支持 quality/size/style/mask，
// 与 OpenAI 的 multipart 契约完全不同，因此重写 CreateImageEdits 而非沿用内嵌的 OpenAIProvider 版本。
// 参考 xAI 官方文档：https://docs.x.ai/developers/model-capabilities/images/editing

type xaiImageURL struct {
	URL  string `json:"url"`
	Type string `json:"type"`
}

type xaiImageEditRequest struct {
	Model          string       `json:"model"`
	Prompt         string       `json:"prompt"`
	Image          *xaiImageURL `json:"image,omitempty"`
	ImageURLs      []string     `json:"image_urls,omitempty"`
	N              int          `json:"n,omitempty"`
	ResponseFormat string       `json:"response_format,omitempty"`
}

func (p *XAIProvider) CreateImageEdits(request *types.ImageEditRequest) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode) {
	if request.Prompt == "" {
		return nil, common.StringErrorWrapperLocal("prompt is required", "invalid_request_error", http.StatusBadRequest)
	}

	url, errWithCode := p.GetSupportedAPIUri(config.RelayModeImagesEdits)
	if errWithCode != nil {
		return nil, errWithCode
	}
	fullRequestURL := p.GetFullRequestURL(url, request.Model)
	headers := p.GetRequestHeaders()

	var body any
	// 客户端已按 xAI 的 JSON 契约发送时原样透传；OpenAI 标准 multipart 上传则转成 xAI JSON。
	if strings.Contains(p.Context.ContentType(), "json") {
		rawBody, exists := p.GetRawBody()
		if !exists {
			return nil, common.StringErrorWrapperLocal("request body not found", "request_body_not_found", http.StatusInternalServerError)
		}
		body = rawBody
	} else {
		jsonBody, errWithCode := buildXAIImageEditBody(request)
		if errWithCode != nil {
			return nil, errWithCode
		}
		body = jsonBody
	}

	req, err := p.Requester.NewRequest(
		http.MethodPost,
		fullRequestURL,
		p.Requester.WithBody(body),
		p.Requester.WithHeader(headers),
		p.Requester.WithContentType("application/json"))
	if err != nil {
		return nil, common.ErrorWrapper(err, "new_request_failed", http.StatusInternalServerError)
	}
	defer req.Body.Close()

	response := &openai.OpenAIProviderImageResponse{}
	_, errWithCode = p.Requester.SendRequest(req, response, false)

	// 即便后续判错也先落 usage：覆盖"HTTP 200 + body 带 error 字段 + 仍含 usage"这种聚合上游场景。
	if response.Usage != nil && response.Usage.TotalTokens > 0 {
		*p.Usage = *response.Usage.ToOpenAIUsage()
	}

	if errWithCode != nil {
		return nil, errWithCode
	}

	openaiErr := openai.ErrorHandle(&response.OpenAIErrorResponse)
	if openaiErr != nil {
		return nil, &types.OpenAIErrorWithStatusCode{
			OpenAIError: *openaiErr,
			StatusCode:  http.StatusBadRequest,
		}
	}

	if p.Usage.TotalTokens == 0 {
		// 上游未返回 usage，按生成图像数量兜底，避免空回复计费（grok-imagine 非 gpt-image，沿用 258 常数）。
		p.Usage.CompletionTokens = len(response.Data) * 258
		p.Usage.TotalTokens = p.Usage.PromptTokens + p.Usage.CompletionTokens
	}

	return &response.ImageResponse, nil
}

// buildXAIImageEditBody 把 OpenAI multipart 上传的图片转成 base64 data URL，组装成 xAI JSON。
// 单图走 image 对象，多图走 image_urls 数组（xAI 最多支持 3 张）。
func buildXAIImageEditBody(request *types.ImageEditRequest) ([]byte, *types.OpenAIErrorWithStatusCode) {
	headers := make([]*multipart.FileHeader, 0, 1+len(request.Images))
	if request.Image != nil {
		headers = append(headers, request.Image)
	}
	headers = append(headers, request.Images...)
	if len(headers) == 0 {
		return nil, common.StringErrorWrapperLocal("image is required", "invalid_request_error", http.StatusBadRequest)
	}

	dataURLs := make([]string, 0, len(headers))
	for _, header := range headers {
		dataURL, err := fileHeaderToDataURL(header)
		if err != nil {
			return nil, common.ErrorWrapperLocal(err, "read_image_failed", http.StatusBadRequest)
		}
		dataURLs = append(dataURLs, dataURL)
	}

	body := xaiImageEditRequest{
		Model:          request.Model,
		Prompt:         request.Prompt,
		N:              request.N,
		ResponseFormat: request.ResponseFormat,
	}
	if len(dataURLs) == 1 {
		body.Image = &xaiImageURL{URL: dataURLs[0], Type: "image_url"}
	} else {
		body.ImageURLs = dataURLs
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, common.ErrorWrapperLocal(err, "build_request_failed", http.StatusBadRequest)
	}
	return jsonBody, nil
}

func fileHeaderToDataURL(header *multipart.FileHeader) (string, error) {
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
