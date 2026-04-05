package controller

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/notify"
	"done-hub/model"
	"done-hub/types"
	"fmt"
	"net/http"
	"regexp"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

var disableGroup singleflight.Group

// 正则表达式匹配特定的文件访问权限错误，这类错误不应该禁用渠道
var fileAccessPermissionRegex = regexp.MustCompile(`You do not have permission to access the File .+ or it may not exist\.`)

// 模型限制为特定客户端使用的错误，这类错误不应该禁用渠道（渠道本身没问题，只是特定模型不可用）
var modelRestrictedRegex = regexp.MustCompile(`(?i)restricted to .+ clients only`)

func shouldEnableChannel(err error, openAIErr *types.OpenAIErrorWithStatusCode) bool {
	if !config.AutomaticEnableChannelEnabled {
		return false
	}
	if err != nil {
		return false
	}
	if openAIErr != nil {
		return false
	}
	return true
}

func ShouldDisableChannel(channelType int, err *types.OpenAIErrorWithStatusCode) bool {
	if !config.AutomaticDisableChannelEnabled || err == nil || err.LocalError {
		return false
	}

	// 检查是否为特定的文件访问权限错误，这类错误不应该禁用渠道
	if fileAccessPermissionRegex.MatchString(err.OpenAIError.Message) {
		return false
	}

	// 检查是否为模型限制为特定客户端的错误，这类错误不应该禁用渠道
	if modelRestrictedRegex.MatchString(err.OpenAIError.Message) {
		return false
	}

	// 状态码检查（优先级最高）
	if err.StatusCode == http.StatusUnauthorized {
		return true
	}
	// 403 Forbidden 自动禁用（Gemini, Codex, GeminiCli, ClaudeCode）
	if err.StatusCode == http.StatusForbidden {
		switch channelType {
		case config.ChannelTypeGemini, config.ChannelTypeCodex, config.ChannelTypeGeminiCli, config.ChannelTypeClaudeCode:
			return true
		}
	}

	// 禁用关键词检查
	if common.DisableChannelKeywordsInstance.IsContains(err.OpenAIError.Message) {
		return true
	}

	// 错误代码检查
	switch err.OpenAIError.Code {
	case "invalid_api_key", "account_deactivated", "billing_not_active":
		return true
	}

	// 错误类型检查
	switch err.OpenAIError.Type {
	case "insufficient_quota", "authentication_error", "permission_error", "forbidden":
		return true
	}

	switch err.OpenAIError.Param {
	case "PERMISSIONDENIED":
		return true
	}

	return false
}

// disable & notify
func DisableChannel(channelId int, channelName string, reason string, sendNotify bool) {
	key := fmt.Sprintf("disable_channel_%d", channelId)

	// 使用 singleflight 确保同一渠道的并发禁用请求只执行一次
	_, err, _ := disableGroup.Do(key, func() (interface{}, error) {
		// 检查渠道当前状态，避免重复禁用和重复发送邮件
		channel, err := model.GetChannelById(channelId)
		if err != nil {
			return nil, err
		}

		// 如果渠道已经被禁用，不需要重复操作
		if channel.Status == config.ChannelStatusAutoDisabled || channel.Status == config.ChannelStatusManuallyDisabled {
			return nil, nil
		}

		// 执行禁用操作
		model.UpdateChannelStatusById(channelId, config.ChannelStatusAutoDisabled)

		// 发送通知
		if sendNotify {
			subject := fmt.Sprintf("通道「%s」（#%d）已被禁用", channelName, channelId)
			content := fmt.Sprintf("通道「%s」（#%d）已被禁用，原因：%s", channelName, channelId, reason)
			notify.Send(subject, content)
		}

		return nil, nil
	})

	// 处理错误
	if err != nil {
		logger.SysError(fmt.Sprintf("DisableChannel failed for channel %d: %v", channelId, err))
	}
}

// enable & notify
func EnableChannel(channelId int, channelName string, sendNotify bool) {
	model.UpdateChannelStatusById(channelId, config.ChannelStatusEnabled)
	if !sendNotify {
		return
	}

	subject := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
	content := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
	notify.Send(subject, content)
}

func RelayNotFound(c *gin.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

// validateAndUseInviteCodeForOAuth 为第三方登录验证和使用邀请码
// 返回值：inviteCode string, error
func validateAndUseInviteCodeForOAuth(c *gin.Context, tx *gorm.DB) (string, error) {
	// 如果未启用邀请码注册，直接返回
	if !config.InviteCodeRegisterEnabled {
		return "", nil
	}

	session := sessions.Default(c)
	inviteCodeInterface := session.Get("oauth_invite_code")
	if inviteCodeInterface == nil {
		return "", fmt.Errorf("NEED_INVITE_CODE:管理员开启了邀请码注册，请提供邀请码")
	}

	// 安全的类型断言
	inviteCode, ok := inviteCodeInterface.(string)
	if !ok {
		return "", fmt.Errorf("邀请码格式错误")
	}

	if inviteCode == "" {
		return "", fmt.Errorf("邀请码不能为空")
	}

	// 验证邀请码
	if err := model.CheckInviteCode(inviteCode); err != nil {
		return "", err
	}

	// 在事务中使用邀请码
	if err := model.UseInviteCodeWithTx(tx, inviteCode); err != nil {
		return "", err
	}

	// 清除会话中的邀请码信息
	session.Delete("oauth_invite_code")
	if err := session.Save(); err != nil {
		// 记录日志但不影响主流程
		logger.SysError("Failed to save session after clearing invite code: " + err.Error())
	}

	return inviteCode, nil
}
