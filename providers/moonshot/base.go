package moonshot

import (
	"done-hub/common/requester"
	"done-hub/model"
	"done-hub/providers/base"
	"done-hub/providers/openai"
)

type MoonshotProviderFactory struct{}

// 创建 MoonshotProvider
func (f MoonshotProviderFactory) Create(channel *model.Channel) base.ProviderInterface {
	config := getMoonshotConfig()
	return &MoonshotProvider{
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

func getMoonshotConfig() base.ProviderConfig {
	return base.ProviderConfig{
		BaseURL:         "https://api.moonshot.cn",
		ChatCompletions: "/v1/chat/completions",
		ModelList:       "/v1/models",
	}
}

type MoonshotProvider struct {
	openai.OpenAIProvider
}
