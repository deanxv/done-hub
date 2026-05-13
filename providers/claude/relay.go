package claude

import (
	"bytes"
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/model_utils"
	"done-hub/common/requester"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type ClaudeRelayStreamHandler struct {
	Usage      *types.Usage
	ModelName  string
	Prefix     string
	StartUsage *Usage

	AddEvent bool
}

func (p *ClaudeProvider) CreateClaudeChat(request *ClaudeRequest) (*ClaudeResponse, *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getClaudeNativeRequest(request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	claudeResponse := &ClaudeResponse{}
	// 发送请求
	_, errWithCode = p.Requester.SendRequest(req, claudeResponse, false)
	if errWithCode != nil {
		return nil, errWithCode
	}

	usage := p.GetUsage()

	isOk := ClaudeUsageToOpenaiUsage(&claudeResponse.Usage, usage)
	if !isOk {
		usage.CompletionTokens = ClaudeOutputUsage(claudeResponse)
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
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
	}

	// 发送请求
	resp, errWithCode := p.Requester.SendRequestRaw(req)
	if errWithCode != nil {
		return nil, errWithCode
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
// 注意事项：
//   - 字段【值】完全保留原始字节（含未知字段、新格式 metadata.user_id 等）；
//     但顶层 key 顺序不保证 —— Go 的 json.Marshal 对 map 会按 key 字典序输出。
//     这是 JSON 语义无关项，relay-code-github 等 JSON 字段级校验不受影响。
//   - thinking 字段只支持"移除/保留"，不支持 done-hub 主动添加（当前业务无此路径）。
func (p *ClaudeProvider) patchClaudeRequestBody(request *ClaudeRequest) ([]byte, bool) {
	if p.Context == nil {
		return nil, false
	}
	rawBody, err := common.ReadBodyRaw(p.Context)
	if err != nil || len(rawBody) == 0 {
		return nil, false
	}

	var bodyMap map[string]json.RawMessage
	if err := json.Unmarshal(rawBody, &bodyMap); err != nil {
		return nil, false
	}
	// 必须看起来像 Claude 原生 /v1/messages 请求（含 messages 字段），
	// 否则可能是 OpenAI→Claude 转换路径走错了入口，直接放弃透传。
	if _, ok := bodyMap["messages"]; !ok {
		return nil, false
	}

	// 1) 模型重写（done-hub 的模型映射会改 request.Model）
	if mb, err := json.Marshal(request.Model); err == nil {
		bodyMap["model"] = mb
	}

	// 2) max_tokens 可能被 applyClaudeThinkingConstraints 改写。
	//    当前业务只会把它调高（不会归零），所以 > 0 即可安全回写。
	if request.MaxTokens > 0 {
		if mb, err := json.Marshal(request.MaxTokens); err == nil {
			bodyMap["max_tokens"] = mb
		}
	}

	// 3) Thinking 约束可能把 thinking 置 nil（tool_choice=any/tool 时禁用）。
	//    本路径不支持 done-hub 主动【添加】 thinking，只支持移除/保留。
	if request.Thinking == nil {
		delete(bodyMap, "thinking")
	}

	out, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, false
	}
	return out, true
}
