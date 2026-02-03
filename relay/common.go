package relay

import (
	"bytes"
	"context"
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/requester"
	"done-hub/common/utils"
	"done-hub/controller"
	"done-hub/metrics"
	"done-hub/model"
	"done-hub/providers"
	providersBase "done-hub/providers/base"
	"done-hub/types"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func Path2Relay(c *gin.Context, path string) RelayBaseInterface {
	var relay RelayBaseInterface
	if strings.HasPrefix(path, "/v1/chat/completions") {
		relay = NewRelayChat(c)
	} else if strings.HasPrefix(path, "/v1/completions") {
		relay = NewRelayCompletions(c)
	} else if strings.HasPrefix(path, "/v1/embeddings") {
		relay = NewRelayEmbeddings(c)
	} else if strings.HasPrefix(path, "/v1/moderations") {
		relay = NewRelayModerations(c)
	} else if strings.HasPrefix(path, "/v1/images/generations") || strings.HasPrefix(path, "/recraftAI/v1/images/generations") {
		relay = newRelayImageGenerations(c)
	} else if strings.HasPrefix(path, "/v1/images/edits") {
		relay = NewRelayImageEdits(c)
	} else if strings.HasPrefix(path, "/v1/images/variations") {
		relay = NewRelayImageVariations(c)
	} else if strings.HasPrefix(path, "/v1/audio/speech") {
		relay = NewRelaySpeech(c)
	} else if strings.HasPrefix(path, "/v1/audio/transcriptions") {
		relay = NewRelayTranscriptions(c)
	} else if strings.HasPrefix(path, "/v1/audio/translations") {
		relay = NewRelayTranslations(c)
	} else if strings.HasPrefix(path, "/claude") {
		relay = NewRelayClaudeOnly(c)
	} else if strings.HasPrefix(path, "/gemini") {
		if strings.Contains(path, "veo") && strings.Contains(path, ":predictLongRunning") {
			relay = NewRelayVeoOnly(c)
		} else if strings.Contains(path, ":predict") {
			relay = newRelayImageGenerations(c)
		} else {
			relay = NewRelayGeminiOnly(c)
		}
	} else if strings.HasPrefix(path, "/v1/responses") {
		relay = NewRelayResponses(c)
	}

	return relay
}

func CheckLimitModel(c *gin.Context, modelName string) error {
	// 判断modelName是否在token的setting.limits.models[]范围内

	// 从context中获取token设置
	tokenSetting, exists := c.Get("token_setting")
	if !exists {
		// 如果没有token设置，则不进行限制
		return nil
	}

	// 类型断言为TokenSetting指针
	setting, ok := tokenSetting.(*model.TokenSetting)
	if !ok || setting == nil {
		// 类型断言失败或为空，不进行限制
		return nil
	}

	// 检查是否启用了模型限制
	if !setting.Limits.LimitModelSetting.Enabled {
		// 未启用模型限制，允许所有模型
		return nil
	}

	// 检查模型列表是否为空
	if len(setting.Limits.LimitModelSetting.Models) == 0 {
		// Empty model list means no models are allowed
		return errors.New("No available models configured for current token")
	}

	// Check if modelName is in the allowed models list
	for _, allowedModel := range setting.Limits.LimitModelSetting.Models {
		if allowedModel == modelName {
			// Found matching model, allow usage
			return nil
		}
	}

	// modelName is not in the allowed models list
	return fmt.Errorf("Model %s is not supported for current token", modelName)
}

// buildGroupChain 构建分组降级链
func buildGroupChain(tokenGroup, backupGroup, userGroup string) []string {
	var chain []string

	// 如果Token配置了主分组或备用分组，只使用Token配置的分组
	if tokenGroup != "" || backupGroup != "" {
		// 添加主分组
		if tokenGroup != "" {
			chain = append(chain, tokenGroup)
		}

		// 添加备用分组（如果与主分组不同）
		if backupGroup != "" && backupGroup != tokenGroup {
			chain = append(chain, backupGroup)
		}

		return chain
	}

	// 只有Token完全没配置分组时，才使用用户分组作为兜底
	if userGroup != "" {
		chain = append(chain, userGroup)
	}

	return chain
}

func GetProvider(c *gin.Context, modelName string) (provider providersBase.ProviderInterface, newModelName string, fail error) {
	// 检查令牌模型限制
	err := CheckLimitModel(c, modelName)
	if err != nil {
		return nil, "", err
	}

	// 获取分组信息
	tokenGroup := c.GetString("token_group")
	backupGroup := c.GetString("token_backup_group")
	userGroup := c.GetString("group")

	// 构建分组降级链：主分组 -> 备用分组 -> 用户分组
	groupChain := buildGroupChain(tokenGroup, backupGroup, userGroup)

	if len(groupChain) == 0 {
		common.AbortWithMessage(c, http.StatusServiceUnavailable, "分组不存在")
		return
	}

	// 保存原始的第一优先级分组（用于日志记录）
	originalGroup := groupChain[0]

	// 尝试每个分组，直到成功获取渠道
	var lastErr error
	var actualModelName string
	var channel *model.Channel
	var usedGroup string
	var isBackupGroup bool

	for i, groupName := range groupChain {
		matchedModelName, err := model.ChannelGroup.GetMatchedModelName(groupName, modelName)
		if err != nil {
			lastErr = err
			continue // 尝试下一个分组
		}

		actualModelName = matchedModelName

		// 临时设置当前分组用于获取渠道
		c.Set("token_group", groupName)
		channel, err = fetchChannel(c, actualModelName)
		if err != nil {
			lastErr = err
			continue // 尝试下一个分组
		}

		// 成功获取渠道
		usedGroup = groupName
		isBackupGroup = (i > 0) // 如果不是第一个分组，说明使用了降级分组

		break
	}

	// 所有分组都失败
	if channel == nil {
		fail = lastErr
		if fail == nil {
			fail = errors.New("所有分组都无可用渠道")
		}
		return
	}

	// 设置最终使用的分组和相关信息
	c.Set("token_group", usedGroup)
	c.Set("original_token_group", originalGroup) // 保存原始第一优先级分组，用于日志记录
	c.Set("is_backupGroup", isBackupGroup)
	c.Set("channel_id", channel.Id)
	c.Set("channel_type", channel.Type)

	// 重新设置分组倍率
	groupRatio := model.GlobalUserGroupRatio.GetBySymbol(usedGroup)
	if groupRatio != nil {
		c.Set("group_ratio", groupRatio.Ratio)
	}

	provider = providers.GetProvider(channel, c)
	if provider == nil {
		fail = errors.New("channel not found")
		return
	}
	provider.SetOriginalModel(modelName) // 保存用户原始请求的模型名称
	c.Set("original_model", modelName)

	newModelName, fail = provider.ModelMappingHandler(actualModelName) // 使用匹配到的模型名称进行映射
	if fail != nil {
		return
	}

	BillingOriginalModel := false

	if strings.HasPrefix(newModelName, "+") {
		newModelName = newModelName[1:]
		BillingOriginalModel = true
	}

	c.Set("new_model", newModelName)
	c.Set("billing_original_model", BillingOriginalModel)

	return
}

func fetchChannel(c *gin.Context, modelName string) (channel *model.Channel, fail error) {
	channelId := c.GetInt("specific_channel_id")
	ignore := c.GetBool("specific_channel_id_ignore")
	if channelId > 0 && !ignore {
		return fetchChannelById(channelId)
	}

	return fetchChannelByModel(c, modelName)
}

func fetchChannelById(channelId int) (*model.Channel, error) {
	channel, err := model.GetChannelById(channelId)
	if err != nil {
		return nil, errors.New(model.ErrInvalidChannelId)
	}
	if channel.Status != config.ChannelStatusEnabled {
		return nil, errors.New(model.ErrChannelDisabled)
	}

	return channel, nil
}

// buildChannelFilters 构建渠道过滤器列表
func buildChannelFilters(c *gin.Context, modelName string) []model.ChannelsFilterFunc {
	var filters []model.ChannelsFilterFunc

	if skipOnlyChat := c.GetBool("skip_only_chat"); skipOnlyChat {
		filters = append(filters, model.FilterOnlyChat())
	}

	if skipChannelIds, ok := utils.GetGinValue[[]int](c, "skip_channel_ids"); ok {
		filters = append(filters, model.FilterChannelId(skipChannelIds))
	}

	if types, exists := c.Get("allow_channel_type"); exists {
		if allowTypes, ok := types.([]int); ok {
			filters = append(filters, model.FilterChannelTypes(allowTypes))
		}
	}

	if isStream := c.GetBool("is_stream"); isStream {
		filters = append(filters, model.FilterDisabledStream(modelName))
	}

	return filters
}

func fetchChannelByModel(c *gin.Context, modelName string) (*model.Channel, error) {
	group := c.GetString("token_group")
	filters := buildChannelFilters(c, modelName)

	// 传递 gin.Context 给 balancer，用于生成 session hash
	channel, err := model.ChannelGroup.NextByValidatedModel(group, modelName, c, filters...)
	if err != nil {
		// 这里只处理渠道相关的错误，模型匹配错误已在上层处理
		message := fmt.Sprintf(model.ErrNoAvailableChannelForModel, model.GlobalUserGroupRatio.GetDisplayName(group), modelName)
		if channel != nil {
			logger.SysError(fmt.Sprintf("渠道不存在：%d", channel.Id))
			message = model.ErrDatabaseConsistencyBroken
		}
		return nil, errors.New(message)
	}

	return channel, nil
}

func responseJsonClient(c *gin.Context, data interface{}) *types.OpenAIErrorWithStatusCode {
	// 将data转换为 JSON，禁用 HTML 转义以避免 & 被转为 \u0026
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(data)
	if err != nil {
		logger.LogError(c.Request.Context(), "marshal_response_body_failed:"+err.Error())
		return nil
	}

	// Encode 会在末尾添加换行符，需要去掉
	responseBody := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))

	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(http.StatusOK)
	_, err = c.Writer.Write(responseBody)
	if err != nil {
		logger.LogError(c.Request.Context(), "write_response_body_failed:"+err.Error())
	}

	return nil
}

type StreamEndHandler func() string

func responseStreamClient(c *gin.Context, stream requester.StreamReaderInterface[string], endHandler StreamEndHandler) (firstResponseTime time.Time, errWithOP *types.OpenAIErrorWithStatusCode) {
	requester.SetEventStreamHeaders(c)
	dataChan, errChan := stream.Recv()

	done := make(chan struct{})
	var finalErr *types.OpenAIErrorWithStatusCode

	defer stream.Close()

	var isFirstResponse bool
	ctx := c.Request.Context()
	clientDisconnected := false

	go func() {
		defer close(done)

		for {
			select {
			case data, ok := <-dataChan:
				if !ok {
					return
				}

				if !isFirstResponse {
					firstResponseTime = time.Now()
					isFirstResponse = true
				}

				// 客户端断开后继续消费数据以确保计费准确，但不写入
				if !clientDisconnected {
					select {
					case <-ctx.Done():
						clientDisconnected = true
					default:
						c.Writer.Write([]byte("data: " + data + "\n\n"))
						c.Writer.Flush()
					}
				}

			case err := <-errChan:
				if !errors.Is(err, io.EOF) {
					if !clientDisconnected {
						select {
						case <-ctx.Done():
							clientDisconnected = true
						default:
							c.Writer.Write([]byte("data: " + err.Error() + "\n\n"))
							c.Writer.Flush()
						}
					}
					finalErr = common.StringErrorWrapper(err.Error(), "stream_error", 900)
					logger.LogError(c.Request.Context(), "Stream err:"+err.Error())
				} else {
					if finalErr == nil && endHandler != nil {
						if streamData := endHandler(); streamData != "" && !clientDisconnected {
							select {
							case <-ctx.Done():
							default:
								c.Writer.Write([]byte("data: " + streamData + "\n\n"))
								c.Writer.Flush()
							}
						}
					}
					if !clientDisconnected {
						select {
						case <-ctx.Done():
						default:
							c.Writer.Write([]byte("data: [DONE]\n\n"))
							c.Writer.Flush()
						}
					}
				}
				return

			case <-ctx.Done():
				clientDisconnected = true
			}
		}
	}()

	<-done
	return firstResponseTime, finalErr
}

func responseGeneralStreamClient(c *gin.Context, stream requester.StreamReaderInterface[string], endHandler StreamEndHandler) (firstResponseTime time.Time) {
	requester.SetEventStreamHeaders(c)
	dataChan, errChan := stream.Recv()

	done := make(chan struct{})
	defer stream.Close()

	var isFirstResponse bool
	ctx := c.Request.Context()
	clientDisconnected := false

	go func() {
		defer close(done)

		for {
			select {
			case data, ok := <-dataChan:
				if !ok {
					return
				}
				if !isFirstResponse {
					firstResponseTime = time.Now()
					isFirstResponse = true
				}
				if !clientDisconnected {
					select {
					case <-ctx.Done():
						clientDisconnected = true
					default:
						fmt.Fprint(c.Writer, data)
						c.Writer.Flush()
					}
				}

			case err := <-errChan:
				if !errors.Is(err, io.EOF) {
					if !clientDisconnected {
						select {
						case <-ctx.Done():
							clientDisconnected = true
						default:
							fmt.Fprint(c.Writer, err.Error())
							c.Writer.Flush()
						}
					}
					logger.LogError(c.Request.Context(), "Stream err:"+err.Error())
				} else {
					if endHandler != nil {
						if streamData := endHandler(); streamData != "" && !clientDisconnected {
							select {
							case <-ctx.Done():
							default:
								fmt.Fprint(c.Writer, streamData)
								c.Writer.Flush()
							}
						}
					}
				}
				return

			case <-ctx.Done():
				clientDisconnected = true
			}
		}
	}()

	<-done
	return firstResponseTime
}

func responseMultipart(c *gin.Context, resp *http.Response) *types.OpenAIErrorWithStatusCode {
	defer resp.Body.Close()

	for k, v := range resp.Header {
		c.Writer.Header().Set(k, v[0])
	}

	c.Writer.WriteHeader(resp.StatusCode)

	_, err := io.Copy(c.Writer, resp.Body)
	if err != nil {
		return common.ErrorWrapper(err, "write_response_body_failed", http.StatusInternalServerError)
	}

	return nil
}

func responseCustom(c *gin.Context, response *types.AudioResponseWrapper) *types.OpenAIErrorWithStatusCode {
	for k, v := range response.Headers {
		c.Writer.Header().Set(k, v)
	}
	c.Writer.WriteHeader(http.StatusOK)

	_, err := c.Writer.Write(response.Body)
	if err != nil {
		return common.ErrorWrapper(err, "write_response_body_failed", http.StatusInternalServerError)
	}

	return nil
}

func responseCache(c *gin.Context, response string, isStream bool) {
	if isStream {
		requester.SetEventStreamHeaders(c)
		c.Stream(func(w io.Writer) bool {
			fmt.Fprint(w, response)
			return false
		})
	} else {
		c.Data(http.StatusOK, "application/json", []byte(response))
	}

}

func shouldRetry(c *gin.Context, apiErr *types.OpenAIErrorWithStatusCode, channelType int) bool {
	channelId := c.GetInt("specific_channel_id")
	ignore := c.GetBool("specific_channel_id_ignore")

	if apiErr == nil {
		return false
	}

	metrics.RecordProvider(c, apiErr.StatusCode)

	if apiErr.LocalError ||
		(channelId > 0 && !ignore) {
		return false
	}

	switch apiErr.StatusCode {
	case http.StatusTooManyRequests, http.StatusTemporaryRedirect:
		return true
	case http.StatusRequestTimeout, http.StatusGatewayTimeout, 524:
		return false
	case http.StatusBadRequest:
		return shouldRetryBadRequest(channelType, apiErr)
	}

	if apiErr.StatusCode/100 == 5 {
		return true
	}

	if apiErr.StatusCode/100 == 2 {
		return false
	}
	return true
}

func shouldRetryBadRequest(channelType int, apiErr *types.OpenAIErrorWithStatusCode) bool {
	switch channelType {
	case config.ChannelTypeAnthropic:
		return strings.Contains(apiErr.OpenAIError.Message, "Your credit balance is too low")
	case config.ChannelTypeBedrock:
		return strings.Contains(apiErr.OpenAIError.Message, "Operation not allowed")
	default:
		// gemini
		if apiErr.OpenAIError.Param == "INVALID_ARGUMENT" && strings.Contains(apiErr.OpenAIError.Message, "API key not valid") {
			return true
		}
		return false
	}
}

func processChannelRelayError(ctx context.Context, channelId int, channelName string, err *types.OpenAIErrorWithStatusCode, channelType int) {
	if controller.ShouldDisableChannel(channelType, err) {
		logger.LogError(ctx, fmt.Sprintf("channel_disabled channel_id=%d channel_name=\"%s\" channel_type=%d status_code=%d error=\"%s\" auto_disabled=true",
			channelId, channelName, channelType, err.StatusCode, err.Message))
		controller.DisableChannel(channelId, channelName, err.Message, true)
	}
}

var (
	requestIdRegex = regexp.MustCompile(`\(request id: [^\)]+\)`)
	quotaKeywords  = []string{"余额", "额度", "quota", model.KeywordNoAvailableChannel, "令牌"}
)

func FilterOpenAIErr(c *gin.Context, err *types.OpenAIErrorWithStatusCode) (errWithStatusCode types.OpenAIErrorWithStatusCode) {
	newErr := types.OpenAIErrorWithStatusCode{}
	if err != nil {
		newErr = *err
	}

	if newErr.StatusCode == http.StatusTooManyRequests {
		newErr.OpenAIError.Message = "当前分组上游负载已饱和，请稍后再试"
	}

	// 如果message中已经包含 request id: 则不再添加
	if strings.Contains(newErr.Message, "(request id:") {
		newErr.Message = requestIdRegex.ReplaceAllString(newErr.Message, "")
	}

	requestId := c.GetString(logger.RequestIdKey)
	newErr.OpenAIError.Message = utils.MessageWithRequestId(newErr.OpenAIError.Message, requestId)

	channelType := c.GetInt("channel_type")

	// GeminiCli 错误处理（优先处理，避免被通用逻辑覆盖）
	if channelType == config.ChannelTypeGeminiCli && !newErr.LocalError {
		if newErr.OpenAIError.Type == "geminicli_error" || newErr.OpenAIError.Type == "geminicli_token_error" {
			if newErr.StatusCode == http.StatusUnauthorized || newErr.StatusCode == http.StatusForbidden {
				if cachedErr, exists := c.Get("first_non_auth_error"); exists {
					if firstErr, ok := cachedErr.(*types.OpenAIErrorWithStatusCode); ok {
						newErr = *firstErr
						if newErr.OpenAIError.Type == "geminicli_error" {
							newErr.OpenAIError.Type = "system_error"
						}
						newErr.OpenAIError.Message = utils.MessageWithRequestId(newErr.OpenAIError.Message, requestId)
						return newErr
					}
				}
				if newErr.StatusCode == http.StatusUnauthorized {
					newErr.OpenAIError.Type = "authentication_error"
				} else {
					newErr.OpenAIError.Type = "access_denied"
				}
				newErr.OpenAIError.Message = utils.MessageWithRequestId("上游负载已饱和，请稍后再试", requestId)
				newErr.StatusCode = http.StatusTooManyRequests
				return newErr
			} else {
				newErr.OpenAIError.Type = "system_error"
			}
		}
	}

	// ClaudeCode 错误处理（优先处理，避免被通用逻辑覆盖）
	if channelType == config.ChannelTypeClaudeCode && !newErr.LocalError {
		if newErr.OpenAIError.Type == "claudecode_error" || newErr.OpenAIError.Type == "claudecode_token_error" {
			if newErr.StatusCode == http.StatusUnauthorized || newErr.StatusCode == http.StatusForbidden {
				if cachedErr, exists := c.Get("first_non_auth_error"); exists {
					if firstErr, ok := cachedErr.(*types.OpenAIErrorWithStatusCode); ok {
						newErr = *firstErr
						if newErr.OpenAIError.Type == "claudecode_error" {
							newErr.OpenAIError.Type = "system_error"
						}
						newErr.OpenAIError.Message = utils.MessageWithRequestId(newErr.OpenAIError.Message, requestId)
						return newErr
					}
				}
				if newErr.StatusCode == http.StatusUnauthorized {
					newErr.OpenAIError.Type = "authentication_error"
				} else {
					newErr.OpenAIError.Type = "access_denied"
				}
				newErr.OpenAIError.Message = utils.MessageWithRequestId("上游负载已饱和，请稍后再试", requestId)
				newErr.StatusCode = http.StatusTooManyRequests
				return newErr
			} else {
				newErr.OpenAIError.Type = "system_error"
			}
		}
	}

	// Codex 错误处理（优先处理，避免被通用逻辑覆盖）
	if channelType == config.ChannelTypeCodex && !newErr.LocalError {
		if newErr.OpenAIError.Type == "codex_error" || newErr.OpenAIError.Type == "codex_token_error" {
			if newErr.StatusCode == http.StatusUnauthorized || newErr.StatusCode == http.StatusForbidden {
				if cachedErr, exists := c.Get("first_non_auth_error"); exists {
					if firstErr, ok := cachedErr.(*types.OpenAIErrorWithStatusCode); ok {
						newErr = *firstErr
						if newErr.OpenAIError.Type == "codex_error" {
							newErr.OpenAIError.Type = "system_error"
						}
						newErr.OpenAIError.Message = utils.MessageWithRequestId(newErr.OpenAIError.Message, requestId)
						return newErr
					}
				}
				if newErr.StatusCode == http.StatusUnauthorized {
					newErr.OpenAIError.Type = "authentication_error"
				} else {
					newErr.OpenAIError.Type = "access_denied"
				}
				newErr.OpenAIError.Message = utils.MessageWithRequestId("上游负载已饱和，请稍后再试", requestId)
				newErr.StatusCode = http.StatusTooManyRequests
				return newErr
			} else {
				newErr.OpenAIError.Type = "system_error"
			}
		}
	}

	// Antigravity 错误处理（优先处理，避免被通用逻辑覆盖）
	if channelType == config.ChannelTypeAntigravity && !newErr.LocalError {
		if newErr.OpenAIError.Type == "antigravity_error" || newErr.OpenAIError.Type == "antigravity_token_error" {
			if newErr.StatusCode == http.StatusUnauthorized || newErr.StatusCode == http.StatusForbidden {
				if cachedErr, exists := c.Get("first_non_auth_error"); exists {
					if firstErr, ok := cachedErr.(*types.OpenAIErrorWithStatusCode); ok {
						newErr = *firstErr
						if newErr.OpenAIError.Type == "antigravity_error" {
							newErr.OpenAIError.Type = "system_error"
						}
						newErr.OpenAIError.Message = utils.MessageWithRequestId(newErr.OpenAIError.Message, requestId)
						return newErr
					}
				}
				if newErr.StatusCode == http.StatusUnauthorized {
					newErr.OpenAIError.Type = "authentication_error"
				} else {
					newErr.OpenAIError.Type = "access_denied"
				}
				newErr.OpenAIError.Message = utils.MessageWithRequestId("上游负载已饱和，请稍后再试", requestId)
				newErr.StatusCode = http.StatusTooManyRequests
				return newErr
			} else {
				newErr.OpenAIError.Type = "system_error"
			}
		}
	}

	// 通用错误处理
	if !newErr.LocalError && (newErr.OpenAIError.Type == "one_hub_error" || strings.HasSuffix(newErr.OpenAIError.Type, "_api_error")) {
		newErr.OpenAIError.Type = "system_error"
		if utils.ContainsString(newErr.Message, quotaKeywords) {
			newErr.Message = "上游负载已饱和，请稍后再试"
			newErr.StatusCode = http.StatusTooManyRequests
		}
	}

	if code, ok := newErr.OpenAIError.Code.(string); ok && code == "bad_response_status_code" && !strings.Contains(newErr.OpenAIError.Message, "bad response status code") {
		newErr.OpenAIError.Message = fmt.Sprintf("Provider API error: bad response status code %s", newErr.OpenAIError.Param)
	}

	return newErr
}

func relayResponseWithOpenAIErr(c *gin.Context, err *types.OpenAIErrorWithStatusCode) {
	c.JSON(err.StatusCode, gin.H{
		"error": err.OpenAIError,
	})
}

func relayRerankResponseWithErr(c *gin.Context, err *types.OpenAIErrorWithStatusCode) {
	// 如果message中已经包含 request id: 则不再添加
	if !strings.Contains(err.Message, "request id:") {
		requestId := c.GetString(logger.RequestIdKey)
		err.OpenAIError.Message = utils.MessageWithRequestId(err.OpenAIError.Message, requestId)
	}

	if err.OpenAIError.Type == "new_api_error" || err.OpenAIError.Type == "one_api_error" {
		err.OpenAIError.Type = "system_error"
	}

	c.JSON(err.StatusCode, gin.H{
		"detail": err.OpenAIError.Message,
	})
}

// removeNestedParam removes a parameter from the map, supporting nested paths like "generationConfig.thinkingConfig"
func removeNestedParam(requestMap map[string]interface{}, paramPath string) {
	// 使用 "." 分割路径
	parts := strings.Split(paramPath, ".")
	
	// 如果只有一层，直接删除
	if len(parts) == 1 {
		delete(requestMap, paramPath)
		return
	}
	
	// 处理嵌套路径
	current := requestMap
	for i := 0; i < len(parts)-1; i++ {
		if next, ok := current[parts[i]].(map[string]interface{}); ok {
			current = next
		} else {
			// 如果中间路径不存在或不是 map，则无法继续
			return
		}
	}
	
	// 删除最后一级的键
	delete(current, parts[len(parts)-1])
}

// mergeCustomParamsForPreMapping applies custom parameter logic similar to OpenAI provider
func mergeCustomParamsForPreMapping(requestMap map[string]interface{}, customParams map[string]interface{}) map[string]interface{} {
	// 检查是否需要覆盖已有参数
	shouldOverwrite := false
	if overwriteValue, exists := customParams["overwrite"]; exists {
		if boolValue, ok := overwriteValue.(bool); ok {
			shouldOverwrite = boolValue
		}
	}

	// 检查是否按照模型粒度控制
	perModel := false
	if perModelValue, exists := customParams["per_model"]; exists {
		if boolValue, ok := perModelValue.(bool); ok {
			perModel = boolValue
		}
	}

	customParamsModel := customParams
	if perModel {
		if modelValue, ok := requestMap["model"].(string); ok {
			if v, exists := customParams[modelValue]; exists {
				if modelConfig, ok := v.(map[string]interface{}); ok {
					customParamsModel = modelConfig
				} else {
					customParamsModel = map[string]interface{}{}
				}
			} else {
				customParamsModel = map[string]interface{}{}
			}
		}
	}

	// 处理参数删除
	if removeParams, exists := customParamsModel["remove_params"]; exists {
		if paramsList, ok := removeParams.([]interface{}); ok {
			for _, param := range paramsList {
				if paramName, ok := param.(string); ok {
					removeNestedParam(requestMap, paramName)
				}
			}
		}
	}

	// 添加额外参数
	for key, value := range customParamsModel {
		if key == "stream" || key == "overwrite" || key == "per_model" || key == "pre_add" || key == "remove_params" {
			continue
		}

		// 根据覆盖设置决定如何添加参数
		if shouldOverwrite {
			// 覆盖模式：直接添加/覆盖参数
			requestMap[key] = value
		} else {
			// 非覆盖模式：仅当参数不存在时添加
			if _, exists := requestMap[key]; !exists {
				requestMap[key] = value
			}
		}
	}

	return requestMap
}
