package bedrock

import (
	"bytes"
	"crypto/sha256"
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/requester"
	"done-hub/providers/base"
	"done-hub/providers/bedrock/category"
	"done-hub/providers/bedrock/sigv4"
	"done-hub/providers/openai"
	"done-hub/types"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// bedrockSigV4Service 是 bedrock-runtime OpenAI 兼容端点（/openai/v1/responses）的
// SigV4 service name。与 InvokeModel（SDK 内部同为 bedrock）一致。
const bedrockSigV4Service = "bedrock"

// relay 层按运行时类型断言分发（relay/responses.go），断言失败会静默回落 chat 兼容层，
// 这里用编译期检查兜住方法集变化。
var (
	_ base.ResponsesInterface    = (*BedrockProvider)(nil)
	_ base.ResponsesModelSupport = (*BedrockProvider)(nil)
)

// SupportsNativeResponses 原生 /openai/v1/responses 端点仅 GPT-5.x 闭源系可用
// （2026-08 实测：gpt-oss 报 "doesn't support this API"，claude 族不在该端点上）。
// 其余模型由 relay 层回落 chat 兼容层（claude → InvokeModel anthropic payload，
// gpt-oss → InvokeModel openai payload），行为与本渠道未实现 ResponsesInterface 时一致。
func (p *BedrockProvider) SupportsNativeResponses(modelName string) bool {
	resolved := category.GetModelName(modelName, p.Region)
	return strings.Contains(resolved, "openai.") && !strings.Contains(resolved, "gpt-oss")
}

// Sign 对请求做 SigV4 签名（service=bedrock）。bearer token 模式跳过
// （Authorization 已在 getResponsesRequest 设置）。
// 与 providers/bedrockmessages/base.go 的 Sign 同款实现，仅 service name 不同。
func (p *BedrockProvider) Sign(req *http.Request) error {
	if p.APIToken != "" {
		return nil
	}

	var body []byte
	if req.Body == nil {
		body = []byte("")
	} else {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return errors.New("error getting request body: " + err.Error())
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
	}

	sig, err := sigv4.New(
		sigv4.WithCredential(p.AccessKeyID, p.SecretAccessKey, p.SessionToken),
		sigv4.WithRegionService(p.Region, bedrockSigV4Service),
	)
	if err != nil {
		return err
	}

	reqBodyHashHex := fmt.Sprintf("%x", sha256.Sum256(body))
	sig.Sign(req, reqBodyHashHex, sigv4.NewTime(time.Now()))
	return nil
}

func (p *BedrockProvider) CreateResponses(request *types.OpenAIResponsesRequest) (*types.OpenAIResponsesResponses, *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getResponsesRequest(request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	response := &types.OpenAIResponsesResponses{}
	// 开启渠道 PassThroughBody 且 relay 层已放行（入口协议 == responses、响应原样直返）时，
	// 用 outputResp=true 让 SendRequest 回填 resp.Body：既 unmarshal 一份供计费，又能拿到上游
	// 原始字节用于响应字节透传（保留未知字段 / 字段顺序）。
	passThrough := p.Channel.PassThroughBody && p.Context != nil && p.Context.GetBool(config.GinRawPassThroughAllowedKey)
	resp, errWithCode := p.Requester.SendRequest(req, response, passThrough)
	if errWithCode != nil {
		return nil, errWithCode
	}
	if passThrough {
		defer resp.Body.Close()
	}

	// 透传 AWS 指纹响应头（x-amzn-* / apigw-requestid）；
	// 上游 request-id 由 Requester.ResponseHook 统一采集（x-amzn-requestid 在候选表首位）。
	p.storeAWSUpstreamHeaders(resp.Header)

	// 仅在 usage 完全缺失、或有响应内容却把 output_tokens 算成 0（解析异常）时才兜底估算，
	// 避免误杀上游真实返回的空响应（output_tokens=0 且无内容是合法的）而覆盖其 input/details。
	if response.Usage == nil || (response.Usage.OutputTokens == 0 && response.GetContent() != "") {
		response.Usage = &types.ResponsesUsage{
			InputTokens:  p.Usage.PromptTokens,
			OutputTokens: 0,
			TotalTokens:  0,
		}
		response.Usage.OutputTokens = common.CountTokenText(response.GetContent(), request.Model)
		response.Usage.TotalTokens = response.Usage.InputTokens + response.Usage.OutputTokens
	}

	*p.Usage = *response.Usage.ToOpenAIUsage()

	// AWS Responses 端点无 server-side tools（web_search / code_interpreter 等），
	// 不需要 openai 路径的 getResponsesExtraBilling。

	// 暂存上游原始字节，由 relay 层字节透传，保留未知字段 / 字段顺序。
	if passThrough {
		if rawBytes, readErr := io.ReadAll(resp.Body); readErr == nil && len(rawBytes) > 0 {
			if patched, changed := base.UnifyModelInJSONBytes(p.Context, rawBytes, "model"); changed {
				rawBytes = patched
			}
			p.Context.Set(config.GinRawResponseBodyKey, rawBytes)
		}
	}

	// 结构体出口把上游的 AWS model ID（如 global.openai.gpt-5.6-sol）改回用户请求名。
	// gpt-oss/claude 被 SupportsNativeResponses 挡在 chat 兼容层，到不了这里。
	response.Model = p.GetResponseModelName(request.Model)

	return response, nil
}

func (p *BedrockProvider) CreateResponsesStream(request *types.OpenAIResponsesRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getResponsesRequest(request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	// AWS 的 /openai/v1/responses 流式是标准 SSE（event: 行 + data: 前缀），
	// 与 openai.com 一致，直接复用 openai 的 responses 流 handler。
	resp, errWithCode := p.Requester.SendRequestRaw(req)
	if errWithCode != nil {
		return nil, errWithCode
	}

	p.storeAWSUpstreamHeaders(resp.Header)

	chatHandler := openai.OpenAIResponsesStreamHandler{
		Usage:  p.Usage,
		Prefix: `data: `,
		// chat→responses 兼容路径（HandlerChatStream）合成的 chat chunk 用 h.Model 当响应模型名；
		// 原生 responses 路径不读 h.Model，走 response.model 字节改写。
		Model:   p.GetResponseModelName(request.Model),
		Context: p.Context,
	}

	if request.ConvertChat {
		return requester.RequestStream(p.Requester, resp, chatHandler.HandlerChatStream)
	}

	return requester.RequestNoTrimStream(p.Requester, resp, chatHandler.HandlerResponsesStream)
}

// getResponsesRequest 构造发往 bedrock-runtime /openai/v1/responses 的请求：
// 字节透传优先（保留 background / metadata / service_tier 等结构体未定义字段），
// 回退结构体序列化；两条路径都以 SigV4 签名（或 Bearer）收尾。
func (p *BedrockProvider) getResponsesRequest(request *types.OpenAIResponsesRequest) (*http.Request, *types.OpenAIErrorWithStatusCode) {
	url, errWithCode := p.GetSupportedAPIUri(config.RelayModeResponses)
	if errWithCode != nil {
		return nil, errWithCode
	}
	fullRequestURL := p.GetFullRequestURL(url, request.Model)

	headers := p.GetRequestHeaders()
	if p.APIToken != "" {
		headers["Authorization"] = "Bearer " + p.APIToken
	}
	if request.Stream {
		headers["Accept"] = "text/event-stream"
	}

	// 模型 ID 归一化与 chat（InvokeModel）路径共用一张表：别名→AWS ID、profile 前缀
	// 推断（GPT-5.6 任意区拼 global.）、显式前缀原样放行，避免双份映射漂移。
	resolvedModel := category.GetModelName(request.Model, p.Region)

	var req *http.Request
	// chat→responses 兼容路径（ConvertChat=true）时原始字节是 chat 格式，不可透传。
	if !request.ConvertChat {
		if patched, ok := p.patchResponsesPassThroughBody(resolvedModel); ok {
			req, errWithCode = p.NewRequestWithCustomParamsBytes(http.MethodPost, fullRequestURL, patched, headers, request.Model)
			if errWithCode != nil {
				return nil, errWithCode
			}
		}
	}

	// 回退：结构体序列化，model 归一化为 AWS 的 OpenAI 兼容端点 ID。
	if req == nil {
		copyRequest := *request
		copyRequest.Model = resolvedModel
		req, errWithCode = p.NewRequestWithCustomParams(http.MethodPost, fullRequestURL, &copyRequest, headers, request.Model)
		if errWithCode != nil {
			return nil, errWithCode
		}
	}

	if err := p.Sign(req); err != nil {
		return nil, common.StringErrorWrapperLocal(err.Error(), "sign_request_failed", http.StatusInternalServerError)
	}

	return req, nil
}

// patchResponsesPassThroughBody 读取 gin 缓存的原始 /v1/responses 请求体，
// 仅把 model 归一化为 AWS 端点 ID，其余一律按字节保留（未知字段 / 字段顺序 / 数值精度）。
// AWS 会拒绝它不支持的字段（如 background=true → 400），不做预清洗，原样让上游报错。
// 返回 (字节, true) 表示透传可用；(nil, false) 表示应回退结构体序列化。
func (p *BedrockProvider) patchResponsesPassThroughBody(resolvedModel string) ([]byte, bool) {
	// 必须看起来像 /v1/responses 原生请求（含 model 字段），否则放弃透传。
	out, ok := p.ReadNativeRawBody("model")
	if !ok {
		return nil, false
	}

	if resolvedModel != "" && gjson.GetBytes(out, "model").String() != resolvedModel {
		patched, err := sjson.SetBytes(out, "model", resolvedModel)
		if err != nil {
			return nil, false
		}
		out = patched
	}

	return out, true
}

// storeAWSUpstreamHeaders 过滤 AWS 指纹响应头并暂存到 gin.Context，供 relay 层透传写出，
// 受 FingerprintPassThroughEnabled 开关控制；与 aws_client.go 的 captureResponseMiddleware
// 对 SDK 路径的处理对齐（本路径走 HTTPRequester，middleware 不生效，需在此单独处理）。
func (p *BedrockProvider) storeAWSUpstreamHeaders(header http.Header) {
	if !config.FingerprintPassThroughEnabled || p.Context == nil {
		return
	}
	if headers := filterAWSResponseHeaders(header); headers != nil {
		p.Context.Set(config.GinPassThroughHeaders, headers)
	}
}
