package codex

import (
	"context"
	"done-hub/common/cache"
	"done-hub/common/logger"
	"done-hub/common/requester"
	"done-hub/model"
	"done-hub/providers/base"
	"done-hub/providers/openai"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const TokenCacheKey = "api_token:codex"

// OAuth2 配置常量
const (
	DefaultClientID = "pdlLIX2Y72MIl2rhLhTE9VV9bN905kBh"
	TokenEndpoint   = "https://auth0.openai.com/oauth/token"
)

type CodexProviderFactory struct{}

// 创建 CodexProvider
func (f CodexProviderFactory) Create(channel *model.Channel) base.ProviderInterface {
	provider := &CodexProvider{
		OpenAIProvider: openai.OpenAIProvider{
			BaseProvider: base.BaseProvider{
				Config:          getConfig(),
				Channel:         channel,
				Requester:       requester.NewHTTPRequester(channel.GetProxy(), RequestErrorHandle("")),
				SupportResponse: true,
			},
			SupportStreamOptions: true,
		},
	}

	// 解析配置
	parseCodexConfig(provider)

	// 更新 RequestErrorHandle 使用实际的 token
	if provider.Credentials != nil {
		provider.Requester = requester.NewHTTPRequester(channel.GetProxy(), RequestErrorHandle(provider.Credentials.AccessToken))
	}

	return provider
}

// parseCodexConfig 解析 Codex 配置
// 支持两种输入格式：
// 1. 完整的 JSON 格式（包含 access_token, refresh_token 等）- 支持自动刷新
// 2. 纯文本格式（直接输入 access_token）- 不支持自动刷新，但更简单
func parseCodexConfig(provider *CodexProvider) {
	channel := provider.Channel

	if channel.Key == "" {
		return
	}

	key := strings.TrimSpace(channel.Key)

	// 尝试解析为 JSON 格式的完整凭证
	creds, err := FromJSON(key)
	if err == nil {
		provider.Credentials = creds

		// 如果没有 ClientID，使用默认值
		if provider.Credentials.ClientID == "" {
			provider.Credentials.ClientID = DefaultClientID
		}

		// 如果没有 AccountID，从 access_token 中提取
		if provider.Credentials.AccountID == "" && provider.Credentials.AccessToken != "" {
			if accountID := extractAccountIDFromJWT(provider.Credentials.AccessToken); accountID != "" {
				provider.Credentials.AccountID = accountID
			}
		}

		// 如果有 refresh_token 但没有 expires_at，设置一个默认过期时间
		if provider.Credentials.RefreshToken != "" && provider.Credentials.ExpiresAt.IsZero() {
			provider.Credentials.ExpiresAt = time.Now().Add(1 * time.Hour)
		}

		return
	}

	// 如果解析失败，当作纯文本 access_token 处理
	accountID := extractAccountIDFromJWT(key)
	provider.Credentials = &OAuth2Credentials{
		AccessToken: key,
		AccountID:   accountID,
	}
}

type CodexProvider struct {
	openai.OpenAIProvider
	Credentials *OAuth2Credentials // OAuth2 凭证（包含 refresh_token）
}

func getConfig() base.ProviderConfig {
	return base.ProviderConfig{
		BaseURL:         "https://chatgpt.com",
		ChatCompletions: "/backend-api/codex/responses",
		Responses:       "/backend-api/codex/responses",
		ModelList:       "/backend-api/models",
	}
}

// RequestErrorHandle 请求错误处理
func RequestErrorHandle(accessToken string) requester.HttpErrorHandler {
	return func(resp *http.Response) *types.OpenAIError {
		// 读取响应体
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil
		}

		// 尝试解析为 Codex 错误响应（包含 resets_in_seconds）
		var codexErrorResp CodexErrorResponse
		if err := json.Unmarshal(bodyBytes, &codexErrorResp); err == nil && codexErrorResp.Error.Message != "" {
			openAIError := &types.OpenAIError{
				Code:    codexErrorResp.Error.Code,
				Message: codexErrorResp.Error.Message,
				Type:    codexErrorResp.Error.Type,
			}

			// 清理错误消息中的敏感信息
			if accessToken != "" {
				openAIError.Message = strings.Replace(openAIError.Message, accessToken, "xxxxx", -1)
			}

			// 解析 429 错误的响应体中的冻结时间（Codex 使用 resets_in_seconds）
			if resp.StatusCode == http.StatusTooManyRequests && codexErrorResp.Error.ResetsInSeconds > 0 {
				// 计算重置时间戳
				resetTimestamp := time.Now().Unix() + int64(codexErrorResp.Error.ResetsInSeconds)
				openAIError.RateLimitResetAt = resetTimestamp
				logger.SysLog(fmt.Sprintf("[Codex] Rate limit detected, resets in %d seconds, reset at: %s",
					codexErrorResp.Error.ResetsInSeconds, time.Unix(resetTimestamp, 0).Format(time.RFC3339)))
			}

			return openAIError
		}

		// 如果解析失败，尝试标准 OpenAI 错误格式
		openAIError := &types.OpenAIError{}
		if err := json.Unmarshal(bodyBytes, openAIError); err != nil {
			return nil
		}

		if openAIError.Message == "" {
			return nil
		}

		// 清理错误消息中的敏感信息
		if accessToken != "" {
			openAIError.Message = strings.Replace(openAIError.Message, accessToken, "xxxxx", -1)
		}

		return openAIError
	}
}

// getRequestHeadersInternal 获取请求头（内部方法，返回错误）
func (p *CodexProvider) getRequestHeadersInternal() (map[string]string, error) {
	headers := make(map[string]string)

	// 第1层：透传客户端请求头（黑名单过滤，参考 Demo）
	if p.Context != nil {
		p.filterAndPassthroughClientHeaders(headers)
	}

	// 第2层：应用渠道自定义请求头（ModelHeaders）- 会覆盖客户端请求头
	p.CommonRequestHeaders(headers)

	// 第3层：获取 Token
	token, err := p.GetToken()
	if err != nil {
		if p.Context != nil {
			logger.LogError(p.Context.Request.Context(), "Failed to get Codex token: "+err.Error())
		} else {
			logger.SysError("Failed to get Codex token: " + err.Error())
		}
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	// 第4层：设置必需的固定请求头（不可被覆盖）
	headers["Authorization"] = "Bearer " + token
	headers["Content-Type"] = "application/json"

	// 第5层：设置 chatgpt-account-id（如果有）
	if p.Credentials != nil && p.Credentials.AccountID != "" {
		headers["chatgpt-account-id"] = p.Credentials.AccountID
	}

	return headers, nil
}

// filterAndPassthroughClientHeaders 过滤并透传客户端请求头（白名单机制，参考 Demo）
func (p *CodexProvider) filterAndPassthroughClientHeaders(headers map[string]string) {
	if p.Context == nil {
		return
	}

	allowedKeys := []string{
		"version",
		"openai-beta",
		"session_id",
		"x-session-id", // 额外支持 x-session-id
	}

	// 透传白名单中的请求头
	for _, key := range allowedKeys {
		value := p.Context.Request.Header.Get(key)
		if value != "" {
			headers[key] = value
		}
	}
}

// GetRequestHeaders 获取请求头（公开方法，兼容接口）
func (p *CodexProvider) GetRequestHeaders() map[string]string {
	headers, _ := p.getRequestHeadersInternal()
	if headers == nil {
		headers = make(map[string]string)
		p.CommonRequestHeaders(headers)
	}
	return headers
}

func (p *CodexProvider) handleTokenError(err error) *types.OpenAIErrorWithStatusCode {
	errMsg := err.Error()

	return &types.OpenAIErrorWithStatusCode{
		OpenAIError: types.OpenAIError{
			Message: errMsg,
			Type:    "codex_token_error",
			Code:    "codex_token_error",
		},
		StatusCode: http.StatusUnauthorized,
		LocalError: false,
	}
}

func (p *CodexProvider) GetToken() (string, error) {
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

	// 如果没有 refresh_token，直接返回 access_token
	if p.Credentials.RefreshToken == "" {
		return p.Credentials.AccessToken, nil
	}

	// 使用缓存
	cacheKey := fmt.Sprintf("%s:%d", TokenCacheKey, p.Channel.Id)
	cachedToken, _ := cache.GetCache[string](cacheKey)
	if cachedToken != "" {
		return cachedToken, nil
	}

	needsUpdate := false
	if p.Credentials.IsExpired() {
		proxyURL := p.Channel.GetProxy()

		if err := p.Credentials.Refresh(ctx, proxyURL, 3); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to refresh codex token: %s", err.Error()))
			return "", fmt.Errorf("failed to refresh token: %w", err)
		}

		needsUpdate = true
	}

	if needsUpdate {
		if err := p.saveCredentialsToDatabase(ctx); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to save refreshed credentials to database: %s", err.Error()))
		}
	}

	// 缓存 token，默认 55 分钟
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

func (p *CodexProvider) saveCredentialsToDatabase(ctx context.Context) error {
	credentialsJSON, err := p.Credentials.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize credentials: %w", err)
	}

	if err := model.UpdateChannelKey(p.Channel.Id, credentialsJSON); err != nil {
		return fmt.Errorf("failed to update channel key: %w", err)
	}

	logger.LogInfo(ctx, fmt.Sprintf("[Codex] Credentials saved to database for channel %d", p.Channel.Id))
	return nil
}
