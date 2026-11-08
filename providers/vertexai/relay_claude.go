package vertexai

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/requester"
	"done-hub/providers/base"
	"done-hub/providers/claude"
	"done-hub/providers/vertexai/category"
	"done-hub/types"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// vertexUnsupportedBetaTokens 是已知会被 Vertex AI 的 Anthropic 端点拒绝（HTTP 400）的 anthropic-beta token。
// Vertex 支持的 beta 很多（context-1m / context-management / interleaved-thinking / compaction / computer-use 等），
// 所以这里用黑名单而不是白名单：只剔除 Anthropic 直连专属或已确认不被接受的 token，其余原样透传。
// 渠道 ModelHeaders / HeaderOverride 显式配置 anthropic-beta 时优先级最高，不经过此过滤。
//
// 维护提示：若 Vertex 对某个新 token 返回 400（日志里 Vertex 错误含 anthropic-beta / unsupported beta），
// 在此登记即可；发版前管理员可先在渠道 ModelHeaders 里显式指定 anthropic-beta 兜底。
var vertexUnsupportedBetaTokens = map[string]struct{}{
	"oauth-2025-04-20":                {},
	"claude-code-20250219":            {},
	"advisor-tool-2026-03-01":         {},
	"prompt-caching-scope-2026-01-05": {},
	"redact-thinking-2026-02-12":      {},
	"thinking-token-count-2026-05-13": {},
}

// 编译期保证 VertexAIProvider 实现了 Claude 原生接口，relay/claude.go 依赖该断言分流
var _ claude.ClaudeChatInterface = (*VertexAIProvider)(nil)

func (p *VertexAIProvider) CreateClaudeChat(request *claude.ClaudeRequest) (*claude.ClaudeResponse, *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getClaudeNativeRequest(request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	claudeResponse := &claude.ClaudeResponse{}
	// 指纹保真：passThrough=true 时 TeeReader 回填 resp.Body，既 unmarshal 一份供计费，
	// 又拿到上游原始字节透传给客户端（保留字段顺序 / 未知字段）。
	passThrough := config.FingerprintPassThroughEnabled && p.Context != nil
	resp, errWithCode := p.Requester.SendRequest(req, claudeResponse, passThrough)
	if errWithCode != nil {
		return nil, errWithCode
	}

	usage := p.GetUsage()
	if isOk := claude.ClaudeUsageToOpenaiUsage(&claudeResponse.Usage, usage); !isOk {
		usage.CompletionTokens = claude.ClaudeOutputUsage(claudeResponse)
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}

	if passThrough && resp != nil {
		if rawBytes, readErr := io.ReadAll(resp.Body); readErr == nil && len(rawBytes) > 0 {
			// Vertex 返回的 model 是上游 ID（如 claude-sonnet-4-5@20250929）；
			// 全局或渠道开启"统一请求响应模型"时改回客户端请求名，未开启则原样透传（与 Claude 直连一致）
			if patched, changed := base.UnifyModelInJSONBytes(p.Context, rawBytes, "model"); changed {
				rawBytes = patched
			}
			p.Context.Set(config.GinRawResponseBodyKey, rawBytes)
		}
		resp.Body.Close()
	}

	return claudeResponse, nil
}

func (p *VertexAIProvider) CreateClaudeChatStream(request *claude.ClaudeRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getClaudeNativeRequest(request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	// streamRawPredict 返回标准 SSE（data: {...}），复用 Claude 直连的流式 handler；
	// 传 Context 使"统一请求响应模型"开关开启时 message_start 里的 model 被改回客户端请求名。
	chatHandler := &claude.ClaudeRelayStreamHandler{
		Usage:     p.Usage,
		ModelName: request.Model,
		Prefix:    `data: {`,
		Context:   p.Context,
	}

	resp, errWithCode := p.Requester.SendRequestRaw(req)
	if errWithCode != nil {
		return nil, errWithCode
	}

	return requester.RequestNoTrimStream(p.Requester, resp, chatHandler.HandlerStream)
}

// getClaudeNativeRequest 构建到 Vertex AI Anthropic 端点的原生 Messages 请求：
// URL 走 publishers/anthropic，body 字节透传优先，anthropic_version 放 body，beta 走 header。
func (p *VertexAIProvider) getClaudeNativeRequest(request *claude.ClaudeRequest) (*http.Request, *types.OpenAIErrorWithStatusCode) {
	if p.ProjectID == "" || p.Region == "" {
		return nil, common.StringErrorWrapperLocal("vertexAI region or project id is empty", "invalid_vertexai_config", http.StatusInternalServerError)
	}

	// 与 relay_gemini.go 的 getGeminiRequest 对称：原生路径不经过 getChatRequest，这里显式选定 Claude 分类，
	// 模型名归一化 / 调用动作 / publisher / 错误处理都从分类取
	p.Category = category.CategoryMap["claude"]
	modelName := p.Category.GetModelName(request.Model)
	fullRequestURL := p.GetFullRequestURL(modelName, p.Category.GetOtherUrl(request.Stream))
	if fullRequestURL == "" {
		return nil, common.ErrorWrapperLocal(nil, "invalid_vertexai_config", http.StatusInternalServerError)
	}

	headers, err := p.getRequestHeadersInternal()
	if err != nil {
		return nil, p.handleTokenError(err)
	}
	if request.Stream {
		headers["Accept"] = "text/event-stream"
	}
	p.applyClaudeBetaHeader(headers)

	p.Requester.ErrorHandler = RequestErrorHandle(p.Category.ErrorHandler, p.ProjectID)

	// 字节透传：保留 cache_control / output_config / context_management 等结构体未覆盖的字段。
	// 走 NewRequestWithCustomParamsBytes，渠道自定义参数仍会通过 sjson 合并进 body。
	if patched, ok := p.patchClaudeRequestBody(request); ok {
		return p.NewRequestWithCustomParamsBytes(http.MethodPost, fullRequestURL, patched, headers, request.Model)
	}

	// 回退：结构体序列化（拿不到原始字节时，如渠道测速构造的伪请求）
	copyRequest := *request
	copyRequest.Model = ""
	vertexRequest := &category.ClaudeRequest{
		ClaudeRequest:    &copyRequest,
		AnthropicVersion: category.AnthropicVersion,
	}
	return p.NewRequestWithCustomParams(http.MethodPost, fullRequestURL, vertexRequest, headers, request.Model)
}

// patchClaudeRequestBody 读取 gin 缓存的原始 /v1/messages 请求体，按字节最小改写：
//   - 删除 model（Vertex 的 model 在 URL 里，body 带 model 会 400）
//   - 注入 anthropic_version
//   - 回写 applyClaudeThinkingConstraints 可能抬高的 max_tokens
//   - thinking 被约束置 nil 时删除
//
// stream 字段保留：Vertex 按 rawPredict / streamRawPredict 区分流式，body 里的 stream 与之一致即可。
// 返回 (字节, true) 表示透传可用；(nil, false) 表示应回退结构体序列化。
func (p *VertexAIProvider) patchClaudeRequestBody(request *claude.ClaudeRequest) ([]byte, bool) {
	out, ok := p.ReadNativeRawBody("messages")
	if !ok {
		return nil, false
	}

	var err error
	if gjson.GetBytes(out, "model").Exists() {
		if out, err = sjson.DeleteBytes(out, "model"); err != nil {
			return nil, false
		}
	}

	if out, err = sjson.SetBytes(out, "anthropic_version", category.AnthropicVersion); err != nil {
		return nil, false
	}

	if request.MaxTokens > 0 && gjson.GetBytes(out, "max_tokens").Int() != int64(request.MaxTokens) {
		if out, err = sjson.SetBytes(out, "max_tokens", request.MaxTokens); err != nil {
			return nil, false
		}
	}

	if request.Thinking == nil && gjson.GetBytes(out, "thinking").Exists() {
		if out, err = sjson.DeleteBytes(out, "thinking"); err != nil {
			return nil, false
		}
	}

	return out, true
}

// applyClaudeBetaHeader 决定下发给 Vertex 的 anthropic-beta：
//   - 渠道 ModelHeaders / HeaderOverride 显式配置了该 header 时原样保留（管理员显式覆盖）
//   - 否则把 HeaderOverride 通配/正则透传进来的值与客户端 anthropic-beta 合并，
//     剔除 Vertex 不接受的 token 后下发；为空则不下发
func (p *VertexAIProvider) applyClaudeBetaHeader(headers map[string]string) {
	if p.channelSetsHeaderExplicitly("anthropic-beta") {
		return
	}

	// 透传规则（"*" / "re:"）可能已经把客户端原值写进 headers，先摘出来一起过滤，避免绕过黑名单
	values := make([]string, 0, 2)
	for key, value := range headers {
		if strings.EqualFold(key, "anthropic-beta") {
			values = append(values, value)
			delete(headers, key)
		}
	}
	if p.Context != nil && p.Context.Request != nil {
		values = append(values, p.Context.Request.Header.Values("anthropic-beta")...)
	}

	if filtered := filterVertexBetaTokens(values); filtered != "" {
		headers["anthropic-beta"] = filtered
	}
}

// channelSetsHeaderExplicitly 判断管理员是否在渠道 ModelHeaders 或 HeaderOverride 中
// 显式为 name 配置了值（固定值 / {api_key} / {client_header:X}）。
// HeaderOverride 里的 "*" / "re:" / "regex:" 透传规则不算显式配置。
func (p *VertexAIProvider) channelSetsHeaderExplicitly(name string) bool {
	if p.Channel == nil {
		return false
	}
	for _, raw := range []*string{p.Channel.ModelHeaders, p.Channel.HeaderOverride} {
		if raw == nil || *raw == "" {
			continue
		}
		var configured map[string]string
		if err := json.Unmarshal([]byte(*raw), &configured); err != nil {
			continue
		}
		for key := range configured {
			if strings.EqualFold(strings.TrimSpace(key), name) {
				return true
			}
		}
	}
	return false
}

// filterVertexBetaTokens 把若干 anthropic-beta header 值按逗号拆分，剔除黑名单 token 并去重，
// 返回逗号拼接的结果；全部被剔除或输入为空时返回空串。
func filterVertexBetaTokens(values []string) string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, token := range strings.Split(value, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			if _, blocked := vertexUnsupportedBetaTokens[token]; blocked {
				continue
			}
			if _, dup := seen[token]; dup {
				continue
			}
			seen[token] = struct{}{}
			out = append(out, token)
		}
	}
	return strings.Join(out, ",")
}
