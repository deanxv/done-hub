package vertexai

import (
	"bytes"
	"context"
	"crypto/rand"
	"done-hub/common"
	"done-hub/common/cache"
	"done-hub/common/logger"
	"done-hub/common/requester"
	"done-hub/common/utils"
	"done-hub/model"
	"done-hub/providers/base"
	"done-hub/providers/vertexai/category"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const TokenCacheKey = "api_token:vertexai"
const defaultScope = "https://www.googleapis.com/auth/cloud-platform"

type VertexAIProviderFactory struct{}

// 创建 VertexAIProvider
func (f VertexAIProviderFactory) Create(channel *model.Channel) base.ProviderInterface {
	proxyAddr := channel.GetProxy()

	vertexAIProvider := &VertexAIProvider{
		BaseProvider: base.BaseProvider{
			Config:    getConfig(),
			Channel:   channel,
			Requester: requester.NewHTTPRequester(proxyAddr, nil),
		},
	}

	getKeyConfig(vertexAIProvider)
	return vertexAIProvider
}

type VertexAIProvider struct {
	base.BaseProvider
	Region    string
	ProjectID string
	Category  *category.Category
}

func getConfig() base.ProviderConfig {
	return base.ProviderConfig{
		// 占位个数与顺序见 vertexURLPlaceholders
		BaseURL:           "https://%s/v1/projects/%s/locations/%s/publishers/%s/models/%s:%s",
		ChatCompletions:   "/",
		ImagesGenerations: "/predict",
	}
}

func getKeyConfig(vertexAI *VertexAIProvider) {
	keys := strings.Split(vertexAI.Channel.Other, "|")
	if len(keys) < 2 {
		return
	}

	vertexAI.ProjectID = keys[len(keys)-1]

	regions := keys[:len(keys)-1]
	if len(regions) == 0 {
		return
	}

	randomIndex, err := rand.Int(rand.Reader, big.NewInt(int64(len(regions))))
	if err != nil {
		// 如果随机数生成失败，使用第一个region作为fallback
		logger.SysError("Failed to generate random number for region selection: " + err.Error())
		vertexAI.Region = regions[0]
		return
	}

	vertexAI.Region = regions[randomIndex.Int64()]
}

// vertexHost 按 region 选择 Vertex AI 服务域名：
//   - global           -> aiplatform.googleapis.com
//   - us / eu 多区域   -> aiplatform.{us|eu}.rep.googleapis.com
//   - 其他具体区域     -> {region}-aiplatform.googleapis.com
func vertexHost(region string) string {
	switch region {
	case "global":
		return "aiplatform.googleapis.com"
	case "us", "eu":
		return "aiplatform." + region + ".rep.googleapis.com"
	default:
		return region + "-aiplatform.googleapis.com"
	}
}

// vertexURLPlaceholders 是 BaseURL 模板需要的 %s 个数：host、projectID、region、publisher、model、action
const vertexURLPlaceholders = 6

// GetFullRequestURL 按当前 Category 的 publisher 构造请求地址；
// Category 未设置时（如 imagen 图片生成）默认 google。
func (p *VertexAIProvider) GetFullRequestURL(modelName string, other string) string {
	publisher := category.PublisherGoogle
	if p.Category != nil && p.Category.Publisher != "" {
		publisher = p.Category.Publisher
	}
	tpl := p.urlTemplate()
	if tpl == "" {
		return ""
	}
	return fmt.Sprintf(tpl, vertexHost(p.Region), p.ProjectID, p.Region, publisher, modelName, other)
}

// invalidBaseURLWarned 记录已经为不合规 base_url 模板告警过的渠道，避免每个请求都刷一条日志。
// 渠道改回正确配置后（计数匹配）不再经过告警分支，无需清理。
var invalidBaseURLWarned sync.Map

// urlTemplate 返回 URL 模板。渠道自定义 base_url 的 %s 个数不等于 vertexURLPlaceholders 时
// （典型是旧版 5 占位符、publisher 写死的模板）返回空串，让调用方以 invalid_vertexai_config 失败，
// 而不是静默回退内置模板——配了代理 base_url 的渠道若被静默回退，流量会绕过代理直连 Google。
func (p *VertexAIProvider) urlTemplate() string {
	tpl := p.GetBaseURL()
	if strings.Count(tpl, "%s") == vertexURLPlaceholders {
		return tpl
	}
	if _, warned := invalidBaseURLWarned.LoadOrStore(p.Channel.Id, struct{}{}); !warned {
		logger.SysError(fmt.Sprintf("vertexai channel %d: custom base_url %q needs %d %%s placeholders (host/project/region/publisher/model/action), request rejected", p.Channel.Id, tpl, vertexURLPlaceholders))
	}
	return ""
}

func (p *VertexAIProvider) GetRequestHeaders() (headers map[string]string) {
	headers, _ = p.getRequestHeadersInternal()
	return headers
}

// getRequestHeadersInternal 内部方法，返回请求头和错误信息
func (p *VertexAIProvider) getRequestHeadersInternal() (headers map[string]string, err error) {
	headers = make(map[string]string)
	p.CommonRequestHeaders(headers)

	token, err := p.GetToken()
	if err != nil {
		logger.SysError("Failed to get token: " + err.Error())
		return nil, err
	}

	headers["Authorization"] = "Bearer " + token
	return headers, nil
}

// handleTokenError 处理token获取失败的错误，检查是否匹配禁用通道关键词
func (p *VertexAIProvider) handleTokenError(err error) *types.OpenAIErrorWithStatusCode {
	errMsg := err.Error()

	// 检查是否匹配禁用通道关键词
	if common.DisableChannelKeywordsInstance.IsContains(errMsg) {
		// 匹配关键词，返回非LocalError，允许重试
		return common.StringErrorWrapper(errMsg, "vertexai_token_error", http.StatusInternalServerError)
	} else {
		// 不匹配关键词，返回LocalError，保持原有行为
		return common.StringErrorWrapperLocal(errMsg, "vertexai_token_error", http.StatusInternalServerError)
	}
}

func (p *VertexAIProvider) GetToken() (string, error) {
	cacheKey := fmt.Sprintf("%s:%d", TokenCacheKey, p.Channel.Id)
	token, err := cache.GetCache[string](cacheKey)
	if err != nil {
		logger.SysError("Failed to get token from cache: " + err.Error())
	}

	if token != "" {
		return token, nil
	}

	config, err := google.JWTConfigFromJSON([]byte(p.Channel.Key), defaultScope)
	if err != nil {
		return "", fmt.Errorf("failed to parse credentials: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	httpClient, err := utils.NewProxyHTTPClient(p.Channel.GetProxy())
	if err != nil {
		return "", fmt.Errorf("failed to create proxy http client: %w", err)
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, httpClient)

	tok, err := config.TokenSource(ctx).Token()
	if err != nil {
		return "", fmt.Errorf("failed to get access token: %w", err)
	}

	duration := time.Until(tok.Expiry) - 5*time.Minute
	if duration <= 0 {
		duration = 30 * time.Second
	}
	cache.SetCache(cacheKey, tok.AccessToken, duration)

	return tok.AccessToken, nil
}

// RequestErrorHandle 先交给分类自己的错误处理（Claude / Gemini 错误体），
// 再兜底解析 Vertex 自身的 {"error":{...}} 结构。projectID 用于脱敏错误信息中的项目路径。
func RequestErrorHandle(otherErr requester.HttpErrorHandler, projectID string) requester.HttpErrorHandler {

	return func(resp *http.Response) *types.OpenAIError {
		requestBody, _ := io.ReadAll(resp.Body)
		resp.Body = io.NopCloser(bytes.NewBuffer(requestBody))

		if otherErr != nil {
			err := otherErr(resp)
			if err != nil {
				return err
			}
		}
		vertexaiErrors := &VertexaiErrors{}
		if err := json.Unmarshal(requestBody, vertexaiErrors); err == nil && len(*vertexaiErrors) > 0 {
			if vertexaiError := vertexaiErrors.Error(); vertexaiError != nil {
				return errorHandle(vertexaiError, projectID)
			}
		} else {
			vertexaiError := &VertexaiError{}
			if err := json.Unmarshal(requestBody, vertexaiError); err == nil {
				return errorHandle(vertexaiError, projectID)
			}
		}

		return nil
	}
}

// errorHandle 把 Vertex 的错误体转成 OpenAI 错误。与 Gemini / Claude 渠道一致，
// 把上游真实 message 透出给调用方（测速界面、客户端）而不是固定文案，便于定位
// 模型未启用 / 区域不支持 / 权限不足等问题；敏感信息处理：
//   - 项目 ID 替换为占位（Vertex 报错常含 projects/{id}/locations/... 路径）
//   - base64 数据截断，避免错误体过长
//
// URL / host 级脱敏由 relay.FilterOpenAIErr 与测速侧的 utils.MaskSensitiveInfo 统一兜底。
func errorHandle(vertexaiError *VertexaiError, projectID string) *types.OpenAIError {
	if vertexaiError.Error.Message == "" {
		return nil
	}

	message := utils.TruncateBase64InMessage(vertexaiError.Error.Message)
	if projectID != "" {
		message = strings.ReplaceAll(message, projectID, "xxxxx")
	}

	logger.SysError(fmt.Sprintf("VertexAI error: %s", message))

	return &types.OpenAIError{
		Message: message,
		Type:    "vertexai_error",
		Param:   vertexaiError.Error.Status,
		Code:    vertexaiError.Error.Code,
	}
}
