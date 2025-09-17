package controller

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/stmp"
	"done-hub/model"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// 基础状态（精简版，去除业务字段）
// GetStatus godoc
// @Summary Get system status
// @Description 获取框架基础状态与站点信息
// @Tags System
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /status [get]
func GetStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"version":            config.Version,
			"start_time":         config.StartTime,
			"email_verification": config.EmailVerificationEnabled,
			"oidc_auth":          config.OIDCAuthEnabled,
			"system_name":        config.SystemName,
			"logo":               config.Logo,
			"language":           config.Language,
			"footer_html":        config.Footer,
			"server_address":     config.ServerAddress,
			"turnstile_check":    config.TurnstileCheckEnabled,
			"turnstile_site_key": config.TurnstileSiteKey,
			"PaymentUSDRate":     config.PaymentUSDRate,
			"PaymentMinAmount":   config.PaymentMinAmount,
		},
	})
}

// 发送邮箱验证码（注册/绑定等场景通用）
// SendEmailVerification godoc
// @Summary Send email verification code
// @Description 发送邮箱验证码（用于注册/绑定等场景）
// @Tags Email
// @Produce json
// @Param email query string true "邮箱地址"
// @Success 200 {object} map[string]interface{}
// @Router /verification [get]
func SendEmailVerification(c *gin.Context) {
	email := c.Query("email")
	if err := common.ValidateEmailStrict(email); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "邮箱格式不符合要求"})
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
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "邮箱域名不在白名单中"})
			return
		}
	}
	if model.IsEmailAlreadyTaken(email) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "邮箱地址已被占用"})
		return
	}
	code := common.GenerateVerificationCode(6)
	common.RegisterVerificationCodeWithKey(email, code, common.EmailVerificationPurpose)
	if err := stmp.SendVerificationCodeEmail(email, code); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// 发送重置密码邮件（返回带 token 的链接）
// SendPasswordResetEmail godoc
// @Summary Send password reset email
// @Description 发送重置密码链接到邮箱
// @Tags Email
// @Produce json
// @Param email query string true "邮箱地址"
// @Success 200 {object} map[string]interface{}
// @Router /reset_password [get]
func SendPasswordResetEmail(c *gin.Context) {
	email := c.Query("email")
	if err := common.Validate.Var(email, "required,email"); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	user := &model.User{Email: email}
	if err := user.FillUserByEmail(); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "该邮箱地址未注册"})
		return
	}
	userName := user.DisplayName
	if userName == "" {
		userName = user.Username
	}
	code := common.GenerateVerificationCode(0)
	common.RegisterVerificationCodeWithKey(email, code, common.PasswordResetPurpose)
	link := fmt.Sprintf("%s/user/reset?email=%s&token=%s", config.ServerAddress, email, code)
	if err := stmp.SendPasswordResetEmail(userName, email, link); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

type PasswordResetRequest struct {
	Email string `json:"email"`
	Token string `json:"token"`
}

// 重置密码：校验 token，通过后直接重置为随机密码并返回
// ResetPassword godoc
// @Summary Reset password with token
// @Description 通过邮箱令牌重置密码，返回新密码
// @Tags User
// @Accept json
// @Produce json
// @Param body body PasswordResetRequest true "重置请求"
// @Success 200 {object} map[string]interface{}
// @Router /user/reset [post]
func ResetPassword(c *gin.Context) {
	var req PasswordResetRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	if req.Email == "" || req.Token == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	if !common.VerifyCodeWithKey(req.Email, req.Token, common.PasswordResetPurpose) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "重置链接非法或已过期"})
		return
	}
	password := common.GenerateVerificationCode(12)
	if err := model.ResetUserPasswordByEmail(req.Email, password); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	common.DeleteKey(req.Email, common.PasswordResetPurpose)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": password})
}
