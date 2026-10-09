package controller

import (
	"bytes"
	"context"
	"crypto/rand"
	"done-hub/common"
	"done-hub/common/cache"
	"done-hub/common/logger"
	"done-hub/providers/geminicli"
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
	// OAuth 状态缓存前缀
	OAuthStateCachePrefix = "geminicli_oauth_state:"
	// OAuth 结果缓存前缀
	OAuthResultCachePrefix = "geminicli_oauth_result:"
	// OAuth 状态缓存时长（30分钟）
	OAuthStateCacheDuration = 30 * time.Minute
	// OAuth 结果缓存时长（10分钟）
	OAuthResultCacheDuration = 10 * time.Minute
)

// OAuthStateData OAuth 状态数据
type OAuthStateData struct {
	ChannelID int    `json:"channel_id"`
	ProjectID string `json:"project_id"`
	Proxy     string `json:"proxy"` // 代理配置（JSON 字符串）
	CreatedAt int64  `json:"created_at"`
}

// OAuthResultData OAuth 结果数据
type OAuthResultData struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	ProjectID   string `json:"project_id"`
	Credentials string `json:"credentials"`
	CompletedAt int64  `json:"completed_at"`
}

// StartGeminiCliOAuthRequest 开始 OAuth 认证请求
type StartGeminiCliOAuthRequest struct {
	ChannelID int    `json:"channel_id"` // 可选，新建时为 0
	ProjectID string `json:"project_id"` // 可选，为空时自动检测
	Proxy     string `json:"proxy"`      // 可选，代理配置（JSON 字符串）
}

// StartGeminiCliOAuth 开始 GeminiCli OAuth 认证流程
// POST /api/geminicli/oauth/start
func StartGeminiCliOAuth(c *gin.Context) {
	var req StartGeminiCliOAuthRequest
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

	// 保存 state 到缓存（包含代理配置）
	stateData := OAuthStateData{
		ChannelID: req.ChannelID,
		ProjectID: req.ProjectID,
		Proxy:     req.Proxy, // 保存代理配置，用于后续 token 交换
		CreatedAt: time.Now().Unix(),
	}
	cacheKey := OAuthStateCachePrefix + state
	cache.SetCache(cacheKey, stateData, OAuthStateCacheDuration)

	// 构建 OAuth 授权 URL
	redirectURI := "http://localhost:8080/api/geminicli/oauth/callback"

	params := url.Values{}
	params.Set("client_id", geminicli.DefaultClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", strings.Join([]string{
		"https://www.googleapis.com/auth/cloud-platform",
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
	}, " "))
	params.Set("response_type", "code")
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")
	params.Set("include_granted_scopes", "true")
	params.Set("state", state)

	// 将 project_id 添加到授权 URL 参数中，以便 Google 知道要授权哪个项目
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

// GetGeminiCliOAuthStatus 查询 OAuth 授权状态
// GET /api/geminicli/oauth/status/:state
func GetGeminiCliOAuthStatus(c *gin.Context) {
	state := c.Param("state")
	if state == "" {
		common.APIRespondWithError(c, http.StatusOK, fmt.Errorf("state parameter is required"))
		return
	}

	// 从缓存获取结果
	resultCacheKey := OAuthResultCachePrefix + state
	result, err := cache.GetCache[OAuthResultData](resultCacheKey)
	if err != nil {
		// 还没有结果，返回 pending 状态
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"status":  "pending",
			"message": "授权进行中，请完成授权流程",
		})
		return
	}

	// 返回结果
	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"status":      "completed",
		"result":      result.Success,
		"message":     result.Message,
		"project_id":  result.ProjectID,
		"credentials": result.Credentials,
	})
}

// GeminiCliOAuthCallback OAuth 回调处理
// GET /api/geminicli/oauth/callback
func GeminiCliOAuthCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	errorParam := c.Query("error")

	// 检查是否有错误
	if errorParam != "" {
		errorDesc := c.Query("error_description")
		logger.SysError(fmt.Sprintf("OAuth callback error: %s - %s", errorParam, errorDesc))

		// 清理 error_description，防止 XSS 攻击
		safeErrorDesc := truncateErrorDescription(errorDesc)

		// 保存错误结果到缓存
		if state != "" {
			resultCacheKey := OAuthResultCachePrefix + state
			resultData := OAuthResultData{
				Success:     false,
				Message:     fmt.Sprintf("授权失败: %s", safeErrorDesc),
				CompletedAt: time.Now().Unix(),
			}
			cache.SetCache(resultCacheKey, resultData, OAuthResultCacheDuration)
		}

		renderOAuthResult(c, false, fmt.Sprintf("授权失败: %s", safeErrorDesc), "", "", state)
		return
	}

	// 验证 state
	if state == "" {
		renderOAuthResult(c, false, "无效的 state 参数", "", "", "")
		return
	}

	// 从缓存获取 state 数据
	cacheKey := OAuthStateCachePrefix + state
	stateData, err := cache.GetCache[OAuthStateData](cacheKey)
	if err != nil {
		logger.SysError(fmt.Sprintf("Failed to get OAuth state from cache: %s", err.Error()))

		// 保存错误结果到缓存
		resultCacheKey := OAuthResultCachePrefix + state
		resultData := OAuthResultData{
			Success:     false,
			Message:     "OAuth 状态已过期或无效，请重新开始授权流程",
			CompletedAt: time.Now().Unix(),
		}
		cache.SetCache(resultCacheKey, resultData, OAuthResultCacheDuration)

		renderOAuthResult(c, false, "OAuth 状态已过期或无效，请重新开始授权流程", "", "", state)
		return
	}

	// 使用 code 交换 token（使用会话中保存的代理配置）
	redirectURI := "http://localhost:8080/api/geminicli/oauth/callback"

	tokenResp, err := exchangeCodeForToken(code, redirectURI, stateData.Proxy)
	if err != nil {
		logger.SysError(fmt.Sprintf("Failed to exchange code for token: %s", err.Error()))

		// 保存错误结果到缓存（state 保留，依赖 TTL 过期）
		resultCacheKey := OAuthResultCachePrefix + state
		resultData := OAuthResultData{
			Success:     false,
			Message:     fmt.Sprintf("获取 token 失败: %s", err.Error()),
			CompletedAt: time.Now().Unix(),
		}
		cache.SetCache(resultCacheKey, resultData, OAuthResultCacheDuration)

		renderOAuthResult(c, false, fmt.Sprintf("获取 token 失败: %s", err.Error()), "", "", state)
		return
	}

	// 确定项目 ID
	projectID := stateData.ProjectID
	autoDetected := false

	// 如果没有提供项目 ID，尝试自动检测（使用会话中保存的代理配置）
	if projectID == "" {
		ctx := c.Request.Context()
		logger.LogInfo(ctx, "Project ID 未提供，尝试自动检测...")

		// 优先使用 Code Assist API 获取 project_id（可以自动激活未激活的用户）
		var err error
		projectID, err = fetchProjectIDFromCodeAssist(ctx, tokenResp.AccessToken, stateData.Proxy)
		if err != nil {
			logger.LogInfo(ctx, fmt.Sprintf("Code Assist API 获取 project_id 失败: %s，回退到 Resource Manager API...", err.Error()))

			// 回退到原有的 Cloud Resource Manager API 方式
			projects, err := getUserProjects(tokenResp.AccessToken, stateData.Proxy)
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("Failed to get user projects: %s", err.Error()))

				// 保存错误结果到缓存
				resultCacheKey := OAuthResultCachePrefix + state
				resultData := OAuthResultData{
					Success:     false,
					Message:     fmt.Sprintf("自动检测项目 ID 失败: %s。请手动填写 Project ID 后重新授权", err.Error()),
					CompletedAt: time.Now().Unix(),
				}
				cache.SetCache(resultCacheKey, resultData, OAuthResultCacheDuration)

				renderOAuthResult(c, false, fmt.Sprintf("自动检测项目 ID 失败: %s。请手动填写 Project ID 后重新授权", err.Error()), "", "", state)
				return
			}

			if len(projects) == 0 {
				logger.LogInfo(ctx, "未检测到可访问的项目，使用随机生成的 Project ID")
				projectID = generateRandomProjectID()
			} else {
				// 选择默认项目
				projectID = selectDefaultProject(projects)
				logger.LogInfo(ctx, fmt.Sprintf("通过 Resource Manager API 自动检测到项目 ID: %s (共 %d 个可用项目)", projectID, len(projects)))
			}
		} else {
			logger.LogInfo(ctx, fmt.Sprintf("通过 Code Assist API 自动检测到项目 ID: %s", projectID))
		}

		autoDetected = true
	}

	// 构建完整的凭证
	creds := geminicli.OAuth2Credentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ClientID:     geminicli.DefaultClientID,
		ClientSecret: geminicli.DefaultClientSecret,
		ProjectID:    projectID,
		TokenType:    tokenResp.TokenType,
	}

	// 计算过期时间
	if tokenResp.ExpiresIn > 0 {
		creds.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	// 序列化凭证为 JSON
	credsJSON, err := creds.ToJSON()
	if err != nil {
		logger.SysError(fmt.Sprintf("Failed to serialize credentials: %s", err.Error()))

		// 保存错误结果到缓存
		resultCacheKey := OAuthResultCachePrefix + state
		resultData := OAuthResultData{
			Success:     false,
			Message:     "凭证序列化失败",
			CompletedAt: time.Now().Unix(),
		}
		cache.SetCache(resultCacheKey, resultData, OAuthResultCacheDuration)

		renderOAuthResult(c, false, "凭证序列化失败", "", "", state)
		return
	}

	// 自动启用必需的 API 服务（异步执行，不阻塞响应）
	go func() {
		if err := enableRequiredAPIs(tokenResp.AccessToken, projectID); err != nil {
			logger.SysError(fmt.Sprintf("Failed to enable required APIs for project %s: %s", projectID, err.Error()))
		}
	}()

	// 构建成功消息
	successMessage := "授权成功"
	if autoDetected {
		// 不做 HTML 转义：renderOAuthResult 会对整个 message 统一转义，此处再转会双重转义
		successMessage = fmt.Sprintf("授权成功！已自动检测并使用项目 ID: %s", projectID)
	}

	// 保存结果到缓存，供前端轮询
	resultCacheKey := OAuthResultCachePrefix + state
	resultData := OAuthResultData{
		Success:     true,
		Message:     successMessage,
		ProjectID:   projectID,
		Credentials: credsJSON,
		CompletedAt: time.Now().Unix(),
	}
	cache.SetCache(resultCacheKey, resultData, OAuthResultCacheDuration)

	// 成功后删除 state 缓存（失败路径保留，依赖 TTL 过期）
	cache.DeleteCache(cacheKey)

	// 返回 HTML 页面，通过 postMessage 发送凭证给父窗口（如果有的话）
	renderOAuthResult(c, true, successMessage, projectID, credsJSON, state)
}

// renderOAuthResult 渲染 OAuth 结果页面
func renderOAuthResult(c *gin.Context, success bool, message, projectID, credentials, state string) {
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
    <title>OAuth 授权</title>
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
        console.log('OAuth callback page loaded');
        console.log('Success:', %s);
        console.log('ProjectID:', %s);
        console.log('Credentials:', %s);

        // 绑定关闭按钮事件（CSP 不允许 inline event handler）
        document.getElementById('closeBtn').addEventListener('click', closeWindow);

        // 发送消息给父窗口
        if (window.opener && !window.opener.closed) {
            console.log('Sending message to parent window');
            window.opener.postMessage({
                type: 'geminicli_oauth_result',
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

// enableRequiredAPIs 启用必需的 API 服务
func enableRequiredAPIs(accessToken, projectID string) error {
	requiredServices := []string{
		"geminicloudassist.googleapis.com", // Gemini Cloud Assist API
		"cloudaicompanion.googleapis.com",  // Gemini for Google Cloud API
	}

	headers := map[string]string{
		"Authorization": "Bearer " + accessToken,
		"Content-Type":  "application/json",
		"User-Agent":    "done-hub-geminicli/1.0",
	}

	for _, service := range requiredServices {
		// 检查服务是否已启用
		checkURL := fmt.Sprintf("https://serviceusage.googleapis.com/v1/projects/%s/services/%s", projectID, service)
		req, err := http.NewRequest("GET", checkURL, nil)
		if err != nil {
			logger.SysError(fmt.Sprintf("Failed to create check request for %s: %s", service, err.Error()))
			continue
		}

		for k, v := range headers {
			req.Header.Set(k, v)
		}

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			logger.SysError(fmt.Sprintf("Failed to check service %s: %s", service, err.Error()))
		} else {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				var serviceData map[string]interface{}
				if err := json.NewDecoder(resp.Body).Decode(&serviceData); err == nil {
					if state, ok := serviceData["state"].(string); ok && state == "ENABLED" {
						continue
					}
				}
			}
		}

		// 启用服务
		enableURL := fmt.Sprintf("https://serviceusage.googleapis.com/v1/projects/%s/services/%s:enable", projectID, service)
		req, err = http.NewRequest("POST", enableURL, strings.NewReader("{}"))
		if err != nil {
			logger.SysError(fmt.Sprintf("Failed to create enable request for %s: %s", service, err.Error()))
			continue
		}

		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err = client.Do(req)
		if err != nil {
			logger.SysError(fmt.Sprintf("Failed to enable service %s: %s", service, err.Error()))
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 && resp.StatusCode != 201 && resp.StatusCode != 400 {
			body, _ := io.ReadAll(resp.Body)
			logger.SysError(fmt.Sprintf("Failed to enable service %s: %d - %s", service, resp.StatusCode, string(body)))
		}
	}

	return nil
}

// exchangeCodeForToken 使用授权码交换 token（支持代理）
func exchangeCodeForToken(code, redirectURI, proxyURL string) (*geminicli.TokenRefreshResponse, error) {
	data := url.Values{}
	data.Set("client_id", geminicli.DefaultClientID)
	data.Set("client_secret", geminicli.DefaultClientSecret)
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)
	data.Set("grant_type", "authorization_code")

	req, err := http.NewRequest("POST", geminicli.TokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// 创建 HTTP 客户端
	client := &http.Client{Timeout: 30 * time.Second}

	// 如果有代理配置，设置代理
	if proxyURL != "" {
		proxyURLParsed, err := url.Parse(proxyURL)
		if err == nil {
			client.Transport = &http.Transport{
				Proxy: http.ProxyURL(proxyURLParsed),
			}
			logger.SysLog(fmt.Sprintf("Using proxy for token exchange: %s", proxyURL))
		} else {
			logger.SysError(fmt.Sprintf("Failed to parse proxy URL: %s", err.Error()))
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp geminicli.TokenRefreshError
		if err := json.Unmarshal(bodyBytes, &errResp); err == nil {
			return nil, fmt.Errorf("token exchange failed: %s - %s", errResp.Error, errResp.ErrorDescription)
		}
		return nil, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var tokenResp geminicli.TokenRefreshResponse
	if err := json.Unmarshal(bodyBytes, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &tokenResp, nil
}

// GoogleCloudProject Google Cloud 项目信息
type GoogleCloudProject struct {
	ProjectID      string `json:"projectId"`
	ProjectNumber  string `json:"projectNumber"`
	DisplayName    string `json:"name"`
	LifecycleState string `json:"lifecycleState"`
}

// LoadCodeAssistRequest loadCodeAssist 请求结构
type LoadCodeAssistRequest struct {
	Metadata LoadCodeAssistMetadata `json:"metadata"`
}

// LoadCodeAssistMetadata loadCodeAssist 元数据
type LoadCodeAssistMetadata struct {
	IDEType    string `json:"ideType"`
	Platform   string `json:"platform"`
	PluginType string `json:"pluginType"`
}

// LoadCodeAssistResponse loadCodeAssist 响应结构
type LoadCodeAssistResponse struct {
	CurrentTier             string        `json:"currentTier,omitempty"`
	CloudAICompanionProject string        `json:"cloudaicompanionProject,omitempty"`
	AllowedTiers            []AllowedTier `json:"allowedTiers,omitempty"`
}

// AllowedTier 允许的 tier 信息
type AllowedTier struct {
	ID        string `json:"id"`
	IsDefault bool   `json:"isDefault,omitempty"`
}

// OnboardUserRequest onboardUser 请求结构
type OnboardUserRequest struct {
	TierID   string                 `json:"tierId"`
	Metadata LoadCodeAssistMetadata `json:"metadata"`
}

// OnboardUserResponse onboardUser 响应结构 (长时间运行操作)
type OnboardUserResponse struct {
	Done     bool                   `json:"done"`
	Response *OnboardUserResultData `json:"response,omitempty"`
	Name     string                 `json:"name,omitempty"`
}

// OnboardUserResultData onboardUser 结果数据
type OnboardUserResultData struct {
	CloudAICompanionProject interface{} `json:"cloudaicompanionProject,omitempty"`
}

// CodeAssistEndpoint GeminiCli 内部 API 端点
const CodeAssistEndpoint = "https://cloudcode-pa.googleapis.com"

// GoogleCloudProjectsResponse Google Cloud 项目列表响应
type GoogleCloudProjectsResponse struct {
	Projects []GoogleCloudProject `json:"projects"`
}

// getUserProjects 获取用户可访问的 Google Cloud 项目列表（支持代理）
func getUserProjects(accessToken, proxyURL string) ([]GoogleCloudProject, error) {
	req, err := http.NewRequest("GET", "https://cloudresourcemanager.googleapis.com/v1/projects", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", "geminicli-oauth/1.0")

	// 创建 HTTP 客户端
	client := &http.Client{Timeout: 30 * time.Second}

	// 如果有代理配置，设置代理
	if proxyURL != "" {
		proxyURLParsed, err := url.Parse(proxyURL)
		if err == nil {
			client.Transport = &http.Transport{
				Proxy: http.ProxyURL(proxyURLParsed),
			}
			logger.SysLog(fmt.Sprintf("Using proxy for project detection: %s", proxyURL))
		} else {
			logger.SysError(fmt.Sprintf("Failed to parse proxy URL: %s", err.Error()))
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get projects: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	var projectsResp GoogleCloudProjectsResponse
	if err := json.Unmarshal(bodyBytes, &projectsResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// 只返回活跃的项目
	activeProjects := make([]GoogleCloudProject, 0)
	for _, project := range projectsResp.Projects {
		if project.LifecycleState == "ACTIVE" {
			activeProjects = append(activeProjects, project)
		}
	}

	return activeProjects, nil
}

// selectDefaultProject 从项目列表中选择默认项目
func selectDefaultProject(projects []GoogleCloudProject) string {
	if len(projects) == 0 {
		return ""
	}

	// 策略1：查找包含 "default" 的项目
	for _, project := range projects {
		if strings.Contains(strings.ToLower(project.DisplayName), "default") ||
			strings.Contains(strings.ToLower(project.ProjectID), "default") {
			logger.SysLog(fmt.Sprintf("选择默认项目: %s (%s)", project.ProjectID, project.DisplayName))
			return project.ProjectID
		}
	}

	// 策略2：选择第一个项目
	firstProject := projects[0]
	logger.SysLog(fmt.Sprintf("选择第一个项目作为默认: %s (%s)", firstProject.ProjectID, firstProject.DisplayName))
	return firstProject.ProjectID
}

// fetchProjectIDFromCodeAssist 通过 Code Assist API 获取 project_id
// 优先使用 loadCodeAssist，如果用户未激活则使用 onboardUser 激活
func fetchProjectIDFromCodeAssist(ctx context.Context, accessToken, proxyURL string) (string, error) {
	logger.LogInfo(ctx, "尝试通过 Code Assist API 获取 project_id...")

	// 步骤1: 尝试 loadCodeAssist
	projectID, err := loadCodeAssist(ctx, accessToken, proxyURL)
	if err == nil && projectID != "" {
		logger.LogInfo(ctx, fmt.Sprintf("loadCodeAssist 成功获取 project_id: %s", projectID))
		return projectID, nil
	}

	if err != nil {
		logger.LogInfo(ctx, fmt.Sprintf("loadCodeAssist 失败: %s，尝试 onboardUser...", err.Error()))
	} else {
		logger.LogInfo(ctx, "loadCodeAssist 未返回 project_id（用户可能未激活），尝试 onboardUser...")
	}

	// 步骤2: 回退到 onboardUser
	projectID, err = onboardUser(ctx, accessToken, proxyURL)
	if err != nil {
		return "", fmt.Errorf("onboardUser failed: %w", err)
	}

	if projectID == "" {
		return "", fmt.Errorf("onboardUser completed but no project_id returned")
	}

	logger.LogInfo(ctx, fmt.Sprintf("onboardUser 成功获取 project_id: %s", projectID))
	return projectID, nil
}

// callLoadCodeAssistAPI 调用 loadCodeAssist API 并返回完整响应
func callLoadCodeAssistAPI(accessToken, proxyURL string) (*LoadCodeAssistResponse, error) {
	requestURL := CodeAssistEndpoint + "/v1internal:loadCodeAssist"

	reqBody := LoadCodeAssistRequest{
		Metadata: LoadCodeAssistMetadata{
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
	req.Header.Set("User-Agent", "GeminiCLI/0.1.5 (Windows; AMD64)")

	client := createHTTPClient(proxyURL, 30*time.Second)

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

	var loadResp LoadCodeAssistResponse
	if err := json.Unmarshal(respBody, &loadResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &loadResp, nil
}

// loadCodeAssist 调用 loadCodeAssist API 检查用户是否已激活
func loadCodeAssist(ctx context.Context, accessToken, proxyURL string) (string, error) {
	loadResp, err := callLoadCodeAssistAPI(accessToken, proxyURL)
	if err != nil {
		return "", err
	}

	// 检查用户是否已激活（有 currentTier）
	if loadResp.CurrentTier != "" {
		logger.LogInfo(ctx, fmt.Sprintf("用户已激活，tier: %s", loadResp.CurrentTier))
		return loadResp.CloudAICompanionProject, nil
	}

	// 用户未激活
	logger.LogInfo(ctx, "用户未激活（无 currentTier）")
	return "", nil
}

// onboardUser 调用 onboardUser API 激活用户
func onboardUser(ctx context.Context, accessToken, proxyURL string) (string, error) {
	// 首先获取用户的 tier（复用 callLoadCodeAssistAPI）
	loadResp, err := callLoadCodeAssistAPI(accessToken, proxyURL)
	if err != nil {
		return "", fmt.Errorf("failed to get tier: %w", err)
	}

	// 查找默认的 tier
	tierID := "LEGACY" // 默认值
	for _, tier := range loadResp.AllowedTiers {
		if tier.IsDefault {
			tierID = tier.ID
			logger.LogInfo(ctx, fmt.Sprintf("找到默认 tier: %s", tierID))
			break
		}
	}

	logger.LogInfo(ctx, fmt.Sprintf("用户 tier: %s，开始激活...", tierID))

	requestURL := CodeAssistEndpoint + "/v1internal:onboardUser"

	reqBody := OnboardUserRequest{
		TierID: tierID,
		Metadata: LoadCodeAssistMetadata{
			IDEType:    "ANTIGRAVITY",
			Platform:   "PLATFORM_UNSPECIFIED",
			PluginType: "GEMINI",
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	client := createHTTPClient(proxyURL, 30*time.Second)

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
		req.Header.Set("User-Agent", "GeminiCLI/0.1.5 (Windows; AMD64)")

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

		var onboardResp OnboardUserResponse
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

// createHTTPClient 创建 HTTP 客户端（支持代理）
func createHTTPClient(proxyURL string, timeout time.Duration) *http.Client {
	client := &http.Client{Timeout: timeout}

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
