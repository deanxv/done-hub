package relay

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/utils"
	"done-hub/model"
	providersBase "done-hub/providers/base"
	"done-hub/types"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func RelayRerank(c *gin.Context) {
	// 在请求完成后清理缓存的请求体，防止内存泄漏
	defer func() {
		c.Set(config.GinRequestBodyKey, nil)
		c.Set(config.GinProcessedBodyKey, nil)
		c.Set(config.GinProcessedBodyIsVertexAI, nil)
	}()

	relay := NewRelayRerank(c)

	if err := relay.setRequest(); err != nil {
		common.AbortWithErr(c, http.StatusBadRequest, &types.RerankError{Detail: err.Error()})
		return
	}

	if err := relay.setProvider(relay.getOriginalModel()); err != nil {
		common.AbortWithErr(c, http.StatusServiceUnavailable, &types.RerankError{Detail: err.Error()})
		return
	}

	apiErr, done := RelayHandler(relay)
	if apiErr == nil {
		return
	}

	channel := relay.getProvider().GetChannel()
	notifyChannelRelayError(c.Request.Context(), c, channel, apiErr)

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
		// 冻结通道
		shouldCooldowns(c, channel, apiErr)
		if err := relay.setProvider(relay.getOriginalModel()); err != nil {
			continue
		}

		channel = relay.getProvider().GetChannel()

		// 计算渠道信息用于日志显示
		groupName := c.GetString("token_group")
		if groupName == "" {
			groupName = c.GetString("group")
		}
		modelName := c.GetString("new_model")

		// 更新尝试计数
		attemptCount := c.GetInt("attempt_count")
		c.Set("attempt_count", attemptCount+1)

		// 计算剩余可重试的渠道数（不包括当前渠道，因为当前渠道正在使用）
		filters := buildChannelFilters(c, modelName)
		skipChannelIds, _ := utils.GetGinValue[[]int](c, "skip_channel_ids")
		tempFilters := append(filters, model.FilterChannelId(skipChannelIds))
		remainChannels := model.ChannelGroup.CountAvailableChannels(groupName, modelName, tempFilters...)

		// 获取实际重试次数
		actualRetryTimes := c.GetInt("actual_retry_times")

		// 记录重试尝试 - 使用统一的结构化日志格式
		cooldownApplied := true // rerank 中已经调用了 shouldCooldowns
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("retry_attempt model=%s channel_id=%d attempt=%d/%d remaining_channels=%d total_channels=%d cooldown_applied=%t",
			modelName, channel.Id, attemptCount, actualRetryTimes, remainChannels, c.GetInt("total_channels_at_start"), cooldownApplied))

		apiErr, done = RelayHandler(relay)
		if apiErr == nil {
			// 重试成功
			logger.LogInfo(c.Request.Context(), fmt.Sprintf("retry_success model=%s channel_id=%d attempt=%d/%d total_channels=%d",
				modelName, channel.Id, attemptCount, actualRetryTimes, c.GetInt("total_channels_at_start")))
			return
		}

		// 记录重试失败
		logger.LogError(c.Request.Context(), fmt.Sprintf("retry_failed model=%s channel_id=%d attempt=%d/%d status_code=%d error_type=\"%s\" error=\"%s\"",
			modelName, channel.Id, attemptCount, actualRetryTimes, apiErr.StatusCode, apiErr.OpenAIError.Type, utils.TruncateBase64InMessage(apiErr.OpenAIError.Message)))

		notifyChannelRelayError(c.Request.Context(), c, channel, apiErr)
		if done || !shouldRetry(c, apiErr, channel.Type) {
			logger.LogError(c.Request.Context(), fmt.Sprintf("retry_stop_condition model=%s channel_id=%d attempt=%d/%d done=%t should_retry=%t",
				modelName, channel.Id, attemptCount, actualRetryTimes, done, shouldRetry(c, apiErr, channel.Type)))
			break
		}
	}

	// 记录最终失败
	if apiErr != nil {
		finalAttempt := c.GetInt("attempt_count")
		actualRetryTimes := c.GetInt("actual_retry_times")
		logger.LogError(c.Request.Context(), fmt.Sprintf("retry_exhausted model=%s channel_id=%d total_attempts=%d total_channels=%d config_max_retries=%d actual_max_retries=%d status_code=%d error=\"%s\"",
			modelName, channel.Id, finalAttempt, c.GetInt("total_channels_at_start"), retryTimes, actualRetryTimes, apiErr.StatusCode, utils.TruncateBase64InMessage(apiErr.OpenAIError.Message)))

		// rerank 走自有响应格式（detail 字段），不经过 FilterOpenAIErr 的坍缩路径，
		// 因此 ChannelFailErrorWrapEnabled 总开关在这里要单独判一次，
		// 否则运维关掉开关想看上游真实 429 错误时，rerank 仍会替换文案，与 main 路径行为分叉。
		if apiErr.StatusCode == http.StatusTooManyRequests && config.ChannelFailErrorWrapEnabled {
			apiErr.OpenAIError.Message = config.GetChannelFailErrorMessage()
		}
		relayRerankResponseWithErr(c, apiErr)
	}
}

type relayRerank struct {
	relayBase
	request types.RerankRequest
}

func NewRelayRerank(c *gin.Context) *relayRerank {
	relay := &relayRerank{}
	relay.c = c
	return relay
}

func (r *relayRerank) setRequest() error {
	if err := common.UnmarshalBodyReusable(r.c, &r.request); err != nil {
		return err
	}

	r.setOriginalModel(r.request.Model)

	return nil
}

func (r *relayRerank) getPromptTokens() (int, error) {
	channel := r.provider.GetChannel()
	return common.CountTokenRerankMessages(r.request, r.modelName, channel.PreCost), nil
}

func (r *relayRerank) send() (err *types.OpenAIErrorWithStatusCode, done bool) {
	chatProvider, ok := r.provider.(providersBase.RerankInterface)
	if !ok {
		err = common.StringErrorWrapperLocal("channel not implemented", "channel_error", http.StatusServiceUnavailable)
		done = true
		return
	}

	r.request.Model = r.modelName

	var response *types.RerankResponse
	response, err = chatProvider.CreateRerank(&r.request)
	if err != nil {
		return
	}
	err = responseJsonClient(r.c, response)

	if err != nil {
		done = true
	}

	return
}
