package controller

import (
	"context"
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/oidc"
	"done-hub/common/utils"
	"done-hub/model"
	"errors"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
	"strconv"
)

func OIDCEndpoint(c *gin.Context) {
	if !config.OIDCAuthEnabled {
		c.JSON(http.StatusOK, gin.H{
			"message": "管理员未开启通过OIDC登录",
			"success": false,
		})
		return
	}
	oidcConfig, err := oidc.GetOIDCConfigInstance()
	if err != nil {
		logger.SysError("获取 OIDC 配置失败, err: " + err.Error())
		c.JSON(http.StatusOK, gin.H{
			"message": "获取 OIDC 配置失败",
			"success": false,
		})
		return
	}

	session := sessions.Default(c)
	state := utils.GetRandomString(12)
	session.Set("oauth_state", state)
	loginURL := oidcConfig.LoginURL(state)
	err = session.Save()
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
		"data":    loginURL,
	})
}

// OIDCAuth 通过OIDC登录
// 首先通过OIDC ID进行登录、如果登录失败尝试使用USERNAME 进行登录（遵循用户禁用条件），如果OIDC ID和USERNAME都不存在则注册新用户（遵循是否开启注册功能条件）
func OIDCAuth(c *gin.Context) {
	if !config.OIDCAuthEnabled {
		c.JSON(http.StatusOK, gin.H{
			"message": "管理员未开启通过OIDC登录",
			"success": false,
		})
		return
	}

	// 验证state参数
	session := sessions.Default(c)
	state := c.Query("state")
	if state == "" || session.Get("oauth_state") == nil || state != session.Get("oauth_state").(string) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "state is empty or not same",
		})
		return
	}

	// 获取OIDC配置
	oidcConfig, err := oidc.GetOIDCConfigInstance()
	if err != nil {
		logger.SysError("获取 OIDC 配置失败, err: " + err.Error())
		c.JSON(http.StatusOK, gin.H{
			"message": "获取 OIDC 配置失败",
			"success": false,
		})
		return
	}

	// 处理授权码并获取token
	code := c.Query("code")
	ctx := context.Background()
	token, err := oidcConfig.OAuth2Config.Exchange(ctx, code)
	if err != nil {
		c.String(http.StatusBadRequest, "Failed to exchange token: %v", err)
		return
	}

	// 验证ID Token
	idToken, err := oidcConfig.Verifier.Verify(ctx, token.Extra("id_token").(string))
	if err != nil {
		c.String(http.StatusBadRequest, "Failed to verify ID token: %v", err)
		return
	}

	// 检测OIDC用户ID
	if idToken.Subject == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "ID Token 中没有 Subject",
		})
		return
	}

	// 解析用户信息
	claims := make(map[string]interface{})
	if err := idToken.Claims(&claims); err != nil {
		c.String(http.StatusBadRequest, "Failed to parse claims: %v", err)
		return
	}

	// 获取用户名
	userName, ok := claims[config.OIDCUsernameClaims]
	if !ok || userName == nil {
		c.JSON(http.StatusOK, gin.H{
			"message": "用户没有OIDC登录权限",
			"success": false,
		})
		return
	}

	// 初始化用户对象
	user := model.User{
		Username: userName.(string),
		OidcId:   idToken.Subject,
	}

	// 尝试通过OIDCid查询用户
	if err = user.FillUserByOidcId(); err == nil {
		if user.Status == config.UserStatusEnabled {
			setupLogin(&user, c)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message": "用户已被封禁或不存在",
			"success": false,
		})
		return
	}

	// OIDC ID 查不到用户：不按用户名绑定已有账号（OIDC 用户名声明可能被用户修改，会导致账号被接管），直接注册新用户
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.SysError("查询用户错误: " + err.Error())
		c.JSON(http.StatusOK, gin.H{
			"message": err.Error(),
			"success": false,
		})
		return
	}

	// 注册新用户
	if !config.RegisterEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "管理员关闭了新用户注册",
		})
		return
	}

	// 检测推荐码
	var inviterId int
	affCode := c.Query("aff")
	if affCode != "" {
		inviterId, _ = model.GetUserIdByAffCode(affCode)
	}

	// 填充用户信息
	user.Username = userName.(string)
	if model.IsUsernameAlreadyTaken(user.Username) {
		user.Username = "oidc_" + strconv.Itoa(model.GetMaxUserId()+1)
	}
	if email, ok := claims["email"]; ok && email != nil {
		emailStr := email.(string)
		// 验证 OIDC 提供的邮箱格式
		if emailStr != "" {
			if err := common.ValidateEmailStrict(emailStr); err != nil {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": "邮箱格式不符合要求",
				})
				return
			}
		}
		user.Email = emailStr
	}
	if displayName, ok := claims["displayName"]; ok && displayName != nil {
		user.DisplayName = displayName.(string)
	}
	if avatarUrl, ok := claims["avatar"]; ok && avatarUrl != nil {
		user.AvatarUrl = avatarUrl.(string)
	}
	user.OidcId = idToken.Subject
	user.Role = config.RoleCommonUser
	user.Status = config.UserStatusEnabled

	// 使用事务创建用户并处理邀请码
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		// 验证和使用邀请码（如果启用）
		usedInviteCode, err := validateAndUseInviteCodeForOAuth(c, tx)
		if err != nil {
			return err
		}

		// 设置邀请人ID（使用原有推荐码逻辑）
		if inviterId > 0 {
			user.InviterId = inviterId
		}

		// 设置使用的邀请码
		if usedInviteCode != "" {
			user.UsedInviteCode = usedInviteCode
		}

		// 在事务中创建用户
		return user.InsertWithTx(tx, user.InviterId)
	})

	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	setupLogin(&user, c)
}
