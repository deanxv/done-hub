package claude

import (
	"bytes"
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/model_utils"
	"done-hub/common/requester"
	"done-hub/providers/base"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type ClaudeRelayStreamHandler struct {
	Usage      *types.Usage
	ModelName  string
	Prefix     string
	StartUsage *Usage
	Context    *gin.Context

	AddEvent bool

	// SkipModelUnify 为 true 时跳过 message_start 中 message.model 的统一改写，
	// 用于 Bedrock 指纹保真：保留上游 AWS 返回的原始 model 名。
	SkipModelUnify bool
}

// claudeUpstreamHeaderExcluded 传输层头，绝不透传（key 全小写）。
var claudeUpstreamHeaderExcluded = map[string]struct{}{
	"content-length":    {},
	"content-type":      {},
	"content-encoding":  {},
	"transfer-encoding": {},
	"connection":        {},
	"keep-alive":        {},
}

// claudeUpstreamHeaderPrefixes 前缀白名单（全小写）：限流 / 优先级 / fast mode 指纹头。
var claudeUpstreamHeaderPrefixes = []string{
	"anthropic-ratelimit-",
	"anthropic-priority-",
	"anthropic-fast-",
}

// claudeUpstreamHeaderExact 精确白名单（全小写）：退避指令。
// 注意：retry-after / x-should-retry 是上游 429/529 错误响应才带的头，而错误路径由
// SendRequest 的 HandleErrorResp 提前接管，不会走到本过滤器——故当前仅在成功响应可达。
// 保留在白名单是零成本的前瞻：若将来打通错误响应头透传，此处即自动生效。
var claudeUpstreamHeaderExact = map[string]struct{}{
	"retry-after":    {},
	"x-should-retry": {},
}

// filterClaudeUpstreamHeaders 从上游 Claude 响应头中挑出可透传给客户端的指纹头，
// 目的是让 done-hub 中转的响应尽量贴近直连 Anthropic（携带 anthropic-ratelimit-* /
// retry-after 等）。request-id / x-request-id 不直透（避免覆盖本地追踪 ID），单独提取
// 为字符串返回，由 relay 层以 X-Upstream-Request-Id 回写。大小写不敏感；多值 header 全部
// 保留；无命中时返回的 http.Header 为 nil。
func filterClaudeUpstreamHeaders(src http.Header) (http.Header, string) {
	if len(src) == 0 {
		return nil, ""
	}
	out := http.Header{}
	var requestID string
	for name, values := range src {
		lower := strings.ToLower(name)
		if _, excluded := claudeUpstreamHeaderExcluded[lower]; excluded {
			continue
		}
		if lower == "request-id" || lower == "x-request-id" {
			if requestID == "" && len(values) > 0 {
				requestID = values[0]
			}
			continue
		}
		matched := false
		if _, ok := claudeUpstreamHeaderExact[lower]; ok {
			matched = true
		} else {
			for _, prefix := range claudeUpstreamHeaderPrefixes {
				if strings.HasPrefix(lower, prefix) {
					matched = true
					break
				}
			}
		}
		if !matched {
			continue
		}
		for _, v := range values {
			out.Add(name, v)
		}
	}
	if len(out) == 0 {
		out = nil
	}
	return out, requestID
}

// storeClaudeUpstreamHeaders 过滤上游响应头并暂存到 gin.Context，供 relay 层透传写出。
// 流式与非流式共用。
func (p *ClaudeProvider) storeClaudeUpstreamHeaders(header http.Header) {
	if p.Context == nil {
		return
	}
	headers, requestID := filterClaudeUpstreamHeaders(header)
	if headers != nil {
		p.Context.Set(config.GinPassThroughHeaders, headers)
	}
	if requestID != "" {
		p.Context.Set(config.GinUpstreamRequestIdKey, requestID)
	}
}

func (p *ClaudeProvider) CreateClaudeChat(request *ClaudeRequest) (*ClaudeResponse, *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getClaudeNativeRequest(request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	claudeResponse := &ClaudeResponse{}
	// 指纹保真：用 outputResp=true 让 TeeReader 回填 resp.Body，既能 unmarshal 一份供计费，
	// 又能拿到上游原始字节。只有在「无模型映射需要改 model」时才真正字节透传，
	// 否则回退结构体路径由 unifyResponseModel 改写 model（字节透传无法改 model）。
	// 与 Bedrock 共用 FingerprintPassThroughEnabled 开关；关闭即回退结构体序列化（无额外字节缓存）。
	passThrough := config.FingerprintPassThroughEnabled && p.Context != nil
	resp, errWithCode := p.Requester.SendRequest(req, claudeResponse, passThrough)
	if errWithCode != nil {
		return nil, errWithCode
	}

	usage := p.GetUsage()

	isOk := ClaudeUsageToOpenaiUsage(&claudeResponse.Usage, usage)
	if !isOk {
		usage.CompletionTokens = ClaudeOutputUsage(claudeResponse)
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}

	// 字节透传保留字段顺序 / 未知字段。有别名映射需改回请求名时，在原始字节上就地 sjson
	// 改写顶层 model（不改字段顺序 / 不丢未知字段）；无映射时 UnifyModelInJSONBytes 恒 no-op。
	if passThrough && resp != nil {
		// 透传上游响应头（限流 / 退避等）：无论字节是否改写都需要。
		p.storeClaudeUpstreamHeaders(resp.Header)
		if rawBytes, readErr := io.ReadAll(resp.Body); readErr == nil && len(rawBytes) > 0 {
			if patched, changed := base.UnifyModelInJSONBytes(p.Context, rawBytes, "model"); changed {
				rawBytes = patched
			}
			p.Context.Set(config.GinRawResponseBodyKey, rawBytes)
		}
		resp.Body.Close()
	}

	return claudeResponse, nil
}

func (p *ClaudeProvider) CreateClaudeChatStream(request *ClaudeRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getClaudeNativeRequest(request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	chatHandler := &ClaudeRelayStreamHandler{
		Usage:     p.Usage,
		ModelName: request.Model,
		Prefix:    `data: {`,
		Context:   p.Context,
	}

	// 发送请求
	resp, errWithCode := p.Requester.SendRequestRaw(req)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 指纹保真：此刻 resp.Header 已就绪、resp.Body 尚未被消费，先透传上游响应头。
	// 守卫条件与非流式 CreateClaudeChat 的 passThrough 对齐。
	if config.FingerprintPassThroughEnabled && p.Context != nil {
		p.storeClaudeUpstreamHeaders(resp.Header)
	}

	stream, errWithCode := requester.RequestNoTrimStream(p.Requester, resp, chatHandler.HandlerStream)
	if errWithCode != nil {
		return nil, errWithCode
	}

	return stream, nil
}

func (h *ClaudeRelayStreamHandler) HandlerStream(rawLine *[]byte, dataChan chan string, errChan chan error) {
	rawStr := string(*rawLine)
	// 如果rawLine 前缀不为data:，则直接返回
	if !strings.HasPrefix(rawStr, h.Prefix) {
		dataChan <- rawStr
		return
	}

	if h.AddEvent {
		rawStr = fmt.Sprintf("data: %s\n", rawStr)
	}

	noSpaceLine := bytes.TrimSpace(*rawLine)
	if strings.HasPrefix(string(noSpaceLine), "data: ") {
		// 去除前缀
		noSpaceLine = noSpaceLine[6:]
	}

	var claudeResponse ClaudeStreamResponse
	err := json.Unmarshal(noSpaceLine, &claudeResponse)
	if err != nil {
		errChan <- ErrorToClaudeErr(err)
		return
	}

	if claudeResponse.Error != nil {
		if h.AddEvent {
			event := "event: error\n"
			dataChan <- event
		}

		errChan <- claudeResponse.Error
		return
	}

	if h.AddEvent {
		event := fmt.Sprintf("event: %s\n", claudeResponse.Type)
		dataChan <- event
	}

	switch claudeResponse.Type {
	case "message_start":
		ClaudeUsageToOpenaiUsage(&claudeResponse.Message.Usage, h.Usage)
		h.StartUsage = &claudeResponse.Message.Usage
		// 统一请求响应模型：model 仅出现在 message_start 的 message.model。
		// 在剥离前缀的纯 JSON 上字节级改写（gjson 读 + sjson 改，仅动 model 一个字段，
		// 其余字段顺序/内容不变），再把改写后的 JSON 回填到 rawStr，保留其原有的 data: 前缀与尾部。
		// SkipModelUnify 时跳过（Bedrock 指纹保真：保留 AWS 原始 model 名）。
		if !h.SkipModelUnify {
			if patched, changed := base.UnifyModelInJSONBytes(h.Context, noSpaceLine, "message.model"); changed {
				rawStr = strings.Replace(rawStr, string(noSpaceLine), string(patched), 1)
			}
		}
	case "message_delta":
		ClaudeUsageMerge(&claudeResponse.Usage, h.StartUsage)
		ClaudeUsageToOpenaiUsage(&claudeResponse.Usage, h.Usage)
	case "content_block_delta":
		h.Usage.TextBuilder.WriteString(claudeResponse.Delta.Text)
	}

	dataChan <- rawStr

	if h.AddEvent {
		event := "\n"
		dataChan <- event
	}
}

// getClaudeNativeRequest 专供 Claude→Claude 原生格式中继路径使用。
// 与 getChatRequest 的关键区别：尽可能用客户端原始请求体的字节直接转发给上游，
// 只在必要时（模型重写 / Thinking 约束 / max_tokens 调整）按 JSON 字段级别做最小补丁，
// 从而避免因为反序列化到结构体导致的：
//   - 顶层未知字段被丢弃（如 service_tier、anthropic_version）
//   - cache_control / system / tools 等用 any 接住后字段顺序被打乱
//   - 任何 ClaudeRequest 结构未覆盖的新字段被吃掉
//
// 这条路径只在 CreateClaudeChat / CreateClaudeChatStream 里被调用，
// 与 OpenAI→Claude 转换路径完全隔离。
func (p *ClaudeProvider) getClaudeNativeRequest(request *ClaudeRequest) (*http.Request, *types.OpenAIErrorWithStatusCode) {
	url, errWithCode := p.GetSupportedAPIUri(config.RelayModeChatCompletions)
	if errWithCode != nil {
		return nil, errWithCode
	}

	fullRequestURL := p.GetFullRequestURL(url)
	if fullRequestURL == "" {
		return nil, common.ErrorWrapperLocal(nil, "invalid_claude_config", http.StatusInternalServerError)
	}

	headers := p.GetRequestHeaders()
	if request.Stream {
		headers["Accept"] = "text/event-stream"
	}
	// 仅在用户没自定义 anthropic-beta 时设默认值（与 getChatRequest 保持一致）
	if !hasHeaderCI(headers, "anthropic-beta") {
		if model_utils.HasPrefixCaseInsensitive(request.Model, "claude-3-5-sonnet") {
			headers["anthropic-beta"] = "max-tokens-3-5-sonnet-2024-07-15"
		} else if model_utils.HasPrefixCaseInsensitive(request.Model, "claude-3-7-sonnet") {
			headers["anthropic-beta"] = "output-128k-2025-02-19"
		}
	}

	// 尝试基于原始字节透传。走 NewRequestWithCustomParamsBytes 而不是直接 NewRequest，
	// 这样 Channel 的自定义参数（remove_params / 模型粒度覆盖 / overwrite 等）仍会通过
	// MergeCustomParamsBytes(sjson) 合并到 body，行为与项目内 Gemini 大 body 路径一致。
	if patched, ok := p.patchClaudeRequestBody(request); ok {
		return p.NewRequestWithCustomParamsBytes(http.MethodPost, fullRequestURL, patched, headers, request.Model)
	}

	// 拿不到原始字节（比如非 HTTP 路径触发、上下文丢失）就退回结构体序列化路径
	return p.NewRequestWithCustomParams(http.MethodPost, fullRequestURL, request, headers, request.Model)
}

// patchClaudeRequestBody 读 gin 缓存里的原始 /v1/messages 请求体，
// 仅对 model / max_tokens / thinking 做字段级最小修改，其它一律按字节保留。
// 返回 (字节, true) 表示透传成功；返回 (nil, false) 表示透传不可用、调用方应回退。
//
// 实现：用 sjson 直接在字节层面就地改写指定字段，不做 unmarshal/marshal 往返。
// 这样可以同时保留：
//   - 字段【值】的原始字节（未知字段、metadata.user_id 等指纹字段）
//   - 顶层 key 顺序（Claude Code CLI 发出的固定顺序，部分上游会作为客户端识别依据）
//
// 注意事项：
//   - thinking 字段只支持"移除/保留"，不支持 done-hub 主动添加（当前业务无此路径）。
func (p *ClaudeProvider) patchClaudeRequestBody(request *ClaudeRequest) ([]byte, bool) {
	// 必须看起来像 Claude 原生 /v1/messages 请求（含 messages 字段），
	// 否则可能是 OpenAI→Claude 转换路径走错了入口，直接放弃透传。
	rawBody, ok := p.ReadNativeRawBody("messages")
	if !ok {
		return nil, false
	}

	out := rawBody

	// 1) 模型重写（done-hub 的模型映射会改 request.Model）。
	//    仅在模型名实际发生变化时才写入，避免无意义改动。
	if request.Model != "" && gjson.GetBytes(out, "model").String() != request.Model {
		patched, err := sjson.SetBytes(out, "model", request.Model)
		if err != nil {
			return nil, false
		}
		out = patched
	}

	// 2) max_tokens 可能被 applyClaudeThinkingConstraints 改写。
	//    当前业务只会把它调高（不会归零），所以 > 0 且实际变化时才回写。
	if request.MaxTokens > 0 && gjson.GetBytes(out, "max_tokens").Int() != int64(request.MaxTokens) {
		patched, err := sjson.SetBytes(out, "max_tokens", request.MaxTokens)
		if err != nil {
			return nil, false
		}
		out = patched
	}

	// 3) Thinking 约束可能把 thinking 置 nil（tool_choice=any/tool 时禁用）。
	//    本路径不支持 done-hub 主动【添加】 thinking，只支持移除/保留。
	if request.Thinking == nil && gjson.GetBytes(out, "thinking").Exists() {
		patched, err := sjson.DeleteBytes(out, "thinking")
		if err != nil {
			return nil, false
		}
		out = patched
	}

	return out, true
}
