package claudecode

import (
	"context"
	"done-hub/common/cache"
	"done-hub/common/logger"
	"done-hub/common/requester"
	"done-hub/model"
	"done-hub/providers/base"
	"done-hub/providers/claude"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const TokenCacheKey = "api_token:claudecode"

// OAuth2 配置常量
const (
	DefaultClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	TokenEndpoint   = "https://console.anthropic.com/v1/oauth/token"
	DefaultScope    = "user:inference user:profile"
)

type ClaudeCodeProviderFactory struct{}

// 创建 ClaudeCodeProvider
func (f ClaudeCodeProviderFactory) Create(channel *model.Channel) base.ProviderInterface {
	provider := &ClaudeCodeProvider{
		ClaudeProvider: claude.ClaudeProvider{
			BaseProvider: base.BaseProvider{
				Config:    getConfig(),
				Channel:   channel,
				Requester: requester.NewHTTPRequester(channel.GetProxy(), RequestErrorHandle("")),
			},
		},
	}

	// 解析配置
	parseClaudeCodeConfig(provider)

	// 更新 RequestErrorHandle 使用实际的 token
	if provider.Credentials != nil {
		provider.Requester = requester.NewHTTPRequester(channel.GetProxy(), RequestErrorHandle(provider.Credentials.AccessToken))
	}

	return provider
}

// parseClaudeCodeConfig 解析 ClaudeCode 配置
// 支持两种输入格式：
// 1. 完整的 JSON 格式（包含 access_token, refresh_token 等）- 支持自动刷新
// 2. 纯文本格式（直接输入 access_token）- 不支持自动刷新，但更简单
func parseClaudeCodeConfig(provider *ClaudeCodeProvider) {
	channel := provider.Channel

	if channel.Key == "" {
		return
	}

	key := strings.TrimSpace(channel.Key)

	creds, err := FromJSON(key)
	if err == nil {
		provider.Credentials = creds

		if provider.Credentials.ClientID == "" {
			provider.Credentials.ClientID = DefaultClientID
		}

		if provider.Credentials.RefreshToken != "" && provider.Credentials.ExpiresAt.IsZero() {
			provider.Credentials.ExpiresAt = time.Now().Add(1 * time.Hour)
		}

		return
	}

	provider.Credentials = &OAuth2Credentials{
		AccessToken: key,
	}
}

type ClaudeCodeProvider struct {
	claude.ClaudeProvider
	Credentials *OAuth2Credentials
}

func getConfig() base.ProviderConfig {
	return base.ProviderConfig{
		BaseURL:         "https://api.anthropic.com",
		ChatCompletions: "/v1/messages",
		ModelList:       "/v1/models",
	}
}

func RequestErrorHandle(accessToken string) requester.HttpErrorHandler {
	return func(resp *http.Response) *types.OpenAIError {
		claudeError := &claude.ClaudeError{}
		err := json.NewDecoder(resp.Body).Decode(claudeError)
		if err != nil {
			return nil
		}

		openAIError := errorHandle(claudeError)

		// 解析 429 错误的响应头中的冻结时间
		if openAIError != nil && resp.StatusCode == http.StatusTooManyRequests {
			resetHeader := resp.Header.Get("anthropic-ratelimit-unified-reset")
			if resetHeader != "" {
				if resetTimestamp, parseErr := strconv.ParseInt(resetHeader, 10, 64); parseErr == nil {
					openAIError.RateLimitResetAt = resetTimestamp
					logger.SysLog(fmt.Sprintf("[ClaudeCode] Rate limit detected, reset at: %d (%s)",
						resetTimestamp, time.Unix(resetTimestamp, 0).Format(time.RFC3339)))
				}
			}
		}

		return openAIError
	}
}

func errorHandle(claudeError *claude.ClaudeError) *types.OpenAIError {
	if claudeError == nil {
		return nil
	}

	if claudeError.Type == "" {
		return nil
	}

	return &types.OpenAIError{
		Message: claudeError.ErrorInfo.Message,
		Type:    "claudecode_error",
		Code:    claudeError.ErrorInfo.Type,
		Param:   claudeError.Type,
	}
}

func (p *ClaudeCodeProvider) GetRequestHeaders() map[string]string {
	headers := make(map[string]string)
	p.CommonRequestHeaders(headers)

	token, err := p.GetToken()
	if err != nil {
		if p.Context != nil {
			logger.LogError(p.Context.Request.Context(), "Failed to get ClaudeCode token: "+err.Error())
		} else {
			logger.SysError("Failed to get ClaudeCode token: " + err.Error())
		}
		return headers
	}

	headers["Authorization"] = "Bearer " + token
	headers["anthropic-version"] = "2023-06-01"

	if p.Context != nil {
		anthropicVersion := p.Context.Request.Header.Get("anthropic-version")
		if anthropicVersion != "" {
			headers["anthropic-version"] = anthropicVersion
		}
	}

	return headers
}

func (p *ClaudeCodeProvider) handleTokenError(err error) *types.OpenAIErrorWithStatusCode {
	errMsg := err.Error()

	return &types.OpenAIErrorWithStatusCode{
		OpenAIError: types.OpenAIError{
			Message: errMsg,
			Type:    "claudecode_token_error",
			Code:    "claudecode_token_error",
		},
		StatusCode: http.StatusUnauthorized,
		LocalError: false,
	}
}

func (p *ClaudeCodeProvider) GetToken() (string, error) {
	var ctx context.Context
	if p.Context != nil {
		ctx = p.Context.Request.Context()
	} else {
		ctx = context.Background()
	}

	if p.Credentials == nil {
		return "", fmt.Errorf("credentials not configured")
	}

	if p.Credentials.AccessToken == "" {
		return "", fmt.Errorf("access token is empty")
	}

	if p.Credentials.RefreshToken == "" {
		return p.Credentials.AccessToken, nil
	}

	cacheKey := fmt.Sprintf("%s:%d", TokenCacheKey, p.Channel.Id)
	cachedToken, _ := cache.GetCache[string](cacheKey)
	if cachedToken != "" {
		return cachedToken, nil
	}

	needsUpdate := false
	if p.Credentials.IsExpired() {
		proxyURL := p.Channel.GetProxy()

		if err := p.Credentials.Refresh(ctx, proxyURL, 3); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to refresh claudecode token: %s", err.Error()))
			return "", fmt.Errorf("failed to refresh token: %w", err)
		}

		needsUpdate = true
	}

	if needsUpdate {
		if err := p.saveCredentialsToDatabase(ctx); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to save refreshed credentials to database: %s", err.Error()))
		}
	}

	cacheDuration := 55 * time.Minute
	if !p.Credentials.ExpiresAt.IsZero() {
		timeUntilExpiry := time.Until(p.Credentials.ExpiresAt)
		if timeUntilExpiry > 0 && timeUntilExpiry < cacheDuration {
			cacheDuration = timeUntilExpiry
		}
	}

	cache.SetCache(cacheKey, p.Credentials.AccessToken, cacheDuration)

	return p.Credentials.AccessToken, nil
}

func (p *ClaudeCodeProvider) saveCredentialsToDatabase(ctx context.Context) error {
	credentialsJSON, err := p.Credentials.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize credentials: %w", err)
	}

	if err := model.UpdateChannelKey(p.Channel.Id, credentialsJSON); err != nil {
		return fmt.Errorf("failed to update channel key: %w", err)
	}

	logger.LogInfo(ctx, fmt.Sprintf("[ClaudeCode] Credentials saved to database for channel %d", p.Channel.Id))
	return nil
}

func (p *ClaudeCodeProvider) GetFullRequestURL(requestURL string) string {
	baseURL := strings.TrimSuffix(p.GetBaseURL(), "/")
	return fmt.Sprintf("%s%s", baseURL, requestURL)
}
