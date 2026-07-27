package bedrock

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/requester"
	"done-hub/providers/bedrock/category"
	"done-hub/providers/claude"
	"done-hub/types"
	"encoding/json"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	awsAccept      = "application/json"
	awsContentType = "application/json"
)

func (p *BedrockProvider) CreateClaudeChat(request *claude.ClaudeRequest) (*claude.ClaudeResponse, *types.OpenAIErrorWithStatusCode) {
	body, modelID, errWithCode := p.buildInvokeBody(request)
	if errWithCode != nil {
		return nil, errWithCode
	}

	client, err := p.getAwsClient()
	if err != nil {
		return nil, common.ErrorWrapper(err, "aws_client_error", http.StatusInternalServerError)
	}

	out, err := client.InvokeModel(p.invokeContext(), &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(modelID),
		Accept:      aws.String(awsAccept),
		ContentType: aws.String(awsContentType),
		Body:        body,
	})
	if err != nil {
		return nil, p.awsErrorToOpenAI(err)
	}

	// out.Body 就是上游原始响应字节：既 unmarshal 一份供计费，
	// 指纹保真开启时又原样透传给客户端（保留字段顺序 / 未知字段 / AWS model 原名）。
	// x-amzn-* 响应头由 captureResponseMiddleware 存入 context。
	claudeResponse := &claude.ClaudeResponse{}
	if jsonErr := json.Unmarshal(out.Body, claudeResponse); jsonErr != nil {
		return nil, common.ErrorWrapper(jsonErr, "unmarshal_response_failed", http.StatusInternalServerError)
	}

	if config.FingerprintPassThroughEnabled && p.Context != nil {
		p.Context.Set(config.GinBedrockRawResponseBodyKey, out.Body)
	}

	usage := p.GetUsage()
	if isOk := claude.ClaudeUsageToOpenaiUsage(&claudeResponse.Usage, usage); !isOk {
		usage.CompletionTokens = claude.ClaudeOutputUsage(claudeResponse)
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}

	return claudeResponse, nil
}

func (p *BedrockProvider) CreateClaudeChatStream(request *claude.ClaudeRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	body, modelID, errWithCode := p.buildInvokeBody(request)
	if errWithCode != nil {
		return nil, errWithCode
	}

	client, err := p.getAwsClient()
	if err != nil {
		return nil, common.ErrorWrapper(err, "aws_client_error", http.StatusInternalServerError)
	}

	chatHandler := &claude.ClaudeRelayStreamHandler{
		Usage:     p.Usage,
		ModelName: request.Model,
		Prefix:    `{"type"`,
		AddEvent:  true,
		// 指纹保真：保留 message_start 里 AWS 的 message.model（anthropic.claude-xxx-v1:0），
		// 不改写成用户请求名，否则会暴露"被中转"。
		SkipModelUnify: config.FingerprintPassThroughEnabled,
	}

	out, err := client.InvokeModelWithResponseStream(p.invokeContext(), &bedrockruntime.InvokeModelWithResponseStreamInput{
		ModelId:     aws.String(modelID),
		Accept:      aws.String(awsAccept),
		ContentType: aws.String(awsContentType),
		Body:        body,
	})
	if err != nil {
		return nil, p.awsErrorToOpenAI(err)
	}

	// x-amzn-* 响应头由 captureResponseMiddleware 存入 context。
	return newAWSStreamReader(out.GetStream(), chatHandler.HandlerStream), nil
}

// buildInvokeBody 复用现有请求体构造逻辑（字节级透传 / custom_params 合并 / thinking 约束），
// 产出可直接作为 InvokeModelInput.Body 的字节，并返回目标 AWS modelId。
// 借道 NewRequestWithCustomParams* 构造 *http.Request 只为复用其 body 处理，随后取出 body 字节。
func (p *BedrockProvider) buildInvokeBody(request *claude.ClaudeRequest) ([]byte, string, *types.OpenAIErrorWithStatusCode) {
	var err error
	p.Category, err = category.GetCategory(request.Model, p.Region)
	if err != nil || p.Category == nil {
		return nil, "", common.StringErrorWrapperLocal("bedrock provider not found", "bedrock_err", http.StatusInternalServerError)
	}

	headers := p.GetRequestHeaders()

	var req *http.Request
	var errWithCode *types.OpenAIErrorWithStatusCode
	// Bedrock 跑的就是 Claude，与 Anthropic 渠道一致：原生请求恒字节透传（保留客户端
	// 指纹/未知字段），仅去掉 Bedrock 不接受的 model/stream 并注入 anthropic_version；
	// 取不到原始字节时回退结构体序列化。两条路径都经 custom_params 合并。
	if patched, ok := p.patchPassThroughBody(request); ok {
		req, errWithCode = p.NewRequestWithCustomParamsBytes(http.MethodPost, "", patched, headers, request.Model)
	} else {
		copyRequest := *request
		bedrockRequest := &category.ClaudeRequest{
			ClaudeRequest:    &copyRequest,
			AnthropicVersion: category.AnthropicVersion,
		}
		bedrockRequest.Model = ""
		bedrockRequest.Stream = false
		req, errWithCode = p.NewRequestWithCustomParams(http.MethodPost, "", bedrockRequest, headers, request.Model)
	}
	if errWithCode != nil {
		return nil, "", errWithCode
	}

	body, readErr := io.ReadAll(req.Body)
	req.Body.Close()
	if readErr != nil {
		return nil, "", common.ErrorWrapper(readErr, "read_request_body_failed", http.StatusInternalServerError)
	}

	return body, p.Category.ModelName, nil
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
