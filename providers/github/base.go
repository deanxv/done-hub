package github

import (
	"done-hub/common/requester"
	"done-hub/model"
	"done-hub/providers/base"
	"done-hub/providers/openai"
)

type GithubProviderFactory struct{}

// 创建 GithubProvider
func (f GithubProviderFactory) Create(channel *model.Channel) base.ProviderInterface {
	config := getGithubConfig()
	return &GithubProvider{
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

func getGithubConfig() base.ProviderConfig {
	return base.ProviderConfig{
		BaseURL:         "https://models.inference.ai.azure.com",
		ChatCompletions: "/chat/completions",
		Embeddings:      "/embeddings",
		ModelList:       "/models",
	}
}

type GithubProvider struct {
	openai.OpenAIProvider
}
