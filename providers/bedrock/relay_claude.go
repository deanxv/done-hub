package bedrock

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/requester"
	"done-hub/providers/bedrock/category"
	"done-hub/providers/claude"
	"done-hub/types"
	"io"
	"net/http"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func (p *BedrockProvider) CreateClaudeChat(request *claude.ClaudeRequest) (*claude.ClaudeResponse, *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getClaudeRequest(request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	claudeResponse := &claude.ClaudeResponse{}
	// 指纹保真开启时用 outputResp=true：TeeReader 会把上游原始字节回填 resp.Body，
	// 既能 unmarshal 一份供计费，又能拿到原始字节透传给客户端（保留字段顺序/未知字段/AWS model 原名）。
	passThrough := config.FingerprintPassThroughEnabled && p.Context != nil
	resp, openaiErr := p.Requester.SendRequest(req, claudeResponse, passThrough)
	if openaiErr != nil {
		return nil, openaiErr
	}

	if passThrough && resp != nil {
		if rawBytes, readErr := io.ReadAll(resp.Body); readErr == nil {
			p.Context.Set(config.GinBedrockRawResponseBodyKey, rawBytes)
		}
		if headers := filterAWSResponseHeaders(resp.Header); headers != nil {
			p.Context.Set(config.GinBedrockPassThroughHeaders, headers)
		}
		resp.Body.Close()
	}

	usage := p.GetUsage()
	if isOk := claude.ClaudeUsageToOpenaiUsage(&claudeResponse.Usage, usage); !isOk {
		usage.CompletionTokens = claude.ClaudeOutputUsage(claudeResponse)
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}

	return claudeResponse, nil
}

func (p *BedrockProvider) CreateClaudeChatStream(request *claude.ClaudeRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getClaudeRequest(request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	chatHandler := &claude.ClaudeRelayStreamHandler{
		Usage:     p.Usage,
		ModelName: request.Model,
		Prefix:    `{"type"`,
		AddEvent:  true,
		// 指纹保真：保留 message_start 里 AWS 的 message.model（anthropic.claude-xxx-v1:0），
		// 不改写成用户请求名，否则会暴露"被中转"。
		SkipModelUnify: config.FingerprintPassThroughEnabled,
	}

	// 发送请求
	resp, openaiErr := p.Requester.SendRequestRaw(req)
	if openaiErr != nil {
		return nil, openaiErr
	}

	if config.FingerprintPassThroughEnabled && p.Context != nil {
		if headers := filterAWSResponseHeaders(resp.Header); headers != nil {
			p.Context.Set(config.GinBedrockPassThroughHeaders, headers)
		}
	}

	stream, openaiErr := RequestStream(resp, chatHandler.HandlerStream)
	if openaiErr != nil {
		return nil, openaiErr
	}

	return stream, nil
}

func (p *BedrockProvider) getClaudeRequest(request *claude.ClaudeRequest) (*http.Request, *types.OpenAIErrorWithStatusCode) {
	var err error
	p.Category, err = category.GetCategory(request.Model, p.Region)
	if err != nil || p.Category == nil {
		return nil, common.StringErrorWrapperLocal("bedrock provider not found", "bedrock_err", http.StatusInternalServerError)
	}

	url, errWithCode := p.GetSupportedAPIUri(config.RelayModeChatCompletions)
	if errWithCode != nil {
		return nil, common.StringErrorWrapperLocal("bedrock config error", "invalid_bedrock_config", http.StatusInternalServerError)
	}

	if request.Stream {
		url += "-with-response-stream"
	}

	// 获取请求地址
	fullRequestURL := p.GetFullRequestURL(url, p.Category.ModelName)
	if fullRequestURL == "" {
		return nil, common.StringErrorWrapperLocal("bedrock config error", "invalid_bedrock_config", http.StatusInternalServerError)
	}

	headers := p.GetRequestHeaders()

	if headers == nil {
		return nil, common.StringErrorWrapperLocal("bedrock config error", "invalid_bedrock_config", http.StatusInternalServerError)
	}

	// Bedrock 跑的就是 Claude，与 Anthropic 渠道一致：原生请求恒字节透传（保留客户端
	// 指纹/未知字段），仅去掉 Bedrock 不接受的 model/stream 并注入 anthropic_version；
	// 取不到原始字节时回退结构体序列化。两条路径都经 custom_params 合并。
	if patched, ok := p.patchPassThroughBody(request); ok {
		req, errWithCode := p.NewRequestWithCustomParamsBytes(http.MethodPost, fullRequestURL, patched, headers, request.Model)
		if errWithCode != nil {
			return nil, errWithCode
		}
		p.Sign(req)
		return req, nil
	}

	copyRequest := *request
	bedrockRequest := &category.ClaudeRequest{
		ClaudeRequest:    &copyRequest,
		AnthropicVersion: category.AnthropicVersion,
	}
	bedrockRequest.Model = ""
	bedrockRequest.Stream = false

	req, errWithCode := p.NewRequestWithCustomParams(http.MethodPost, fullRequestURL, bedrockRequest, headers, request.Model)
	if errWithCode != nil {
		return nil, errWithCode
	}

	p.Sign(req)

	return req, nil
}

// patchPassThroughBody 读取 gin 缓存的原始 Claude 原生请求体，去掉 Bedrock 不接受的
// model（走 URL）/ stream（走 API 选择）字段并注入 anthropic_version，其余字节原样保留。
// 同时回写 applyClaudeThinkingConstraints 对结构体做的两处约束（max_tokens 抬高、
// thinking 置 nil），否则透传出去的 body 会带着违规字段被 Bedrock 拒绝。
// 返回 (字节, true) 表示透传可用；返回 (nil, false) 表示应回退结构体序列化路径。
func (p *BedrockProvider) patchPassThroughBody(request *claude.ClaudeRequest) ([]byte, bool) {
	// 必须是 Claude 原生 /v1/messages 请求（含 messages 字段），否则放弃透传
	out, ok := p.ReadNativeRawBody("messages")
	if !ok {
		return nil, false
	}

	for _, field := range []string{"model", "stream"} {
		if !gjson.GetBytes(out, field).Exists() {
			continue
		}
		patched, err := sjson.DeleteBytes(out, field)
		if err != nil {
			return nil, false
		}
		out = patched
	}

	// max_tokens 可能被 applyClaudeThinkingConstraints 抬高（thinking.budget+4000）
	if request.MaxTokens > 0 && gjson.GetBytes(out, "max_tokens").Int() != int64(request.MaxTokens) {
		patched, err := sjson.SetBytes(out, "max_tokens", request.MaxTokens)
		if err != nil {
			return nil, false
		}
		out = patched
	}

	// thinking 可能被约束置 nil（tool_choice=any/tool 时与 thinking 互斥）
	if request.Thinking == nil && gjson.GetBytes(out, "thinking").Exists() {
		patched, err := sjson.DeleteBytes(out, "thinking")
		if err != nil {
			return nil, false
		}
		out = patched
	}

	out, err := sjson.SetBytes(out, "anthropic_version", category.AnthropicVersion)
	if err != nil {
		return nil, false
	}

	return out, true
}
