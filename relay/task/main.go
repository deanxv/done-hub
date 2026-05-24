package task

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/utils"
	"done-hub/metrics"
	"done-hub/model"
	"done-hub/relay/relay_util"
	"done-hub/relay/task/base"
	"done-hub/types"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// buildTaskChannelFilters 为任务构建渠道过滤器列表
func buildTaskChannelFilters(c *gin.Context) []model.ChannelsFilterFunc {
	var filters []model.ChannelsFilterFunc

	if skipChannelIds, ok := utils.GetGinValue[[]int](c, "skip_channel_ids"); ok {
		filters = append(filters, model.FilterChannelId(skipChannelIds))
	}

	if types, exists := c.Get("allow_channel_type"); exists {
		if allowTypes, ok := types.([]int); ok {
			filters = append(filters, model.FilterChannelTypes(allowTypes))
		}
	}

	return filters
}

func RelayTaskSubmit(c *gin.Context) {
	var taskErr *base.TaskError
	taskAdaptor, err := GetTaskAdaptor(GetRelayMode(c), c)
	if err != nil {
		taskErr = base.StringTaskError(http.StatusBadRequest, "adaptor_not_found", "adaptor not found", true)
		c.JSON(http.StatusBadRequest, taskErr)
		return
	}

	taskErr = taskAdaptor.Init()
	if taskErr != nil {
		taskAdaptor.HandleError(taskErr)
		return
	}

	taskErr = taskAdaptor.SetProvider()
	if taskErr != nil {
		taskAdaptor.HandleError(taskErr)
		return
	}

	quotaInstance := relay_util.NewQuota(c, taskAdaptor.GetModelName(), 1000)
	if errWithOA := quotaInstance.PreQuotaConsumption(); errWithOA != nil {
		taskAdaptor.HandleError(base.OpenAIErrToTaskErr(errWithOA))
		return
	}

	taskErr = taskAdaptor.Relay()
	if taskErr == nil {
		CompletedTask(quotaInstance, taskAdaptor, relay_util.NewConsumeSnapshot(c))
		// 返回结果
		taskAdaptor.GinResponse()
		metrics.RecordProvider(c, 200)
		return
	}

	quotaInstance.Undo(c)

	retryTimes := config.RetryTimes

	// 在重试开始前计算并缓存总渠道数，避免重试过程中动态变化
	groupName := c.GetString("token_group")
	if groupName == "" {
		groupName = c.GetString("group")
	}
	modelName := taskAdaptor.GetModelName()
	totalChannelsAtStart := model.ChannelGroup.CountAvailableChannels(groupName, modelName)

	channel := taskAdaptor.GetProvider().GetChannel()

	if !taskAdaptor.ShouldRetry(c, taskErr) {
		logger.LogError(c.Request.Context(), fmt.Sprintf("retry_skip model=%s channel_id=%d status_code=%d should_retry=false total_channels=%d error=\"%s\"",
			modelName, channel.Id, taskErr.StatusCode, totalChannelsAtStart, taskErr.Message))
		retryTimes = 0
	}

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
		modelName, channel.Id, totalChannelsAtStart, retryTimes, actualRetryTimes, taskErr.StatusCode, taskErr.Message))
	for i := actualRetryTimes; i > 0; i-- {
		model.ChannelGroup.SetCooldowns(channel.Id, taskAdaptor.GetModelName())
		taskErr = taskAdaptor.SetProvider()
		if taskErr != nil {
			continue
		}

		channel = taskAdaptor.GetProvider().GetChannel()

		// 计算渠道信息用于日志显示
		groupName := c.GetString("token_group")
		if groupName == "" {
			groupName = c.GetString("group")
		}
		modelName := taskAdaptor.GetModelName()

		// 更新尝试计数
		attemptCount := c.GetInt("attempt_count")
		c.Set("attempt_count", attemptCount+1)

		// 计算剩余可重试的渠道数（不包括当前渠道，因为当前渠道正在使用）
		filters := buildTaskChannelFilters(c)
		skipChannelIds, _ := utils.GetGinValue[[]int](c, "skip_channel_ids")
		tempFilters := append(filters, model.FilterChannelId(skipChannelIds))
		remainChannels := model.ChannelGroup.CountAvailableChannels(groupName, modelName, tempFilters...)

		// 获取实际重试次数
		actualRetryTimes := c.GetInt("actual_retry_times")

		// 记录重试尝试 - 使用统一的结构化日志格式
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("retry_attempt model=%s channel_id=%d attempt=%d/%d remaining_channels=%d total_channels=%d",
			modelName, channel.Id, attemptCount, actualRetryTimes, remainChannels, c.GetInt("total_channels_at_start")))

		taskErr = taskAdaptor.Relay()
		if taskErr == nil {
			// 重试成功
			logger.LogInfo(c.Request.Context(), fmt.Sprintf("retry_success model=%s channel_id=%d attempt=%d/%d total_channels=%d",
				modelName, channel.Id, attemptCount, actualRetryTimes, c.GetInt("total_channels_at_start")))
			// 在 spawn TrackedGoroutine 之前先 snapshot，闭包持有值，避免 c-pool 复用竞态
			snap := relay_util.NewConsumeSnapshot(c)
			common.TrackedGoroutine(func() {
				CompletedTask(quotaInstance, taskAdaptor, snap)
			})
			return
		}

		// 记录重试失败
		logger.LogError(c.Request.Context(), fmt.Sprintf("retry_failed model=%s channel_id=%d attempt=%d/%d status_code=%d error=\"%s\"",
			modelName, channel.Id, attemptCount, actualRetryTimes, taskErr.StatusCode, taskErr.Message))

		quotaInstance.Undo(c)
		if !taskAdaptor.ShouldRetry(c, taskErr) {
			logger.LogError(c.Request.Context(), fmt.Sprintf("retry_stop_condition model=%s channel_id=%d attempt=%d/%d should_retry=false",
				modelName, channel.Id, attemptCount, actualRetryTimes))
			break
		}

	}

	// 记录最终失败
	if taskErr != nil {
		finalAttempt := c.GetInt("attempt_count")
		actualRetryTimes := c.GetInt("actual_retry_times")
		logger.LogError(c.Request.Context(), fmt.Sprintf("retry_exhausted model=%s channel_id=%d total_attempts=%d total_channels=%d config_max_retries=%d actual_max_retries=%d status_code=%d error=\"%s\"",
			modelName, channel.Id, finalAttempt, c.GetInt("total_channels_at_start"), retryTimes, actualRetryTimes, taskErr.StatusCode, taskErr.Message))
		taskAdaptor.HandleError(taskErr)
	}

}

func CompletedTask(quotaInstance *relay_util.Quota, taskAdaptor base.TaskInterface, snap relay_util.ConsumeSnapshot) {
	quotaInstance.ConsumeWithSnapshot(snap, &types.Usage{CompletionTokens: 0, PromptTokens: 1, TotalTokens: 1}, false)

	task := taskAdaptor.GetTask()
	task.Quota = int(quotaInstance.GetInputRatio() * 1000)

	err := task.Insert()
	if err != nil {
		logger.SysError(fmt.Sprintf("task error: %s", err.Error()))
	}

	// 激活任务
	ActivateUpdateTaskBulk()
}

func GetRelayMode(c *gin.Context) int {
	relayMode := config.RelayModeUnknown
	path := c.Request.URL.Path
	if strings.HasPrefix(path, "/suno") {
		relayMode = config.RelayModeSuno
	} else if strings.HasPrefix(path, "/kling") {
		relayMode = config.RelayModeKling
	}

	return relayMode
}
