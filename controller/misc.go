package controller

import (
	"crypto/sha256"
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/stmp"
	"done-hub/common/telegram"
	"done-hub/model"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Health 免鉴权健康检查端点，供负载均衡/容器存活探针使用。
// 主要面向 RELAY_ONLY 从节点：此模式下 /api/status 被关闭，探针改用此端点。
func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func GetStatus(c *gin.Context) {
	telegramBot := ""
	if telegram.TGEnabled {
		telegramBot = telegram.TGBot.User.Username
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"version":                config.Version,
			"start_time":             config.StartTime,
			"email_verification":     config.EmailVerificationEnabled,
			"github_oauth":           config.GitHubOAuthEnabled,
			"github_client_id":       config.GitHubClientId,
			"linuxDo_oauth":          config.LinuxDoOAuthEnabled,
			"linuxDo_client_id":      config.LinuxDoClientId,
			"oidc_auth":              config.OIDCAuthEnabled,
			"lark_login":             config.LarkAuthEnabled,
			"lark_client_id":         config.LarkClientId,
			"system_name":            config.SystemName,
			"logo":                   config.Logo,
			"language":               config.Language,
			"footer_html":            config.Footer,
			"analytics_code":         config.AnalyticsCode,
			"wechat_qrcode":          config.WeChatAccountQRCodeImageURL,
			"invite_code_register":   config.InviteCodeRegisterEnabled,
			"user_agreement_enabled": config.UserAgreementEnabled,
			"privacy_policy_enabled": config.PrivacyPolicyEnabled,
			"wechat_login":           config.WeChatAuthEnabled,
			"server_address":         config.ServerAddress,
			"turnstile_check":        config.TurnstileCheckEnabled,
			"turnstile_site_key":     config.TurnstileSiteKey,
			"top_up_link":            config.TopUpLink,
			"chat_link":              config.ChatLink,
			"quota_per_unit":         config.QuotaPerUnit,
			"display_in_currency":    config.DisplayInCurrencyEnabled,
			"telegram_bot":           telegramBot,
			"mj_notify_enabled":      config.MjNotifyEnabled,
			"builtin_chat_enabled":   config.BuiltinChatEnabled,
			"chat_links":             config.ChatLinks,
			"PaymentUSDRate":         config.PaymentUSDRate,
			"PaymentMinAmount":       config.PaymentMinAmount,
			"RechargeDiscount":       config.RechargeDiscount,
			"EnableSafe":             config.EnableSafe,
			"SafeToolName":           config.SafeToolName,
			"SafeKeyWords":           config.SafeKeyWords,
			"UserInvoiceMonth":       config.UserInvoiceMonth,
			"UptimeDomain":           config.UPTIMEKUMA_DOMAIN,
			"UptimePageName":         config.UPTIMEKUMA_STATUS_PAGE_NAME,
			"UptimeEnabled":          config.UPTIMEKUMA_ENABLE,
			"GeminiAPIEnabled":       config.GeminiAPIEnabled,
			"ClaudeAPIEnabled":       config.ClaudeAPIEnabled,
			"max_log_query_days":     MaxLogQueryDays,
		},
	})
}

func GetNotice(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    config.GlobalOption.Get("Notice"),
	})
}

func GetAbout(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    config.GlobalOption.Get("About"),
	})
}

func GetHomePageContent(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    config.GlobalOption.Get("HomePageContent"),
	})
}

func GetUserAgreement(c *gin.Context) {
	content := ""
	if config.UserAgreementEnabled {
		content = config.GlobalOption.Get("UserAgreement")
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    content,
	})
}

func GetPrivacyPolicy(c *gin.Context) {
	content := ""
	if config.PrivacyPolicyEnabled {
		content = config.GlobalOption.Get("PrivacyPolicy")
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    content,
	})
}

// agreementVersion 用协议正文的哈希作为版本标识：正文变则版本变，正文为空则无版本。
// 注册写入、GetSelf 判定、AgreeToTerms 写入三处同源同算法。
func agreementVersion(content string) string {
	if content == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", sum)[:32]
}

func SendEmailVerification(c *gin.Context) {
	email := c.Query("email")
	if err := common.ValidateEmailStrict(email); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "邮箱格式不符合要求",
		})
		return
	}
	if config.EmailDomainRestrictionEnabled {
		allowed := false
		for _, domain := range config.EmailDomainWhitelist {
			if strings.HasSuffix(email, "@"+domain) {
				allowed = true
				break
			}
		}
		if !allowed {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "管理员启用了邮箱域名白名单，您的邮箱地址的域名不在白名单中",
			})
			return
		}
	}
	if model.IsEmailAlreadyTaken(email) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "邮箱地址已被占用",
		})
		return
	}
	code := common.GenerateVerificationCode(6)
	common.RegisterVerificationCodeWithKey(email, code, common.EmailVerificationPurpose)
	err := stmp.SendVerificationCodeEmail(email, code)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func SendPasswordResetEmail(c *gin.Context) {
	email := c.Query("email")
	if err := common.Validate.Var(email, "required,email"); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}

	user := &model.User{
		Email: email,
	}

	if err := user.FillUserByEmail(); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "该邮箱地址未注册",
		})
		return
	}

	userName := user.DisplayName
	if userName == "" {
		userName = user.Username
	}

	code := common.GenerateVerificationCode(0)
	common.RegisterVerificationCodeWithKey(email, code, common.PasswordResetPurpose)
	link := fmt.Sprintf("%s/user/reset?email=%s&token=%s", config.ServerAddress, email, code)
	err := stmp.SendPasswordResetEmail(userName, email, link)

	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

type PasswordResetRequest struct {
	Email string `json:"email"`
	Token string `json:"token"`
}

func ResetPassword(c *gin.Context) {
	var req PasswordResetRequest
	err := json.NewDecoder(c.Request.Body).Decode(&req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}

	if req.Email == "" || req.Token == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}
	if !common.VerifyCodeWithKey(req.Email, req.Token, common.PasswordResetPurpose) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "重置链接非法或已过期",
		})
		return
	}
	password := common.GenerateVerificationCode(12)
	err = model.ResetUserPasswordByEmail(req.Email, password)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	common.DeleteKey(req.Email, common.PasswordResetPurpose)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    password,
	})
}
