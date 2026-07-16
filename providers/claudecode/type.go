package claudecode

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
)

type OAuth2Credentials struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ClientID     string    `json:"client_id,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	Scopes       []string  `json:"scopes,omitempty"`
}

type TokenRefreshResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope,omitempty"`
}

type TokenRefreshError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func (c *OAuth2Credentials) IsExpired() bool {
	if c.ExpiresAt.IsZero() {
		return true
	}

	buffer := 3 * time.Minute
	return time.Now().Add(buffer).After(c.ExpiresAt)
}

func (c *OAuth2Credentials) Refresh(ctx context.Context, proxyURL string, maxRetries int) error {
	if c.RefreshToken == "" {
		return fmt.Errorf("refresh token is empty")
	}

	clientID := c.ClientID
	if clientID == "" {
		clientID = DefaultClientID
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", clientID)
	data.Set("refresh_token", c.RefreshToken)

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			if ctx != nil {
				logger.LogError(ctx, fmt.Sprintf("[ClaudeCode] Token refresh retry %d/%d after %v", attempt, maxRetries, backoff))
			} else {
				logger.SysLog(fmt.Sprintf("[ClaudeCode] Token refresh retry %d/%d after %v", attempt, maxRetries, backoff))
			}
			time.Sleep(backoff)
		}

		client := &http.Client{
			Timeout: 30 * time.Second,
		}

		if proxyURL != "" {
			proxyClient, err := utils.NewProxyHTTPClient(proxyURL)
			if err != nil {
				return fmt.Errorf("failed to create proxy http client: %w", err)
			}
			proxyClient.Timeout = 30 * time.Second
			client = proxyClient
		}

		req, err := http.NewRequest("POST", TokenEndpoint, strings.NewReader(data.Encode()))
		if err != nil {
			lastErr = fmt.Errorf("failed to create refresh request: %w", err)
			continue
		}

		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", "claude-cli/1.0.56 (external, cli)")
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

		if resp.StatusCode != http.StatusOK {
			var errResp TokenRefreshError
			if err := json.Unmarshal(bodyBytes, &errResp); err == nil {
				if isNonRetryableError(errResp.Error) {
					return fmt.Errorf("token refresh failed (non-retryable): %s - %s", errResp.Error, errResp.ErrorDescription)
				}
				lastErr = fmt.Errorf("token refresh failed: %s - %s", errResp.Error, errResp.ErrorDescription)
			} else {
				lastErr = fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode, string(bodyBytes))
			}
			continue
		}

		var tokenResp TokenRefreshResponse
		if err := json.Unmarshal(bodyBytes, &tokenResp); err != nil {
			lastErr = fmt.Errorf("failed to parse refresh response: %w", err)
			continue
		}

		c.AccessToken = tokenResp.AccessToken
		if tokenResp.RefreshToken != "" {
			c.RefreshToken = tokenResp.RefreshToken
		}
		if tokenResp.TokenType != "" {
			c.TokenType = tokenResp.TokenType
		}

		if tokenResp.Scope != "" {
			c.Scopes = strings.Split(tokenResp.Scope, " ")
		}

		if tokenResp.ExpiresIn > 0 {
			c.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		}

		if ctx != nil {
			logger.LogInfo(ctx, fmt.Sprintf("[ClaudeCode] Token refreshed successfully, expires at: %s", c.ExpiresAt.Format(time.RFC3339)))
		} else {
			logger.SysLog(fmt.Sprintf("[ClaudeCode] Token refreshed successfully, expires at: %s", c.ExpiresAt.Format(time.RFC3339)))
		}
		return nil
	}

	return fmt.Errorf("token refresh failed after %d retries: %w", maxRetries, lastErr)
}

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

func (c *OAuth2Credentials) ToJSON() (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func FromJSON(jsonStr string) (*OAuth2Credentials, error) {
	var creds OAuth2Credentials
	if err := json.Unmarshal([]byte(jsonStr), &creds); err != nil {
		return nil, err
	}
	return &creds, nil
}
