package controller

import (
	"bytes"
	"context"
	"crypto/rand"
	"done-hub/common"
	"done-hub/common/cache"
	"done-hub/common/logger"
	"done-hub/providers/antigravity"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// Antigravity OAuth 状态缓存前缀
	AntigravityOAuthStateCachePrefix = "antigravity_oauth_state:"
	// Antigravity OAuth 结果缓存前缀
	AntigravityOAuthResultCachePrefix = "antigravity_oauth_result:"
	// Antigravity API 端点
	AntigravityCodeAssistEndpoint = "https://cloudcode-pa.googleapis.com"
	// Antigravity OAuth 状态缓存时长（30分钟）
	AntigravityOAuthStateCacheDuration = 30 * time.Minute
	// Antigravity OAuth 结果缓存时长（10分钟）
	AntigravityOAuthResultCacheDuration = 10 * time.Minute
)

// AntigravityOAuthStateData OAuth 状态数据
type AntigravityOAuthStateData struct {
	ChannelID int    `json:"channel_id"`
	ProjectID string `json:"project_id"`
	Proxy     string `json:"proxy"`
	CreatedAt int64  `json:"created_at"`
}

// AntigravityOAuthResultData OAuth 结果数据
type AntigravityOAuthResultData struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	ProjectID   string `json:"project_id"`
	Credentials string `json:"credentials"`
	CompletedAt int64  `json:"completed_at"`
}

// StartAntigravityOAuthRequest 开始 OAuth 认证请求
type StartAntigravityOAuthRequest struct {
	ChannelID int    `json:"channel_id"`
	ProjectID string `json:"project_id"`
	Proxy     string `json:"proxy"`
}

// StartAntigravityOAuth 开始 Antigravity OAuth 认证流程
// POST /api/antigravity/oauth/start
func StartAntigravityOAuth(c *gin.Context) {
	var req StartAntigravityOAuthRequest
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

	// 保存 state 到缓存
	stateData := AntigravityOAuthStateData{
		ChannelID: req.ChannelID,
		ProjectID: req.ProjectID,
		Proxy:     req.Proxy,
		CreatedAt: time.Now().Unix(),
	}
	cacheKey := AntigravityOAuthStateCachePrefix + state
	cache.SetCache(cacheKey, stateData, AntigravityOAuthStateCacheDuration)

	// 构建 OAuth 授权 URL（使用 Antigravity 的 client_id 和 scopes）
	redirectURI := "http://localhost:8080/api/antigravity/oauth/callback"

	params := url.Values{}
	params.Set("client_id", antigravity.AntigravityClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", strings.Join(antigravity.AntigravityScopes, " "))
	params.Set("response_type", "code")
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")
	params.Set("include_granted_scopes", "true")
	params.Set("state", state)

	if req.ProjectID != "" {
		params.Set("project_id", req.ProjectID)
	}

	authURL := "https://accounts.google.com/o/oauth2/auth?" + params.Encode()

	message := "请在浏览器中访问 auth_url 完成授权"
	autoDetect := false
	if req.ProjectID == "" {
		message = "请在浏览器中访问 auth_url 完成授权，授权完成后将自动检测项目 ID"
		autoDetect = true
	}

	c.JSON(http.StatusOK, gin.H{
		"success":             true,
		"auth_url":            authURL,
		"state":               state,
		"message":             message,
		"auto_project_detect": autoDetect,
		"detected_project_id": req.ProjectID,
	})
}

// GetAntigravityOAuthStatus 查询 OAuth 授权状态
// GET /api/antigravity/oauth/status/:state
func GetAntigravityOAuthStatus(c *gin.Context) {
	state := c.Param("state")
	if state == "" {
		common.APIRespondWithError(c, http.StatusOK, fmt.Errorf("state parameter is required"))
		return
	}

	// 从缓存获取结果
	resultCacheKey := AntigravityOAuthResultCachePrefix + state
	result, err := cache.GetCache[AntigravityOAuthResultData](resultCacheKey)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"status":  "pending",
			"message": "授权进行中，请完成授权流程",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"status":      "completed",
		"result":      result.Success,
		"message":     result.Message,
		"project_id":  result.ProjectID,
		"credentials": result.Credentials,
	})
}

// AntigravityOAuthCallback OAuth 回调处理
// GET /api/antigravity/oauth/callback
func AntigravityOAuthCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	errorParam := c.Query("error")

	// 检查是否有错误
	if errorParam != "" {
		errorDesc := c.Query("error_description")
		logger.SysError(fmt.Sprintf("Antigravity OAuth callback error: %s - %s", errorParam, errorDesc))

		// 清理 error_description，防止 XSS 攻击
		safeErrorDesc := truncateErrorDescription(errorDesc)

		if state != "" {
			resultCacheKey := AntigravityOAuthResultCachePrefix + state
			resultData := AntigravityOAuthResultData{
				Success:     false,
				Message:     fmt.Sprintf("授权失败: %s", safeErrorDesc),
				CompletedAt: time.Now().Unix(),
			}
			cache.SetCache(resultCacheKey, resultData, AntigravityOAuthResultCacheDuration)
		}

		renderAntigravityOAuthResult(c, false, fmt.Sprintf("授权失败: %s", safeErrorDesc), "", "", state)
		return
	}

	// 验证 state
	if state == "" {
		renderAntigravityOAuthResult(c, false, "无效的 state 参数", "", "", "")
		return
	}

	// 从缓存获取 state 数据
	cacheKey := AntigravityOAuthStateCachePrefix + state
	stateData, err := cache.GetCache[AntigravityOAuthStateData](cacheKey)
	if err != nil {
		logger.SysError(fmt.Sprintf("Failed to get Antigravity OAuth state from cache: %s", err.Error()))

		resultCacheKey := AntigravityOAuthResultCachePrefix + state
		resultData := AntigravityOAuthResultData{
			Success:     false,
			Message:     "授权状态已过期，请重新发起授权",
			CompletedAt: time.Now().Unix(),
		}
		cache.SetCache(resultCacheKey, resultData, AntigravityOAuthResultCacheDuration)

		renderAntigravityOAuthResult(c, false, "授权状态已过期，请重新发起授权", "", "", state)
		return
	}

	// 使用 code 交换 token
	tokenResp, err := exchangeAntigravityToken(code, stateData.Proxy)
	if err != nil {
		logger.SysError(fmt.Sprintf("Failed to exchange Antigravity token: %s", err.Error()))

		resultCacheKey := AntigravityOAuthResultCachePrefix + state
		resultData := AntigravityOAuthResultData{
			Success:     false,
			Message:     fmt.Sprintf("获取访问令牌失败: %s", err.Error()),
			CompletedAt: time.Now().Unix(),
		}
		cache.SetCache(resultCacheKey, resultData, AntigravityOAuthResultCacheDuration)

		renderAntigravityOAuthResult(c, false, fmt.Sprintf("获取访问令牌失败: %s", err.Error()), "", "", state)
		return
	}

	// 尝试获取 project_id
	projectID := stateData.ProjectID
	autoDetected := false

	if projectID == "" {
		ctx := c.Request.Context()
		logger.LogInfo(ctx, "Project ID 未提供，尝试自动检测...")

		// 使用 Code Assist API 获取 project_id
		var err error
		projectID, err = fetchAntigravityProjectID(ctx, tokenResp.AccessToken, stateData.Proxy)
		if err != nil {
			logger.LogInfo(ctx, fmt.Sprintf("自动检测项目 ID 失败: %s，使用随机生成的 Project ID", err.Error()))
			projectID = generateRandomProjectID()
		}

		autoDetected = true
		logger.LogInfo(ctx, fmt.Sprintf("自动检测到项目 ID: %s", projectID))
	}

	// 构建凭证
	credentials := antigravity.OAuth2Credentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ClientID:     antigravity.AntigravityClientID,
		ClientSecret: antigravity.AntigravityClientSecret,
		ProjectID:    projectID,
	}

	if tokenResp.ExpiresIn > 0 {
		credentials.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	// 序列化凭证
	credentialsJSON, err := credentials.ToJSON()
	if err != nil {
		logger.SysError(fmt.Sprintf("Failed to serialize Antigravity credentials: %s", err.Error()))

		resultCacheKey := AntigravityOAuthResultCachePrefix + state
		resultData := AntigravityOAuthResultData{
			Success:     false,
			Message:     "凭证序列化失败",
			CompletedAt: time.Now().Unix(),
		}
		cache.SetCache(resultCacheKey, resultData, AntigravityOAuthResultCacheDuration)

		renderAntigravityOAuthResult(c, false, "凭证序列化失败", "", "", state)
		return
	}

	// 保存成功结果到缓存
	resultCacheKey := AntigravityOAuthResultCachePrefix + state
	resultData := AntigravityOAuthResultData{
		Success:     true,
		Message:     "授权成功",
		ProjectID:   projectID,
		Credentials: credentialsJSON,
		CompletedAt: time.Now().Unix(),
	}
	cache.SetCache(resultCacheKey, resultData, AntigravityOAuthResultCacheDuration)

	// 清理 state 缓存
	cache.DeleteCache(cacheKey)

	message := "授权成功"
	if autoDetected {
		// 不做 HTML 转义：renderAntigravityOAuthResult 会对整个 message 统一转义
		message = fmt.Sprintf("授权成功，自动检测到项目 ID: %s", projectID)
	}

	renderAntigravityOAuthResult(c, true, message, projectID, credentialsJSON, state)
}

// AntigravityTokenResponse Token 响应
type AntigravityTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

// exchangeAntigravityToken 使用 code 交换 token
func exchangeAntigravityToken(code string, proxyURL string) (*AntigravityTokenResponse, error) {
	redirectURI := "http://localhost:8080/api/antigravity/oauth/callback"

	data := url.Values{}
	data.Set("client_id", antigravity.AntigravityClientID)
	data.Set("client_secret", antigravity.AntigravityClientSecret)
	data.Set("code", code)
	data.Set("grant_type", "authorization_code")
	data.Set("redirect_uri", redirectURI)

	client := createAntigravityHTTPClient(proxyURL, 30*time.Second)

	req, err := http.NewRequest("POST", antigravity.TokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed: HTTP %d - %s", resp.StatusCode, string(body))
	}

	var tokenResp AntigravityTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &tokenResp, nil
}

// fetchAntigravityProjectID 获取项目 ID
func fetchAntigravityProjectID(ctx context.Context, accessToken, proxyURL string) (string, error) {
	logger.LogInfo(ctx, "尝试通过 loadCodeAssist API 获取 project_id...")

	// 先尝试 loadCodeAssist
	projectID, err := callAntigravityLoadCodeAssist(ctx, accessToken, proxyURL)
	if err == nil && projectID != "" {
		return projectID, nil
	}

	if err != nil {
		logger.LogInfo(ctx, fmt.Sprintf("loadCodeAssist 失败: %s，尝试 onboardUser...", err.Error()))
	} else {
		logger.LogInfo(ctx, "loadCodeAssist 未返回 project_id，尝试 onboardUser...")
	}

	// 回退到 onboardUser
	projectID, err = callAntigravityOnboardUser(ctx, accessToken, proxyURL)
	if err != nil {
		return "", fmt.Errorf("onboardUser failed: %w", err)
	}

	if projectID == "" {
		return "", fmt.Errorf("onboardUser completed but no project_id returned")
	}

	return projectID, nil
}

// callAntigravityLoadCodeAssist 调用 loadCodeAssist API
func callAntigravityLoadCodeAssist(ctx context.Context, accessToken, proxyURL string) (string, error) {
	loadResp, err := getAntigravityLoadCodeAssistResponse(accessToken, proxyURL)
	if err != nil {
		return "", err
	}

	if loadResp.CurrentTier != "" {
		logger.LogInfo(ctx, fmt.Sprintf("用户已激活，tier: %s", loadResp.CurrentTier))
		return loadResp.CloudAICompanionProject, nil
	}

	logger.LogInfo(ctx, "用户未激活（无 currentTier）")
	return "", nil
}

// callAntigravityOnboardUser 调用 onboardUser API
func callAntigravityOnboardUser(ctx context.Context, accessToken, proxyURL string) (string, error) {
	// 首先获取 tier
	loadResp, err := getAntigravityLoadCodeAssistResponse(accessToken, proxyURL)
	if err != nil {
		return "", fmt.Errorf("failed to get tier: %w", err)
	}

	tierID := "LEGACY"
	for _, tier := range loadResp.AllowedTiers {
		if tier.IsDefault {
			tierID = tier.ID
			logger.LogInfo(ctx, fmt.Sprintf("找到默认 tier: %s", tierID))
			break
		}
	}

	logger.LogInfo(ctx, fmt.Sprintf("用户 tier: %s，开始激活...", tierID))

	requestURL := AntigravityCodeAssistEndpoint + "/v1internal:onboardUser"

	reqBody := antigravity.OnboardUserRequest{
		TierID: tierID,
		Metadata: antigravity.LoadCodeAssistMetadata{
			IDEType:    "ANTIGRAVITY",
			Platform:   "PLATFORM_UNSPECIFIED",
			PluginType: "GEMINI",
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	client := createAntigravityHTTPClient(proxyURL, 30*time.Second)

	// 像 demo 一样：每次轮询都调用 onboardUser，直到返回 done: true
	maxAttempts := 5
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		logger.LogInfo(ctx, fmt.Sprintf("调用 onboardUser %d/%d...", attempt, maxAttempts))

		req, err := http.NewRequest("POST", requestURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", antigravity.AntigravityUserAgent)

		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("failed to send request: %w", err)
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
		}

		var onboardResp antigravity.OnboardUserResponse
		if err := json.Unmarshal(respBody, &onboardResp); err != nil {
			return "", fmt.Errorf("failed to parse response: %w", err)
		}

		// 检查操作是否完成
		if onboardResp.Done {
			logger.LogInfo(ctx, "onboardUser 操作完成")
			if onboardResp.Response != nil && onboardResp.Response.CloudAICompanionProject != nil {
				switch v := onboardResp.Response.CloudAICompanionProject.(type) {
				case string:
					return v, nil
				case map[string]interface{}:
					if id, ok := v["id"].(string); ok {
						return id, nil
					}
				}
			}
			// 操作完成但没有返回 project_id
			logger.LogInfo(ctx, "onboardUser 完成但未返回 project_id")
			return "", nil
		}

		// 操作未完成，等待 2 秒后重试
		logger.LogInfo(ctx, "onboardUser 操作进行中，等待 2 秒...")
		time.Sleep(2 * time.Second)
	}

	return "", fmt.Errorf("onboardUser timeout after %d attempts", maxAttempts)
}

// getAntigravityLoadCodeAssistResponse 获取完整的 loadCodeAssist 响应
func getAntigravityLoadCodeAssistResponse(accessToken, proxyURL string) (*antigravity.LoadCodeAssistResponse, error) {
	requestURL := AntigravityCodeAssistEndpoint + "/v1internal:loadCodeAssist"

	reqBody := antigravity.LoadCodeAssistRequest{
		Metadata: antigravity.LoadCodeAssistMetadata{
			IDEType:    "ANTIGRAVITY",
			Platform:   "PLATFORM_UNSPECIFIED",
			PluginType: "GEMINI",
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", requestURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", antigravity.AntigravityUserAgent)

	client := createAntigravityHTTPClient(proxyURL, 30*time.Second)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var loadResp antigravity.LoadCodeAssistResponse
	if err := json.Unmarshal(respBody, &loadResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &loadResp, nil
}

// createAntigravityHTTPClient 创建 HTTP 客户端
func createAntigravityHTTPClient(proxyURL string, timeout time.Duration) *http.Client {
	client := &http.Client{
		Timeout: timeout,
	}

	if proxyURL != "" {
		proxyURLParsed, err := url.Parse(proxyURL)
		if err == nil {
			client.Transport = &http.Transport{
				Proxy: http.ProxyURL(proxyURLParsed),
			}
		}
	}

	return client
}

// renderAntigravityOAuthResult 渲染 OAuth 结果页面
func renderAntigravityOAuthResult(c *gin.Context, success bool, message, projectID, credentials, state string) {
	// 生成 nonce 用于 CSP；失败时拒绝渲染而非降级到可预测值
	nonce := generateNonce()
	if nonce == "" {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// 在 render 层统一做 HTML 转义，覆盖所有调用路径（包括 err.Error() 分支）
	safeMessage := html.EscapeString(message)
	// projectID 与 credentials 一致，用 json.Marshal 生成 JS 字面量：
	// 输出自带引号，且默认转义 < > &（覆盖 </script>），无需手写转义。
	safeProjectID := "null"
	if b, err := json.Marshal(projectID); err == nil {
		safeProjectID = string(b)
	}

	successStr := "true"
	credentialsJSON := "null"
	statusClass := "success"
	statusText := "授权成功"
	iconSVG := `<svg width="80" height="80" viewBox="0 0 80 80" fill="none" xmlns="http://www.w3.org/2000/svg">
		<circle cx="40" cy="40" r="36" fill="#34C759" fill-opacity="0.1"/>
		<circle cx="40" cy="40" r="32" fill="#34C759"/>
		<path d="M25 40L35 50L55 30" stroke="white" stroke-width="4" stroke-linecap="round" stroke-linejoin="round"/>
	</svg>`

	if !success {
		statusClass = "error"
		statusText = "授权失败"
		successStr = "false"
		iconSVG = `<svg width="80" height="80" viewBox="0 0 80 80" fill="none" xmlns="http://www.w3.org/2000/svg">
			<circle cx="40" cy="40" r="36" fill="#FF3B30" fill-opacity="0.1"/>
			<circle cx="40" cy="40" r="32" fill="#FF3B30"/>
			<path d="M30 30L50 50M50 30L30 50" stroke="white" stroke-width="4" stroke-linecap="round"/>
		</svg>`
	} else if credentials != "" {
		// 用 json.Marshal 生成 JS 字符串字面量：默认会把 < > & 转义为 < 等，
		// 覆盖 </script>、控制字符等所有在 <script> 块中危险的字符。
		if b, err := json.Marshal(credentials); err == nil {
			credentialsJSON = string(b)
		}
	}

	htmlContent := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Antigravity OAuth 授权</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }

        body {
            font-family: -apple-system, BlinkMacSystemFont, 'SF Pro Display', 'Segoe UI', Roboto, Helvetica, Arial, sans-serif;
            display: flex;
            justify-content: center;
            align-items: center;
            min-height: 100vh;
            background: #f5f5f7;
            -webkit-font-smoothing: antialiased;
            -moz-osx-font-smoothing: grayscale;
        }

        .container {
            background: white;
            padding: 48px 40px;
            border-radius: 18px;
            box-shadow: 0 4px 24px rgba(0, 0, 0, 0.06);
            text-align: center;
            max-width: 400px;
            width: 90%%;
            animation: slideUp 0.4s cubic-bezier(0.16, 1, 0.3, 1);
        }

        @keyframes slideUp {
            from {
                opacity: 0;
                transform: translateY(20px);
            }
            to {
                opacity: 1;
                transform: translateY(0);
            }
        }

        .icon {
            margin-bottom: 24px;
            animation: scaleIn 0.5s cubic-bezier(0.16, 1, 0.3, 1) 0.1s both;
        }

        @keyframes scaleIn {
            from {
                opacity: 0;
                transform: scale(0.8);
            }
            to {
                opacity: 1;
                transform: scale(1);
            }
        }

        h1 {
            font-size: 28px;
            font-weight: 600;
            color: #1d1d1f;
            margin-bottom: 12px;
            letter-spacing: -0.5px;
        }

        .message {
            font-size: 17px;
            color: #86868b;
            margin-bottom: 32px;
            line-height: 1.5;
        }

        .countdown {
            font-size: 15px;
            color: #86868b;
            margin-bottom: 24px;
        }

        .close-btn {
            width: 100%%;
            padding: 14px 24px;
            background: #007AFF;
            color: white;
            border: none;
            border-radius: 12px;
            cursor: pointer;
            font-size: 17px;
            font-weight: 500;
            transition: all 0.2s ease;
            -webkit-tap-highlight-color: transparent;
        }

        .close-btn:hover {
            background: #0051D5;
            transform: scale(0.98);
        }

        .close-btn:active {
            transform: scale(0.96);
        }

        .success h1 {
            color: #34C759;
        }

        .error h1 {
            color: #FF3B30;
        }
    </style>
</head>
<body>
    <div class="container %s">
        <div class="icon">%s</div>
        <h1>%s</h1>
        <p class="message">%s</p>
        <p class="countdown" id="countdown">窗口将在 3 秒后自动关闭</p>
        <button class="close-btn" id="closeBtn">关闭窗口</button>
    </div>
    <script nonce="%s">
        console.log('Antigravity OAuth callback page loaded');
        console.log('Success:', %s);
        console.log('ProjectID:', %s);
        console.log('Credentials:', %s);

        // 绑定关闭按钮事件（CSP 不允许 inline event handler）
        document.getElementById('closeBtn').addEventListener('click', closeWindow);

        // 发送消息给父窗口
        if (window.opener && !window.opener.closed) {
            console.log('Sending message to parent window');
            window.opener.postMessage({
                type: 'antigravity_oauth_result',
                success: %s,
                projectId: %s,
                credentials: %s
            }, '*');
            console.log('Message sent');
        } else {
            console.error('No opener window found');
        }

        function closeWindow() {
            window.close();
            setTimeout(function() {
                if (!window.closed) {
                    document.getElementById('countdown').innerHTML = '请手动关闭此窗口';
                }
            }, 100);
        }

        // 3秒倒计时后自动关闭
        var countdown = 3;
        var countdownEl = document.getElementById('countdown');
        var countdownInterval = setInterval(function() {
            countdown--;
            if (countdown > 0) {
                countdownEl.innerHTML = '窗口将在 ' + countdown + ' 秒后自动关闭';
            } else {
                clearInterval(countdownInterval);
                console.log('Auto closing window');
                window.close();
                setTimeout(function() {
                    if (!window.closed) {
                        countdownEl.innerHTML = '请手动关闭此窗口';
                    }
                }, 100);
            }
        }, 1000);
    </script>
</body>
</html>
`, statusClass, iconSVG, statusText, safeMessage, nonce, successStr, safeProjectID, credentialsJSON, successStr, safeProjectID, credentialsJSON)

	// 添加安全 headers - 使用 nonce 而非 unsafe-inline
	c.Header("Content-Security-Policy", fmt.Sprintf("default-src 'none'; script-src 'nonce-%s'; style-src 'unsafe-inline'; img-src 'self'", nonce))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Frame-Options", "DENY")
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, htmlContent)
}
