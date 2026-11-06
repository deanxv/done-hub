package openai

import (
	"bytes"
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/providers/base"
	"done-hub/types"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"

	"github.com/tidwall/sjson"
)

func (p *OpenAIProvider) CreateImageEdits(request *types.ImageEditRequest) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getRequestImageBody(config.RelayModeImagesEdits, request.Model, request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	response := &OpenAIProviderImageResponse{}
	// 开启渠道 PassThroughBody 且 relay 层已放行（入口协议 == image edits、响应原样直返）时，
	// 用 outputResp=true 让 SendRequest 回填 resp.Body：既 unmarshal 一份供计费，又能拿到上游
	// 原始字节用于响应字节透传，保留 output_tokens_details.image_tokens/text_tokens 等未知字段。
	passThrough := p.Channel.PassThroughBody && p.Context != nil && p.Context.GetBool(config.GinRawPassThroughAllowedKey)
	// 发送请求
	resp, errWithCode := p.Requester.SendRequest(req, response, passThrough)
	if passThrough && resp != nil {
		defer resp.Body.Close()
	}

	// 即便后续判错也先落 usage：覆盖"HTTP 200 + body 带 error 字段 + 仍含 usage"这种聚合上游场景。
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
		// 上游漏返 usage 兜底：与 generations 对齐——gpt-image-* 走 OpenAI 官方 quality+size
		// 公式，避免恒定 258 对 gpt-image edits（单图 1056~6240）低估最多 24 倍的白嫖；dall-e
		// 等其他维持 258 常数。优先采用响应回显的真实渲染档位，回显缺失再回落请求值。
		imageCount := len(response.Data)
		quality, size := request.Quality, request.Size
		if v := RawMessageToString(response.Quality); v != "" {
			quality = v
		}
		if v := RawMessageToString(response.Size); v != "" {
			size = v
		}
		perImage := ImageFallbackOutputTokens(request.Model, quality, size)
		p.Usage.CompletionTokens = imageCount * perImage
		p.Usage.TotalTokens = p.Usage.PromptTokens + p.Usage.CompletionTokens
	}

	// 暂存上游原始字节，由 relay 层字节透传，保留未知字段 / 字段顺序。
	// 有别名映射需改 model 时，在原始字节上就地 sjson 改写顶层 model；无映射时恒 no-op。
	// images 官方响应无顶层 model 字段，此处仅兜聚合上游额外回显 model 的情形。
	// 必须放在 ErrorHandle 之后：出错时若落键，错误 body 会残留在同一 gin.Context 上
	// （relay/main.go 的 defer 不清理此 key），被后续重试成功但不落键的渠道经
	// writeRawResponseBodyIfPresent 当 200 直返给客户端。
	if passThrough && resp != nil {
		if rawBytes, readErr := io.ReadAll(resp.Body); readErr == nil && len(rawBytes) > 0 {
			if patched, changed := base.UnifyModelInJSONBytes(p.Context, rawBytes, "model"); changed {
				rawBytes = patched
			}
			p.Context.Set(config.GinRawResponseBodyKey, rawBytes)
		}
	}

	return &response.ImageResponse, nil
}

func (p *OpenAIProvider) getRequestImageBody(relayMode int, ModelName string, request *types.ImageEditRequest) (*http.Request, *types.OpenAIErrorWithStatusCode) {
	url, errWithCode := p.GetSupportedAPIUri(relayMode)
	if errWithCode != nil {
		return nil, errWithCode
	}
	// 获取请求地址
	fullRequestURL := p.GetFullRequestURL(url, ModelName)

	// 获取请求头
	headers := p.GetRequestHeaders()

	body, exists := p.GetRawBody()
	if !exists {
		return nil, common.StringErrorWrapperLocal("request body not found", "request_body_not_found", http.StatusInternalServerError)
	}
	contentType := p.Context.Request.Header.Get("Content-Type")

	// 模型映射时在原始字节上仅替换 model：multipart 逐 part 重写，JSON（聚合上游契约）走
	// sjson 单键改写。background/quality/output_format 等现有及未来新增字段全部原样保留，
	// 避免按已知字段重建表单造成静默丢参。
	if p.OriginalModel != request.Model {
		var err error
		if mediaType, _, _ := mime.ParseMediaType(contentType); mediaType == "multipart/form-data" {
			body, contentType, err = rewriteMultipartModel(body, contentType, request.Model)
		} else {
			body, err = sjson.SetBytes(body, "model", request.Model)
		}
		if err != nil {
			return nil, common.ErrorWrapper(err, "rewrite_model_failed", http.StatusInternalServerError)
		}
	}

	req, err := p.Requester.NewRequest(
		http.MethodPost,
		fullRequestURL,
		p.Requester.WithBody(body),
		p.Requester.WithHeader(headers),
		p.Requester.WithContentType(contentType))
	if err != nil {
		return nil, common.ErrorWrapper(err, "new_request_failed", http.StatusInternalServerError)
	}
	req.ContentLength = int64(len(body))

	return req, nil
}

// rewriteMultipartModel 逐 part 复制 multipart 表单，仅把 model 字段的值替换为映射后的模型名，
// 其余 part（含文件）头部与字节原样保留。返回重写后的 body 与携带新 boundary 的 Content-Type。
func rewriteMultipartModel(body []byte, contentType, model string) ([]byte, string, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return nil, "", fmt.Errorf("invalid multipart content type %q: %w", contentType, err)
	}

	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	modelWritten := false

	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("reading multipart part: %w", err)
		}

		target, err := writer.CreatePart(part.Header)
		if err != nil {
			return nil, "", fmt.Errorf("creating multipart part: %w", err)
		}
		if part.FormName() == "model" && part.FileName() == "" {
			if _, err := io.WriteString(target, model); err != nil {
				return nil, "", fmt.Errorf("writing model name: %w", err)
			}
			modelWritten = true
			continue
		}
		if _, err := io.Copy(target, part); err != nil {
			return nil, "", fmt.Errorf("copying multipart part: %w", err)
		}
	}

	if !modelWritten {
		if err := writer.WriteField("model", model); err != nil {
			return nil, "", fmt.Errorf("writing model name: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("closing multipart writer: %w", err)
	}

	return buf.Bytes(), writer.FormDataContentType(), nil
}
