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
		BaseURL:           "https://%saiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:%s",
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

func (p *VertexAIProvider) GetFullRequestURL(modelName string, other string) string {
	if p.Region == "global" {
		return fmt.Sprintf(p.GetBaseURL(), "", p.ProjectID, p.Region, modelName, other)
	}
	return fmt.Sprintf(p.GetBaseURL(), p.Region+"-", p.ProjectID, p.Region, modelName, other)
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
	cacheKey := fmt.Sprintf("%s:%s", TokenCacheKey, p.ProjectID)
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

func RequestErrorHandle(otherErr requester.HttpErrorHandler) requester.HttpErrorHandler {

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
		if err := json.Unmarshal(requestBody, vertexaiErrors); err == nil {
			if vertexaiError := vertexaiErrors.Error(); vertexaiError != nil {
				return errorHandle(vertexaiError)
			}
		} else {
			vertexaiError := &VertexaiError{}
			if err := json.Unmarshal(requestBody, vertexaiError); err == nil {
				return errorHandle(vertexaiError)
			}
		}

		return nil
	}
}

func errorHandle(vertexaiError *VertexaiError) *types.OpenAIError {
	if vertexaiError.Error.Message == "" {
		return nil
	}

	logger.SysError(fmt.Sprintf("VertexAI error: %s", utils.TruncateBase64InMessage(vertexaiError.Error.Message)))

	return &types.OpenAIError{
		Message: "VertexAI错误",
		Type:    "gemini_error",
		Param:   vertexaiError.Error.Status,
		Code:    vertexaiError.Error.Code,
	}
}
