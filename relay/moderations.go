package relay

import (
	"done-hub/common"
	providersBase "done-hub/providers/base"
	"done-hub/types"
	"net/http"

	"github.com/gin-gonic/gin"
)

type relayModerations struct {
	relayBase
	request types.ModerationRequest
}

func NewRelayModerations(c *gin.Context) *relayModerations {
	relay := &relayModerations{}
	relay.c = c
	return relay
}

func (r *relayModerations) setRequest() error {
	if err := common.UnmarshalBodyReusable(r.c, &r.request); err != nil {
		return err
	}

	if r.request.Model == "" {
		r.request.Model = "omni-moderation-latest"
	}

	r.setOriginalModel(r.request.Model)

	return nil
}

func (r *relayModerations) getPromptTokens() (int, error) {
	return common.CountTokenInput(r.request.Input, r.modelName), nil
}

func (r *relayModerations) send() (err *types.OpenAIErrorWithStatusCode, done bool) {
	provider, ok := r.provider.(providersBase.ModerationInterface)
	if !ok {
		err = common.StringErrorWrapperLocal("channel not implemented", "channel_error", http.StatusServiceUnavailable)
		done = true
		return
	}

	r.request.Model = r.modelName

	response, err := provider.CreateModeration(&r.request)
	if err != nil {
		return
	}
	err = responseJsonClient(r.c, response)

	if err != nil {
		done = true
	}

	return
}
