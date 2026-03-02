package deepseek

import (
	"done-hub/common/requester"
	"done-hub/model"
	"done-hub/providers/base"
	"done-hub/providers/openai"
)

type DeepseekProviderFactory struct{}

// 创建 DeepseekProvider
func (f DeepseekProviderFactory) Create(channel *model.Channel) base.ProviderInterface {
	config := getDeepseekConfig()
	return &DeepseekProvider{
		OpenAIProvider: openai.OpenAIProvider{
			BaseProvider: base.BaseProvider{
				Config:    config,
				Channel:   channel,
				Requester: requester.NewHTTPRequester(channel.GetProxy(), openai.RequestErrorHandle),
			},
			BalanceAction: false,
		},
	}
}

func getDeepseekConfig() base.ProviderConfig {
	return base.ProviderConfig{
		BaseURL:         "https://api.deepseek.com",
		ChatCompletions: "/v1/chat/completions",
		ModelList:       "/v1/models",
	}
}

type DeepseekProvider struct {
	openai.OpenAIProvider
}
