package bedrockmessages

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/requester"
	"done-hub/providers/claude"
	"done-hub/types"
	"io"
	"net/http"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func (p *BedrockMessagesProvider) CreateClaudeChat(request *claude.ClaudeRequest) (*claude.ClaudeResponse, *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getClaudeRequest(request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	claudeResponse := &claude.ClaudeResponse{}
	// 指纹保真：passThrough=true 时 TeeReader 回填 resp.Body，既 unmarshal 一份供计费，
	// 又拿到上游原始字节透传给客户端（保留字段顺序/未知字段/model 原名）。
	passThrough := config.FingerprintPassThroughEnabled && p.Context != nil
	resp, openaiErr := p.Requester.SendRequest(req, claudeResponse, passThrough)
	if openaiErr != nil {
		return nil, openaiErr
	}

	if passThrough && resp != nil {
		if rawBytes, readErr := io.ReadAll(resp.Body); readErr == nil && len(rawBytes) > 0 {
			p.Context.Set(config.GinBedrockRawResponseBodyKey, rawBytes)
		}
		if headers := filterAWSResponseHeaders(resp.Header); headers != nil {
			p.Context.Set(config.GinPassThroughHeaders, headers)
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

func (p *BedrockMessagesProvider) CreateClaudeChatStream(request *claude.ClaudeRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getClaudeRequest(request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	// mantle 是标准 SSE（data: {...}），与 Anthropic 直连一致，复用 claude 的流式 handler。
	// 指纹保真：与非流式路径对齐，保留 message_start 里上游的 message.model（anthropic.xxx），
	// 不改写成用户请求名，否则流式/非流式暴露的 model 名不一致。
	chatHandler := &claude.ClaudeRelayStreamHandler{
		Usage:          p.Usage,
		ModelName:      request.Model,
		Prefix:         `data: {`,
		SkipModelUnify: config.FingerprintPassThroughEnabled,
	}

	resp, openaiErr := p.Requester.SendRequestRaw(req)
	if openaiErr != nil {
		return nil, openaiErr
	}

	if config.FingerprintPassThroughEnabled && p.Context != nil {
		if headers := filterAWSResponseHeaders(resp.Header); headers != nil {
			p.Context.Set(config.GinPassThroughHeaders, headers)
		}
	}

	// 标准 SSE：走 RequestNoTrimStream（不做 AWS event-stream 二进制帧解码）。
	stream, openaiErr := requester.RequestNoTrimStream(p.Requester, resp, chatHandler.HandlerStream)
	if openaiErr != nil {
		return nil, openaiErr
	}

	return stream, nil
}

// getClaudeRequest 构建到 mantle 端点的请求：字节透传优先，签名收尾。
func (p *BedrockMessagesProvider) getClaudeRequest(request *claude.ClaudeRequest) (*http.Request, *types.OpenAIErrorWithStatusCode) {
	url, errWithCode := p.GetSupportedAPIUri(config.RelayModeChatCompletions)
	if errWithCode != nil {
		return nil, common.StringErrorWrapperLocal("bedrock messages config error", "invalid_bedrock_config", http.StatusInternalServerError)
	}

	fullRequestURL := p.GetFullRequestURL(url, request.Model)
	if fullRequestURL == "" {
		return nil, common.StringErrorWrapperLocal("bedrock messages config error", "invalid_bedrock_config", http.StatusInternalServerError)
	}

	headers := p.GetRequestHeaders()
	if headers == nil {
		return nil, common.StringErrorWrapperLocal("bedrock messages config error", "invalid_bedrock_config", http.StatusInternalServerError)
	}
	if request.Stream {
		headers["Accept"] = "text/event-stream"
	}

	// mantle 端点与 Anthropic 直连字段兼容（context_management / output_config 等原样保留），
	// 只需把 model 归一化为 anthropic.xxx 格式，并回写结构体路径做的 thinking / max_tokens 约束。
	if patched, ok := p.patchPassThroughBody(request); ok {
		req, errWithCode := p.NewRequestWithCustomParamsBytes(http.MethodPost, fullRequestURL, patched, headers, request.Model)
		if errWithCode != nil {
			return nil, errWithCode
		}
		if err := p.Sign(req); err != nil {
			return nil, common.StringErrorWrapperLocal(err.Error(), "sign_request_failed", http.StatusInternalServerError)
		}
		return req, nil
	}

	// 回退：结构体序列化。model 归一化后由 NewRequestWithCustomParams 序列化。
	copyRequest := *request
	copyRequest.Model = resolveMantleModelName(request.Model)
	req, errWithCode := p.NewRequestWithCustomParams(http.MethodPost, fullRequestURL, &copyRequest, headers, request.Model)
	if errWithCode != nil {
		return nil, errWithCode
	}
	if err := p.Sign(req); err != nil {
		return nil, common.StringErrorWrapperLocal(err.Error(), "sign_request_failed", http.StatusInternalServerError)
	}
	return req, nil
}

// patchPassThroughBody 读取 gin 缓存的原始 Claude 原生请求体，按字节最小改写：
//   - model 归一化为 mantle 要求的 anthropic.xxx 格式
//   - max_tokens 可能被 applyClaudeThinkingConstraints 抬高，回写
//   - thinking 可能被约束置 nil，删除
//
// 与 legacy Bedrock 不同：不删 context_management / output_config（mantle 支持），
// 不注入 anthropic_version（走 HTTP header）。
// 返回 (字节, true) 表示透传可用；(nil, false) 表示应回退结构体序列化。
func (p *BedrockMessagesProvider) patchPassThroughBody(request *claude.ClaudeRequest) ([]byte, bool) {
	out, ok := p.ReadNativeRawBody("messages")
	if !ok {
		return nil, false
	}

	// model 归一化：done-hub 的模型映射结果 request.Model，再转 anthropic.xxx 格式
	resolvedModel := resolveMantleModelName(request.Model)
	if resolvedModel != "" && gjson.GetBytes(out, "model").String() != resolvedModel {
		patched, err := sjson.SetBytes(out, "model", resolvedModel)
		if err != nil {
			return nil, false
		}
		out = patched
	}

	// max_tokens 可能被抬高（thinking.budget+4000）
	if request.MaxTokens > 0 && gjson.GetBytes(out, "max_tokens").Int() != int64(request.MaxTokens) {
		patched, err := sjson.SetBytes(out, "max_tokens", request.MaxTokens)
		if err != nil {
			return nil, false
		}
		out = patched
	}

	// thinking 可能被约束置 nil（tool_choice=any/tool 时互斥）
	if request.Thinking == nil && gjson.GetBytes(out, "thinking").Exists() {
		patched, err := sjson.DeleteBytes(out, "thinking")
		if err != nil {
			return nil, false
		}
		out = patched
	}

	return out, true
}
