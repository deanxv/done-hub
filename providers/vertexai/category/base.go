package category

import (
	"done-hub/common/model_utils"
	"done-hub/common/requester"
	"done-hub/providers/base"
	"done-hub/types"
	"errors"
	"net/http"
)

type Category struct {
	Category                  string
	ChatComplete              ChatCompletionConvert
	ResponseChatComplete      ChatCompletionResponse
	ResponseChatCompleteStrem ChatCompletionStreamResponse
	ErrorHandler              requester.HttpErrorHandler
	GetModelName              func(string) string
	GetOtherUrl               func(bool) string
}

var CategoryMap = map[string]*Category{}

func GetCategory(modelName string) (*Category, error) {

	category := ""

	if model_utils.HasPrefixCaseInsensitive(modelName, "gemini") || model_utils.HasPrefixCaseInsensitive(modelName, "imagen") || model_utils.HasPrefixCaseInsensitive(modelName, "lyria") {
		category = "gemini"
	} else if model_utils.HasPrefixCaseInsensitive(modelName, "claude") {
		category = "claude"
	}

	if category == "" {
		return nil, errors.New("category_not_found")
	}

	return CategoryMap[category], nil

}

type ChatCompletionConvert func(*types.ChatCompletionRequest) (any, *types.OpenAIErrorWithStatusCode)
type ChatCompletionResponse func(base.ProviderInterface, *http.Response, *types.ChatCompletionRequest) (*types.ChatCompletionResponse, *types.OpenAIErrorWithStatusCode)

type ChatCompletionStreamResponse func(base.ProviderInterface, *types.ChatCompletionRequest) requester.HandlerPrefix[string]
