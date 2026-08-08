package relay

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/requester"
	providersBase "done-hub/providers/base"
	"done-hub/providers/openai"
	"done-hub/types"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

type relayImageEdits struct {
	relayBase
	request types.ImageEditRequest
}

func NewRelayImageEdits(c *gin.Context) *relayImageEdits {
	relay := &relayImageEdits{}
	relay.c = c
	return relay
}

func (r *relayImageEdits) setRequest() error {
	if err := common.UnmarshalBodyReusable(r.c, &r.request); err != nil {
		return err
	}

	if r.request.Prompt == "" {
		return errors.New("field prompt is required")
	}

	if r.request.Model == "" {
		r.request.Model = "dall-e-2"
	}

	if r.request.Size == "" {
		r.request.Size = "1024x1024"
	}

	r.request.PartialImages = clampPartialImages(r.request.PartialImages)

	r.setOriginalModel(r.request.Model)

	return nil
}

func (r *relayImageEdits) getPromptTokens() (int, error) {
	// PromptTokens应该根据请求中的prompt文本计算，而不是图像参数
	return common.CountTokenText(r.request.Prompt, r.getOriginalModel()), nil
}

func (r *relayImageEdits) IsStream() bool {
	return r.request.StreamEnabled()
}

func (r *relayImageEdits) send() (err *types.OpenAIErrorWithStatusCode, done bool) {
	provider, ok := r.provider.(providersBase.ImageEditsInterface)
	if !ok {
		err = common.StringErrorWrapperLocal("channel not implemented", "channel_error", http.StatusServiceUnavailable)
		done = true
		return
	}

	r.request.Model = r.modelName

	if r.request.StreamEnabled() {
		var stream requester.StreamReaderInterface[string]
		if streamProvider, ok := r.provider.(providersBase.ImageEditsStreamInterface); ok {
			stream, err = streamProvider.CreateImageEditsStream(&r.request)
		} else {
			err = providersBase.ImageStreamNotSupportedError()
		}

		// 渠道不支持流式：降级走非流式请求，把结果合成 completed 事件回吐给客户端。
		// 哨兵是本地错误、未发过上游请求，降级不会造成重复生成。
		if providersBase.IsImageStreamNotSupported(err) {
			// 拷贝后摘掉流式参数再发非流式请求，防按结构体序列化的渠道把 stream:true 外泄给
			// 上游；不动 r.request，send 之后 RelayHandler 还要用 IsStream() 落计费口径
			fallbackRequest := r.request
			fallbackRequest.Stream = nil
			fallbackRequest.PartialImages = nil
			var response *types.ImageResponse
			response, err = provider.CreateImageEdits(&fallbackRequest)
			if err != nil {
				return
			}
			// 无 error 但 Data 为空时合成结果是零事件流，客户端等不到 completed 也收不到报错，
			// 显式报错走重试链路（对齐 codex parseImagesStream 的 no_image_output 口径）
			if len(response.Data) == 0 {
				err = common.StringErrorWrapper("upstream did not return any image output", "no_image_output", http.StatusBadGateway)
				return
			}
			stream = openai.NewImageSyntheticStream(openai.ImageResponseToSSE(openai.ImageEditStreamPrefix, response))
		}
		if err != nil {
			return
		}

		// 官方 images SSE 靠连接关闭收尾、不发 [DONE]
		doneStr := func() string {
			return ""
		}
		firstResponseTime := responseGeneralStreamClient(r.c, stream, doneStr)
		r.SetFirstResponseTime(firstResponseTime)
		return
	}

	// 入口协议 == image edits 且响应原样直返：放行 provider 字节透传，
	// 保留上游 usage.output_tokens_details.image_tokens/text_tokens 等未知字段。
	// 仅非流式路径放行：流式降级路径不走 responseJsonClient，暂存的原始字节无人消费。
	r.c.Set(config.GinRawPassThroughAllowedKey, true)

	response, err := provider.CreateImageEdits(&r.request)
	if err != nil {
		return
	}
	err = responseJsonClient(r.c, response)

	if err != nil {
		done = true
	}

	return
}
