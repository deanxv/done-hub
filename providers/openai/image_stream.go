package openai

import (
	"bytes"
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/requester"
	"done-hub/types"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/bytedance/gopkg/util/gopool"
)

// 官方 images SSE 事件名前缀（generations / edits），真实上游事件与降级合成事件共用。
const (
	ImageGenerationStreamPrefix = "image_generation"
	ImageEditStreamPrefix       = "image_edit"
)

// imageStreamEvent 上游 images SSE 事件里本地需要读取的字段。b64_json 刻意不定义：
// partial 帧的 base64 可达 MB 级，只按原始字节转发，不落结构体、绝不写入 usage.TextBuilder
// （relay 层会对 TextBuilder 做 tokenize 兜底，MB 级 base64 会被当文本反算）。
// quality/size 与 ImageResponse 同理用 RawMessage，防聚合上游返数字导致解析失败。
type imageStreamEvent struct {
	types.OpenAIErrorResponse
	Type    string                `json:"type"`
	Quality json.RawMessage       `json:"quality"`
	Size    json.RawMessage       `json:"size"`
	Usage   *types.ResponsesUsage `json:"usage"`
}

// imagePartialFrameTokens 每个 partial 帧的官方计费增量（100 image output tokens，
// 来源 https://platform.openai.com/docs/guides/image-generation）。仅入兜底账本。
const imagePartialFrameTokens = 100

type OpenAIImageStreamHandler struct {
	Usage      *types.Usage
	Model      string
	ReqQuality string
	ReqSize    string

	// SSE 事件缓冲：event 行与 data 行合并成完整事件块后一次性下发
	eventBuffer strings.Builder

	// 双账本，绝不相加混算：reportedUsage 记录上游最后一次有效 usage——官方口径 usage 是
	// 整次生成的累计值（total tokens used for the image generation），后到覆盖先到，与非流式
	// response.usage 语义一致；fallbackTokens 按官方公式逐帧累加（completed 按 quality+size
	// 查表、partial 每帧 +100）。h.Usage 每帧刷新为「有上游 usage 则整取 reported，否则整取
	// fallback」，防止 n>1 时部分事件带 usage、部分不带导致两种口径叠加。
	hasReportedUsage bool
	reportedUsage    types.Usage
	fallbackTokens   int
}

// settleUsage 按当前账本刷新 h.Usage。每帧调用而非只在流末：客户端断开后 relay 会继续
// drain 到上游收尾，但流被 Close 等异常截断时，已收帧的读数也要能落账。
func (h *OpenAIImageStreamHandler) settleUsage() {
	if h.hasReportedUsage {
		*h.Usage = h.reportedUsage
		return
	}
	h.Usage.CompletionTokens = h.fallbackTokens
	h.Usage.TotalTokens = h.Usage.PromptTokens + h.Usage.CompletionTokens
}

func (h *OpenAIImageStreamHandler) HandlerImageStream(rawLine *[]byte, dataChan chan string, errChan chan error) {
	rawStr := string(*rawLine)

	// event 行：缓冲，等 data 行合并成完整事件块（NoTrim 流保留行尾换行，原样拼接）
	if strings.HasPrefix(rawStr, "event: ") {
		h.eventBuffer.Reset()
		h.eventBuffer.WriteString(rawStr)
		return
	}

	// 非 data 行（空行 / 注释等）：有未完成事件则并入缓冲，否则原样转发（空行是事件分隔符）
	if !strings.HasPrefix(rawStr, "data: ") {
		if h.eventBuffer.Len() > 0 {
			h.eventBuffer.WriteString(rawStr)
		} else {
			dataChan <- rawStr
		}
		return
	}

	data := bytes.TrimSpace((*rawLine)[6:])

	// 官方 images SSE 靠连接关闭收尾、不发 [DONE]，这里兜聚合上游补发的情形
	if string(data) == "[DONE]" {
		errChan <- io.EOF
		*rawLine = requester.StreamClosed
		return
	}

	var event imageStreamEvent
	if err := json.Unmarshal(data, &event); err != nil {
		errChan <- common.ErrorToOpenAIError(err)
		return
	}

	if openaiErr := ErrorHandle(&event.OpenAIErrorResponse); openaiErr != nil {
		errChan <- openaiErr
		return
	}

	// 任意事件带有效 usage 都记录（官方只在 completed 挂、且是整次生成的累计值，后到覆盖先到）；
	// 兜底账本按帧累加：completed 按官方 quality+size 公式计一张，partial 每帧 +100
	if event.Usage != nil && event.Usage.TotalTokens > 0 {
		h.hasReportedUsage = true
		h.reportedUsage = *event.Usage.ToOpenAIUsage()
	}
	if strings.HasSuffix(event.Type, ".completed") {
		quality, size := h.ReqQuality, h.ReqSize
		if v := rawMessageToString(event.Quality); v != "" {
			quality = v
		}
		if v := rawMessageToString(event.Size); v != "" {
			size = v
		}
		h.fallbackTokens += ImageFallbackOutputTokens(h.Model, quality, size)
	} else if strings.HasSuffix(event.Type, ".partial_image") {
		h.fallbackTokens += imagePartialFrameTokens
	}
	h.settleUsage()

	h.eventBuffer.WriteString(rawStr)
	dataChan <- h.eventBuffer.String()
	h.eventBuffer.Reset()
}

// imageStreamCompletedEvent 合成 completed 事件的字段集，对齐官方事件结构。
// b64 放最后：万一传输截断，前面的元数据（usage/档位）仍完整可读。
type imageStreamCompletedEvent struct {
	Type          string                `json:"type"`
	CreatedAt     any                   `json:"created_at,omitempty"`
	Background    json.RawMessage       `json:"background,omitempty"`
	OutputFormat  json.RawMessage       `json:"output_format,omitempty"`
	Quality       json.RawMessage       `json:"quality,omitempty"`
	Size          json.RawMessage       `json:"size,omitempty"`
	Usage         *types.ResponsesUsage `json:"usage,omitempty"`
	URL           string                `json:"url,omitempty"`
	RevisedPrompt string                `json:"revised_prompt,omitempty"`
	B64JSON       string                `json:"b64_json,omitempty"`
}

// ImageResponseToSSE 把非流式图像响应合成官方风格的 *.completed SSE 事件文本：
// 每张图一个事件，usage 只挂最后一个事件，避免客户端把同一份读数累加多次。
// 两处降级共用：渠道不支持流式（relay 层回落非流式）与上游无视 stream 返回 JSON。
func ImageResponseToSSE(prefix string, response *types.ImageResponse) string {
	eventType := prefix + ".completed"
	var sb strings.Builder
	for i, item := range response.Data {
		event := imageStreamCompletedEvent{
			Type:          eventType,
			CreatedAt:     response.Created,
			Background:    response.Background,
			OutputFormat:  response.OutputFormat,
			Quality:       response.Quality,
			Size:          response.Size,
			URL:           item.URL,
			RevisedPrompt: item.RevisedPrompt,
			B64JSON:       item.B64JSON,
		}
		if i == len(response.Data)-1 {
			event.Usage = response.Usage
		}
		data, err := json.Marshal(event)
		if err != nil {
			continue
		}
		sb.WriteString("event: ")
		sb.WriteString(eventType)
		sb.WriteString("\ndata: ")
		sb.Write(data)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// imageSyntheticStream 把合成好的 SSE 文本包装成单事件流。用无缓冲 channel + goroutine
// 保证消费方先收到数据再收到 EOF（带缓冲时 select 可能先命中 EOF 丢数据）。
type imageSyntheticStream struct {
	data string
}

func (s *imageSyntheticStream) Recv() (<-chan string, <-chan error) {
	dataChan := make(chan string)
	errChan := make(chan error)
	gopool.Go(func() {
		dataChan <- s.data
		errChan <- io.EOF
	})
	return dataChan, errChan
}

func (s *imageSyntheticStream) Close() {}

// NewImageSyntheticStream 供 relay 层降级路径使用：渠道不支持流式时，把非流式结果
// 合成的 SSE 文本包装成流，复用与真实流式一致的写回链路（responseGeneralStreamClient）。
func NewImageSyntheticStream(data string) requester.StreamReaderInterface[string] {
	return &imageSyntheticStream{data: data}
}

// imageJSONToStream 处理"请求 stream 但上游返回 JSON 200"的聚合上游：请求已发出、
// 生成成本已产生，不能降级重发，按非流式口径解析计费后合成 completed 事件返回。
// usage 落账顺序与非流式路径一致：先落上游 usage（即便后续判 body 内错误），再兜底估算。
func (p *OpenAIProvider) imageJSONToStream(resp *http.Response, prefix, model, reqQuality, reqSize string) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, common.ErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}

	response := &OpenAIProviderImageResponse{}
	if err = json.Unmarshal(bodyBytes, response); err != nil {
		return nil, common.ErrorWrapper(err, "decode_response_failed", http.StatusInternalServerError)
	}

	if response.Usage != nil && response.Usage.TotalTokens > 0 {
		*p.Usage = *response.Usage.ToOpenAIUsage()
	}

	if openaiErr := ErrorHandle(&response.OpenAIErrorResponse); openaiErr != nil {
		return nil, &types.OpenAIErrorWithStatusCode{
			OpenAIError: *openaiErr,
			StatusCode:  http.StatusBadRequest,
		}
	}

	if p.Usage.TotalTokens == 0 {
		quality, size := reqQuality, reqSize
		if v := rawMessageToString(response.Quality); v != "" {
			quality = v
		}
		if v := rawMessageToString(response.Size); v != "" {
			size = v
		}
		p.Usage.CompletionTokens = len(response.Data) * ImageFallbackOutputTokens(model, quality, size)
		p.Usage.TotalTokens = p.Usage.PromptTokens + p.Usage.CompletionTokens
	}

	return &imageSyntheticStream{data: ImageResponseToSSE(prefix, &response.ImageResponse)}, nil
}

func (p *OpenAIProvider) CreateImageGenerationsStream(request *types.ImageRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.GetRequestTextBody(config.RelayModeImagesGenerations, request.Model, request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	resp, errWithCode := p.Requester.SendRequestRaw(req)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 透传上游响应头（限流指纹等）：与字节透传解耦，成功响应即捕获。
	p.storeOpenAIUpstreamHeaders(resp.Header)

	// 聚合上游可能无视 stream 参数返回 JSON 200，此时不能按 SSE 逐行解析
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return p.imageJSONToStream(resp, ImageGenerationStreamPrefix, request.Model, request.Quality, request.Size)
	}

	handler := OpenAIImageStreamHandler{
		Usage:      p.Usage,
		Model:      request.Model,
		ReqQuality: request.Quality,
		ReqSize:    request.Size,
	}

	return requester.RequestNoTrimStream(p.Requester, resp, handler.HandlerImageStream)
}

func (p *OpenAIProvider) CreateImageEditsStream(request *types.ImageEditRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getRequestImageBody(config.RelayModeImagesEdits, request.Model, request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	resp, errWithCode := p.Requester.SendRequestRaw(req)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 透传上游响应头（限流指纹等）：与字节透传解耦，成功响应即捕获。
	p.storeOpenAIUpstreamHeaders(resp.Header)

	// 聚合上游可能无视 stream 参数返回 JSON 200，此时不能按 SSE 逐行解析
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		// ImageEditRequest 无 quality 字段，兜底档位依赖响应回显，缺失时回落请求 size
		return p.imageJSONToStream(resp, ImageEditStreamPrefix, request.Model, "", request.Size)
	}

	handler := OpenAIImageStreamHandler{
		Usage:   p.Usage,
		Model:   request.Model,
		ReqSize: request.Size,
	}

	return requester.RequestNoTrimStream(p.Requester, resp, handler.HandlerImageStream)
}
