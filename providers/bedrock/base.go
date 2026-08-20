package bedrock

import (
	"done-hub/common/requester"
	"done-hub/model"
	"done-hub/providers/base"
	"done-hub/providers/openai"
	"fmt"
	"net/http"
	"strings"

	"done-hub/providers/bedrock/category"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

type BedrockProviderFactory struct{}

// 创建 BedrockProvider
func (f BedrockProviderFactory) Create(channel *model.Channel) base.ProviderInterface {

	bedrockProvider := &BedrockProvider{
		BaseProvider: base.BaseProvider{
			Config:  getConfig(),
			Channel: channel,
			// chat（InvokeModel）路径仅借 Requester 复用 NewRequestWithCustomParams* 的
			// 请求体构造逻辑，实际发送走 bedrockruntime SDK；responses 路径则直接经
			// Requester 发 HTTP（AWS 的 /openai/v1/responses 返回标准 OpenAI 错误格式），
			// 故错误回调用 openai.RequestErrorHandle。
			Requester:       requester.NewHTTPRequester(channel.GetProxy(), openai.RequestErrorHandle),
			SupportResponse: true,
		},
	}

	getKeyConfig(bedrockProvider)

	return bedrockProvider
}

type BedrockProvider struct {
	base.BaseProvider
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	APIToken        string
	Category        *category.Category
	client          *bedrockruntime.Client
	// errBody 缓存最近一次上游错误响应的原始 body（由 captureResponseMiddleware 在
	// Deserialize 前 tee 出来）。用于在 SDK 反序列化失败（如中间层返回 HTML）时，
	// 让 awsErrorToOpenAI 拿到上游真实返回而非 SDK 的解析器噪声。
	errBody []byte
}

func getConfig() base.ProviderConfig {
	return base.ProviderConfig{
		BaseURL:         "https://bedrock-runtime.%s.amazonaws.com",
		ChatCompletions: "/model/%s/invoke",
		Responses:       "/openai/v1/responses",
	}
}

// GetFullRequestURL 拼接完整 URL（BaseURL 含 region 占位符）。仅 responses 等
// 走 HTTPRequester 的路径使用；chat（InvokeModel）路径由 SDK 自行构造 URL，不经这里。
func (p *BedrockProvider) GetFullRequestURL(requestURL string, _ string) string {
	baseURL := strings.TrimSuffix(p.GetBaseURL(), "/")
	return fmt.Sprintf(baseURL, p.Region) + requestURL
}

func (p *BedrockProvider) GetRequestHeaders() (headers map[string]string) {
	headers = make(map[string]string)
	p.CommonRequestHeaders(headers)
	headers["Accept"] = "*/*"

	return headers
}

func getKeyConfig(bedrock *BedrockProvider) {
	keys := strings.Split(bedrock.Channel.Key, "|")
	if len(keys) < 2 {
		return
	}
	bedrock.Region = keys[0]
	if len(keys) == 2 {
		bedrock.APIToken = keys[1]
		return
	}
	bedrock.AccessKeyID = keys[1]
	bedrock.SecretAccessKey = keys[2]
	if len(keys) == 4 && keys[3] != "" {
		bedrock.SessionToken = keys[3]
	}
}

// awsResponseHeaderExcluded 是禁止透传的响应头（传输层 / 由下游自行设置）。
// 透传这些会与 done-hub 自己写的响应头冲突或破坏分块传输。
//
// 注意：当前 filterAWSResponseHeaders 的白名单已限定为 x-amzn-* / apigw-requestid，
// 这些传输层头不会命中该白名单，故此排除集当前是冗余的防御性兜底——仅在未来放宽
// 白名单（如加入 x-amz-* 前缀）时才真正生效，防止误透传传输层头。
var awsResponseHeaderExcluded = map[string]struct{}{
	"content-length":    {},
	"content-type":      {},
	"content-encoding":  {},
	"transfer-encoding": {},
	"connection":        {},
	"keep-alive":        {},
}

// filterAWSResponseHeaders 从上游 Bedrock 响应头中挑出可透传给客户端的 AWS 指纹头，
// 目的是让 done-hub 中转的响应看起来像直连 AWS（携带 x-amzn-requestid /
// x-amzn-bedrock-input-token-count / x-amzn-bedrock-output-token-count / apigw-requestid 等）。
// 只透传 x-amzn- 前缀及 apigw-requestid，其余（包括 x-amz- 非 amzn 前缀）暂不透传，避免误伤。
func filterAWSResponseHeaders(src http.Header) http.Header {
	if len(src) == 0 {
		return nil
	}
	out := http.Header{}
	for name, values := range src {
		lower := strings.ToLower(name)
		if _, excluded := awsResponseHeaderExcluded[lower]; excluded {
			continue
		}
		if strings.HasPrefix(lower, "x-amzn-") || lower == "apigw-requestid" {
			for _, v := range values {
				out.Add(name, v)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
