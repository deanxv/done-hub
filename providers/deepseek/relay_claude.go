package deepseek

import (
	"done-hub/common/requester"
	"done-hub/providers/base"
	"done-hub/providers/claude"
	"done-hub/types"
)

// getClaudeConfig 返回 deepseek 原生 Anthropic 端点配置。
// deepseek 官方在 OpenAI 端点之外提供 /anthropic 兼容端点，客户端以 Claude 格式
// （/claude/v1/messages）请求时走这里，端点路径与 x-api-key 认证沿用 Anthropic 原生约定。
func getClaudeConfig() base.ProviderConfig {
	return base.ProviderConfig{
		BaseURL:         "https://api.deepseek.com",
		ChatCompletions: "/anthropic/v1/messages",
		ModelList:       "/v1/models",
	}
}

// claudeProvider 基于当前渠道委托出一个走 Anthropic 原生转发路径的 ClaudeProvider，
// 复用同一 BaseProvider（共享 Channel/Context/Usage），仅替换端点配置与错误解析。
func (p *DeepseekProvider) claudeProvider() *claude.ClaudeProvider {
	provider := &claude.ClaudeProvider{BaseProvider: p.BaseProvider}
	provider.Config = getClaudeConfig()
	provider.Requester = requester.NewHTTPRequester(p.Channel.GetProxy(), claude.RequestErrorHandle)
	return provider
}

func (p *DeepseekProvider) CreateClaudeChat(request *claude.ClaudeRequest) (*claude.ClaudeResponse, *types.OpenAIErrorWithStatusCode) {
	return p.claudeProvider().CreateClaudeChat(request)
}

func (p *DeepseekProvider) CreateClaudeChatStream(request *claude.ClaudeRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	return p.claudeProvider().CreateClaudeChatStream(request)
}
