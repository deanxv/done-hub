package relay

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/requester"
	"done-hub/providers/gemini"
	"done-hub/safty"
	"done-hub/types"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

var AllowGeminiChannelType = []int{config.ChannelTypeGemini, config.ChannelTypeVertexAI, config.ChannelTypeGeminiCli, config.ChannelTypeAntigravity, config.ChannelTypeVertexAIExpress}

type relayGeminiOnly struct {
	relayBase
	geminiRequest *gemini.GeminiChatRequest
	requestBody   []byte // 原始请求 bytes，延迟到 getChatRequest 再反序列化，避免大 payload 提前膨胀内存
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
	channel := r.provider.GetChannel()
	return countGeminiTokenMessagesFromBytes(r.requestBody, r.geminiRequest.Model, channel.PreCost)
}

func (r *relayGeminiOnly) send() (err *types.OpenAIErrorWithStatusCode, done bool) {
	chatProvider, ok := r.provider.(gemini.GeminiChatInterface)
	if !ok {
		return nil, false
	}

	// 内容审查：使用 gjson 直接在原始 bytes 上提取 text，避免完整 JSON 反序列化
	if config.EnableSafe {
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

	r.geminiRequest.Model = r.modelName

	if r.geminiRequest.Stream {
		var response requester.StreamReaderInterface[string]
		response, err = chatProvider.CreateGeminiChatStream(r.geminiRequest)
		if err != nil {
			return
		}

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
			return
		}

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
