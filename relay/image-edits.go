package relay

import (
	"done-hub/common"
	"done-hub/common/config"
	providersBase "done-hub/providers/base"
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

	r.setOriginalModel(r.request.Model)

	return nil
}

func (r *relayImageEdits) getPromptTokens() (int, error) {
	// PromptTokens应该根据请求中的prompt文本计算，而不是图像参数
	return common.CountTokenText(r.request.Prompt, r.getOriginalModel()), nil
}

func (r *relayImageEdits) send() (err *types.OpenAIErrorWithStatusCode, done bool) {
	provider, ok := r.provider.(providersBase.ImageEditsInterface)
	if !ok {
		err = common.StringErrorWrapperLocal("channel not implemented", "channel_error", http.StatusServiceUnavailable)
		done = true
		return
	}

	r.request.Model = r.modelName

	// 入口协议 == image edits 且响应原样直返：放行 provider 字节透传，
	// 保留上游 usage.output_tokens_details.image_tokens/text_tokens 等未知字段。
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
