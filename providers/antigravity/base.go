package antigravity

import (
	"bytes"
	"context"
	"done-hub/common/cache"
	"done-hub/common/logger"
	"done-hub/common/requester"
	"done-hub/model"
	"done-hub/providers/base"
	"done-hub/providers/gemini"
	"done-hub/providers/openai"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const TokenCacheKey = "api_token:antigravity"

type AntigravityProviderFactory struct{}

// Create 创建 AntigravityProvider
func (f AntigravityProviderFactory) Create(channel *model.Channel) base.ProviderInterface {
	provider := &AntigravityProvider{
		GeminiProvider: gemini.GeminiProvider{
			OpenAIProvider: openai.OpenAIProvider{
				BaseProvider: base.BaseProvider{
					Config:    getConfig("https://daily-cloudcode-pa.googleapis.com"),
					Channel:   channel,
					Requester: requester.NewHTTPRequester(*channel.Proxy, RequestErrorHandle("")),
				},
				SupportStreamOptions: true,
			},
		},
	}

	// 解析配置
	parseAntigravityConfig(provider)

	// 更新 RequestErrorHandle 使用实际的 token
	if provider.Credentials != nil {
		provider.Requester = requester.NewHTTPRequester(*channel.Proxy, RequestErrorHandle(provider.Credentials.AccessToken))
	}

	return provider
}

// parseAntigravityConfig 解析 Antigravity 配置
func parseAntigravityConfig(provider *AntigravityProvider) {
	channel := provider.Channel

	// 默认配置
	endpoint := "https://daily-cloudcode-pa.googleapis.com"

	// 尝试从 Plugin 中获取配置
	if channel.Plugin != nil {
		plugin := channel.Plugin.Data()
		if antigravityConfig, ok := plugin["antigravity"]; ok {
			if epVal, ok := antigravityConfig["endpoint"]; ok {
				if ep, ok := epVal.(string); ok && ep != "" {
					endpoint = ep
				}
			}
		}
	}

	provider.Endpoint = endpoint
	provider.Config = getConfig(endpoint)

	// 尝试解析完整的 OAuth2 凭证（优先）
	if channel.Key != "" {
		// 尝试解析为 JSON 格式的完整凭证
		creds, err := FromJSON(channel.Key)
		if err == nil && creds.ProjectID != "" {
			// 成功解析为完整凭证
			provider.Credentials = creds
			provider.ProjectID = creds.ProjectID
			return
		}

		// 尝试解析为简单格式: project_id|access_token
		parts := strings.SplitN(channel.Key, "|", 2)
		if len(parts) == 2 {
			provider.ProjectID = parts[0]
			provider.Credentials = &OAuth2Credentials{
				AccessToken: parts[1],
				ProjectID:   parts[0],
			}
			return
		}
	}

	// 从 Plugin 中获取配置（兼容旧版本）
	if channel.Plugin != nil {
		plugin := channel.Plugin.Data()
		if antigravityConfig, ok := plugin["antigravity"]; ok {
			projectID := ""
			accessToken := ""

			if pidVal, ok := antigravityConfig["project_id"]; ok {
				if pid, ok := pidVal.(string); ok && pid != "" {
					projectID = pid
				}
			}
			if tokenVal, ok := antigravityConfig["access_token"]; ok {
				if token, ok := tokenVal.(string); ok && token != "" {
					accessToken = token
				}
			}

			if projectID != "" && accessToken != "" {
				provider.ProjectID = projectID
				provider.Credentials = &OAuth2Credentials{
					AccessToken: accessToken,
					ProjectID:   projectID,
				}
			}
		}
	}
}

type AntigravityProvider struct {
	gemini.GeminiProvider
	Endpoint    string
	ProjectID   string
	Credentials *OAuth2Credentials // OAuth2 凭证（包含 refresh_token）
}

func getConfig(endpoint string) base.ProviderConfig {
	if endpoint == "" {
		endpoint = "https://daily-cloudcode-pa.googleapis.com"
	}
	return base.ProviderConfig{
		BaseURL:           endpoint,
		ChatCompletions:   "/v1internal/chat/completions",
		ModelList:         "/models",
		ImagesGenerations: "1",
	}
}

// RequestErrorHandle 请求错误处理
func RequestErrorHandle(token string) requester.HttpErrorHandler {
	return func(resp *http.Response) *types.OpenAIError {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil
		}
		resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		geminiError := &gemini.GeminiErrorResponse{}
		resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		if err := json.NewDecoder(resp.Body).Decode(geminiError); err == nil {
			openAIError := errorHandle(geminiError, token)

			// 解析 429 错误的响应体中的冻结时间
			if openAIError != nil && geminiError.ErrorInfo != nil && geminiError.ErrorInfo.Code == http.StatusTooManyRequests {
				for _, detail := range geminiError.ErrorInfo.Details {
					if detail.Metadata != nil {
						if quotaResetDelay, ok := detail.Metadata["quotaResetDelay"].(string); ok && quotaResetDelay != "" {
							if duration, parseErr := time.ParseDuration(quotaResetDelay); parseErr == nil {
								resetTimestamp := time.Now().Unix() + int64(duration.Seconds())
								openAIError.RateLimitResetAt = resetTimestamp
								logger.SysLog(fmt.Sprintf("[Antigravity] Rate limit detected, quota reset delay: %s, reset at: %s",
									quotaResetDelay, time.Unix(resetTimestamp, 0).Format(time.RFC3339)))
								break
							}
						}
					}
				}
			}

			return openAIError
		}

		geminiErrors := &gemini.GeminiErrors{}
		if err := json.Unmarshal(bodyBytes, geminiErrors); err == nil {
			openAIError := errorHandle(geminiErrors.Error(), token)

			if openAIError != nil && geminiErrors.Error() != nil && geminiErrors.Error().ErrorInfo != nil && geminiErrors.Error().ErrorInfo.Code == http.StatusTooManyRequests {
				for _, detail := range geminiErrors.Error().ErrorInfo.Details {
					if detail.Metadata != nil {
						if quotaResetDelay, ok := detail.Metadata["quotaResetDelay"].(string); ok && quotaResetDelay != "" {
							if duration, parseErr := time.ParseDuration(quotaResetDelay); parseErr == nil {
								resetTimestamp := time.Now().Unix() + int64(duration.Seconds())
								openAIError.RateLimitResetAt = resetTimestamp
								logger.SysLog(fmt.Sprintf("[Antigravity] Rate limit detected, quota reset delay: %s, reset at: %s",
									quotaResetDelay, time.Unix(resetTimestamp, 0).Format(time.RFC3339)))
								break
							}
						}
					}
				}
			}

			return openAIError
		}

		return nil
	}
}

// errorHandle 错误处理
func errorHandle(geminiError *gemini.GeminiErrorResponse, token string) *types.OpenAIError {
	if geminiError.ErrorInfo == nil || geminiError.ErrorInfo.Message == "" {
		return nil
	}

	cleaningError(geminiError.ErrorInfo, token)

	return &types.OpenAIError{
		Message: geminiError.ErrorInfo.Message,
		Type:    "antigravity_error",
		Param:   geminiError.ErrorInfo.Status,
		Code:    geminiError.ErrorInfo.Code,
	}
}

func cleaningError(errorInfo *gemini.GeminiError, token string) {
	if token == "" {
		return
	}
	message := strings.Replace(errorInfo.Message, token, "xxxxx", 1)
	message = truncateBase64InMessage(message)
	errorInfo.Message = message
}

// truncateBase64InMessage 截断错误消息中的 base64 数据
func truncateBase64InMessage(message string) string {
	const maxBase64Length = 50

	result := message
	offset := 0

	for {
		idx := strings.Index(result[offset:], ";base64,")
		if idx == -1 {
			break
		}

		actualIdx := offset + idx
		start := actualIdx + 8

		end := start
		for end < len(result) && isBase64Char(result[end]) {
			end++
		}

		if end-start > maxBase64Length {
			result = result[:start+maxBase64Length] + "...[truncated]" + result[end:]
			offset = start + maxBase64Length + len("...[truncated]")
		} else {
			offset = end
		}
	}

	return result
}

// isBase64Char 检查字符是否是 base64 字符
func isBase64Char(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '='
}

func (p *AntigravityProvider) GetFullRequestURL(requestURL string, modelName string) string {
	baseURL := strings.TrimSuffix(p.GetBaseURL(), "/")
	// Antigravity 使用内部 API 格式
	return fmt.Sprintf("%s/v1internal:%s", baseURL, requestURL)
}

func (p *AntigravityProvider) GetToken() (string, error) {
	if p.Credentials == nil {
		return "", fmt.Errorf("credentials not configured")
	}

	var ctx context.Context
	if p.Context != nil {
		ctx = p.Context.Request.Context()
	} else {
		ctx = context.Background()
	}

	cacheKey := fmt.Sprintf("%s:%s", TokenCacheKey, p.ProjectID)

	cachedToken, _ := cache.GetCache[string](cacheKey)
	if cachedToken != "" {
		return cachedToken, nil
	}

	needsUpdate := false
	if p.Credentials.IsExpired() && p.Credentials.RefreshToken != "" {
		proxyURL := ""
		if p.Channel.Proxy != nil && *p.Channel.Proxy != "" {
			proxyURL = *p.Channel.Proxy
		}

		if err := p.Credentials.Refresh(ctx, proxyURL, 3); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to refresh antigravity token: %s", err.Error()))
			return "", fmt.Errorf("failed to refresh token: %w", err)
		}

		needsUpdate = true
	}

	if p.Credentials.AccessToken == "" {
		return "", fmt.Errorf("access token is empty")
	}

	if needsUpdate {
		if err := p.saveCredentialsToDatabase(ctx); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to save refreshed credentials to database: %s", err.Error()))
		}
	}

	duration := 30 * time.Minute
	if !p.Credentials.ExpiresAt.IsZero() {
		timeUntilExpiry := time.Until(p.Credentials.ExpiresAt)
		if timeUntilExpiry > 5*time.Minute {
			duration = timeUntilExpiry - 5*time.Minute
		} else if timeUntilExpiry > 0 {
			duration = timeUntilExpiry
		}
	}

	cache.SetCache(cacheKey, p.Credentials.AccessToken, duration)

	return p.Credentials.AccessToken, nil
}

func (p *AntigravityProvider) saveCredentialsToDatabase(ctx context.Context) error {
	credentialsJSON, err := p.Credentials.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize credentials: %w", err)
	}

	if err := model.UpdateChannelKey(p.Channel.Id, credentialsJSON); err != nil {
		return fmt.Errorf("failed to update channel key: %w", err)
	}

	logger.LogInfo(ctx, fmt.Sprintf("[Antigravity] Credentials saved to database for channel %d", p.Channel.Id))
	return nil
}

func (p *AntigravityProvider) handleTokenError(err error) *types.OpenAIErrorWithStatusCode {
	errMsg := err.Error()

	return &types.OpenAIErrorWithStatusCode{
		OpenAIError: types.OpenAIError{
			Message: errMsg,
			Type:    "antigravity_token_error",
			Code:    "antigravity_token_error",
		},
		StatusCode: http.StatusUnauthorized,
		LocalError: false,
	}
}

func (p *AntigravityProvider) getRequestHeadersInternal() (headers map[string]string, err error) {
	headers = make(map[string]string)
	p.CommonRequestHeaders(headers)

	token, err := p.GetToken()
	if err != nil {
		if p.Context != nil {
			logger.LogError(p.Context.Request.Context(), "Failed to get antigravity token: "+err.Error())
		} else {
			logger.SysError("Failed to get antigravity token: " + err.Error())
		}
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	headers["Authorization"] = fmt.Sprintf("Bearer %s", token)
	headers["Content-Type"] = "application/json"
	headers["User-Agent"] = AntigravityUserAgent

	return headers, nil
}

// GetRequestHeaders 获取请求头
func (p *AntigravityProvider) GetRequestHeaders() (headers map[string]string) {
	headers, _ = p.getRequestHeadersInternal()
	return headers
}
