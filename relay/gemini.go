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
)

var AllowGeminiChannelType = []int{config.ChannelTypeGemini, config.ChannelTypeVertexAI, config.ChannelTypeGeminiCli, config.ChannelTypeAntigravity, config.ChannelTypeVertexAIExpress}

type relayGeminiOnly struct {
	relayBase
	geminiRequest *gemini.GeminiChatRequest
	requestMap    map[string]interface{} // 预解析的请求 map，消除 struct 反序列化
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

	// 直接读取为 map，跳过 struct 反序列化
	requestMap, err := common.ReadBodyToMap(r.c)
	if err != nil {
		return err
	}
	r.requestMap = requestMap

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
	return countGeminiTokenMessagesFromMap(r.requestMap, r.geminiRequest.Model, channel.PreCost)
}

func (r *relayGeminiOnly) send() (err *types.OpenAIErrorWithStatusCode, done bool) {
	chatProvider, ok := r.provider.(gemini.GeminiChatInterface)
	if !ok {
		return nil, false
	}

	// 内容审查
	if config.EnableSafe {
		if contents, ok := r.requestMap["contents"].([]interface{}); ok {
			for _, content := range contents {
				if contentMap, ok := content.(map[string]interface{}); ok {
					if parts, ok := contentMap["parts"].([]interface{}); ok {
						for _, part := range parts {
							if partMap, ok := part.(map[string]interface{}); ok {
								if text, ok := partMap["text"].(string); ok && text != "" {
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

func countGeminiTokenMessagesFromMap(requestMap map[string]interface{}, model string, preCostType int) (int, error) {
	if preCostType == config.PreContNotAll {
		return 0, nil
	}

	tokenEncoder := common.GetTokenEncoder(model)

	tokenNum := 0
	tokensPerMessage := 4
	var textMsg strings.Builder

	contents, _ := requestMap["contents"].([]interface{})
	for _, content := range contents {
		tokenNum += tokensPerMessage
		contentMap, ok := content.(map[string]interface{})
		if !ok {
			continue
		}
		parts, _ := contentMap["parts"].([]interface{})
		for _, part := range parts {
			partMap, ok := part.(map[string]interface{})
			if !ok {
				continue
			}
			if text, ok := partMap["text"].(string); ok && text != "" {
				textMsg.WriteString(text)
			}
			if _, ok := partMap["inlineData"]; ok {
				tokenNum += 200
			} else if _, ok := partMap["inline_data"]; ok {
				tokenNum += 200
			}
		}
	}

	if textMsg.Len() > 0 {
		tokenNum += common.GetTokenNum(tokenEncoder, textMsg.String())
	}
	return tokenNum, nil
}
