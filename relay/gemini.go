package relay

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/requester"
	"done-hub/providers/gemini"
	"done-hub/safty"
	"done-hub/types"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

var AllowGeminiChannelType = []int{config.ChannelTypeGemini, config.ChannelTypeVertexAI, config.ChannelTypeGeminiCli, config.ChannelTypeAntigravity, config.ChannelTypeVertexAIExpress}

type relayGeminiOnly struct {
	relayBase
	geminiRequest      *gemini.GeminiChatRequest
	requestBody        []byte // 原始请求 bytes，延迟到 getChatRequest 再反序列化，避免大 payload 提前膨胀内存
	cachedPromptTokens int    // 缓存 token 计数结果，避免 retry 时读已释放的 requestBody
	promptTokensCached bool   // 标记 token 计数是否已缓存
}

func NewRelayGeminiOnly(c *gin.Context) *relayGeminiOnly {
	c.Set("allow_channel_type", AllowGeminiChannelType)
	relay := &relayGeminiOnly{
		relayBase: relayBase{
			allowHeartbeat: true,
			c:              c,
		},
	}

	return relay
}

func (r *relayGeminiOnly) setRequest() error {
	// 支持两种格式: /:version/models/:model 和 /:version/models/*action
	modelAction := r.c.Param("model")
	if modelAction == "" {
		// 尝试获取action参数（用于 model:predict 格式）
		actionPath := r.c.Param("action")
		if actionPath == "" {
			return errors.New("model is required")
		}
		// 去掉开头的斜杠
		actionPath = strings.TrimPrefix(actionPath, "/")
		modelAction = actionPath
	}

	modelList := strings.Split(modelAction, ":")
	if len(modelList) != 2 {
		return errors.New("model error")
	}

	isStream := false
	action := modelList[1]
	if action == "streamGenerateContent" {
		isStream = true
	}

	// 只读取原始 bytes，不做 JSON 反序列化
	// 避免 json.Unmarshal 对大 payload（如 base64 图片）的字符串分配开销
	// 反序列化延迟到 getChatRequest 中按需执行
	rawBody, err := common.ReadBodyRaw(r.c)
	if err != nil {
		return err
	}
	r.requestBody = rawBody

	r.geminiRequest = &gemini.GeminiChatRequest{
		Model:  modelList[0],
		Stream: isStream,
		Action: action,
	}
	r.setOriginalModel(r.geminiRequest.Model)
	// 设置原始模型到 Context，用于统一请求响应模型功能
	r.c.Set("original_model", r.geminiRequest.Model)

	return nil
}

func (r *relayGeminiOnly) getRequest() interface{} {
	return r.geminiRequest
}

func (r *relayGeminiOnly) IsStream() bool {
	return r.geminiRequest.Stream
}

func (r *relayGeminiOnly) getPromptTokens() (int, error) {
	// 使用缓存：retry 时 r.requestBody 可能已被释放，直接返回首次计算结果
	if r.promptTokensCached {
		return r.cachedPromptTokens, nil
	}
	channel := r.provider.GetChannel()
	tokens, err := countGeminiTokenMessagesFromBytes(r.requestBody, r.geminiRequest.Model, channel.PreCost)
	if err != nil {
		return 0, err
	}
	r.cachedPromptTokens = tokens
	r.promptTokensCached = true
	return tokens, nil
}

func (r *relayGeminiOnly) send() (err *types.OpenAIErrorWithStatusCode, done bool) {
	chatProvider, ok := r.provider.(gemini.GeminiChatInterface)
	if !ok {
		return nil, false
	}

	// 内容审查：使用 gjson 直接在原始 bytes 上提取 text，避免完整 JSON 反序列化
	// 注意：这是 r.requestBody 最后一次被使用的地方
	if config.EnableSafe && r.requestBody != nil {
		contents := gjson.GetBytes(r.requestBody, "contents")
		for _, content := range contents.Array() {
			for _, part := range content.Get("parts").Array() {
				if text := part.Get("text").String(); text != "" {
					CheckResult, _ := safty.CheckContent(text)
					if !CheckResult.IsSafe {
						err = common.StringErrorWrapperLocal(CheckResult.Reason, CheckResult.Code, http.StatusBadRequest)
						done = true
						return
					}
				}
			}
		}
	}

	// 阶梯1释放：安全检查完毕后，relay 不再需要 raw bytes
	// provider 通过 GinRequestBodyKey 独立访问（getChatRequest 处理后会释放）
	// retry 时安全检查可跳过（内容不变），token 计数使用缓存
	r.requestBody = nil

	r.geminiRequest.Model = r.modelName

	if r.geminiRequest.Stream {
		var response requester.StreamReaderInterface[string]
		response, err = chatProvider.CreateGeminiChatStream(r.geminiRequest)
		if err != nil {
			// cache 失效的 403 → 剥字段，让下一轮 retry 不再带上失效引用
			r.handleCachedContentFailure(err)
			return
		}

		// 阶梯2释放：上游请求成功建立，不会再 retry，释放所有 body 引用
		// 流式模式下 body 在响应流传输期间（可能数分钟）不再需要
		r.releaseBody()

		if r.heartbeat != nil {
			r.heartbeat.Stop()
		}

		doneStr := func() string {
			return ""
		}
		firstResponseTime := responseGeneralStreamClient(r.c, response, doneStr)
		r.SetFirstResponseTime(firstResponseTime)
	} else {
		var response *gemini.GeminiChatResponse
		response, err = chatProvider.CreateGeminiChat(r.geminiRequest)
		if err != nil {
			// cache 失效的 403 → 剥字段，让下一轮 retry 不再带上失效引用
			r.handleCachedContentFailure(err)
			return
		}

		// 阶梯2释放：上游请求成功完成，不会再 retry，释放所有 body 引用
		r.releaseBody()

		if r.heartbeat != nil {
			r.heartbeat.Stop()
		}

		err = responseJsonClient(r.c, response)
	}

	if err != nil {
		done = true
	}

	return
}

// releaseBody 释放所有 body 相关的内存引用
// 仅在确认不会再 retry 时调用（上游请求成功后）
func (r *relayGeminiOnly) releaseBody() {
	r.requestBody = nil
	r.c.Set(config.GinRequestBodyKey, nil)
	r.c.Set(config.GinProcessedBytesKey, nil)
	r.c.Set(config.GinProcessedBytesIsVertexAI, nil)
}

// handleCachedContentFailure 撞上 "CachedContent not found" 后剥掉 cachedContent，
// 让下一轮 retry 复用已 clean 的 body 但不再带失效引用。
// AllowGeminiChannelType 下两类 provider 用不同缓存：
//   - Gemini / VertexAI / VertexAIExpress: 写 GinProcessedBytesKey (bytes)；读取顺序 bytes → raw → map
//   - GeminiCli / Antigravity:             写 GinProcessedBodyKey (map)；只读 map 系列，不读 bytes 缓存
//
// 两个 key 都尝试剥可覆盖：同组渠道间 retry、以及 "cli/antigravity 首攻 → Gemini 家 retry 通过 map 回退命中"。
// 反方向（Gemini 家首攻 → cli/antigravity retry）会撞预先存在的 "request body not found"——
// 因为 SetProcessedBodyBytes 会 nil 掉 raw bytes 和 raw map，cli 这边没东西可读。本函数不解决这条。
func (r *relayGeminiOnly) handleCachedContentFailure(apiErr *types.OpenAIErrorWithStatusCode) {
	if apiErr == nil || apiErr.StatusCode != http.StatusForbidden {
		return
	}
	if !strings.Contains(apiErr.OpenAIError.Message, gemini.CachedContentNotFoundMsg) {
		return
	}

	stripped := false
	// bytes 路径：Gemini / VertexAI / VertexAIExpress provider
	if raw, ok := r.c.Get(config.GinProcessedBytesKey); ok {
		if data, ok := raw.([]byte); ok && len(data) > 0 {
			r.c.Set(config.GinProcessedBytesKey, gemini.StripCachedContentBytes(data))
			stripped = true
		}
	}
	// map 路径：GeminiCli / Antigravity provider
	if raw, ok := r.c.Get(config.GinProcessedBodyKey); ok {
		if m, ok := raw.(map[string]interface{}); ok {
			gemini.StripCachedContentMap(m)
			stripped = true
		}
	}
	if !stripped {
		return
	}

	var channelId int
	if r.provider != nil {
		if ch := r.provider.GetChannel(); ch != nil {
			channelId = ch.Id
		}
	}
	logger.LogWarn(r.c.Request.Context(), fmt.Sprintf(
		"gemini_cached_content_stripped reason=upstream_cache_invalid model=%s channel_id=%d",
		r.modelName, channelId))
}

func (r *relayGeminiOnly) GetError(err *types.OpenAIErrorWithStatusCode) (int, any) {
	newErr := FilterOpenAIErr(r.c, err)

	geminiErr := gemini.OpenaiErrToGeminiErr(&newErr)

	return newErr.StatusCode, geminiErr.GeminiErrorResponse
}

func (r *relayGeminiOnly) HandleJsonError(err *types.OpenAIErrorWithStatusCode) {
	statusCode, response := r.GetError(err)
	r.c.JSON(statusCode, response)
}

func (r *relayGeminiOnly) HandleStreamError(err *types.OpenAIErrorWithStatusCode) {
	_, response := r.GetError(err)

	str, jsonErr := json.Marshal(response)
	if jsonErr != nil {
		return
	}
	r.c.Writer.Write([]byte("data: " + string(str) + "\n\n"))
	r.c.Writer.Flush()
}

// countGeminiTokenMessagesFromBytes 使用 gjson 直接在原始 bytes 上计算 token 数
// 相比 map 方式，避免了对整个 body（含 base64 图片等大字段）的 json.Unmarshal
func countGeminiTokenMessagesFromBytes(requestBody []byte, model string, preCostType int) (int, error) {
	if preCostType == config.PreContNotAll {
		return 0, nil
	}

	tokenEncoder := common.GetTokenEncoder(model)

	tokenNum := 0
	tokensPerMessage := 4
	var textMsg strings.Builder

	contents := gjson.GetBytes(requestBody, "contents")
	for _, content := range contents.Array() {
		tokenNum += tokensPerMessage
		parts := content.Get("parts")
		for _, part := range parts.Array() {
			if text := part.Get("text"); text.Exists() {
				if s := text.String(); s != "" {
					textMsg.WriteString(s)
				}
			}
			if part.Get("inlineData").Exists() || part.Get("inline_data").Exists() {
				tokenNum += 200
			}
		}
	}

	if textMsg.Len() > 0 {
		tokenNum += common.GetTokenNum(tokenEncoder, textMsg.String())
	}
	return tokenNum, nil
}
