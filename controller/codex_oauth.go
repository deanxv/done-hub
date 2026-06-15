package controller

import (
	"crypto/rand"
	"crypto/sha256"
	"done-hub/common"
	"done-hub/common/cache"
	"done-hub/common/logger"
	"done-hub/providers/codex"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	// Codex OAuth 状态缓存前缀
	CodexOAuthStateCachePrefix = "codex_oauth_state:"
	// Codex OAuth 状态缓存时长（10分钟）
	CodexOAuthStateCacheDuration = 10 * time.Minute
)

// Codex OAuth 配置常量
const (
	CodexAuthorizeURL = "https://auth.openai.com/oauth/authorize"
	CodexTokenURL     = "https://auth.openai.com/oauth/token"
	CodexClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	CodexRedirectURI  = "http://localhost:1455/auth/callback"
	CodexScopes       = "openid profile email offline_access"
)

// CodexOAuthStateData OAuth 状态数据
type CodexOAuthStateData struct {
	ChannelID    int    `json:"channel_id"`
	CodeVerifier string `json:"code_verifier"`
	State        string `json:"state"`
	Proxy        string `json:"proxy"` // 代理配置（JSON 字符串）
	CreatedAt    int64  `json:"created_at"`
}

// StartCodexOAuthRequest 开始 OAuth 认证请求
type StartCodexOAuthRequest struct {
	ChannelID int    `json:"channel_id"` // 可选，新建时为 0
	Proxy     string `json:"proxy"`      // 可选，代理配置（JSON 字符串）
}

// generateCodexCodeVerifier 生成随机的 code verifier（PKCE）
func generateCodexCodeVerifier() (string, error) {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(b), nil
}

// generateCodexCodeChallenge 生成 code challenge（PKCE）
func generateCodexCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(hash[:])
}

// StartCodexOAuth 开始 Codex OAuth 认证流程
// POST /api/codex/oauth/start
func StartCodexOAuth(c *gin.Context) {
	var req StartCodexOAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	// 生成随机 state
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		common.APIRespondWithError(c, http.StatusOK, fmt.Errorf("failed to generate state: %w", err))
		return
	}
	state := base64.URLEncoding.EncodeToString(stateBytes)

	// 生成 PKCE code verifier
	codeVerifier, err := generateCodexCodeVerifier()
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, fmt.Errorf("failed to generate code verifier: %w", err))
		return
	}

	// 生成 code challenge
	codeChallenge := generateCodexCodeChallenge(codeVerifier)

	// 保存 state 到缓存（包含代理配置）
	stateData := CodexOAuthStateData{
		ChannelID:    req.ChannelID,
		CodeVerifier: codeVerifier,
		State:        state,
		Proxy:        req.Proxy, // 保存代理配置，用于后续 token 交换
		CreatedAt:    time.Now().Unix(),
	}
	cacheKey := CodexOAuthStateCachePrefix + state
	cache.SetCache(cacheKey, stateData, CodexOAuthStateCacheDuration)

	// 构建 OAuth 授权 URL
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", CodexClientID)
	params.Set("redirect_uri", CodexRedirectURI)
	params.Set("scope", CodexScopes)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("state", state)
	params.Set("id_token_add_organizations", "true")
	params.Set("codex_cli_simplified_flow", "true")

	authURL := fmt.Sprintf("%s?%s", CodexAuthorizeURL, params.Encode())

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"auth_url":   authURL,
			"state":      state,
			"session_id": state, // 使用 state 作为 session_id
			"instructions": []string{
				"1. 点击授权链接，在新窗口中登录 OpenAI 账户",
				"2. 同意应用权限",
				"3. 授权成功后，复制浏览器地址栏中的完整 URL",
				"4. 将完整的回调 URL 粘贴到下方输入框中",
			},
		},
	})
}

// ExchangeCodexCodeRequest 交换授权码请求
type ExchangeCodexCodeRequest struct {
	SessionID         string `json:"session_id"`         // session_id (即 state)
	AuthorizationCode string `json:"authorization_code"` // 授权码或完整的回调 URL
	CallbackURL       string `json:"callback_url"`       // 完整的回调 URL（可选）
}

// CodexOAuthCallback 处理用户提交的授权码
// POST /api/codex/oauth/exchange-code
func CodexOAuthCallback(c *gin.Context) {
	var req ExchangeCodexCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	if req.SessionID == "" || (req.AuthorizationCode == "" && req.CallbackURL == "") {
		common.APIRespondWithError(c, http.StatusOK, fmt.Errorf("session_id and authorization_code (or callback_url) are required"))
		return
	}

	state := req.SessionID

	// 从缓存中获取 state 数据
	cacheKey := CodexOAuthStateCachePrefix + state
	stateData, err := cache.GetCache[CodexOAuthStateData](cacheKey)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, fmt.Errorf("invalid or expired OAuth session"))
		return
	}

	// 删除已使用的 state
	cache.DeleteCache(cacheKey)

	// 解析授权码（可能是完整的 URL 或直接的 code）
	inputValue := req.CallbackURL
	if inputValue == "" {
		inputValue = req.AuthorizationCode
	}

	code, err := parseCodexCallbackURL(inputValue)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, fmt.Errorf("failed to parse authorization code: %w", err))
		return
	}

	// 使用 code 交换 token（使用会话中保存的代理配置）
	tokenResp, err := exchangeCodexCodeForToken(code, stateData.CodeVerifier, stateData.State, stateData.Proxy)
	if err != nil {
		logger.SysError(fmt.Sprintf("Failed to exchange code for token: %s", err.Error()))
		common.APIRespondWithError(c, http.StatusOK, fmt.Errorf("failed to exchange code for token: %w", err))
		return
	}

	// 从 id_token 中提取 account_id（优先使用 id_token，如果没有则使用 access_token）
	accountID := ""
	if tokenResp.IDToken != "" {
		accountID = extractAccountIDFromToken(tokenResp.IDToken)
	}
	if accountID == "" && tokenResp.AccessToken != "" {
		accountID = extractAccountIDFromToken(tokenResp.AccessToken)
	}

	// 构建凭证对象
	credentials := &codex.OAuth2Credentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ClientID:     CodexClientID,
		AccountID:    accountID,
		TokenType:    tokenResp.TokenType,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}

	// 序列化凭证
	credentialsJSON, err := credentials.ToJSON()
	if err != nil {
		logger.SysError(fmt.Sprintf("Failed to serialize credentials: %s", err.Error()))
		common.APIRespondWithError(c, http.StatusOK, fmt.Errorf("failed to serialize credentials: %w", err))
		return
	}

	// 返回成功响应
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "授权成功",
		"data": gin.H{
			"credentials": credentialsJSON,
		},
	})
}

// parseCodexCallbackURL 解析回调 URL 或授权码
func parseCodexCallbackURL(input string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("empty input")
	}

	trimmedInput := strings.TrimSpace(input)

	// 情况1: 尝试作为完整URL解析
	if strings.HasPrefix(trimmedInput, "http://") || strings.HasPrefix(trimmedInput, "https://") {
		parsedURL, err := url.Parse(trimmedInput)
		if err != nil {
			return "", fmt.Errorf("invalid URL format: %w", err)
		}

		code := parsedURL.Query().Get("code")
		if code == "" {
			return "", fmt.Errorf("code parameter not found in callback URL")
		}

		return code, nil
	}

	// 情况2: 直接的授权码（可能包含URL fragments）
	cleanedCode := strings.Split(strings.Split(trimmedInput, "#")[0], "&")[0]

	// 验证授权码格式
	if len(cleanedCode) < 10 {
		return "", fmt.Errorf("authorization code too short")
	}

	return cleanedCode, nil
}

// extractAccountIDFromToken 从 JWT access_token 中提取 account_id
func extractAccountIDFromToken(accessToken string) string {
	// 解析 JWT（不验证签名，只提取 payload）
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	token, _, err := parser.ParseUnverified(accessToken, jwt.MapClaims{})
	if err != nil {
		logger.SysError(fmt.Sprintf("Failed to parse JWT: %s", err.Error()))
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

// exchangeCodexCodeForToken 使用授权码交换访问令牌（支持代理）
func exchangeCodexCodeForToken(code, codeVerifier, state, proxyURL string) (*codex.TokenRefreshResponse, error) {
	// 准备请求数据（使用 form-urlencoded 格式）
	requestBody := url.Values{}
	requestBody.Set("grant_type", "authorization_code")
	requestBody.Set("client_id", CodexClientID)
	requestBody.Set("code", code)
	requestBody.Set("redirect_uri", CodexRedirectURI)
	requestBody.Set("code_verifier", codeVerifier)

	// 创建请求
	req, err := http.NewRequest("POST", CodexTokenURL, strings.NewReader(requestBody.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", codex.DefaultCodexUserAgent)
	req.Header.Set("Accept", "application/json")

	// 创建 HTTP 客户端
	client := &http.Client{Timeout: 30 * time.Second}

	// 如果有代理配置，设置代理
	if proxyURL != "" {
		proxyURLParsed, err := url.Parse(proxyURL)
		if err == nil {
			client.Transport = &http.Transport{
				Proxy: http.ProxyURL(proxyURLParsed),
			}
			logger.SysLog(fmt.Sprintf("Using proxy for Codex token exchange: %s", proxyURL))
		} else {
			logger.SysError(fmt.Sprintf("Failed to parse proxy URL: %s", err.Error()))
		}
	}

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// 解析响应
	var tokenResp codex.TokenRefreshResponse
	if err := json.Unmarshal(bodyBytes, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &tokenResp, nil
}
