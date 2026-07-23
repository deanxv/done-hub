package bedrockmessages

import (
	"bytes"
	"crypto/sha256"
	"done-hub/common/requester"
	"done-hub/model"
	"done-hub/providers/base"
	"done-hub/providers/bedrock/sigv4"
	"done-hub/providers/claude"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type BedrockMessagesProviderFactory struct{}

// Create 创建 BedrockMessagesProvider（Claude in Amazon Bedrock，原生 Messages 端点）
func (f BedrockMessagesProviderFactory) Create(channel *model.Channel) base.ProviderInterface {
	p := &BedrockMessagesProvider{
		BaseProvider: base.BaseProvider{
			Config:    getConfig(),
			Channel:   channel,
			Requester: requester.NewHTTPRequester(channel.GetProxy(), claude.RequestErrorHandle),
		},
	}
	getKeyConfig(p)
	return p
}

type BedrockMessagesProvider struct {
	base.BaseProvider
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	APIToken        string
}

func getConfig() base.ProviderConfig {
	return base.ProviderConfig{
		// mantle 端点：host 含 region 占位符，path 固定 /anthropic/v1/messages（不含 model）
		BaseURL:         "https://bedrock-mantle.%s.api.aws",
		ChatCompletions: "/anthropic/v1/messages",
	}
}

// getKeyConfig 解析 channel.Key，支持两种格式：
//   - SigV4:  Region|AccessKeyID|SecretAccessKey|SessionToken（SessionToken 可空）
//   - Bearer: Region|BearerToken
func getKeyConfig(p *BedrockMessagesProvider) {
	keys := strings.Split(p.Channel.Key, "|")
	if len(keys) < 2 {
		return
	}
	p.Region = keys[0]
	if len(keys) == 2 {
		p.APIToken = keys[1]
		return
	}
	p.AccessKeyID = keys[1]
	p.SecretAccessKey = keys[2]
	if len(keys) == 4 && keys[3] != "" {
		p.SessionToken = keys[3]
	}
}

// GetFullRequestURL 拼接完整 URL。modelName 不参与 URL（mantle 的 model 放 body）。
func (p *BedrockMessagesProvider) GetFullRequestURL(requestURL string, _ string) string {
	baseURL := strings.TrimSuffix(p.GetBaseURL(), "/")
	return fmt.Sprintf(baseURL, p.Region) + requestURL
}

// GetRequestHeaders 构建请求头。anthropic-version / anthropic-beta 走 HTTP header（与直连一致）。
// bearer 模式用 x-api-key；SigV4 模式的 Authorization 由 Sign() 生成。
func (p *BedrockMessagesProvider) GetRequestHeaders() (headers map[string]string) {
	headers = make(map[string]string)
	p.CommonRequestHeaders(headers)

	anthropicVersion := ""
	if p.Context != nil {
		anthropicVersion = p.Context.Request.Header.Get("anthropic-version")
	}
	if anthropicVersion == "" {
		anthropicVersion = defaultAnthropicVersion
	}
	headers["anthropic-version"] = anthropicVersion

	// 透传客户端 anthropic-beta（仅在 ModelHeaders 未自定义时）
	if _, exists := headers["anthropic-beta"]; !exists && p.Context != nil {
		if anthropicBeta := p.Context.Request.Header.Get("anthropic-beta"); anthropicBeta != "" {
			headers["anthropic-beta"] = anthropicBeta
		}
	}

	// bearer token 模式：走 x-api-key（不签名）
	if p.APIToken != "" {
		headers["x-api-key"] = p.APIToken
	}

	headers["Accept"] = "*/*"
	return headers
}

// Sign 对请求做 SigV4 签名（service=bedrock-mantle）。bearer token 模式跳过。
func (p *BedrockMessagesProvider) Sign(req *http.Request) error {
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
		sigv4.WithRegionService(p.Region, bedrockMantleService),
	)
	if err != nil {
		return err
	}

	reqBodyHashHex := fmt.Sprintf("%x", sha256.Sum256(body))
	sig.Sign(req, reqBodyHashHex, sigv4.NewTime(time.Now()))
	return nil
}

// filterAWSResponseHeaders 从上游响应头挑出可透传的 AWS 指纹头（x-amzn-* / apigw-requestid）。
func filterAWSResponseHeaders(src http.Header) http.Header {
	if len(src) == 0 {
		return nil
	}
	out := http.Header{}
	for name, values := range src {
		lower := strings.ToLower(name)
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
