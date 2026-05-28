package relay

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/utils"
	"done-hub/metrics"
	"done-hub/model"
	"done-hub/relay/relay_util"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func Relay(c *gin.Context) {
	// 在请求完成后清理缓存的请求体，防止内存泄漏
	defer func() {
		c.Set(config.GinRequestBodyKey, nil)
		c.Set(config.GinProcessedBodyKey, nil)
		c.Set(config.GinProcessedBodyIsVertexAI, nil)
		c.Set(config.GinRawMapBodyKey, nil)
		c.Set(config.GinProcessedBytesKey, nil)
		c.Set(config.GinProcessedBytesIsVertexAI, nil)
	}()

	relay := Path2Relay(c, c.Request.URL.Path)
	if relay == nil {
		common.AbortWithMessage(c, http.StatusNotFound, "Not Found")
		return
	}

	// Apply pre-mapping before setRequest to ensure request body modifications take effect
	applyPreMappingBeforeRequest(c)

	if err := relay.setRequest(); err != nil {
		openaiErr := common.StringErrorWrapperLocal(err.Error(), "one_hub_error", http.StatusBadRequest)
		relay.HandleJsonError(openaiErr)
		return
	}

	c.Set("is_stream", relay.IsStream())
	if err := relay.setProvider(relay.getOriginalModel()); err != nil {
		openaiErr := common.StringErrorWrapperLocal(err.Error(), "one_hub_error", http.StatusServiceUnavailable)
		relay.HandleJsonError(openaiErr)
		return
	}

	heartbeat := relay.SetHeartbeat(relay.IsStream())
	if heartbeat != nil {
		defer heartbeat.Close()
	}

	apiErr, done := RelayHandler(relay)
	if apiErr == nil {
		metrics.RecordProvider(c, 200)
		return
	}

	channel := relay.getProvider().GetChannel()
	go processChannelRelayError(c.Request.Context(), channel.Id, channel.Name, apiErr, channel.Type)

	retryTimes := config.RetryTimes
	// 在重试开始前计算并缓存总渠道数，避免重试过程中动态变化
	groupName := c.GetString("token_group")
	if groupName == "" {
		groupName = c.GetString("group")
	}
	modelName := c.GetString("new_model")
	totalChannelsAtStart := model.ChannelGroup.CountAvailableChannels(groupName, modelName)

	if done || !shouldRetry(c, apiErr, channel.Type) {
		logger.LogError(c.Request.Context(), fmt.Sprintf("retry_skip model=%s channel_id=%d status_code=%d done=%t should_retry=%t total_channels=%d error=\"%s\"",
			modelName, channel.Id, apiErr.StatusCode, done, shouldRetry(c, apiErr, channel.Type), totalChannelsAtStart, utils.TruncateBase64InMessage(apiErr.OpenAIError.Message)))
		retryTimes = 0
	}

	startTime := c.GetTime("requestStartTime")
	timeout := time.Duration(config.RetryTimeOut) * time.Second

	// 实际重试次数 = min(配置的重试数, 可用渠道数)
	actualRetryTimes := retryTimes
	if totalChannelsAtStart < retryTimes {
		actualRetryTimes = totalChannelsAtStart
	}

	c.Set("total_channels_at_start", totalChannelsAtStart)
	c.Set("actual_retry_times", actualRetryTimes)
	c.Set("attempt_count", 1) // 初始化尝试计数

	// 记录初始失败 - 使用统一的结构化日志格式
	logger.LogError(c.Request.Context(), fmt.Sprintf("retry_start model=%s channel_id=%d total_channels=%d config_max_retries=%d actual_max_retries=%d status_code=%d error=\"%s\"",
		modelName, channel.Id, totalChannelsAtStart, retryTimes, actualRetryTimes, apiErr.StatusCode, utils.TruncateBase64InMessage(apiErr.OpenAIError.Message)))

	if apiErr.StatusCode != http.StatusUnauthorized && apiErr.StatusCode != http.StatusForbidden {
		c.Set("first_non_auth_error", apiErr)
	}

	// breakReason 区分循环退出原因，避免最终日志一律打成 retry_exhausted 而产生误导。
	// 默认值 "exhausted" 表示循环自然跑完（真的把可用渠道用完了）。
	// 注意：done==true 或 shouldRetry==false 触发的早跳过路径上方会把 retryTimes 置 0，
	// 导致 actualRetryTimes=0，循环根本不进入；此时若不预置 "skipped"，最终会落到
	// "retry_exhausted reason=exhausted actual_max_retries=0" 的自相矛盾日志。
	breakReason := "exhausted"
	if actualRetryTimes == 0 {
		breakReason = "skipped"
	}

	for i := actualRetryTimes; i > 0; i-- {
		// 冻结通道并记录是否应用了冷却
		cooldownApplied := shouldCooldowns(c, channel, apiErr)

		if time.Since(startTime) > timeout {
			logger.LogError(c.Request.Context(), fmt.Sprintf("retry_timeout model=%s channel_id=%d elapsed_time=%.2fs timeout=%.2fs",
				modelName, channel.Id, time.Since(startTime).Seconds(), timeout.Seconds()))
			apiErr = common.StringErrorWrapperLocal("重试超时，上游负载已饱和，请稍后再试", "system_error", http.StatusTooManyRequests)
			breakReason = "timeout"
			break
		}

		if err := relay.setProvider(relay.getOriginalModel()); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("retry_provider_error model=%s channel_id=%d error=\"%s\"",
				modelName, channel.Id, err.Error()))
			breakReason = "provider_error"
			break
		}

		channel = relay.getProvider().GetChannel()

		// 更新尝试计数
		attemptCount := c.GetInt("attempt_count")
		c.Set("attempt_count", attemptCount+1)

		// 计算剩余渠道数
		filters := buildChannelFilters(c, modelName)
		skipChannelIds, _ := utils.GetGinValue[[]int](c, "skip_channel_ids")
		tempFilters := append(filters, model.FilterChannelId(skipChannelIds))
		remainChannels := model.ChannelGroup.CountAvailableChannels(groupName, modelName, tempFilters...)

		// 获取实际重试次数
		actualRetryTimes := c.GetInt("actual_retry_times")

		// 记录重试尝试 - 使用统一的结构化日志格式
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("retry_attempt model=%s channel_id=%d attempt=%d/%d remaining_channels=%d total_channels=%d cooldown_applied=%t",
			modelName, channel.Id, attemptCount, actualRetryTimes, remainChannels, c.GetInt("total_channels_at_start"), cooldownApplied))

		apiErr, done = RelayHandler(relay)
		if apiErr == nil {
			// 重试成功
			logger.LogInfo(c.Request.Context(), fmt.Sprintf("retry_success model=%s channel_id=%d attempt=%d/%d total_channels=%d",
				modelName, channel.Id, attemptCount, actualRetryTimes, c.GetInt("total_channels_at_start")))
			metrics.RecordProvider(c, 200)
			return
		}

		// 记录重试失败
		logger.LogError(c.Request.Context(), fmt.Sprintf("retry_failed model=%s channel_id=%d attempt=%d/%d status_code=%d error_type=\"%s\" error=\"%s\"",
			modelName, channel.Id, attemptCount, actualRetryTimes, apiErr.StatusCode, apiErr.OpenAIError.Type, utils.TruncateBase64InMessage(apiErr.OpenAIError.Message)))

		if apiErr.StatusCode != http.StatusUnauthorized && apiErr.StatusCode != http.StatusForbidden {
			if _, exists := c.Get("first_non_auth_error"); !exists {
				c.Set("first_non_auth_error", apiErr)
			}
		}

		go processChannelRelayError(c.Request.Context(), channel.Id, channel.Name, apiErr, channel.Type)
		if done || !shouldRetry(c, apiErr, channel.Type) {
			logger.LogError(c.Request.Context(), fmt.Sprintf("retry_stop_condition model=%s channel_id=%d attempt=%d/%d done=%t should_retry=%t",
				modelName, channel.Id, attemptCount, actualRetryTimes, done, shouldRetry(c, apiErr, channel.Type)))
			breakReason = "stop_condition"
			break
		}
	}

	// 记录最终失败：循环自然跑完用 retry_exhausted，中途 break 用 retry_aborted + reason
	finalAttempt := c.GetInt("attempt_count")
	actualRetryTimes = c.GetInt("actual_retry_times")
	finalLogTag := "retry_exhausted"
	if breakReason != "exhausted" {
		finalLogTag = "retry_aborted"
	}
	logger.LogError(c.Request.Context(), fmt.Sprintf("%s reason=%s model=%s channel_id=%d total_attempts=%d total_channels=%d config_max_retries=%d actual_max_retries=%d status_code=%d error=\"%s\"",
		finalLogTag, breakReason, modelName, channel.Id, finalAttempt, c.GetInt("total_channels_at_start"), retryTimes, actualRetryTimes, apiErr.StatusCode, utils.TruncateBase64InMessage(apiErr.OpenAIError.Message)))

	if apiErr != nil {
		// 确保 channel_type 存在，用于 FilterOpenAIErr 正确过滤错误
		// 如果 channel_type 为 0（可能在重试失败后被清空），使用最后一个渠道的类型
		if c.GetInt("channel_type") == 0 && channel != nil {
			c.Set("channel_type", channel.Type)
		}

		if heartbeat != nil && heartbeat.IsSafeWriteStream() {
			relay.HandleStreamError(apiErr)
			return
		}

		relay.HandleJsonError(apiErr)
	}
}

func RelayHandler(relay RelayBaseInterface) (err *types.OpenAIErrorWithStatusCode, done bool) {
	promptTokens, tonkeErr := relay.getPromptTokens()
	if tonkeErr != nil {
		err = common.ErrorWrapperLocal(tonkeErr, "token_error", http.StatusBadRequest)
		done = true
		return
	}

	usage := &types.Usage{
		PromptTokens: promptTokens,
	}

	relay.getProvider().SetUsage(usage)

	quota := relay_util.NewQuota(relay.getContext(), relay.getModelName(), promptTokens)
	if err = quota.PreQuotaConsumption(); err != nil {
		done = true
		return
	}

	err, done = relay.send()
	// 最后处理流式中断时计算tokens
	if usage.CompletionTokens == 0 && usage.TextBuilder.Len() > 0 {
		usage.CompletionTokens = common.CountTokenText(usage.TextBuilder.String(), relay.getModelName())
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}

	// 即使出错，只要有实际输出就记录计费，避免上游已计费但本地无记录。
	// CompletionTokens 来自上游返回的 usage（image_*.go 在 ErrorHandle 前也会落 usage），
	// 是"上游真的处理了请求"的可靠信号；PromptTokens 不行，它在 send 之前就被本地 tokenize 填了。
	if err != nil {
		if usage.CompletionTokens > 0 {
			quota.SetFirstResponseTime(relay.GetFirstResponseTime())
			quota.Consume(relay.getContext(), usage, relay.IsStream())
		} else {
			quota.Undo(relay.getContext())
		}
		return
	}

	quota.SetFirstResponseTime(relay.GetFirstResponseTime())

	quota.Consume(relay.getContext(), usage, relay.IsStream())

	return
}

func shouldCooldowns(c *gin.Context, channel *model.Channel, apiErr *types.OpenAIErrorWithStatusCode) bool {
	modelName := c.GetString("new_model")
	channelId := channel.Id
	statusCode := apiErr.StatusCode

	// 决定冻结时长（秒）。优先级：
	//   1. 上游返回的精确恢复时间（RateLimitResetAt，如 Gemini retryDelay / anthropic-ratelimit-unified-reset）
	//   2. 管理员配置的按状态码冻结时长（RetryCooldownPerStatus）
	//   3. 全局兜底 RetryCooldownSeconds，仅对 429 启用以保持向后兼容
	//
	// 任何一步算出 duration <= 0 都视为"不冻结"，直接跳过 channel 而不冻它。
	//
	// 注意：RateLimitResetAt 是对任何状态码生效的——provider 只应在拿到上游精确的
	// Retry-After 信号时设置此字段，详见 types/common.go 上的字段注释。
	var duration int64
	var reason string

	if apiErr.RateLimitResetAt > 0 {
		nowTime := time.Now().Unix()
		duration = apiErr.RateLimitResetAt - nowTime
		if duration > 0 {
			reason = "upstream_retry_after"
		} else {
			// 上游告诉的时间已过，落到下一级配置
			duration = 0
		}
	}

	if duration <= 0 {
		if secs, ok := config.GetRetryCooldownForStatus(statusCode); ok {
			duration = int64(secs)
			reason = fmt.Sprintf("per_status_%d", statusCode)
		} else if statusCode == http.StatusTooManyRequests {
			duration = int64(config.RetryCooldownSeconds)
			reason = "rate_limit"
		}
	}

	if duration > 0 {
		model.ChannelGroup.SetCooldownsWithDuration(channelId, modelName, duration)
		extra := ""
		if apiErr.RateLimitResetAt > 0 && reason == "upstream_retry_after" {
			extra = fmt.Sprintf(" reset_at=%s", time.Unix(apiErr.RateLimitResetAt, 0).Format(time.RFC3339))
		}
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("channel_cooldown channel_id=%d model=\"%s\" status_code=%d duration=%ds reason=\"%s\"%s",
			channelId, modelName, statusCode, duration, reason, extra))
	} else if reason != "" {
		// 配置命中（如 per-status=0）但显式不冻结。打日志方便线上排查"为什么没冷却"。
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("channel_cooldown_skipped channel_id=%d model=\"%s\" status_code=%d reason=\"%s\"",
			channelId, modelName, statusCode, reason))
	}

	skipChannelIds, ok := utils.GetGinValue[[]int](c, "skip_channel_ids")
	if !ok {
		skipChannelIds = make([]int, 0)
	}

	skipChannelIds = append(skipChannelIds, channelId)
	c.Set("skip_channel_ids", skipChannelIds)

	return duration > 0
}

// applies pre-mapping before setRequest to ensure modifications take effect
func applyPreMappingBeforeRequest(c *gin.Context) {
	// check if this is a chat completion request that needs pre-mapping
	path := c.Request.URL.Path
	if !(strings.HasPrefix(path, "/v1/chat/completions") || strings.HasPrefix(path, "/v1/completions")) {
		return
	}

	// 使用 ReadBodyRaw 读取并缓存请求体，避免与下游 setRequest 重复读 body
	bodyBytes, err := common.ReadBodyRaw(c)
	if err != nil {
		return
	}

	// gjson 提取 model 字段，替代 json.Unmarshal 整个 body
	modelName := gjson.GetBytes(bodyBytes, "model").String()
	if modelName == "" {
		return
	}

	// 保存原始的 context 值，避免被 GetProvider 修改
	originalTokenGroup := c.GetString("token_group")
	originalBackupGroup := c.GetString("token_backup_group")
	originalGroup := c.GetString("group")
	originalGroupRatio := c.GetFloat64("group_ratio")

	// 确保恢复原始值，防止 GetProvider 内部修改导致状态污染
	defer func() {
		c.Set("token_group", originalTokenGroup)
		c.Set("token_backup_group", originalBackupGroup)
		c.Set("group", originalGroup)
		c.Set("group_ratio", originalGroupRatio)
		// 清除 GetProvider 设置的其他字段
		c.Set("original_token_group", nil)
		c.Set("is_backupGroup", nil)
		c.Set("channel_id", nil)
		c.Set("channel_type", nil)
		c.Set("original_model", nil)
		c.Set("new_model", nil)
		c.Set("billing_original_model", nil)
	}()

	provider, _, err := GetProvider(c, modelName)
	if err != nil {
		return
	}

	customParams, err := provider.CustomParameterHandler()
	if err != nil || customParams == nil {
		return
	}

	preAdd, exists := customParams["pre_add"]
	if !exists || preAdd != true {
		return
	}

	var requestMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &requestMap); err != nil {
		return
	}

	// Apply custom parameter merging
	modifiedRequestMap := mergeCustomParamsForPreMapping(requestMap, customParams)

	// Convert back to JSON - if successful, update cache
	if modifiedBodyBytes, err := json.Marshal(modifiedRequestMap); err == nil {
		c.Set(config.GinRequestBodyKey, modifiedBodyBytes)
	}
}
