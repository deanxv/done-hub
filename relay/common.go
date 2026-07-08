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
	"done-hub/providers/claude"
	"done-hub/providers/gemini"
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
	} else if strings.HasPrefix(path, "/v1/responses/compact") {
		relay = NewRelayResponsesCompact(c)
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

// errModelNotFoundSentinel 标记"配置层就不可用"的错误，调用方据此选 ModelNotFoundError 而非
// UpstreamUnavailableError。GetProvider 用 fmt.Errorf("%w: %s", sentinel, modelName) 构造返回，
// 所以判定必须用 errors.Is 跨 wrap 链识别。
var errModelNotFoundSentinel = errors.New("model not found")

func IsModelNotFound(err error) bool {
	return errors.Is(err, errModelNotFoundSentinel)
}

// shouldReportModelNotFound 决定 GetProvider 全分组失败时是否把错误收敛为 model_not_found。
// modelName 为空（如 RelayOnly 的 /files、/batches 等无 model 字段端点）时不收敛，避免 sentinel
// 被 wrap 成 "model not found: " 这种带空尾巴的误导文案传给客户端。
func shouldReportModelNotFound(allConfigErr bool, modelName string) bool {
	return allConfigErr && modelName != ""
}

// isRuntimeChannelErr 命中"模型有配但当前没渠道"的运行时状态，不命中"模型在分组里根本就没配"的配置错误。
//
// 4 个 sentinel 来源不同 —— fetchChannelByModel 只 wrap 前两个，后两个是 fetchChannelById 直接产出：
//   - ErrNoChannelsAvailableSentinel / ErrNoAvailableChannelsAfterFilteringSentinel：来自 NextByValidatedModel，
//     经 fetchChannelByModel 的 fmt.Errorf("%w ...") wrap，跨层判定必须用 errors.Is。
//   - ErrInvalidChannelIdSentinel / ErrChannelDisabledSentinel：来自 fetchChannelById，直接返回 sentinel 不 wrap。
func isRuntimeChannelErr(err error) bool {
	return errors.Is(err, model.ErrNoChannelsAvailableSentinel) ||
		errors.Is(err, model.ErrNoAvailableChannelsAfterFilteringSentinel) ||
		errors.Is(err, model.ErrInvalidChannelIdSentinel) ||
		errors.Is(err, model.ErrChannelDisabledSentinel)
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

	// originalGroup 必须先取（用于日志和 isBackupGroup 判定）；下面的 validChain 是它的过滤产物。
	originalGroup := groupChain[0]

	// 剔除 user_group 表里查不到（不存在/已禁用）的分组，避免错误归因到 balancer 的"无可用渠道"模板。
	missingGroups := make([]string, 0, len(groupChain))
	validChain := make([]string, 0, len(groupChain))
	for _, g := range groupChain {
		if model.GlobalUserGroupRatio.GetBySymbol(g) == nil {
			missingGroups = append(missingGroups, g)
			continue
		}
		validChain = append(validChain, g)
	}

	// 尝试每个分组，直到成功获取渠道
	var lastErr error
	var actualModelName string
	var channel *model.Channel
	var usedGroup string
	var isBackupGroup bool
	// 任一分组报运行时错误就置 false，最终走 503 保留 SDK 重试；全配置错误才收敛为 model_not_found。
	allConfigErr := true
	configErrs := make([]string, 0)

	for _, groupName := range validChain {
		matchedModelName, err := model.ChannelGroup.GetMatchedModelName(groupName, modelName)
		if err != nil {
			lastErr = err
			configErrs = append(configErrs, fmt.Sprintf("%s: %v", model.GlobalUserGroupRatio.GetDisplayNameWithStatus(groupName), err))
			continue // 配置类，allConfigErr 不变
		}

		actualModelName = matchedModelName

		// 临时设置当前分组用于获取渠道
		c.Set("token_group", groupName)
		channel, err = fetchChannel(c, actualModelName)
		if err != nil {
			lastErr = err
			if isRuntimeChannelErr(err) {
				allConfigErr = false
			} else {
				configErrs = append(configErrs, fmt.Sprintf("%s: %v", model.GlobalUserGroupRatio.GetDisplayNameWithStatus(groupName), err))
			}
			continue // 尝试下一个分组
		}

		// 成功获取渠道
		usedGroup = groupName
		isBackupGroup = (groupName != originalGroup) // 不是原始第一优先级分组，说明走了降级

		break
	}

	// 所有分组都失败
	if channel == nil {
		// 循环里 c.Set("token_group", groupName) 会污染 context；失败时还原到调用方传入的原始值，
		// 避免下游中间件（含未来新增的 post-fail 处理）读到"最后一次尝试的分组"。
		c.Set("token_group", tokenGroup)

		// 全是配置错误（含分组不存在/禁用）→ model_not_found，让 SDK 不重试；详情进日志。
		// modelName="" 时（RelayOnly 路径）由 shouldReportModelNotFound 拦掉，避免空尾巴文案。
		if shouldReportModelNotFound(allConfigErr, modelName) {
			displayMissing := make([]string, len(missingGroups))
			for i, g := range missingGroups {
				displayMissing[i] = model.GlobalUserGroupRatio.GetDisplayNameWithStatus(g)
			}
			logger.LogError(c.Request.Context(), fmt.Sprintf(
				"model_not_found user=%d model=%s original_group=%s missing_groups=%v config_errs=%v",
				c.GetInt("id"), modelName, originalGroup, displayMissing, configErrs,
			))
			fail = fmt.Errorf("%w: %s", errModelNotFoundSentinel, modelName)
			return
		}

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
		return nil, model.ErrInvalidChannelIdSentinel
	}
	if channel.Status != config.ChannelStatusEnabled {
		return nil, model.ErrChannelDisabledSentinel
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
		// 这里只判 NextByValidatedModel 自产的两个 sentinel；isRuntimeChannelErr 还列了另外 2 个
		// （ErrInvalidChannelIdSentinel / ErrChannelDisabledSentinel）来自 fetchChannelById 直接返回，不经此分支。
		// 命中 runtime sentinel 时显式 wrap 出来，否则会落到下方默认分支被 errors.New(message) 重建，
		// sentinel 链断裂导致上层 isRuntimeChannelErr 漏判，渠道全冷却 / 状态异常会被误收敛成 model_not_found（SDK 不重试）。
		if errors.Is(err, model.ErrNoChannelsAvailableSentinel) || errors.Is(err, model.ErrNoAvailableChannelsAfterFilteringSentinel) {
			return nil, fmt.Errorf("%w (group=%s model=%s)", err, group, modelName)
		}
		message := fmt.Sprintf(model.ErrNoAvailableChannelForModel, model.GlobalUserGroupRatio.GetDisplayName(group), modelName)
		if channel != nil {
			logger.SysError(fmt.Sprintf("渠道不存在：%d", channel.Id))
			message = model.ErrDatabaseConsistencyBroken
		}
		return nil, errors.New(message)
	}

	return channel, nil
}

// unifyResponseModel 在响应写出前统一把含 model 字段的响应对象改写为用户原始请求模型名。
// 仅在 UnifiedRequestResponseModelEnabled 开启且 context 存在 original_model 时生效（由
// GetResponseModelNameFromContext 内部判断）；未启用时返回原值，幂等无副作用。
// 这是所有非流式 JSON 响应（chat/completions/embeddings/moderations/rerank/responses/claude/gemini）
// 的统一出口拦截点，避免逐个 provider 手动改写导致的覆盖遗漏。
func unifyResponseModel(c *gin.Context, data interface{}) {
	switch v := data.(type) {
	case *types.ChatCompletionResponse:
		v.Model = providersBase.GetResponseModelNameFromContext(c, v.Model)
	case *types.CompletionResponse:
		v.Model = providersBase.GetResponseModelNameFromContext(c, v.Model)
	case *types.EmbeddingResponse:
		v.Model = providersBase.GetResponseModelNameFromContext(c, v.Model)
	case *types.ModerationResponse:
		v.Model = providersBase.GetResponseModelNameFromContext(c, v.Model)
	case *types.RerankResponse:
		v.Model = providersBase.GetResponseModelNameFromContext(c, v.Model)
	case *types.OpenAIResponsesResponses:
		v.Model = providersBase.GetResponseModelNameFromContext(c, v.Model)
	case *claude.ClaudeResponse:
		v.Model = providersBase.GetResponseModelNameFromContext(c, v.Model)
	case *gemini.GeminiChatResponse:
		// Gemini 原生响应回显的是 modelVersion；同时存在 model 字段，二者都按需改写。
		// 仅当原值非空时改写，避免给本不含该字段的响应凭空注入。
		if v.ModelVersion != "" {
			v.ModelVersion = providersBase.GetResponseModelNameFromContext(c, v.ModelVersion)
		}
		if v.Model != "" {
			v.Model = providersBase.GetResponseModelNameFromContext(c, v.Model)
		}
	}
}

// applyBedrockPassThroughHeaders 把 provider 暂存的上游 AWS 响应头（x-amzn-* 等）
// 写入下游响应，必须在 WriteHeader 之前调用。仅 Bedrock 渠道会暂存该 key，其它渠道无影响。
func applyBedrockPassThroughHeaders(c *gin.Context) {
	v, ok := c.Get(config.GinBedrockPassThroughHeaders)
	if !ok {
		return
	}
	headers, ok := v.(http.Header)
	if !ok {
		return
	}
	for name, values := range headers {
		for _, value := range values {
			c.Writer.Header().Add(name, value)
		}
	}
}

// writeRawResponseBodyIfPresent 若 provider 暂存了上游原始响应字节，则直接透传，
// 保留上游的字段顺序 / 未知字段 / model 原名，避免结构体 re-marshal 洗掉指纹。
// Bedrock 专用 key 额外透传 x-amzn-* 头；通用 key（如 Claude 官方）仅透传字节。
// 命中并写出时返回 true，调用方应据此提前返回。
func writeRawResponseBodyIfPresent(c *gin.Context) bool {
	rawKeys := []struct {
		key         string
		withHeaders bool
	}{
		{config.GinBedrockRawResponseBodyKey, true},
		{config.GinRawResponseBodyKey, false},
	}
	for _, rk := range rawKeys {
		raw, ok := c.Get(rk.key)
		if !ok {
			continue
		}
		rawBytes, ok := raw.([]byte)
		if !ok || len(rawBytes) == 0 {
			continue
		}
		if rk.withHeaders {
			applyBedrockPassThroughHeaders(c)
		}
		c.Writer.Header().Set("Content-Type", "application/json")
		c.Writer.WriteHeader(http.StatusOK)
		if _, err := c.Writer.Write(rawBytes); err != nil {
			logger.LogError(c.Request.Context(), "write_response_body_failed:"+err.Error())
		}
		return true
	}
	return false
}

func responseJsonClient(c *gin.Context, data interface{}) *types.OpenAIErrorWithStatusCode {
	if writeRawResponseBodyIfPresent(c) {
		return nil
	}

	// 统一改写响应里的 model 字段为用户原始请求模型名（开关开启且存在映射时）
	unifyResponseModel(c, data)

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

		ctxDone := ctx.Done()

		// 安全写入：客户端断开后静默跳过
		tryWrite := func(msg string) {
			if !clientDisconnected {
				c.Writer.Write([]byte(msg))
				c.Writer.Flush()
			}
		}

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
				tryWrite("data: " + data + "\n\n")

			case err := <-errChan:
				if !errors.Is(err, io.EOF) {
					tryWrite("data: " + err.Error() + "\n\n")
					finalErr = common.StringErrorWrapper(err.Error(), "stream_error", 900)
					logger.LogError(c.Request.Context(), "Stream err:"+err.Error())
				} else {
					if finalErr == nil && endHandler != nil {
						if streamData := endHandler(); streamData != "" {
							tryWrite("data: " + streamData + "\n\n")
						}
					}
					tryWrite("data: [DONE]\n\n")
				}
				return

			case <-ctxDone:
				clientDisconnected = true
				ctxDone = nil // 置 nil 后此 case 不再命中，避免 CPU 空转
			}
		}
	}()

	<-done
	return firstResponseTime, finalErr
}

func responseGeneralStreamClient(c *gin.Context, stream requester.StreamReaderInterface[string], endHandler StreamEndHandler) (firstResponseTime time.Time) {
	requester.SetEventStreamHeaders(c)
	// Bedrock 指纹保真：透传上游 AWS 响应头（x-amzn-* 等）。必须在首次写入前设置。
	applyBedrockPassThroughHeaders(c)
	dataChan, errChan := stream.Recv()

	done := make(chan struct{})
	defer stream.Close()

	var isFirstResponse bool
	ctx := c.Request.Context()
	clientDisconnected := false

	go func() {
		defer close(done)

		ctxDone := ctx.Done()

		tryWrite := func(msg string) {
			if !clientDisconnected {
				fmt.Fprint(c.Writer, msg)
				c.Writer.Flush()
			}
		}

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
				tryWrite(data)

			case err := <-errChan:
				if !errors.Is(err, io.EOF) {
					tryWrite(err.Error())
					logger.LogError(c.Request.Context(), "Stream err:"+err.Error())
				} else {
					if endHandler != nil {
						if streamData := endHandler(); streamData != "" {
							tryWrite(streamData)
						}
					}
				}
				return

			case <-ctxDone:
				clientDisconnected = true
				ctxDone = nil // 置 nil 后此 case 不再命中，避免 CPU 空转
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
	case http.StatusRequestTimeout:
		return false
	case http.StatusBadRequest:
		return shouldRetryBadRequest(c, channelType, apiErr)
	}

	if apiErr.StatusCode/100 == 5 {
		return true
	}

	if apiErr.StatusCode/100 == 2 {
		return false
	}
	return true
}

func shouldRetryBadRequest(c *gin.Context, channelType int, apiErr *types.OpenAIErrorWithStatusCode) bool {
	switch channelType {
	case config.ChannelTypeAnthropic:
		return strings.Contains(apiErr.OpenAIError.Message, "Your credit balance is too low")
	case config.ChannelTypeBedrock:
		return strings.Contains(apiErr.OpenAIError.Message, "Operation not allowed")
	default:
		// gemini: 单渠道密钥失效属于"该渠道的问题"，应继续重试其他渠道而不是终止整条链。
		// 上游已知的 message 变体（大小写不统一，故全部 ToLower 后匹配）：
		//   - "API key not valid. Please pass a valid API key."
		//   - "API Key not found. Please pass a valid API key."
		//   - "API key expired. Please renew the API key."
		// 注意：reason=API_KEY_INVALID 来自 errorInfo.Details[].Reason，不会进 Message，
		// 因此这里只匹配 Message 文案，不要尝试匹配 reason 字符串。
		if apiErr.OpenAIError.Param == "INVALID_ARGUMENT" {
			msg := strings.ToLower(apiErr.OpenAIError.Message)
			if strings.Contains(msg, "api key not valid") ||
				strings.Contains(msg, "api key not found") ||
				strings.Contains(msg, "api key expired") {
				return true
			}
			// Gemini 3 thoughtSignature 跨 channel 校验失败：原 channel 签发的签名
			// 在 retry 到新 channel/key 后无法识别 → 400 INVALID_ARGUMENT
			// "Thought signature is not valid"。
			//
			// 首次撞到（thought_signature_retried 未置）：在此置标志位并返回 true，
			// 进入 retry 循环，retry 前由 relayGeminiOnly.handleThoughtSignatureFailure
			// 将请求里的 thoughtSignature 替换为官方哨兵 skip_thought_signature_validator。
			// 已剥过哨兵还挂说明上游有别的问题，不再死磕。
			//
			// 标志位置位由 shouldRetryBadRequest 集中负责（而非 handleX），保证：
			//   - "决定是否重试" 与 "改写 body" 的责任分离
			//   - 即使没有 bytes 缓存（如不带签名的请求误命中），也不会因 handleX 走空路径
			//     而漏置标志位、退化为无限重试
			if strings.Contains(msg, gemini.ThoughtSignatureInvalidMsg) {
				if c.GetBool("thought_signature_retried") {
					return false
				}
				c.Set("thought_signature_retried", true)
				return true
			}
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

// notifyChannelRelayError 是所有有重试循环的入口（main.go / recraftAI.go / rerank.go 等）
// 在拿到上游失败 apiErr 后的统一通知入口：
//   - 异步触发 processChannelRelayError（判断是否要永久禁用渠道）
//   - 记录 429 信号（upstream_seen_429）供 FilterOpenAIErr 坍缩时保留 status code，
//     让客户端 SDK 的标准退避算法生效
//
// 把两件事绑在一起，未来加新入口只要调一次，从机制上消除"漏调一个就丢 429 信号"的脆弱约定。
func notifyChannelRelayError(ctx context.Context, c *gin.Context, channel *model.Channel, apiErr *types.OpenAIErrorWithStatusCode) {
	go processChannelRelayError(ctx, channel.Id, channel.Name, apiErr, channel.Type)
	if apiErr != nil && apiErr.StatusCode == http.StatusTooManyRequests {
		c.Set("upstream_seen_429", true)
	}
}

var requestIdRegex = regexp.MustCompile(`\(request id: [^\)]+\)`)

func FilterOpenAIErr(c *gin.Context, err *types.OpenAIErrorWithStatusCode) (errWithStatusCode types.OpenAIErrorWithStatusCode) {
	// 兜底脱敏:在最终返回前统一对 Message 脱敏,避免逐条 return 漏改。
	// skipMask=true 时跳过——管理员在后台配置的 ChannelFailErrorMessage 属于受信内容，
	// 运维若特意写了 support@x.com 这类联系方式，不应被脱敏成 ***。
	skipMask := false
	defer func() {
		if !skipMask {
			errWithStatusCode.OpenAIError.Message = utils.MaskSensitiveInfo(errWithStatusCode.OpenAIError.Message)
		}
	}()

	newErr := types.OpenAIErrorWithStatusCode{}
	if err != nil {
		newErr = *err
	}

	// 客户端可见错误白名单：仅 400 透传上游原文（参数错误对客户端有意义），
	// 其余统一坍缩为 503 + 固定文案，避免客户端通过 status code 或 message 差异
	// 反推上游身份/key 状态/路径等内部信息。
	// 坍缩范围：
	//   1) 非 LocalError 且 status != 400：所有上游返回的非 400 错误（401/403/404/429/5xx）
	//   2) LocalError 且 type == "upstream_unavailable"：路由阶段无渠道、重试超时等
	//      "渠道整体不可用"语义的本地错误（消息体仍保留用于内部日志诊断）
	// 其余 LocalError（参数错误、计费错误等）走原路径，文案本就是我们自己写的。
	// 总开关 ChannelFailErrorWrapEnabled 关闭时跳过坍缩，让运维能临时看到上游真实错误用于调试。
	collapse := config.ChannelFailErrorWrapEnabled &&
		((!newErr.LocalError && newErr.StatusCode != http.StatusBadRequest) ||
			(newErr.LocalError && newErr.OpenAIError.Type == "upstream_unavailable"))
	if collapse {
		// 坍缩 message 来自管理员后台配置，跳过脱敏。
		skipMask = true
		requestId := c.GetString(logger.RequestIdKey)
		// 默认坍缩到 503；保留 429 的两个触发源：
		//   - upstream_seen_429：main.go 重试循环里检测到的"链中曾出现过 429"
		//   - newErr.StatusCode == 429：单次入口（relay/relay.go RelayOnly、relay/recraftAI.go 等）的最终错误是 429
		// OR 兜底确保所有路径都能保留 429 让客户端 SDK 的标准退避算法生效。
		// message 仍统一为自定义文案，隐藏上游 provider 名/key 状态等内部信息。
		statusCode := http.StatusServiceUnavailable
		errCode := "service_unavailable"
		if c.GetBool("upstream_seen_429") || newErr.StatusCode == http.StatusTooManyRequests {
			statusCode = http.StatusTooManyRequests
			errCode = "rate_limit_exceeded"
		}
		return types.OpenAIErrorWithStatusCode{
			StatusCode: statusCode,
			OpenAIError: types.OpenAIError{
				Type:    "system_error",
				Code:    errCode,
				Message: utils.MessageWithRequestId(config.GetChannelFailErrorMessage(), requestId),
			},
		}
	}

	// 至此 newErr 落到此分支的两类情况：
	//   1) ChannelFailErrorWrapEnabled=true 的残余路径：
	//      - !LocalError && StatusCode == 400：上游 400 透传给客户端（参数错误对其有意义）
	//      - LocalError 且 type != upstream_unavailable：本地参数/计费/token 等业务错误，文案本就是我们自己写的
	//   2) ChannelFailErrorWrapEnabled=false（运维临时关掉调试）：所有错误都落到这里，包括上游 4xx/5xx 与 upstream_unavailable 类 LocalError
	// 这里做最后的轻度规整：拼 request id、隐藏暴露上游身份或内部 sentinel 的 type 标签、修补 bad_response_status_code 文案。

	// 如果message中已经包含 request id: 则不再添加
	if strings.Contains(newErr.Message, "(request id:") {
		newErr.Message = requestIdRegex.ReplaceAllString(newErr.Message, "")
	}

	requestId := c.GetString(logger.RequestIdKey)
	newErr.OpenAIError.Message = utils.MessageWithRequestId(newErr.OpenAIError.Message, requestId)

	// 隐藏暴露上游身份或仅供内部使用的 type 标签：
	//   - one_hub_error / *_api_error：暴露上游 SDK 身份（anthropic_api_error 等）
	//   - upstream_unavailable：internal sentinel，仅用于 collapse 路由；关闭开关调试时不应漏给客户端
	if newErr.OpenAIError.Type == "upstream_unavailable" ||
		(!newErr.LocalError && (newErr.OpenAIError.Type == "one_hub_error" || strings.HasSuffix(newErr.OpenAIError.Type, "_api_error"))) {
		newErr.OpenAIError.Type = "system_error"
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
