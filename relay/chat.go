package relay

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/requester"
	"done-hub/common/utils"
	providersBase "done-hub/providers/base"
	"done-hub/safty"
	"done-hub/types"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type relayChat struct {
	relayBase
	chatRequest types.ChatCompletionRequest
}

func NewRelayChat(c *gin.Context) *relayChat {
	relay := &relayChat{
		relayBase: relayBase{
			allowHeartbeat: true,
			c:              c,
		},
	}
	return relay
}

func (r *relayChat) setRequest() error {
	if err := common.UnmarshalBodyReusable(r.c, &r.chatRequest); err != nil {
		return err
	}

	common.NormalizeNullContentWithToolCalls(r.chatRequest.Messages)

	if r.chatRequest.MaxTokens < 0 || r.chatRequest.MaxTokens > math.MaxInt32/2 {
		return errors.New("max_tokens is invalid")
	}

	if r.chatRequest.Tools != nil {
		r.c.Set("skip_only_chat", true)
	}

	if !r.chatRequest.Stream {
		r.chatRequest.StreamOptions = nil
	}

	r.setOriginalModel(r.chatRequest.Model)

	otherArg := r.getOtherArg()

	if otherArg == "search" {
		handleSearch(r.c, &r.chatRequest)
		return nil
	}

	return nil
}

func (r *relayChat) getRequest() interface{} {
	return &r.chatRequest
}

func (r *relayChat) IsStream() bool {
	return r.chatRequest.Stream
}

func (r *relayChat) getPromptTokens() (int, error) {
	channel := r.provider.GetChannel()
	return common.CountTokenMessages(r.chatRequest.Messages, r.modelName, channel.PreCost), nil
}

var need2Response = map[string]bool{
	"o3-pro-2025-06-10":                true,
	"o3-pro":                           true,
	"o1-pro-2025-03-19":                true,
	"o1-pro":                           true,
	"o3-deep-research-2025-06-26":      true,
	"o3-deep-research":                 true,
	"o4-mini-deep-research-2025-06-26": true,
	"o4-mini-deep-research":            true,
	"codex-mini-latest":                true,
}

func (r *relayChat) send() (err *types.OpenAIErrorWithStatusCode, done bool) {
	// 图像生成模型走 chat 入口是非标用法，按 chat 协议处理会把上游 base64 当文本反算成百万级 token、
	// 计费爆炸；在 chat 入口分流到 image generations 协议做渠道降级。
	// 同时查映射后名与原始名，避免渠道做了模型重命名时漏判。
	if types.IsImageGenerationModel(r.modelName) || types.IsImageGenerationModel(r.getOriginalModel()) {
		if imgProvider, ok := r.provider.(providersBase.ImageGenerationsInterface); ok {
			return r.compatibleSendImage(imgProvider)
		}
		err = common.StringErrorWrapperLocal(
			"channel does not support image generations for image model",
			"channel_error", http.StatusServiceUnavailable)
		done = true
		return
	}

	if need2Response[r.modelName] {
		resProvider, ok := r.provider.(providersBase.ResponsesInterface)
		if ok {
			return r.compatibleSend(resProvider)
		}
	}

	chatProvider, ok := r.provider.(providersBase.ChatInterface)
	if !ok {
		err = common.StringErrorWrapperLocal("channel not implemented", "channel_error", http.StatusServiceUnavailable)
		done = true
		return
	}

	r.chatRequest.Model = r.modelName
	// 入口协议 == chat 且响应原样直返，放行 provider 的响应字节透传（保留上游指纹）。
	// image / need2Response 等异协议兼容分支在上方已提前返回，不会走到这里。
	r.c.Set(config.GinRawPassThroughAllowedKey, true)
	// 内容审查
	if config.EnableSafe {
		for _, message := range r.chatRequest.Messages {
			if message.Content != nil {
				CheckResult, _ := safty.CheckContent(message.Content)
				if !CheckResult.IsSafe {
					err = common.StringErrorWrapperLocal(CheckResult.Reason, CheckResult.Code, http.StatusBadRequest)
					done = true
					return
				}
			}
		}
	}

	if r.chatRequest.Stream {
		var response requester.StreamReaderInterface[string]
		response, err = chatProvider.CreateChatCompletionStream(&r.chatRequest)
		if err != nil {
			return
		}

		if r.heartbeat != nil {
			r.heartbeat.Stop()
		}

		doneStr := func() string {
			return r.getUsageResponse()
		}

		var firstResponseTime time.Time
		firstResponseTime, err = responseStreamClient(r.c, response, doneStr)
		r.SetFirstResponseTime(firstResponseTime)
	} else {
		var response *types.ChatCompletionResponse
		response, err = chatProvider.CreateChatCompletion(&r.chatRequest)
		if err != nil {
			return
		}

		if r.heartbeat != nil {
			r.heartbeat.Stop()
		}

		err = responseJsonClient(r.c, response)

	}

	if err != nil {
		done = true
	}

	return
}

func (r *relayChat) getUsageResponse() string {
	if r.chatRequest.StreamOptions != nil && r.chatRequest.StreamOptions.IncludeUsage {
		usageResponse := types.ChatCompletionStreamResponse{
			ID:      fmt.Sprintf("chatcmpl-%s", utils.GetUUID()),
			Object:  "chat.completion.chunk",
			Created: utils.GetTimestamp(),
			Model:   r.chatRequest.Model,
			Choices: []types.ChatCompletionStreamChoice{},
			Usage:   r.provider.GetUsage(),
		}

		responseBody, err := json.Marshal(usageResponse)
		if err != nil {
			return ""
		}

		return string(responseBody)
	}

	return ""
}

// compatibleSendImage 把 chat completions 请求降级到 image generations 协议调上游，
// 并把上游返回的 image response 包装回 chat completions 响应（或 SSE）发给客户端。
// 降级后 upstream usage 是真实的 input/output image tokens，prompt 也按 image 协议口径算，
// 不再走 chat 文本 tokenize。
func (r *relayChat) compatibleSendImage(provider providersBase.ImageGenerationsInterface) (err *types.OpenAIErrorWithStatusCode, done bool) {
	imgReq := r.chatRequest.ToImageRequest()
	imgReq.Model = r.modelName

	if imgReq.Prompt == "" {
		err = common.StringErrorWrapperLocal(
			"no user message with text content found; image generation requires a prompt",
			"invalid_request_error", http.StatusBadRequest)
		done = true
		return
	}

	// 协议降级后真正的 prompt 只是最后一条 user 文本，不是整个 messages。在 provider 调上游前
	// 显式用 image 协议口径覆盖 PromptTokens，避免 provider 不覆写 usage 时按 CountTokenMessages
	// 算出来的 chat 风格 token 被持续误算到客户。OpenAI 系 provider 会再用上游 usage 整段覆盖，
	// 此处对它无副作用；对其它不覆写 PromptTokens 的 provider 起防御作用。
	r.provider.GetUsage().PromptTokens = common.CountTokenText(imgReq.Prompt, r.modelName)

	response, err := provider.CreateImageGenerations(imgReq)
	if err != nil {
		return
	}

	if r.heartbeat != nil {
		r.heartbeat.Stop()
	}

	r.SetFirstResponseTime(time.Now())

	images := chatImagesFromImageResponse(response)

	if r.chatRequest.Stream {
		writeImageChatStream(r.c, r.modelName, images, r.getUsageResponse())
	} else {
		err = responseJsonClient(r.c, buildImageChatResponse(r.modelName, images, r.provider.GetUsage()))
	}

	if err != nil {
		done = true
	}
	return
}

func chatImagesFromImageResponse(resp *types.ImageResponse) []types.ChatMessagePart {
	if resp == nil {
		return nil
	}
	images := make([]types.ChatMessagePart, 0, len(resp.Data))
	for _, d := range resp.Data {
		url := d.URL
		if url == "" && d.B64JSON != "" {
			url = "data:" + sniffImageMIME(d.B64JSON) + ";base64," + d.B64JSON
		}
		if url == "" {
			continue
		}
		images = append(images, types.ChatMessagePart{
			Type:     types.ContentTypeImageURL,
			ImageURL: &types.ChatMessageImageURL{URL: url},
		})
	}
	return images
}

// sniffImageMIME 读 base64 数据头几字节嗅图像 MIME；嗅不出回退 image/png。
// gpt-image-1 支持 png/jpeg/webp 输出，硬标 png 会让严格客户端拒绝。
func sniffImageMIME(b64 string) string {
	// PNG / JPEG 头在 base64 编码后有稳定的固定前缀（base64 字符表对前几个字节稳定）。
	// PNG bytes: 89 50 4E 47 → base64 前缀 "iVBORw"
	// JPEG bytes: FF D8 FF → base64 前缀 "/9j/"
	// WebP bytes: 52 49 46 46 ... 57 45 42 50 → base64 前缀 "UklGR"
	switch {
	case strings.HasPrefix(b64, "iVBORw"):
		return "image/png"
	case strings.HasPrefix(b64, "/9j/"):
		return "image/jpeg"
	case strings.HasPrefix(b64, "UklGR"):
		return "image/webp"
	default:
		return "image/png"
	}
}

func buildImageChatResponse(model string, images []types.ChatMessagePart, usage *types.Usage) *types.ChatCompletionResponse {
	resp := &types.ChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%s", utils.GetUUID()),
		Object:  "chat.completion",
		Created: utils.GetTimestamp(),
		Model:   model,
		Choices: []types.ChatCompletionChoice{{
			Index: 0,
			Message: types.ChatCompletionMessage{
				Role:   types.ChatMessageRoleAssistant,
				Images: images,
			},
			FinishReason: types.FinishReasonStop,
		}},
	}
	if usage != nil {
		resp.Usage = usage
	}
	return resp
}

// writeImageChatStream 输出两帧 chat completions SSE + [DONE]：
//   - 内容帧：含 role + images
//   - finish 帧：finish_reason=stop（对齐 OpenAI 流式惯例，严格 SDK 把 finish_reason 当作终止信号）
//
// 中间帧写失败不抛错，行为对齐 responseStreamClient 的 tryWrite 静默路径——SSE 连接已建立时再返回错误也无意义。
func writeImageChatStream(c *gin.Context, model string, images []types.ChatMessagePart, usageChunk string) {
	id := fmt.Sprintf("chatcmpl-%s", utils.GetUUID())
	created := utils.GetTimestamp()

	contentChunk := types.ChatCompletionStreamResponse{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []types.ChatCompletionStreamChoice{{
			Index: 0,
			Delta: types.ChatCompletionStreamChoiceDelta{
				Role:   types.ChatMessageRoleAssistant,
				Images: images,
			},
		}},
	}
	finishChunk := types.ChatCompletionStreamResponse{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []types.ChatCompletionStreamChoice{{
			Index:        0,
			Delta:        types.ChatCompletionStreamChoiceDelta{},
			FinishReason: types.FinishReasonStop,
		}},
	}

	requester.SetEventStreamHeaders(c)
	writeChatStreamFrame(c, contentChunk)
	writeChatStreamFrame(c, finishChunk)
	if usageChunk != "" {
		fmt.Fprintf(c.Writer, "data: %s\n\n", usageChunk)
	}
	fmt.Fprint(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()
}

func writeChatStreamFrame(c *gin.Context, chunk types.ChatCompletionStreamResponse) {
	body, err := json.Marshal(chunk)
	if err != nil {
		return
	}
	fmt.Fprintf(c.Writer, "data: %s\n\n", body)
}

func (r *relayChat) compatibleSend(resProvider providersBase.ResponsesInterface) (err *types.OpenAIErrorWithStatusCode, done bool) {
	resRequest := r.chatRequest.ToResponsesRequest()
	resRequest.ConvertChat = true

	if r.chatRequest.Stream {
		var response requester.StreamReaderInterface[string]
		response, err = resProvider.CreateResponsesStream(resRequest)
		if err != nil {
			return
		}

		if r.heartbeat != nil {
			r.heartbeat.Stop()
		}

		doneStr := func() string {
			return r.getUsageResponse()
		}

		var firstResponseTime time.Time
		firstResponseTime, err = responseStreamClient(r.c, response, doneStr)
		r.SetFirstResponseTime(firstResponseTime)
	} else {
		var response *types.OpenAIResponsesResponses
		response, err = resProvider.CreateResponses(resRequest)
		if err != nil {
			return
		}

		if r.heartbeat != nil {
			r.heartbeat.Stop()
		}
		err = responseJsonClient(r.c, response.ToChat())
	}

	if err != nil {
		done = true
	}

	return
}
