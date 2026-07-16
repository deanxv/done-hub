package codex

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
	"github.com/golang-jwt/jwt/v5"
)

// CodexErrorResponse Codex 错误响应（包含 resets_in_seconds）
type CodexErrorResponse struct {
	Error CodexErrorDetail `json:"error"`
}

// CodexErrorDetail Codex 错误详情
type CodexErrorDetail struct {
	Message         string `json:"message"`
	Type            string `json:"type"`
	Code            any    `json:"code,omitempty"`
	ResetsInSeconds int    `json:"resets_in_seconds,omitempty"` // 429 错误的重置时间（秒）
	ResetsIn        int    `json:"resets_in,omitempty"`         // 备用字段
}

// OAuth2Credentials OAuth2 用户凭证结构
type OAuth2Credentials struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ClientID     string    `json:"client_id,omitempty"`
	AccountID    string    `json:"account_id,omitempty"` // ChatGPT Account ID（从 ID Token 或 Access Token 中提取）
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	Scopes       []string  `json:"scopes,omitempty"`
}

// TokenRefreshResponse OAuth2 token 刷新响应
type TokenRefreshResponse struct {
	IDToken      string `json:"id_token,omitempty"` // ID Token（仅在授权码交换时返回）
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

	// 使用默认的 client_id（如果未提供）
	clientID := c.ClientID
	if clientID == "" {
		clientID = DefaultClientID
	}

	// 准备请求数据
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", clientID)
	data.Set("refresh_token", c.RefreshToken)

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// 指数退避，最大 30 秒
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			if ctx != nil {
				logger.LogError(ctx, fmt.Sprintf("[Codex] Token refresh retry %d/%d after %v", attempt, maxRetries, backoff))
			} else {
				logger.SysLog(fmt.Sprintf("[Codex] Token refresh retry %d/%d after %v", attempt, maxRetries, backoff))
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
		req.Header.Set("User-Agent", DefaultCodexUserAgent)
		req.Header.Set("Accept", "application/json, text/plain, */*")

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

		// 从新的 access_token 中提取 account_id
		if accountID := extractAccountIDFromJWT(tokenResp.AccessToken); accountID != "" {
			c.AccountID = accountID
		}

		// 解析 scope
		if tokenResp.Scope != "" {
			c.Scopes = strings.Split(tokenResp.Scope, " ")
		}

		// 计算过期时间
		if tokenResp.ExpiresIn > 0 {
			c.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		}

		if ctx != nil {
			logger.LogInfo(ctx, fmt.Sprintf("[Codex] Token refreshed successfully, expires at: %s", c.ExpiresAt.Format(time.RFC3339)))
		} else {
			logger.SysLog(fmt.Sprintf("[Codex] Token refreshed successfully, expires at: %s", c.ExpiresAt.Format(time.RFC3339)))
		}
		return nil
	}

	return fmt.Errorf("token refresh failed after %d retries: %w", maxRetries, lastErr)
}

// extractAccountIDFromJWT 从 JWT access_token 中提取 account_id
func extractAccountIDFromJWT(accessToken string) string {
	// 解析 JWT（不验证签名，只提取 payload）
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	token, _, err := parser.ParseUnverified(accessToken, jwt.MapClaims{})
	if err != nil {
		return ""
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return ""
	}

	// 提取 https://api.openai.com/auth.chatgpt_account_id
	authClaims, ok := claims["https://api.openai.com/auth"].(map[string]interface{})
	if !ok {
		return ""
	}

	accountID, ok := authClaims["chatgpt_account_id"].(string)
	if !ok {
		return ""
	}

	return accountID
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

// FromJSON 从 JSON 反序列化凭证
func FromJSON(jsonStr string) (*OAuth2Credentials, error) {
	var creds OAuth2Credentials
	if err := json.Unmarshal([]byte(jsonStr), &creds); err != nil {
		return nil, err
	}
	return &creds, nil
}
