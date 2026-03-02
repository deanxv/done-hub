package suno

import (
	"done-hub/common/requester"
	"done-hub/model"
	"done-hub/providers/base"
	"done-hub/providers/openai"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"net/http"
)

// 定义供应商工厂
type SunoProviderFactory struct{}

// 创建 SunoProvider
func (f SunoProviderFactory) Create(channel *model.Channel) base.ProviderInterface {
	return &SunoProvider{
		OpenAIProvider: openai.OpenAIProvider{
			BaseProvider: base.BaseProvider{
				Config:    getConfig(),
				Channel:   channel,
				Requester: requester.NewHTTPRequester(channel.GetProxy(), RequestErrorHandle),
			},
			BalanceAction: false,
		},
		Account:      "/suno/account",
		Fetchs:       "/suno/fetch",
		Fetch:        "/suno/fetch/%s",
		SubmitMusic:  "/suno/submit/music",
		SubmitLyrics: "/suno/submit/lyrics",
	}
}

func getConfig() base.ProviderConfig {
	return base.ProviderConfig{
		BaseURL:         "",
		ChatCompletions: "/v1/chat/completions",
	}
}

type SunoProvider struct {
	openai.OpenAIProvider
	Account      string
	Fetchs       string
	Fetch        string
	SubmitMusic  string
	SubmitLyrics string
}

func (p *SunoProvider) GetRequestHeaders() (headers map[string]string) {
	headers = make(map[string]string)
	p.CommonRequestHeaders(headers)
	if p.Channel.Key != "" {
		headers["Authorization"] = fmt.Sprintf("Bearer %s", p.Channel.Key)
	}
	return headers
}

// 请求错误处理
func RequestErrorHandle(resp *http.Response) *types.OpenAIError {
	errorResponse := &types.TaskResponse[any]{}
	err := json.NewDecoder(resp.Body).Decode(errorResponse)
	if err != nil {
		return nil
	}

	return ErrorHandle(errorResponse)
}

// 错误处理
func ErrorHandle(err *types.TaskResponse[any]) *types.OpenAIError {
	if err.IsSuccess() {
		return nil
	}

	return &types.OpenAIError{
		Code:    err.Code,
		Message: err.Message,
		Type:    "suno_error",
	}
}
