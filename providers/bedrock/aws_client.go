package bedrock

import (
	"context"
	"done-hub/common/config"
	"done-hub/common/requester"
	"done-hub/common/utils"
	"done-hub/types"
	"errors"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/smithy-go/auth/bearer"
	smithymiddleware "github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
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
		base = p.Context.Request.Context()
	}
	return utils.SetProxy(p.Channel.GetProxy(), base)
}

// captureResponseMiddleware 在 Deserialize 阶段截获原始 *http.Response，
// 把 x-amzn-* / apigw-requestid 响应头存入 gin context，用于指纹保真透传
// （让中转响应看起来像直连 AWS）。SDK 的类型化 Output 不暴露响应头，故需此 middleware。
func (p *BedrockProvider) captureResponseMiddleware() func(*smithymiddleware.Stack) error {
	return func(stack *smithymiddleware.Stack) error {
		return stack.Deserialize.Add(
			smithymiddleware.DeserializeMiddlewareFunc(
				"DoneHubCaptureResponseHeaders",
				func(ctx context.Context, in smithymiddleware.DeserializeInput, next smithymiddleware.DeserializeHandler) (
					out smithymiddleware.DeserializeOutput, md smithymiddleware.Metadata, err error) {
					out, md, err = next.HandleDeserialize(ctx, in)
					if !config.FingerprintPassThroughEnabled || p.Context == nil {
						return out, md, err
					}
					if resp, ok := out.RawResponse.(*smithyhttp.Response); ok && resp != nil && resp.Response != nil {
						if headers := filterAWSResponseHeaders(resp.Header); headers != nil {
							p.Context.Set(config.GinPassThroughHeaders, headers)
						}
					}
					return out, md, err
				},
			),
			smithymiddleware.After,
		)
	}
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

// awsErrorStatusCode 从 AWS SDK error 中提取 HTTP 状态码。
func awsErrorStatusCode(err error) int {
	var httpErr interface{ HTTPStatusCode() int }
	if errors.As(err, &httpErr) {
		return httpErr.HTTPStatusCode()
	}
	return http.StatusInternalServerError
}

// awsErrorToOpenAI 把 SDK error 映射为 done-hub 的 OpenAIErrorWithStatusCode，
// 复用上游 Bedrock 错误信息（若能解析）。
func awsErrorToOpenAI(err error) *types.OpenAIErrorWithStatusCode {
	statusCode := awsErrorStatusCode(err)
	message := err.Error()
	var apiErr interface{ ErrorMessage() string }
	if errors.As(err, &apiErr) {
		if msg := apiErr.ErrorMessage(); msg != "" {
			message = msg
		}
	}
	return &types.OpenAIErrorWithStatusCode{
		OpenAIError: types.OpenAIError{
			Message: message,
			Type:    "Bedrock Error",
		},
		StatusCode: statusCode,
	}
}
