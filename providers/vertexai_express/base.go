package vertexai_express

import (
	"done-hub/common/requester"
	"done-hub/model"
	"done-hub/providers/base"
	"done-hub/providers/gemini"
	"done-hub/providers/openai"
	"done-hub/types"
	"fmt"
	"net/http"
	"strings"
)

type VertexAIExpressProviderFactory struct{}

// 创建 VertexAIExpressProvider
func (f VertexAIExpressProviderFactory) Create(channel *model.Channel) base.ProviderInterface {
	config := getConfig()
	// 从 Other 字段解析 Region 和 ProjectID
	region, projectID := parseOtherConfig(channel.Other)
	if region == "" {
		region = "us-central1"
	}

	// 动态设置 BaseURL
	config.BaseURL = fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1beta1/projects/%s/locations/%s/publishers/google/models",
		region, projectID, region)

	// 处理代理
	proxyAddr := ""
	if channel.Proxy != nil {
		proxyAddr = *channel.Proxy
	}

	return &VertexAIExpressProvider{
		GeminiProvider: gemini.GeminiProvider{
			OpenAIProvider: openai.OpenAIProvider{
				BaseProvider: base.BaseProvider{
					Config:    config,
					Channel:   channel,
					Requester: requester.NewHTTPRequester(proxyAddr, gemini.RequestErrorHandle(channel.Key)),
				},
				SupportStreamOptions: true,
			},
			UseOpenaiAPI:     false,
			UseCodeExecution: false,
		},
		Region:    region,
		ProjectID: projectID,
	}
}

type VertexAIExpressProvider struct {
	gemini.GeminiProvider
	Region    string
	ProjectID string
}

func getConfig() base.ProviderConfig {
	return base.ProviderConfig{
		BaseURL:           "", // 动态设置
		ChatCompletions:   "/",
		ModelList:         "/models",
		ImagesGenerations: "1",
	}
}

// parseOtherConfig 解析 Other 字段，格式为 "Region|ProjectID"
func parseOtherConfig(other string) (region, projectID string) {
	parts := strings.Split(other, "|")
	if len(parts) >= 2 {
		region = parts[0]
		projectID = parts[1]
	} else if len(parts) == 1 {
		// 只有一个值时作为 ProjectID
		projectID = parts[0]
	}
	return
}

// GetFullRequestURL 构建完整的请求 URL
func (p *VertexAIExpressProvider) GetFullRequestURL(requestURL string, modelName string) string {
	baseURL := strings.TrimSuffix(p.GetBaseURL(), "/")

	// 构建 URL: baseURL/modelName:action?key=apiKey
	actionURL := requestURL
	if actionURL == "" {
		actionURL = "generateContent"
	}

	fullURL := fmt.Sprintf("%s/%s:%s?key=%s", baseURL, modelName, actionURL, p.Channel.Key)
	return fullURL
}

// GetRequestHeaders 获取请求头
func (p *VertexAIExpressProvider) GetRequestHeaders() (headers map[string]string) {
	headers = make(map[string]string)
	p.CommonRequestHeaders(headers)
	// API Key 通过 URL 参数传递，不需要在 header 中设置
	return headers
}

// RequestErrorHandle 请求错误处理
func RequestErrorHandle(resp *http.Response) *types.OpenAIError {
	return gemini.RequestErrorHandle("")(resp)
}

