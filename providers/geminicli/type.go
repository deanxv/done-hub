package geminicli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"done-hub/common/logger"
	"done-hub/common/utils"
	"done-hub/providers/gemini"
)

// OAuth2Credentials OAuth2 用户凭证结构
type OAuth2Credentials struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	ProjectID    string    `json:"project_id"`
	TokenType    string    `json:"token_type,omitempty"`
}

// TokenRefreshResponse OAuth2 token 刷新响应
type TokenRefreshResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope,omitempty"`
}

// TokenRefreshError OAuth2 token 刷新错误响应
type TokenRefreshError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// IsExpired 检查 token 是否过期
// 提前 3 分钟认为过期，给刷新留出时间
func (c *OAuth2Credentials) IsExpired() bool {
	if c.ExpiresAt.IsZero() {
		return true
	}

	buffer := 3 * time.Minute
	return time.Now().Add(buffer).After(c.ExpiresAt)
}

// Refresh 刷新访问令牌
func (c *OAuth2Credentials) Refresh(ctx context.Context, proxyURL string, maxRetries int) error {
	if c.RefreshToken == "" {
		return fmt.Errorf("refresh token is empty")
	}

	// 使用默认的 client_id 和 client_secret（如果未提供）
	clientID := c.ClientID
	if clientID == "" {
		clientID = DefaultClientID
	}

	clientSecret := c.ClientSecret
	if clientSecret == "" {
		clientSecret = DefaultClientSecret
	}

	// 准备请求数据
	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	data.Set("refresh_token", c.RefreshToken)
	data.Set("grant_type", "refresh_token")

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// 指数退避，最大 30 秒
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			if ctx != nil {
				logger.LogError(ctx, fmt.Sprintf("[GeminiCli] Token refresh retry %d/%d after %v", attempt, maxRetries, backoff))
			} else {
				logger.SysLog(fmt.Sprintf("[GeminiCli] Token refresh retry %d/%d after %v", attempt, maxRetries, backoff))
			}
			time.Sleep(backoff)
		}

		// 创建 HTTP 客户端
		client := &http.Client{
			Timeout: 30 * time.Second,
		}

		// 如果有代理配置，设置代理（支持 http/https/socks5）
		if proxyURL != "" {
			proxyClient, err := utils.NewProxyHTTPClient(proxyURL)
			if err != nil {
				return fmt.Errorf("failed to create proxy http client: %w", err)
			}
			proxyClient.Timeout = 30 * time.Second
			client = proxyClient
		}

		// 发送刷新请求
		req, err := http.NewRequest("POST", TokenEndpoint, strings.NewReader(data.Encode()))
		if err != nil {
			lastErr = fmt.Errorf("failed to create refresh request: %w", err)
			continue
		}

		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to send refresh request: %w", err)
			continue
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read refresh response: %w", err)
			continue
		}

		// 检查响应状态
		if resp.StatusCode != http.StatusOK {
			// 解析错误响应
			var errResp TokenRefreshError
			if err := json.Unmarshal(bodyBytes, &errResp); err == nil {
				// 检查是否是不可重试的错误
				if isNonRetryableError(errResp.Error) {
					return fmt.Errorf("token refresh failed (non-retryable): %s - %s", errResp.Error, errResp.ErrorDescription)
				}
				lastErr = fmt.Errorf("token refresh failed: %s - %s", errResp.Error, errResp.ErrorDescription)
			} else {
				lastErr = fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode, string(bodyBytes))
			}
			continue
		}

		// 解析成功响应
		var tokenResp TokenRefreshResponse
		if err := json.Unmarshal(bodyBytes, &tokenResp); err != nil {
			lastErr = fmt.Errorf("failed to parse refresh response: %w", err)
			continue
		}

		// 更新凭证
		c.AccessToken = tokenResp.AccessToken
		if tokenResp.RefreshToken != "" {
			c.RefreshToken = tokenResp.RefreshToken
		}
		if tokenResp.TokenType != "" {
			c.TokenType = tokenResp.TokenType
		}

		// 计算过期时间
		if tokenResp.ExpiresIn > 0 {
			c.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		}

		if ctx != nil {
			logger.LogInfo(ctx, fmt.Sprintf("[GeminiCli] Token refreshed successfully, expires at: %s", c.ExpiresAt.Format(time.RFC3339)))
		} else {
			logger.SysLog(fmt.Sprintf("[GeminiCli] Token refreshed successfully, expires at: %s", c.ExpiresAt.Format(time.RFC3339)))
		}
		return nil
	}

	return fmt.Errorf("token refresh failed after %d retries: %w", maxRetries, lastErr)
}

// isNonRetryableError 判断是否是不可重试的错误
func isNonRetryableError(errorType string) bool {
	nonRetryableErrors := []string{
		"invalid_grant",
		"invalid_client",
		"unauthorized_client",
		"access_denied",
		"unsupported_grant_type",
		"invalid_scope",
	}

	for _, e := range nonRetryableErrors {
		if errorType == e {
			return true
		}
	}
	return false
}

// ToJSON 将凭证序列化为 JSON
func (c *OAuth2Credentials) ToJSON() (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// demoCredentials demo 格式的凭证
type demoCredentials struct {
	Token        string `json:"token"`
	Expiry       string `json:"expiry"`       // demo 使用 expiry 而不是 expires_at
	AccessToken  string `json:"access_token"` // 标准格式
	RefreshToken string `json:"refresh_token"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	ExpiresAt    string `json:"expires_at"` // 标准格式（字符串）
	ProjectID    string `json:"project_id"`
	TokenType    string `json:"token_type"`
}

// FromJSON 从 JSON 反序列化凭证（兼容 demo 格式和标准格式）
func FromJSON(jsonStr string) (*OAuth2Credentials, error) {
	// 先尝试解析为兼容格式
	var demo demoCredentials
	if err := json.Unmarshal([]byte(jsonStr), &demo); err != nil {
		return nil, err
	}

	creds := &OAuth2Credentials{
		RefreshToken: demo.RefreshToken,
		ClientID:     demo.ClientID,
		ClientSecret: demo.ClientSecret,
		ProjectID:    demo.ProjectID,
		TokenType:    demo.TokenType,
	}

	// 兼容 token 和 access_token
	if demo.Token != "" {
		creds.AccessToken = demo.Token
	} else {
		creds.AccessToken = demo.AccessToken
	}

	// 兼容 expiry 和 expires_at
	expiryStr := demo.Expiry
	if expiryStr == "" {
		expiryStr = demo.ExpiresAt
	}
	if expiryStr != "" {
		// 尝试解析时间字符串
		if t, err := time.Parse(time.RFC3339, expiryStr); err == nil {
			creds.ExpiresAt = t
		} else if t, err := time.Parse(time.RFC3339Nano, expiryStr); err == nil {
			creds.ExpiresAt = t
		}
	}

	return creds, nil
}

// GeminiCliRequest 内部API请求格式
type GeminiCliRequest struct {
	Model   string                    `json:"model"`
	Project string                    `json:"project"`
	Request *gemini.GeminiChatRequest `json:"request"`
}

// GeminiCliResponse 内部API响应格式（包装了实际的响应）
type GeminiCliResponse struct {
	Response *gemini.GeminiChatResponse `json:"response"`
}
