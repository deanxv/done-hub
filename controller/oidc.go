package controller

import (
	"context"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/oidc"
	"done-hub/common/utils"
	"done-hub/model"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// OIDCEndpoint 返回登录跳转地址（或直接 302 重定向也可）
// OIDCEndpoint godoc
// @Summary Get OIDC login URL
// @Description 返回 OIDC 登录跳转地址
// @Tags OIDC
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /oauth/endpoint [get]
func OIDCEndpoint(c *gin.Context) {
	if !config.OIDCAuthEnabled {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "未启用OIDC"})
		return
	}
	cfg, err := oidc.GetOIDCConfigInstance()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	// 生成并保存 state
	state := utils.GetRandomString(16)
	session := sessions.Default(c)
	session.Set("oidc_state", state)
	_ = session.Save()

	loginURL := cfg.LoginURL(state)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": loginURL})
}

// OIDCAuth 回调入口：校验 state -> 交换 code -> 校验IDToken -> 登陆/创建用户
// OIDCAuth godoc
// @Summary OIDC callback
// @Description OIDC 回调，内部完成用户登录或创建
// @Tags OIDC
// @Produce json
// @Param state query string true "state"
// @Param code  query string true "授权码"
// @Success 200 {object} map[string]interface{}
// @Router /oauth/oidc [get]
func OIDCAuth(c *gin.Context) {
	if !config.OIDCAuthEnabled {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "未启用OIDC"})
		return
	}
	cfg, err := oidc.GetOIDCConfigInstance()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	// 校验 state
	session := sessions.Default(c)
	stateInSession, _ := session.Get("oidc_state").(string)
	state := c.Query("state")
	if state == "" || stateInSession == "" || state != stateInSession {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "非法的回调请求"})
		return
	}

	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "缺少授权码"})
		return
	}

	// 交换 token
	token, err := cfg.OAuth2Config.Exchange(context.Background(), code)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "授权失败: " + err.Error()})
		return
	}

	rawIDToken, _ := token.Extra("id_token").(string)
	if rawIDToken == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "授权失败：缺少ID Token"})
		return
	}

	idToken, err := cfg.Verifier.Verify(context.Background(), rawIDToken)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "ID Token 验证失败: " + err.Error()})
		return
	}

	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "解析ID Token失败: " + err.Error()})
		return
	}

	// 优先使用配置声明字段作为用户名
	username := ""
	if claimKey := strings.TrimSpace(config.OIDCUsernameClaims); claimKey != "" {
		if v, ok := claims[claimKey]; ok {
			username = fmt.Sprint(v)
		}
	}
	if username == "" {
		if v, ok := claims["preferred_username"]; ok {
			username = fmt.Sprint(v)
		} else if v, ok := claims["email"]; ok {
			username = fmt.Sprint(v)
		} else if v, ok := claims["sub"]; ok {
			username = fmt.Sprint(v)
		}
	}
	if username == "" {
		username = "user_" + utils.GetRandomString(6)
	}

	// 尝试读取 email 与 sub 用于绑定
	email := ""
	if v, ok := claims["email"]; ok {
		email = fmt.Sprint(v)
	}
	sub := ""
	if v, ok := claims["sub"]; ok {
		sub = fmt.Sprint(v)
	}

	// 根据 OIDC sub 或 email 查找用户
	var user *model.User
	if sub != "" {
		if u, err := model.FindUserByField("oidc_id", sub); err == nil && u != nil {
			user = u
		}
	}
	if user == nil && email != "" {
		if u, err := model.FindUserByField("email", email); err == nil && u != nil {
			user = u
		}
	}

	if user == nil {
		// 创建用户（随机密码以满足校验；不强制设置邮箱）
		newUser := model.User{
			Username:    username,
			Password:    utils.GetRandomString(12),
			DisplayName: username,
			OidcId:      sub,
			Email:       email,
			Status:      config.UserStatusEnabled,
		}
		if err := newUser.Insert(0); err != nil {
			logger.SysError("OIDC 创建用户失败: " + err.Error())
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "创建用户失败"})
			return
		}
		user = &newUser
	} else {
		// 回填 OidcId
		if user.OidcId == "" && sub != "" {
			user.OidcId = sub
			_ = user.Update(false)
		}
	}

	// 完成登录
	setupLogin(user, c)
}
