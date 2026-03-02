package groq

import (
	"done-hub/common/requester"
	"done-hub/model"
	"done-hub/providers/base"
	"done-hub/providers/openai"
)

// 定义供应商工厂
type GroqProviderFactory struct{}

// 创建 GroqProvider
func (f GroqProviderFactory) Create(channel *model.Channel) base.ProviderInterface {
	return &GroqProvider{
		OpenAIProvider: openai.OpenAIProvider{
			BaseProvider: base.BaseProvider{
				Config:    getConfig(),
				Channel:   channel,
				Requester: requester.NewHTTPRequester(channel.GetProxy(), openai.RequestErrorHandle),
			},
		},
	}
}

func getConfig() base.ProviderConfig {
	return base.ProviderConfig{
		BaseURL:         "https://api.groq.com/openai",
		ChatCompletions: "/v1/chat/completions",
		ModelList:       "/v1/models",
	}
}

type GroqProvider struct {
	openai.OpenAIProvider
}
