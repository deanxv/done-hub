package bedrock

import (
	"bytes"
	"crypto/sha256"
	"done-hub/common/requester"
	"done-hub/model"
	"done-hub/providers/base"
	"done-hub/types"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"done-hub/providers/bedrock/category"
	"done-hub/providers/bedrock/sigv4"
)

type BedrockProviderFactory struct{}

// 创建 BedrockProvider
func (f BedrockProviderFactory) Create(channel *model.Channel) base.ProviderInterface {

	bedrockProvider := &BedrockProvider{
		BaseProvider: base.BaseProvider{
			Config:    getConfig(),
			Channel:   channel,
			Requester: requester.NewHTTPRequester(channel.GetProxy(), requestErrorHandle),
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
}

func getConfig() base.ProviderConfig {
	return base.ProviderConfig{
		BaseURL:         "https://bedrock-runtime.%s.amazonaws.com",
		ChatCompletions: "/model/%s/invoke",
	}
}

// 请求错误处理
func requestErrorHandle(resp *http.Response) *types.OpenAIError {
	bedrockError := &BedrockError{}
	err := json.NewDecoder(resp.Body).Decode(bedrockError)
	if err != nil {
		return nil
	}

	return errorHandle(bedrockError)
}

// 错误处理
func errorHandle(bedrockError *BedrockError) *types.OpenAIError {
	if bedrockError.Message == "" {
		return nil
	}
	return &types.OpenAIError{
		Message: bedrockError.Message,
		Type:    "Bedrock Error",
	}
}

func (p *BedrockProvider) GetFullRequestURL(requestURL string, modelName string) string {
	baseURL := strings.TrimSuffix(p.GetBaseURL(), "/")

	return fmt.Sprintf(baseURL+requestURL, p.Region, modelName)
}

func (p *BedrockProvider) GetRequestHeaders() (headers map[string]string) {
	headers = make(map[string]string)
	p.CommonRequestHeaders(headers)
	if p.APIToken != "" {
		headers["Authorization"] = "Bearer " + p.APIToken
	}
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

func (p *BedrockProvider) Sign(req *http.Request) error {
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
	if p.APIToken != "" {
		return nil
	}
	sig, err := sigv4.New(sigv4.WithCredential(p.AccessKeyID, p.SecretAccessKey, p.SessionToken), sigv4.WithRegionService(p.Region, awsService))
	if err != nil {
		return err
	}

	reqBodyHashHex := fmt.Sprintf("%x", sha256.Sum256(body))
	sig.Sign(req, reqBodyHashHex, sigv4.NewTime(time.Now()))

	return nil
}
