package relay

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/utils"
	"done-hub/metrics"
	modelPkg "done-hub/model"
	"done-hub/providers/recraftAI"
	"done-hub/relay/relay_util"
	"done-hub/types"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// buildChannelFiltersForRecraftAI 为 RecraftAI 构建渠道过滤器列表
func buildChannelFiltersForRecraftAI(c *gin.Context, modelName string) []modelPkg.ChannelsFilterFunc {
	var filters []modelPkg.ChannelsFilterFunc

	if skipOnlyChat := c.GetBool("skip_only_chat"); skipOnlyChat {
		filters = append(filters, modelPkg.FilterOnlyChat())
	}

	if skipChannelIds, ok := utils.GetGinValue[[]int](c, "skip_channel_ids"); ok {
		filters = append(filters, modelPkg.FilterChannelId(skipChannelIds))
	}

	if types, exists := c.Get("allow_channel_type"); exists {
		if allowTypes, ok := types.([]int); ok {
			filters = append(filters, modelPkg.FilterChannelTypes(allowTypes))
		}
	}

	return filters
}

func RelayRecraftAI(c *gin.Context) {
	model := Path2RecraftAIModel(c.Request.URL.Path)

	usage := &types.Usage{
		PromptTokens: 1,
	}

	recraftProvider, err := getRecraftProvider(c, model)
	if err != nil {
		if IsModelNotFound(err) {
			// 必须包成 {"error":...} 外层，否则 OpenAI SDK 读不到 code/type，model_not_found 语义失效。
			errWithCode := FilterOpenAIErr(c, common.ModelNotFoundError(model))
			relayResponseWithOpenAIErr(c, &errWithCode)
		} else {
			common.AbortWithMessage(c, http.StatusServiceUnavailable, err.Error())
		}
		return
	}

	recraftProvider.SetUsage(usage)

	quota := relay_util.NewQuota(c, model, 1)
	if err := quota.PreQuotaConsumption(); err != nil {
		common.AbortWithMessage(c, http.StatusServiceUnavailable, err.Error())
		return
	}

	requestURL := strings.Replace(c.Request.URL.Path, "/recraftAI", "", 1)
	response, apiErr := recraftProvider.CreateRelay(requestURL)
	if apiErr == nil {
		quota.Consume(c, usage, false)

		metrics.RecordProvider(c, 200)
		errWithCode := responseMultipart(c, response)
		logger.LogError(c.Request.Context(), fmt.Sprintf("relay error happen %v, won't retry in this case", errWithCode))
		return
	}

	channel := recraftProvider.GetChannel()
	notifyChannelRelayError(c.Request.Context(), c, channel, apiErr)

	retryTimes := config.RetryTimes
	// 在重试开始前计算并缓存总渠道数，避免重试过程中动态变化
	groupName := c.GetString("token_group")
	if groupName == "" {
		groupName = c.GetString("group")
	}
	modelName := c.GetString("new_model")
	totalChannelsAtStart := modelPkg.ChannelGroup.CountAvailableChannels(groupName, modelName)

	if !shouldRetry(c, apiErr, channel.Type) {
		logger.LogError(c.Request.Context(), fmt.Sprintf("retry_skip model=%s channel_id=%d status_code=%d should_retry=false total_channels=%d error=\"%s\"",
			modelName, channel.Id, apiErr.StatusCode, totalChannelsAtStart, utils.TruncateBase64InMessage(apiErr.OpenAIError.Message)))
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
		modelName, channel.Id, totalChannelsAtStart, retryTimes, actualRetryTimes, apiErr.StatusCode, utils.TruncateBase64InMessage(apiErr.OpenAIError.Message)))

	for i := actualRetryTimes; i > 0; i-- {
		cooldownApplied := shouldCooldowns(c, channel, apiErr)
		if recraftProvider, err = getRecraftProvider(c, model); err != nil {
			continue
		}

		channel = recraftProvider.GetChannel()

		// 更新尝试计数
		attemptCount := c.GetInt("attempt_count")
		c.Set("attempt_count", attemptCount+1)

		// 计算剩余可重试的渠道数（不包括当前渠道，因为当前渠道正在使用）
		filters := buildChannelFiltersForRecraftAI(c, modelName)
		skipChannelIds, _ := utils.GetGinValue[[]int](c, "skip_channel_ids")
		tempFilters := append(filters, modelPkg.FilterChannelId(skipChannelIds))
		remainChannels := modelPkg.ChannelGroup.CountAvailableChannels(groupName, modelName, tempFilters...)

		// 获取实际重试次数
		actualRetryTimes := c.GetInt("actual_retry_times")

		// 记录重试尝试 - 使用统一的结构化日志格式
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("retry_attempt model=%s channel_id=%d attempt=%d/%d remaining_channels=%d total_channels=%d cooldown_applied=%t",
			modelName, channel.Id, attemptCount, actualRetryTimes, remainChannels, c.GetInt("total_channels_at_start"), cooldownApplied))

		response, apiErr := recraftProvider.CreateRelay(requestURL)
		if apiErr == nil {
			quota.Consume(c, usage, false)

			metrics.RecordProvider(c, 200)
			// 重试成功
			logger.LogInfo(c.Request.Context(), fmt.Sprintf("retry_success model=%s channel_id=%d attempt=%d/%d total_channels=%d",
				modelName, channel.Id, attemptCount, actualRetryTimes, c.GetInt("total_channels_at_start")))
			errWithCode := responseMultipart(c, response)
			if errWithCode != nil {
				logger.LogError(c.Request.Context(), fmt.Sprintf("retry_response_error model=%s channel_id=%d error=\"%v\"",
					modelName, channel.Id, errWithCode))
			}
			return
		}

		// 记录重试失败
		logger.LogError(c.Request.Context(), fmt.Sprintf("retry_failed model=%s channel_id=%d attempt=%d/%d status_code=%d error_type=\"%s\" error=\"%s\"",
			modelName, channel.Id, attemptCount, actualRetryTimes, apiErr.StatusCode, apiErr.OpenAIError.Type, utils.TruncateBase64InMessage(apiErr.OpenAIError.Message)))

		notifyChannelRelayError(c.Request.Context(), c, channel, apiErr)
		if !shouldRetry(c, apiErr, channel.Type) {
			logger.LogError(c.Request.Context(), fmt.Sprintf("retry_stop_condition model=%s channel_id=%d attempt=%d/%d should_retry=false",
				modelName, channel.Id, attemptCount, actualRetryTimes))
			break
		}
	}

	// 记录最终失败
	finalAttempt := c.GetInt("attempt_count")
	actualRetryTimes = c.GetInt("actual_retry_times")
	logger.LogError(c.Request.Context(), fmt.Sprintf("retry_exhausted model=%s channel_id=%d total_attempts=%d total_channels=%d config_max_retries=%d actual_max_retries=%d status_code=%d error=\"%s\"",
		modelName, channel.Id, finalAttempt, c.GetInt("total_channels_at_start"), retryTimes, actualRetryTimes, apiErr.StatusCode, utils.TruncateBase64InMessage(apiErr.OpenAIError.Message)))

	quota.Undo(c)
	newErrWithCode := FilterOpenAIErr(c, apiErr)
	// 必须包成 {"error":...} 外层，与 main.go HandleJsonError 一致。
	// 旧的 AbortWithErr(&OpenAIError) 让 gin 直接 marshal 结构体，OpenAI SDK 拿不到 error.code/type。
	relayResponseWithOpenAIErr(c, &newErrWithCode)
}

func Path2RecraftAIModel(path string) string {
	parts := strings.Split(path, "/")
	lastPart := parts[len(parts)-1]

	return "recraft_" + lastPart
}

func getRecraftProvider(c *gin.Context, model string) (*recraftAI.RecraftProvider, error) {
	provider, _, fail := GetProvider(c, model)
	if fail != nil {
		// common.AbortWithMessage(c, http.StatusServiceUnavailable, fail.Error())
		return nil, fail
	}

	recraftProvider, ok := provider.(*recraftAI.RecraftProvider)
	if !ok {
		return nil, errors.New("provider not found")
	}

	return recraftProvider, nil
}
