package geminicli

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

const TokenCacheKey = "api_token:geminicli"

// OAuth2 配置常量
const (
	DefaultClientID     = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
	DefaultClientSecret = "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"
	TokenEndpoint       = "https://oauth2.googleapis.com/token"
	DefaultScope        = "https://www.googleapis.com/auth/cloud-platform"
)

type GeminiCliProviderFactory struct{}

// 创建 GeminiCliProvider
func (f GeminiCliProviderFactory) Create(channel *model.Channel) base.ProviderInterface {
	provider := &GeminiCliProvider{
		GeminiProvider: gemini.GeminiProvider{
			OpenAIProvider: openai.OpenAIProvider{
				BaseProvider: base.BaseProvider{
					Config:    getConfig("https://cloudcode-pa.googleapis.com"),
					Channel:   channel,
					Requester: requester.NewHTTPRequester(channel.GetProxy(), RequestErrorHandle("")),
				},
				SupportStreamOptions: true,
			},
		},
	}

	// 解析配置
	parseGeminiCliConfig(provider)

	// 更新 RequestErrorHandle 使用实际的 token
	if provider.Credentials != nil {
		provider.Requester = requester.NewHTTPRequester(channel.GetProxy(), RequestErrorHandle(provider.Credentials.AccessToken))
	}

	return provider
}

// parseGeminiCliConfig 解析 GeminiCli 配置
func parseGeminiCliConfig(provider *GeminiCliProvider) {
	channel := provider.Channel

	// 默认配置
	endpoint := "https://cloudcode-pa.googleapis.com"

	// 尝试从 Plugin 中获取配置
	if channel.Plugin != nil {
		plugin := channel.Plugin.Data()
		if geminicliConfig, ok := plugin["geminicli"]; ok {
			if epVal, ok := geminicliConfig["endpoint"]; ok {
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
		if geminicliConfig, ok := plugin["geminicli"]; ok {
			projectID := ""
			accessToken := ""

			if pidVal, ok := geminicliConfig["project_id"]; ok {
				if pid, ok := pidVal.(string); ok && pid != "" {
					projectID = pid
				}
			}
			if tokenVal, ok := geminicliConfig["access_token"]; ok {
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

type GeminiCliProvider struct {
	gemini.GeminiProvider
	Endpoint    string
	ProjectID   string
	Credentials *OAuth2Credentials // OAuth2 凭证（包含 refresh_token）
}

func getConfig(endpoint string) base.ProviderConfig {
	return base.ProviderConfig{
		BaseURL:           endpoint,
		ChatCompletions:   "/v1internal/chat/completions",
		ModelList:         "/models",
		ImagesGenerations: "1",
	}
}

// 请求错误处理
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
				// 解析 error.details[].metadata.quotaResetDelay
				for _, detail := range geminiError.ErrorInfo.Details {
					if detail.Metadata != nil {
						if quotaResetDelay, ok := detail.Metadata["quotaResetDelay"].(string); ok && quotaResetDelay != "" {
							if duration, parseErr := time.ParseDuration(quotaResetDelay); parseErr == nil {
								resetTimestamp := time.Now().Unix() + int64(duration.Seconds())
								openAIError.RateLimitResetAt = resetTimestamp
								logger.SysLog(fmt.Sprintf("[GeminiCli] Rate limit detected, quota reset delay: %s, reset at: %s",
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

			// 解析 429 错误的响应体中的冻结时间
			if openAIError != nil && geminiErrors.Error() != nil && geminiErrors.Error().ErrorInfo != nil && geminiErrors.Error().ErrorInfo.Code == http.StatusTooManyRequests {
				// 解析 error.details[].metadata.quotaResetDelay
				for _, detail := range geminiErrors.Error().ErrorInfo.Details {
					if detail.Metadata != nil {
						if quotaResetDelay, ok := detail.Metadata["quotaResetDelay"].(string); ok && quotaResetDelay != "" {
							if duration, parseErr := time.ParseDuration(quotaResetDelay); parseErr == nil {
								resetTimestamp := time.Now().Unix() + int64(duration.Seconds())
								openAIError.RateLimitResetAt = resetTimestamp
								logger.SysLog(fmt.Sprintf("[GeminiCli] Rate limit detected, quota reset delay: %s, reset at: %s",
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

// 错误处理
func errorHandle(geminiError *gemini.GeminiErrorResponse, token string) *types.OpenAIError {
	if geminiError.ErrorInfo == nil || geminiError.ErrorInfo.Message == "" {
		return nil
	}

	cleaningError(geminiError.ErrorInfo, token)

	return &types.OpenAIError{
		Message: geminiError.ErrorInfo.Message,
		Type:    "geminicli_error",
		Param:   geminiError.ErrorInfo.Status,
		Code:    geminiError.ErrorInfo.Code,
	}
}

func cleaningError(errorInfo *gemini.GeminiError, token string) {
	if token == "" {
		return
	}
	message := strings.Replace(errorInfo.Message, token, "xxxxx", 1)

	// 截断 base64 数据，避免日志过长
	message = truncateBase64InMessage(message)

	errorInfo.Message = message
}

// truncateBase64InMessage 截断错误消息中的 base64 数据
func truncateBase64InMessage(message string) string {
	const maxBase64Length = 50 // 只保留前50个字符

	result := message
	offset := 0

	// 循环处理所有的 base64 数据
	for {
		// 在当前偏移位置查找下一个 base64 数据
		idx := strings.Index(result[offset:], ";base64,")
		if idx == -1 {
			break
		}

		// 计算实际位置
		actualIdx := offset + idx
		start := actualIdx + 8 // ";base64," 的长度

		// 查找 base64 数据的结束位置（通常是引号、空格或其他分隔符）
		end := start
		for end < len(result) && isBase64Char(result[end]) {
			end++
		}

		if end-start > maxBase64Length {
			// 截断 base64 数据
			result = result[:start+maxBase64Length] + "...[truncated]" + result[end:]
			// 更新偏移位置，继续查找下一个
			offset = start + maxBase64Length + len("...[truncated]")
		} else {
			// 如果这个 base64 数据不需要截断，移动到下一个位置
			offset = end
		}
	}

	return result
}

// isBase64Char 检查字符是否是 base64 字符
func isBase64Char(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '='
}

func (p *GeminiCliProvider) GetFullRequestURL(requestURL string, modelName string) string {
	baseURL := strings.TrimSuffix(p.GetBaseURL(), "/")

	// GeminiCli使用内部API格式
	// 例如: https://cloudcode-pa.googleapis.com/v1internal:generateContent
	return fmt.Sprintf("%s/v1internal:%s", baseURL, requestURL)
}

func (p *GeminiCliProvider) GetToken() (string, error) {
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
		proxyURL := p.Channel.GetProxy()

		if err := p.Credentials.Refresh(ctx, proxyURL, 3); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to refresh geminicli token: %s", err.Error()))
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

func (p *GeminiCliProvider) saveCredentialsToDatabase(ctx context.Context) error {
	credentialsJSON, err := p.Credentials.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize credentials: %w", err)
	}

	if err := model.UpdateChannelKey(p.Channel.Id, credentialsJSON); err != nil {
		return fmt.Errorf("failed to update channel key: %w", err)
	}

	logger.LogInfo(ctx, fmt.Sprintf("[GeminiCli] Credentials saved to database for channel %d", p.Channel.Id))
	return nil
}

func (p *GeminiCliProvider) handleTokenError(err error) *types.OpenAIErrorWithStatusCode {
	errMsg := err.Error()

	return &types.OpenAIErrorWithStatusCode{
		OpenAIError: types.OpenAIError{
			Message: errMsg,
			Type:    "geminicli_token_error",
			Code:    "geminicli_token_error",
		},
		StatusCode: http.StatusUnauthorized,
		LocalError: false,
	}
}

func (p *GeminiCliProvider) getRequestHeadersInternal() (headers map[string]string, err error) {
	headers = make(map[string]string)
	p.CommonRequestHeaders(headers)

	token, err := p.GetToken()
	if err != nil {
		if p.Context != nil {
			logger.LogError(p.Context.Request.Context(), "Failed to get geminicli token: "+err.Error())
		} else {
			logger.SysError("Failed to get geminicli token: " + err.Error())
		}
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	headers["Authorization"] = fmt.Sprintf("Bearer %s", token)
	headers["Content-Type"] = "application/json"

	return headers, nil
}

// 获取请求头
func (p *GeminiCliProvider) GetRequestHeaders() (headers map[string]string) {
	headers, _ = p.getRequestHeadersInternal()
	return headers
}
