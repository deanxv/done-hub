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

	// 透传 Claude Code 客户端指纹头，避免上游把请求识别为非客户端。
	// 全部对 ModelHeaders 自定义的值让步（通过 hasHeaderCI 做大小写不敏感判断）。
	//
	// 透传规则：除了少数由本函数显式管理的头（host / x-api-key / anthropic-version /
	// anthropic-beta），其它客户端头都透传。这样不需要为每一种新指纹头单独维护白名单：
	//   - User-Agent：Claude Code CLI 携带 "claude-cli/x.x.x"，上游常用此识别客户端
	//   - x-app：relay-code-github 等中转会校验非空
	//   - x-stainless-*：Anthropic SDK 指纹头
	//   - 任何未来新增的客户端识别头
	for headerName, values := range p.Context.Request.Header {
		if len(values) == 0 || values[0] == "" {
			continue
		}
		if isProviderManagedHeader(headerName) {
			continue
		}
		if hasHeaderCI(headers, headerName) {
			continue
		}
		headers[strings.ToLower(headerName)] = values[0]
	}

	return headers
}

// isProviderManagedHeader 判定一个客户端入站头是否禁止原样透传到上游。
// 包含两类：
//  1. 由本 Provider 显式管理的协议头（host / x-api-key / anthropic-version / anthropic-beta）
//  2. 安全敏感或 HTTP 传输层头，原样转发会出问题
func isProviderManagedHeader(name string) bool {
	switch strings.ToLower(name) {
	// Provider 显式管理：必须由本函数控制
	case "host",
		"x-api-key",
		"anthropic-version",
		"anthropic-beta":
		return true
	// 安全敏感：done-hub 用户的认证凭据，禁止泄漏给上游
	case "authorization",
		"cookie",
		"proxy-authorization":
		return true
	// HTTP 传输层：由 http.Client 控制，禁止透传客户端原值
	case "content-length",
		"connection",
		"transfer-encoding",
		"accept-encoding",
		"upgrade",
		"keep-alive",
		"te",
		"trailer":
		return true
	// 浏览器 CORS 语义头：done-hub 作为中转，前端请求会带 Origin/Referer 指向自身域名。
	// 透传给 Anthropic 会被判定为浏览器直连，返回 403 permission_error
	// "CORS requests must set 'anthropic-dangerous-direct-browser-access' header"，
	// 进而触发渠道自动禁用。中转场景这两个头对上游无意义，一律剥离。
	case "origin", "referer":
		return true
	}
	return false
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
