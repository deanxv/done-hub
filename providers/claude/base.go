package claude

import (
	"done-hub/common/requester"
	"done-hub/model"
	"done-hub/providers/base"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type ClaudeProviderFactory struct{}

// 创建 ClaudeProvider
func (f ClaudeProviderFactory) Create(channel *model.Channel) base.ProviderInterface {
	return &ClaudeProvider{
		BaseProvider: base.BaseProvider{
			Config:    getConfig(),
			Channel:   channel,
			Requester: requester.NewHTTPRequester(channel.GetProxy(), RequestErrorHandle),
		},
	}
}

type ClaudeProvider struct {
	base.BaseProvider
}

func getConfig() base.ProviderConfig {
	return base.ProviderConfig{
		BaseURL:         "https://api.anthropic.com",
		ChatCompletions: "/v1/messages",
		ModelList:       "/v1/models",
	}
}

// 请求错误处理
func RequestErrorHandle(resp *http.Response) *types.OpenAIError {
	claudeError := &ClaudeError{}
	err := json.NewDecoder(resp.Body).Decode(claudeError)
	if err != nil {
		return nil
	}

	return errorHandle(claudeError)
}

// 错误处理
func errorHandle(claudeError *ClaudeError) *types.OpenAIError {
	if claudeError == nil {
		return nil
	}

	if claudeError.Type == "" {
		return nil
	}
	return &types.OpenAIError{
		Message: claudeError.ErrorInfo.Message,
		Type:    claudeError.ErrorInfo.Type,
		Code:    claudeError.Type,
	}
}

// 获取请求头
func (p *ClaudeProvider) GetRequestHeaders() (headers map[string]string) {
	headers = make(map[string]string)
	p.CommonRequestHeaders(headers)

	headers["x-api-key"] = p.Channel.Key
	anthropicVersion := p.Context.Request.Header.Get("anthropic-version")
	if anthropicVersion == "" {
		anthropicVersion = "2023-06-01"
	}
	headers["anthropic-version"] = anthropicVersion

	// 透传客户端的 anthropic-beta（仅在 ModelHeaders 未自定义时生效）
	if _, exists := headers["anthropic-beta"]; !exists {
		if anthropicBeta := p.Context.Request.Header.Get("anthropic-beta"); anthropicBeta != "" {
			headers["anthropic-beta"] = anthropicBeta
		}
	}

	// 透传 Claude Code 客户端身份标识头与 SDK 指纹头（仅在已设置时跳过，以让 ModelHeaders 自定义优先）：
	//   - x-app：relay-code-github 的 claude_code_validate.checkRequiredHeaders 会校验非空
	//   - x-stainless-*：Anthropic SDK 指纹头，部分上游会作为客户端识别依据
	// 注意：User-Agent 不在这里透传——由 channel.ModelHeaders 自定义配置，例如：
	//   { "user-agent": "claude-cli/2.1.140 (external, cli)" }
	for headerName, values := range p.Context.Request.Header {
		if len(values) == 0 || values[0] == "" {
			continue
		}
		lower := strings.ToLower(headerName)
		if lower != "x-app" && !strings.HasPrefix(lower, "x-stainless-") {
			continue
		}
		if hasHeaderCI(headers, lower) {
			continue
		}
		headers[lower] = values[0]
	}

	return headers
}

// hasHeaderCI 对 headers map 做大小写不敏感的存在性检查。
// headers 的 key 大小写有两类来源：
//  1. 本文件自身的写入（如 "x-api-key" / "anthropic-version" / "anthropic-beta"），统一小写；
//  2. CommonRequestHeaders 注入的 channel.ModelHeaders（用户自定义 JSON，可能写 "X-App" 也可能写 "x-app"）。
//
// 第 2 类的随意大小写就是这里需要兜底的场景；若不兜底，"已被用户自定义"会被误判为未设，进而被透传覆盖。
func hasHeaderCI(headers map[string]string, name string) bool {
	lower := strings.ToLower(name)
	for k := range headers {
		if strings.ToLower(k) == lower {
			return true
		}
	}
	return false
}

func (p *ClaudeProvider) GetFullRequestURL(requestURL string) string {
	baseURL := strings.TrimSuffix(p.GetBaseURL(), "/")
	if strings.HasPrefix(baseURL, "https://gateway.ai.cloudflare.com") {
		requestURL = strings.TrimPrefix(requestURL, "/v1")
	}

	return fmt.Sprintf("%s%s", baseURL, requestURL)
}

func stopReasonClaude2OpenAI(reason string) string {
	switch reason {
	case "end_turn", "stop_sequence":
		return types.FinishReasonStop
	case "max_tokens":
		return types.FinishReasonLength
	case "tool_use":
		return types.FinishReasonToolCalls
	case "refusal":
		return types.FinishReasonContentFilter
	default:
		return reason
	}
}

func convertRole(role string) string {
	switch role {
	case types.ChatMessageRoleUser, types.ChatMessageRoleTool, types.ChatMessageRoleFunction:
		return types.ChatMessageRoleUser
	default:
		return types.ChatMessageRoleAssistant
	}
}
