package check_channel

import (
	"done-hub/common/config"
	"done-hub/common/model_utils"
	"done-hub/model"
)

// usesClaudeConversion 判断这次检测是否经过 done-hub 的 OpenAI→Claude 请求转换。
// 这条转换和 Anthropic 官方 OpenAI 兼容层一样会忽略 response_format，
// 也不存在非法角色这回事，检测结果需要据此解读，不能算到上游头上。
//
// 只看渠道类型和映射后的上游模型名：OpenAI 兼容类中转即使卖 Claude 模型，
// response_format 也会原样转发，是否支持取决于上游，按普通渠道判定。
func usesClaudeConversion(channelType int, upstreamModel string) bool {
	switch channelType {
	case config.ChannelTypeAnthropic, config.ChannelTypeClaudeCode:
		return true
	case config.ChannelTypeBedrock:
		// Bedrock 按模型分流，anthropic 系列走 Claude 转换
		return model_utils.ContainsCaseInsensitive(upstreamModel, "anthropic") ||
			model_utils.HasPrefixCaseInsensitive(upstreamModel, "claude")
	case config.ChannelTypeVertexAI:
		return model_utils.HasPrefixCaseInsensitive(upstreamModel, "claude")
	}
	return false
}

// rewritesUnknownRole 判断这次检测是否经过会把非法角色改写为合法角色的转换。
// Claude 和 Gemini 两类转换都在 default 分支返回 user/model，所以故意发
// "user11" 角色的请求不会让上游报错——报错是转换之前的事，上游只看到合法请求。
//
// 和 usesClaudeConversion 不同：这里还覆盖了 Gemini 系渠道（原生 Gemini、
// GeminiCli、VertexAI 的 Gemini 模型、Antigravity、VertexAI Express）。
// 这些渠道同样原样转发 response_format，所以 JSON 检测仍按普通渠道判定。
//
// geminiOpenAIAPI 对应 Gemini 渠道的 use_openai_api 插件：开启后走 Gemini 的 OpenAI 兼容端点，
// 请求原样转发，不经过角色转换。
func rewritesUnknownRole(channelType int, upstreamModel string, geminiOpenAIAPI bool) bool {
	if usesClaudeConversion(channelType, upstreamModel) {
		return true
	}
	switch channelType {
	case config.ChannelTypeGemini:
		return !geminiOpenAIAPI
	case config.ChannelTypeGeminiCli, config.ChannelTypeAntigravity, config.ChannelTypeVertexAIExpress:
		// VertexAI Express 构造时固定 UseOpenaiAPI=false，始终走 Gemini 转换
		return true
	case config.ChannelTypeVertexAI:
		// VertexAI 的 Gemini 模型走 Gemini 转换；Claude 模型在 usesClaudeConversion 里已经覆盖
		return model_utils.HasPrefixCaseInsensitive(upstreamModel, "gemini") ||
			model_utils.HasPrefixCaseInsensitive(upstreamModel, "imagen") ||
			model_utils.HasPrefixCaseInsensitive(upstreamModel, "lyria")
	}
	return false
}

// geminiUsesOpenAIAPI 与 GeminiProviderFactory 的判断一致：服务账号 Key 会强制关闭 use_openai_api，
// 这里不复刻那条分支——服务账号渠道开了插件属于少见配置，误判只会让中转层数这一项显示提示。
func geminiUsesOpenAIAPI(channel *model.Channel) bool {
	if channel.Plugin == nil {
		return false
	}
	plugin, ok := channel.Plugin.Data()["use_openai_api"]
	if !ok {
		return false
	}
	enable, _ := plugin["enable"].(bool)
	return enable
}
