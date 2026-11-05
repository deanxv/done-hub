package bedrockmessages

import (
	"bytes"
	"crypto/sha256"
	"done-hub/common/logger"
	"done-hub/common/requester"
	"done-hub/model"
	"done-hub/providers/base"
	"done-hub/providers/bedrock/awscreds"
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
	// RoleArn 非空时走 STS AssumeRole：用 AK/SK 换临时凭证后再签名（见 awscreds 包）。
	RoleArn  string
	APIToken string
}

func getConfig() base.ProviderConfig {
	return base.ProviderConfig{
		// mantle 端点：host 含 region 占位符，path 固定 /anthropic/v1/messages（不含 model）
		BaseURL:         "https://bedrock-mantle.%s.api.aws",
		ChatCompletions: "/anthropic/v1/messages",
	}
}

// getKeyConfig 解析 channel.Key，格式见 awscreds.ParseKey。
func getKeyConfig(p *BedrockMessagesProvider) {
	cfg, ok := awscreds.ParseKey(p.Channel.Key)
	if !ok {
		// 段数不符合约定（不足、超过五段，或五段时第五段不是 RoleArn）时不能静默吞掉：
		// 否则渠道看起来配置成功，实际区域/凭证全空，请求只会以难排查的签名错误告终。
		logger.SysError(fmt.Sprintf("[%s] invalid aws key format, expect Region|AK|SK[|SessionToken[|RoleArn]] or Region|BearerToken", p.Channel.Name))
		return
	}
	p.Region = cfg.Region
	p.APIToken = cfg.APIToken
	p.AccessKeyID = cfg.AccessKeyID
	p.SecretAccessKey = cfg.SecretAccessKey
	p.SessionToken = cfg.SessionToken
	p.RoleArn = cfg.RoleArn
}

// credsConfig 组装 awscreds 所需的凭证配置。
func (p *BedrockMessagesProvider) credsConfig() awscreds.Config {
	return awscreds.Config{
		Region:          p.Region,
		AccessKeyID:     p.AccessKeyID,
		SecretAccessKey: p.SecretAccessKey,
		SessionToken:    p.SessionToken,
		RoleArn:         p.RoleArn,
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

	creds, err := awscreds.Retrieve(p.credsConfig(), p.Channel.GetProxy())
	if err != nil {
		return fmt.Errorf("aws assume role failed: %w", err)
	}

	sig, err := sigv4.New(
		sigv4.WithCredential(creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken),
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
