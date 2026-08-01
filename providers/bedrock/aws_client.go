package bedrock

import (
	"bytes"
	"context"
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/requester"
	"done-hub/common/utils"
	"done-hub/types"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/smithy-go/auth/bearer"
	smithymiddleware "github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

const (
	// maxErrBodyCapture 缓冲错误响应体的字节上限。AWS 结构化错误体极小；
	// 上限只为防御中间层返回超大 HTML 的病态情况，缓冲后原样放回供 SDK 继续解析。
	maxErrBodyCapture = 1 << 20 // 1 MiB
	// errBodyLogLimit 写入日志时的截断长度，避免日志被整页 HTML 灌爆。
	errBodyLogLimit = 1000
)

// getAwsClient 懒构造 bedrockruntime.Client。
// 复用共享的 requester.HTTPClient（继承 relay_timeout / 连接池 / TLS 等全部配置）；
// 代理走 context 注入（见 invokeContext），因此这里不需要 transport-baked client。
func (p *BedrockProvider) getAwsClient() (*bedrockruntime.Client, error) {
	if p.client != nil {
		return p.client, nil
	}

	opts := bedrockruntime.Options{
		Region:     p.Region,
		HTTPClient: requester.HTTPClient,
		// 关闭 SDK 自带重试：done-hub 在 relay 层已有统一的多渠道重试/冷却逻辑，
		// 不需要 SDK 再对单渠道做指数退避（否则单次请求墙钟被放大）。
		Retryer:    aws.NopRetryer{},
		APIOptions: []func(*smithymiddleware.Stack) error{p.captureResponseMiddleware(), p.injectHeadersMiddleware()},
	}

	if p.APIToken != "" {
		opts.BearerAuthTokenProvider = bearer.StaticTokenProvider{Token: bearer.Token{Value: p.APIToken}}
	} else {
		opts.Credentials = aws.NewCredentialsCache(
			credentials.NewStaticCredentialsProvider(p.AccessKeyID, p.SecretAccessKey, p.SessionToken),
		)
	}

	p.client = bedrockruntime.New(opts)
	return p.client, nil
}

// invokeContext 返回调用 SDK 用的 context：以 gin 请求 context 为基（继承取消/超时），
// 并注入渠道代理地址，供共享 requester.HTTPClient 的 transport 读取。
func (p *BedrockProvider) invokeContext() context.Context {
	var base context.Context = context.Background()
	if p.Context != nil {
		// 沿用项目"客户端断开不影响上游请求"的设计理念（见 base.SetContext 的 WithoutCancel）：
		// 客户端提前断开时 gin request context 会被取消，若直接以它为 base，取消信号会透传给
		// AWS SDK，导致向上游的调用被中断（context canceled / status_code=0），且丢失计费与日志。
		// WithoutCancel 保留 context 中的值（trace 等），但屏蔽父级取消；墙钟由共享
		// HTTPClient.Timeout 兜底，不会无限挂起。Bedrock 走 SDK 不经 HTTPRequester，
		// 故 base.SetContext 对 Requester.Context 的 WithoutCancel 处理对本路径无效，需在此单独处理。
		base = context.WithoutCancel(p.Context.Request.Context())
	}
	return utils.SetProxy(p.Channel.GetProxy(), base)
}

// captureResponseMiddleware 在 Deserialize 阶段截获原始 *http.Response，做两件事：
//  1. 把 x-amzn-* / apigw-requestid 响应头存入 gin context，用于指纹保真透传
//     （让中转响应看起来像直连 AWS）。SDK 的类型化 Output 不暴露响应头，故需此 middleware。
//  2. 对 4xx/5xx 响应，在 SDK 反序列化前 tee 出原始 body 存入 p.errBody，并原样放回，
//     供 awsErrorToOpenAI 在解析失败（如中间层返回 HTML）时还原上游真实返回。
//
// 本 middleware 以 After 追加到 Deserialize 步骤，是该步骤的最内层——早于 SDK 的
// operation deserializer 执行，因此此时 body 尚未被消费，tee 是安全的。
func (p *BedrockProvider) captureResponseMiddleware() func(*smithymiddleware.Stack) error {
	return func(stack *smithymiddleware.Stack) error {
		return stack.Deserialize.Add(
			smithymiddleware.DeserializeMiddlewareFunc(
				"DoneHubCaptureResponseHeaders",
				func(ctx context.Context, in smithymiddleware.DeserializeInput, next smithymiddleware.DeserializeHandler) (
					out smithymiddleware.DeserializeOutput, md smithymiddleware.Metadata, err error) {
					// 每次请求进入先清空上一轮的残留，避免复用 provider 时串味。
					p.errBody = nil
					out, md, err = next.HandleDeserialize(ctx, in)
					if resp, ok := out.RawResponse.(*smithyhttp.Response); ok && resp != nil && resp.Response != nil {
						if resp.StatusCode >= http.StatusBadRequest {
							p.errBody = teeErrorBody(resp.Response)
						}
						if config.FingerprintPassThroughEnabled && p.Context != nil {
							if headers := filterAWSResponseHeaders(resp.Header); headers != nil {
								p.Context.Set(config.GinPassThroughHeaders, headers)
							}
						}
					}
					return out, md, err
				},
			),
			smithymiddleware.After,
		)
	}
}

// teeErrorBody 读取错误响应体（至多 maxErrBodyCapture 字节），并把等价的可重读 body
// 放回 resp，使后续 SDK 反序列化不受影响。返回读到的字节；读取失败返回 nil。
func teeErrorBody(resp *http.Response) []byte {
	if resp.Body == nil {
		return nil
	}
	limited := io.LimitReader(resp.Body, maxErrBodyCapture)
	buf, readErr := io.ReadAll(limited)
	// 无论是否读满上限，都用已读字节 + 剩余原 body 重组，保证 SDK 侧 body 完整可读。
	resp.Body = struct {
		io.Reader
		io.Closer
	}{
		Reader: io.MultiReader(bytes.NewReader(buf), resp.Body),
		Closer: resp.Body,
	}
	if readErr != nil && len(buf) == 0 {
		return nil
	}
	return buf
}

// injectHeadersMiddleware 在 Finalize 阶段（签名之前）把渠道自定义头
// （ModelHeaders / HeaderOverride，含可选的 User-Agent 覆盖）写入请求。
// 放在签名前：真实自定义头会进入 SigV4 canonical 参与签名；User-Agent 不在
// 签名范围内，覆盖它不会破坏签名。
func (p *BedrockProvider) injectHeadersMiddleware() func(*smithymiddleware.Stack) error {
	return func(stack *smithymiddleware.Stack) error {
		headers := p.customSDKHeaders()
		if len(headers) == 0 {
			return nil
		}
		return stack.Finalize.Insert(
			smithymiddleware.FinalizeMiddlewareFunc(
				"DoneHubInjectCustomHeaders",
				func(ctx context.Context, in smithymiddleware.FinalizeInput, next smithymiddleware.FinalizeHandler) (
					out smithymiddleware.FinalizeOutput, md smithymiddleware.Metadata, err error) {
					if req, ok := in.Request.(*smithyhttp.Request); ok {
						for k, v := range headers {
							req.Header.Set(k, v)
						}
					}
					return next.HandleFinalize(ctx, in)
				},
			),
			"Signing",
			smithymiddleware.Before,
		)
	}
}

// customSDKHeaders 收集需要注入 SDK 请求的渠道自定义头：
// 复用 CommonRequestHeaders（含 ModelHeaders / HeaderOverride），但剔除由 SDK 自己
// 管理的 Content-Type / Accept（这两个由 InvokeModelInput 决定，重复设置无意义）。
// 结果通常只含用户在渠道配置的自定义头（例如覆盖 User-Agent）。
func (p *BedrockProvider) customSDKHeaders() map[string]string {
	raw := make(map[string]string)
	p.CommonRequestHeaders(raw)

	out := make(map[string]string, len(raw))
	for k, v := range raw {
		switch http.CanonicalHeaderKey(k) {
		case "Content-Type", "Accept":
			continue
		}
		out[k] = v
	}
	return out
}

// awsErrorStatusCode 从 AWS SDK error 中提取 HTTP 状态码；返回 0 表示"根本没拿到上游 HTTP 响应"。
//   - smithy ResponseError 实现 HTTPStatusCode()：有响应时给真实码；客户端断开等场景其内嵌
//     Response.StatusCode 本就是 0，直接透出 0。
//   - DNS/TLS/连接重置/建连超时等被 smithy 包成 RequestSendError，它没有 HTTPStatusCode() 但有
//     ConnectionError()==true（AWS SDK 官方重试分类判连接层错误的同款判据）：归一成 0 哨兵。
//   - 其余（解析失败等非连接类）无从判断，兜底 500。
func awsErrorStatusCode(err error) int {
	var httpErr interface{ HTTPStatusCode() int }
	if errors.As(err, &httpErr) {
		return httpErr.HTTPStatusCode()
	}
	var connErr interface{ ConnectionError() bool }
	if errors.As(err, &connErr) && connErr.ConnectionError() {
		return 0
	}
	return http.StatusInternalServerError
}

// awsErrorToOpenAI 把 SDK error 映射为 done-hub 的 OpenAIErrorWithStatusCode。
//
// 分两类处理，避免"SDK 内部解析错误"污染面向客户端的 message 与渠道自动禁用判定：
//
//   - 结构化上游错误（smithy APIError，如 Bedrock 的 ValidationException/ThrottlingException）：
//     透传 AWS 的真实 message、error code / type，这是对客户端有意义的信息。
//   - 非结构化失败（反序列化失败 / 网络错误 / 中间层返回 HTML）：SDK 只会给出
//     "invalid character '<'..." 之类的解析器噪声。此时——
//     1) 把 captureResponseMiddleware tee 到的原始响应体截断记入日志，供定位中间层；
//     2) 面向客户端/禁用逻辑归一成稳定的 "bad response status code {code}"，与旧版 HTTP
//     路径一致，避免解析器噪声被 DisableChannelKeywords 误命中而永久禁用渠道。
func (p *BedrockProvider) awsErrorToOpenAI(err error) *types.OpenAIErrorWithStatusCode {
	statusCode := awsErrorStatusCode(err)

	// 结构化上游错误：透传 AWS 原文。
	var apiErr smithyAPIError
	if errors.As(err, &apiErr) {
		if msg := apiErr.ErrorMessage(); msg != "" {
			return &types.OpenAIErrorWithStatusCode{
				OpenAIError: types.OpenAIError{
					Message: msg,
					Type:    "Bedrock Error",
					Code:    apiErr.ErrorCode(),
				},
				StatusCode: statusCode,
			}
		}
	}

	// 非结构化失败：记录上游真实返回（若捕获到），面向下游归一成稳定 message。
	if len(p.errBody) > 0 {
		logger.SysError(fmt.Sprintf(
			"bedrock upstream non-JSON error: channel_id=%d status_code=%d body=%q",
			p.Channel.Id, statusCode, truncateForLog(p.errBody)))
	} else {
		logger.SysError(fmt.Sprintf(
			"bedrock upstream error (no body captured): channel_id=%d status_code=%d err=%v",
			p.Channel.Id, statusCode, err))
	}

	// status_code=0：连接层失败，根本没拿到上游 HTTP 响应（DNS/TLS/连接重置/建连超时/客户端断开）。
	// 判据见 awsErrorStatusCode（ResponseError.StatusCode==0 或 ConnectionError()==true）。
	// message / statusCode / code 对齐 common.ErrorWrapper 的网络失败口径：
	//   - 原始错误已进日志（上方），对外归一成"请求上游地址失败" + 白名单安全短词（如 i/o timeout）；
	//   - StatusCode 用 500（0 不是合法 HTTP 码，且 http_requester 网络失败统一传 500）；
	//   - Code 用 http_request_failed（同 http_requester 网络失败路径）：语义上根本没有 response/状态码，
	//     且能避开 relay.FilterOpenAIErr 对 bad_response_status_code 的文案覆盖（回写成 "bad response status code 0"）。
	if statusCode == 0 {
		message := "请求上游地址失败"
		if hint := common.SafeNetErrHint(err.Error()); hint != "" {
			message += ": " + hint
		}
		return &types.OpenAIErrorWithStatusCode{
			OpenAIError: types.OpenAIError{
				Message: message,
				Type:    "upstream_error",
				Code:    "http_request_failed",
			},
			StatusCode: http.StatusInternalServerError,
		}
	}

	return &types.OpenAIErrorWithStatusCode{
		OpenAIError: types.OpenAIError{
			Message: fmt.Sprintf("bad response status code %d", statusCode),
			Type:    "upstream_error",
			Code:    "bad_response_status_code",
			Param:   strconv.Itoa(statusCode),
		},
		StatusCode: statusCode,
	}
}

// smithyAPIError 是 smithy APIError 的最小接口（避免额外导入 smithy 根包）。
type smithyAPIError interface {
	error
	ErrorCode() string
	ErrorMessage() string
}

// truncateForLog 把响应体截断到 errBodyLogLimit 字节，避免整页 HTML 灌爆日志。
func truncateForLog(b []byte) string {
	if len(b) <= errBodyLogLimit {
		return string(b)
	}
	return string(b[:errBodyLogLimit]) + "...(truncated)"
}
