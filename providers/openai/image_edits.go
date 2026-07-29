package openai

import (
	"bytes"
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/requester"
	"done-hub/providers/base"
	"done-hub/types"
	"fmt"
	"io"
	"net/http"
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
		// 等其他维持 258 常数。ImageEditRequest 无 quality 字段，故档位主要依赖响应回显，size
		// 回显缺失时回落请求值。
		imageCount := len(response.Data)
		quality := rawMessageToString(response.Quality)
		size := request.Size
		if v := rawMessageToString(response.Size); v != "" {
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
	// 创建请求
	var req *http.Request
	var err error
	if p.OriginalModel != request.Model {
		var formBody bytes.Buffer
		builder := p.Requester.CreateFormBuilder(&formBody)
		if err := imagesEditsMultipartForm(request, builder); err != nil {
			return nil, common.ErrorWrapper(err, "create_form_builder_failed", http.StatusInternalServerError)
		}
		req, err = p.Requester.NewRequest(
			http.MethodPost,
			fullRequestURL,
			p.Requester.WithBody(&formBody),
			p.Requester.WithHeader(headers),
			p.Requester.WithContentType(builder.FormDataContentType()))
		req.ContentLength = int64(formBody.Len())
	} else {
		body, exists := p.GetRawBody()
		if !exists {
			return nil, common.StringErrorWrapperLocal("request body not found", "request_body_not_found", http.StatusInternalServerError)
		}
		req, err = p.Requester.NewRequest(
			http.MethodPost,
			fullRequestURL,
			p.Requester.WithBody(body),
			p.Requester.WithHeader(headers),
			p.Requester.WithContentType(p.Context.Request.Header.Get("Content-Type")))
		req.ContentLength = p.Context.Request.ContentLength
	}

	if err != nil {
		return nil, common.ErrorWrapper(err, "new_request_failed", http.StatusInternalServerError)
	}

	return req, nil
}

func imagesEditsMultipartForm(request *types.ImageEditRequest, b requester.FormBuilder) (err error) {
	if request.Image != nil {
		err = b.CreateFormFile("image", request.Image)
		if err != nil {
			return fmt.Errorf("creating form image: %w", err)
		}
	}

	if request.Images != nil {
		for _, image := range request.Images {
			err = b.CreateFormFile("image[]", image)
			if err != nil {
				return fmt.Errorf("creating form images: %w", err)
			}
		}
	}

	err = b.WriteField("prompt", request.Prompt)
	if err != nil {
		return fmt.Errorf("writing prompt: %w", err)
	}

	err = b.WriteField("model", request.Model)
	if err != nil {
		return fmt.Errorf("writing model name: %w", err)
	}

	if request.Mask != nil {
		err = b.CreateFormFile("mask", request.Mask)
		if err != nil {
			return fmt.Errorf("writing mask: %w", err)
		}
	}

	if request.ResponseFormat != "" {
		err = b.WriteField("response_format", request.ResponseFormat)
		if err != nil {
			return fmt.Errorf("writing format: %w", err)
		}
	}

	if request.N != 0 {
		err = b.WriteField("n", fmt.Sprintf("%d", request.N))
		if err != nil {
			return fmt.Errorf("writing n: %w", err)
		}
	}

	if request.Size != "" {
		err = b.WriteField("size", request.Size)
		if err != nil {
			return fmt.Errorf("writing size: %w", err)
		}
	}

	if request.User != "" {
		err = b.WriteField("user", request.User)
		if err != nil {
			return fmt.Errorf("writing user: %w", err)
		}
	}

	return b.Close()
}
