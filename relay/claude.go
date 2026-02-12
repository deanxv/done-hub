package relay

import (
	"bufio"
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/model_utils"
	"done-hub/common/requester"
	"done-hub/common/utils"
	"done-hub/providers/antigravity"
	"done-hub/providers/claude"
	"done-hub/providers/gemini"
	"done-hub/providers/openai"
	"done-hub/providers/vertexai"
	"done-hub/relay/transformer"
	"done-hub/safty"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var AllowChannelType = []int{config.ChannelTypeAnthropic, config.ChannelTypeVertexAI, config.ChannelTypeBedrock, config.ChannelTypeCustom, config.ChannelTypeGemini, config.ChannelTypeGeminiCli, config.ChannelTypeClaudeCode, config.ChannelTypeCodex, config.ChannelTypeAntigravity}

type relayClaudeOnly struct {
	relayBase
	claudeRequest *claude.ClaudeRequest
}

func NewRelayClaudeOnly(c *gin.Context) *relayClaudeOnly {
	c.Set("allow_channel_type", AllowChannelType)
	relay := &relayClaudeOnly{
		relayBase: relayBase{
			allowHeartbeat: true,
			c:              c,
		},
	}

	return relay
}

func (r *relayClaudeOnly) setRequest() error {
	r.claudeRequest = &claude.ClaudeRequest{}
	if err := common.UnmarshalBodyReusable(r.c, r.claudeRequest); err != nil {
		return err
	}
	r.setOriginalModel(r.claudeRequest.Model)
	// 设置原始模型到 Context，用于统一请求响应模型功能
	r.c.Set("original_model", r.claudeRequest.Model)

	// 保持原始的流式/非流式状态

	return nil
}

func (r *relayClaudeOnly) getRequest() interface{} {
	return r.claudeRequest
}

func (r *relayClaudeOnly) IsStream() bool {
	return r.claudeRequest.Stream
}

func (r *relayClaudeOnly) getPromptTokens() (int, error) {
	channel := r.provider.GetChannel()
	return CountTokenMessages(r.claudeRequest, channel.PreCost)
}

func (r *relayClaudeOnly) send() (err *types.OpenAIErrorWithStatusCode, done bool) {

	// 应用 Claude Thinking 约束校验（tool_choice 冲突检测 + max_tokens 自动调整）
	r.applyClaudeThinkingConstraints()

	// 检查是否为自定义渠道，如果是则使用Claude->OpenAI->Claude的转换逻辑
	channelType := r.provider.GetChannel().Type

	if channelType == config.ChannelTypeCustom {

		return r.sendCustomChannelWithClaudeFormat()
	}

	// 检查是否为 VertexAI 渠道且模型包含 gemini，如果是则使用 Gemini->Claude 转换逻辑
	if channelType == config.ChannelTypeVertexAI &&
		(model_utils.ContainsCaseInsensitive(r.claudeRequest.Model, "gemini") || model_utils.ContainsCaseInsensitive(r.claudeRequest.Model, "claude-3-5-haiku-20241022")) {
		return r.sendVertexAIGeminiWithClaudeFormat()
	}

	// 检查是否为 Gemini 渠道，如果是则使用 Gemini->Claude 转换逻辑
	if channelType == config.ChannelTypeGemini {
		return r.sendGeminiWithClaudeFormat()
	}

	// 检查是否为 Antigravity 渠道，如果是则使用 Antigravity->Claude 转换逻辑
	if channelType == config.ChannelTypeAntigravity {
		return r.sendAntigravityWithClaudeFormat()
	}

	chatProvider, ok := r.provider.(claude.ClaudeChatInterface)
	if !ok {
		logger.SysError(fmt.Sprintf("[Claude Relay] Provider 不支持 Claude 接口，Provider 类型: %T", r.provider))
		err = common.StringErrorWrapperLocal("channel not implemented", "channel_error", http.StatusServiceUnavailable)
		done = true
		return
	}

	r.claudeRequest.Model = r.modelName
	// 内容审查
	if safetyErr := r.performContentSafety(); safetyErr != nil {
		err = safetyErr
		done = true
		return
	}

	if r.claudeRequest.Stream {
		var response requester.StreamReaderInterface[string]
		response, err = chatProvider.CreateClaudeChatStream(r.claudeRequest)
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
		var response *claude.ClaudeResponse
		response, err = chatProvider.CreateClaudeChat(r.claudeRequest)
		if err != nil {
			return
		}

		if r.heartbeat != nil {
			r.heartbeat.Stop()
		}

		openErr := responseJsonClient(r.c, response)

		if openErr != nil {
			err = openErr
		}
	}

	if err != nil {
		done = true
	}
	return
}

func (r *relayClaudeOnly) GetError(err *types.OpenAIErrorWithStatusCode) (int, any) {
	newErr := FilterOpenAIErr(r.c, err)

	claudeErr := claude.OpenaiErrToClaudeErr(&newErr)

	return newErr.StatusCode, claudeErr.ClaudeError
}

func (r *relayClaudeOnly) HandleJsonError(err *types.OpenAIErrorWithStatusCode) {
	statusCode, response := r.GetError(err)
	r.c.JSON(statusCode, response)
}

func (r *relayClaudeOnly) HandleStreamError(err *types.OpenAIErrorWithStatusCode) {
	_, response := r.GetError(err)

	str, jsonErr := json.Marshal(response)
	if jsonErr != nil {
		return
	}
	r.c.Writer.Write([]byte("event: error\ndata: " + string(str) + "\n\n"))
	r.c.Writer.Flush()
}

// 公共工具函数

// performContentSafety 执行内容安全检查
func (r *relayClaudeOnly) performContentSafety() *types.OpenAIErrorWithStatusCode {
	if !config.EnableSafe {
		return nil
	}

	for _, message := range r.claudeRequest.Messages {
		if message.Content != nil {
			CheckResult, _ := safty.CheckContent(message.Content)
			if !CheckResult.IsSafe {
				return common.StringErrorWrapperLocal(CheckResult.Reason, CheckResult.Code, http.StatusBadRequest)
			}
		}
	}
	return nil
}

// convertFinishReason 转换停止原因从OpenAI格式到Claude格式
func convertFinishReason(finishReason string) string {
	switch finishReason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	case "content_filter":
		return "stop_sequence"
	default:
		return "end_turn"
	}
}

// setStreamHeaders 设置流式响应的HTTP头
func (r *relayClaudeOnly) setStreamHeaders() {
	r.c.Header("Content-Type", "text/event-stream")
	r.c.Header("Cache-Control", "no-cache")
	r.c.Header("Connection", "keep-alive")
}

func CountTokenMessages(request *claude.ClaudeRequest, preCostType int) (int, error) {
	if preCostType == config.PreContNotAll {
		return 0, nil
	}

	tokenEncoder := common.GetTokenEncoder(request.Model)

	tokenNum := 0

	tokensPerMessage := 4
	var textMsg strings.Builder

	for _, message := range request.Messages {
		tokenNum += tokensPerMessage
		switch v := message.Content.(type) {
		case string:
			textMsg.WriteString(v)
		case []any:
			for _, m := range v {
				content := m.(map[string]any)
				switch content["type"] {
				case "text":
					textMsg.WriteString(content["text"].(string))
				default:
					// 不算了  就只算他50吧
					tokenNum += 50
				}
			}
		}
	}

	if textMsg.Len() > 0 {
		tokenNum += common.GetTokenNum(tokenEncoder, textMsg.String())
	}

	return tokenNum, nil
}

// sendCustomChannelWithClaudeFormat 处理自定义渠道的Claude格式请求
// 仅在 /claude/v1/messages 路由时调用，实现 Claude格式 -> OpenAI格式 -> 上游接口 -> OpenAI响应 -> Claude格式 的转换
func (r *relayClaudeOnly) sendCustomChannelWithClaudeFormat() (err *types.OpenAIErrorWithStatusCode, done bool) {

	// 将Claude请求转换为OpenAI格式
	openaiRequest, err := r.convertClaudeToOpenAI()
	if err != nil {

		return err, true
	}

	// 内容审查
	if safetyErr := r.performContentSafety(); safetyErr != nil {
		err = safetyErr
		done = true
		return
	}

	openaiRequest.Model = r.modelName

	// 获取OpenAI provider来处理请求
	openaiProvider, ok := r.provider.(*openai.OpenAIProvider)
	if !ok {
		err = common.StringErrorWrapperLocal("custom channel provider error", "channel_error", http.StatusServiceUnavailable)
		done = true
		return
	}

	if r.claudeRequest.Stream {
		// 处理流式响应

		var stream requester.StreamReaderInterface[string]
		stream, err = openaiProvider.CreateChatCompletionStream(openaiRequest)
		if err != nil {

			return err, true
		}

		if r.heartbeat != nil {
			r.heartbeat.Stop()
		}

		// 转换OpenAI流式响应为Claude格式
		firstResponseTime := r.convertOpenAIStreamToClaude(stream)
		r.SetFirstResponseTime(time.Unix(firstResponseTime, 0))
	} else {
		// 处理非流式响应

		var openaiResponse *types.ChatCompletionResponse
		openaiResponse, err = openaiProvider.CreateChatCompletion(openaiRequest)
		if err != nil {

			return err, true
		}

		if r.heartbeat != nil {
			r.heartbeat.Stop()
		}

		// 转换OpenAI响应为Claude格式
		claudeResponse := r.convertOpenAIResponseToClaude(openaiResponse)
		openErr := responseJsonClient(r.c, claudeResponse)

		if openErr != nil {
			// 对于响应发送错误（如客户端断开连接），不应该触发重试
			// 这种错误是客户端问题，不是服务端问题

			// 不设置 err，避免触发重试机制
		}

	}

	return err, false
}

// Schema清理模式
type schemaCleanMode int

const (
	schemaCleanNone     schemaCleanMode = iota // 不清理
	schemaCleanFull                            // 完全清理（移除不支持字段 + 转换 type 数组为 nullable）
	schemaCleanMetaOnly                        // 仅清理元字段（移除 $schema 等，但保留 type 数组格式）
)

// convertClaudeToOpenAI 将Claude请求转换为OpenAI格式
func (r *relayClaudeOnly) convertClaudeToOpenAI() (*types.ChatCompletionRequest, *types.OpenAIErrorWithStatusCode) {
	return r.convertClaudeToOpenAIWithMode(schemaCleanFull) // 默认完全清理
}

// convertClaudeToOpenAIForVertexAI 专门为VertexAI渠道转换，不进行schema清理
func (r *relayClaudeOnly) convertClaudeToOpenAIForVertexAI() (*types.ChatCompletionRequest, *types.OpenAIErrorWithStatusCode) {
	return r.convertClaudeToOpenAIWithMode(schemaCleanNone) // 不进行schema清理
}

// convertClaudeToOpenAIForAntigravity 专门为Antigravity渠道转换
// 移除 $schema 等元字段，但保留 type 数组格式（兼容 Claude API）
func (r *relayClaudeOnly) convertClaudeToOpenAIForAntigravity() (*types.ChatCompletionRequest, *types.OpenAIErrorWithStatusCode) {
	return r.convertClaudeToOpenAIWithMode(schemaCleanMetaOnly)
}

// convertClaudeToOpenAIWithOptions 向后兼容的包装函数
func (r *relayClaudeOnly) convertClaudeToOpenAIWithOptions(cleanSchema bool) (*types.ChatCompletionRequest, *types.OpenAIErrorWithStatusCode) {
	if cleanSchema {
		return r.convertClaudeToOpenAIWithMode(schemaCleanFull)
	}
	return r.convertClaudeToOpenAIWithMode(schemaCleanNone)
}

// convertClaudeToOpenAIWithMode 将Claude请求转换为OpenAI格式，支持多种清理模式
func (r *relayClaudeOnly) convertClaudeToOpenAIWithMode(cleanMode schemaCleanMode) (*types.ChatCompletionRequest, *types.OpenAIErrorWithStatusCode) {
	openaiRequest := &types.ChatCompletionRequest{
		Model:       r.claudeRequest.Model,
		Messages:    make([]types.ChatCompletionMessage, 0),
		MaxTokens:   r.claudeRequest.MaxTokens,
		Temperature: r.claudeRequest.Temperature,
		TopP:        r.claudeRequest.TopP,
		Stream:      r.claudeRequest.Stream,
	}

	// 处理 Stop 参数，过滤掉 null 值
	if r.claudeRequest.StopSequences != nil {
		openaiRequest.Stop = r.claudeRequest.StopSequences
	}

	// 处理 Thinking 参数 - 将 Claude 的 thinking 转换为 OpenAI 的 Reasoning
	if r.claudeRequest.Thinking != nil && r.claudeRequest.Thinking.Type == "enabled" {
		budgetTokens := r.claudeRequest.Thinking.BudgetTokens
		maxTokens := r.claudeRequest.MaxTokens

		// 安全校验1: 检查 budget >= max_tokens，自动下调
		if budgetTokens >= maxTokens {
			adjustedBudget := maxTokens - 1
			if adjustedBudget <= 0 {
				// 无法下调到正数，跳过 thinking 配置
				goto skipThinking
			}
			budgetTokens = adjustedBudget
		}

		// 安全校验2: 检查历史 assistant 消息是否以 thinking 开头
		if len(r.claudeRequest.Messages) > 0 {
			// 找到最后一条 assistant 消息
			for i := len(r.claudeRequest.Messages) - 1; i >= 0; i-- {
				msg := r.claudeRequest.Messages[i]
				if msg.Role == "assistant" {
					// 检查内容是否以 thinking/redacted_thinking 开头
					if content, ok := msg.Content.([]interface{}); ok && len(content) > 0 {
						if firstBlock, ok := content[0].(map[string]interface{}); ok {
							blockType, _ := firstBlock["type"].(string)
							if blockType != "thinking" && blockType != "redacted_thinking" {
								// 历史消息不以 thinking 开头，跳过 thinking 配置
								goto skipThinking
							}
						}
					}
					break
				}
			}
		}

		openaiRequest.Reasoning = &types.ChatReasoning{
			MaxTokens: budgetTokens,
		}
	}
skipThinking:

	// 处理系统消息
	if r.claudeRequest.System != nil {

		switch sys := r.claudeRequest.System.(type) {
		case string:

			openaiRequest.Messages = append(openaiRequest.Messages, types.ChatCompletionMessage{
				Role:    types.ChatMessageRoleSystem,
				Content: sys,
			})
		case []interface{}:

			// 处理数组形式的系统消息 - 每个文本部分创建单独的系统消息
			for _, item := range sys {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if itemType, exists := itemMap["type"]; exists && itemType == "text" {
						if text, textExists := itemMap["text"]; textExists {
							if textStr, ok := text.(string); ok && textStr != "" {
								openaiRequest.Messages = append(openaiRequest.Messages, types.ChatCompletionMessage{
									Role:    types.ChatMessageRoleSystem,
									Content: textStr,
								})
							}
						}
					}
				}
			}
		}
	}

	// 转换消息
	for _, msg := range r.claudeRequest.Messages {

		openaiMsg := types.ChatCompletionMessage{
			Role: msg.Role,
		}

		// 处理消息内容
		switch content := msg.Content.(type) {
		case string:

			openaiMsg.Content = content
			openaiRequest.Messages = append(openaiRequest.Messages, openaiMsg)
		case []interface{}:
			// 处理复杂内容
			if msg.Role == "user" {
				// 用户消息：处理 tool_result, text 和 image
				toolParts := make([]map[string]interface{}, 0)
				textParts := make([]map[string]interface{}, 0)
				imageParts := make([]map[string]interface{}, 0)

				for _, part := range content {
					if partMap, ok := part.(map[string]interface{}); ok {
						partType, _ := partMap["type"].(string)

						switch partType {
						case "tool_result":
							if _, exists := partMap["tool_use_id"].(string); exists {
								toolParts = append(toolParts, partMap)
							}
						case "text":
							if _, exists := partMap["text"].(string); exists {
								textParts = append(textParts, partMap)
							}
						case "image":
							// Claude 图片格式: {type: "image", source: {type: "base64", media_type, data}}
							if source, exists := partMap["source"].(map[string]interface{}); exists {
								if sourceType, _ := source["type"].(string); sourceType == "base64" {
									imageParts = append(imageParts, partMap)
								}
							}
						}
					}
				}

				// 处理 tool_result 部分
				for _, tool := range toolParts {
					toolContent := ""
					if resultContent, exists := tool["content"]; exists {
						if contentStr, ok := resultContent.(string); ok {
							toolContent = contentStr
						} else {
							contentBytes, _ := json.Marshal(resultContent)
							toolContent = string(contentBytes)
						}
					}

					toolCallID := ""
					if id, ok := tool["tool_use_id"].(string); ok {
						toolCallID = id
					}

					toolResultMsg := types.ChatCompletionMessage{
						Role:       types.ChatMessageRoleTool,
						Content:    toolContent,
						ToolCallID: toolCallID,
					}
					openaiRequest.Messages = append(openaiRequest.Messages, toolResultMsg)
				}

				// 处理 text 和 image 部分 - 合并到同一个消息中
				contentParts := make([]types.ChatMessagePart, 0)

				// 添加文本部分
				for _, textPart := range textParts {
					if text, ok := textPart["text"].(string); ok && text != "" {
						contentParts = append(contentParts, types.ChatMessagePart{
							Type: "text",
							Text: text,
						})
					}
				}

				// 添加图片部分 - 转换为 OpenAI 的 image_url 格式
				for _, imagePart := range imageParts {
					if source, exists := imagePart["source"].(map[string]interface{}); exists {
						mediaType, _ := source["media_type"].(string)
						data, _ := source["data"].(string)
						if mediaType == "" {
							mediaType = "image/png"
						}
						if data != "" {
							// 构建 data URL: data:image/png;base64,xxxxx
							dataURL := fmt.Sprintf("data:%s;base64,%s", mediaType, data)
							contentParts = append(contentParts, types.ChatMessagePart{
								Type: "image_url",
								ImageURL: &types.ChatMessageImageURL{
									URL: dataURL,
								},
							})
						}
					}
				}

				// 只有当有有效内容时才创建消息
				if len(contentParts) > 0 {
					userMsg := types.ChatCompletionMessage{
						Role:    types.ChatMessageRoleUser,
						Content: contentParts,
					}
					openaiRequest.Messages = append(openaiRequest.Messages, userMsg)
				}

			} else if msg.Role == "assistant" {
				// 助手消息：分别处理 thinking, text 和 tool_use
				thinkingParts := make([]map[string]interface{}, 0)
				textParts := make([]map[string]interface{}, 0)
				toolCallParts := make([]map[string]interface{}, 0)

				for _, part := range content {
					if partMap, ok := part.(map[string]interface{}); ok {
						partType, _ := partMap["type"].(string)

						switch partType {
						case "thinking", "redacted_thinking":
							// thinking 块必须有 signature 才能转换
							if signature, exists := partMap["signature"].(string); exists && signature != "" {
								thinkingParts = append(thinkingParts, partMap)
							}
						case "text":
							if _, exists := partMap["text"].(string); exists {
								textParts = append(textParts, partMap)
							}
						case "tool_use":
							if _, exists := partMap["id"].(string); exists {
								toolCallParts = append(toolCallParts, partMap)
							}
						}
					}
				}

				// 创建包含所有内容的 assistant 消息
				contentParts := make([]types.ChatMessagePart, 0)

				// 处理 thinking 部分 - 使用 ChatMessagePart 携带 thinking 信息
				for _, thinkingPart := range thinkingParts {
					partType, _ := thinkingPart["type"].(string)
					signature, _ := thinkingPart["signature"].(string)
					thinkingText := ""

					// thinking 块的文本在 "thinking" 字段
					// redacted_thinking 块的文本可能在 "thinking" 或 "data" 字段
					if text, exists := thinkingPart["thinking"].(string); exists {
						thinkingText = text
					} else if data, exists := thinkingPart["data"].(string); exists {
						thinkingText = data
					}

					contentParts = append(contentParts, types.ChatMessagePart{
						Type:              partType, // "thinking" 或 "redacted_thinking"
						Thinking:          thinkingText,
						ThinkingSignature: signature,
					})
				}

				// 处理 text 部分
				for _, textPart := range textParts {
					if text, ok := textPart["text"].(string); ok && text != "" {
						contentParts = append(contentParts, types.ChatMessagePart{
							Type: "text",
							Text: text,
						})
					}
				}

				// 如果有 thinking 或 text 内容，创建消息
				if len(contentParts) > 0 {
					assistantMsg := types.ChatCompletionMessage{
						Role:    types.ChatMessageRoleAssistant,
						Content: contentParts,
					}
					openaiRequest.Messages = append(openaiRequest.Messages, assistantMsg)
				}

				// 处理 tool_use 部分 - 创建单独的助手消息，content 为 null
				if len(toolCallParts) > 0 {
					toolCalls := make([]*types.ChatCompletionToolCalls, 0)
					for _, toolPart := range toolCallParts {
						// 安全地获取工具调用信息
						var toolId, toolName string
						var input interface{}

						if id, exists := toolPart["id"]; exists && id != nil {
							if idStr, ok := id.(string); ok && idStr != "" {
								toolId = idStr
							}
						}
						if toolId == "" {
							toolId = fmt.Sprintf("call_%d", time.Now().UnixNano())
						}

						if name, exists := toolPart["name"]; exists && name != nil {
							if nameStr, ok := name.(string); ok && nameStr != "" {
								toolName = nameStr
							}
						}
						if toolName == "" {
							continue // 跳过没有名称的工具调用
						}

						if inputData, exists := toolPart["input"]; exists {
							input = inputData
						} else {
							input = map[string]interface{}{}
						}

						inputBytes, _ := json.Marshal(input)

						toolCall := &types.ChatCompletionToolCalls{
							Id:   toolId,
							Type: types.ChatMessageRoleFunction,
							Function: &types.ChatCompletionToolCallsFunction{
								Name:      toolName,
								Arguments: string(inputBytes),
							},
						}
						toolCalls = append(toolCalls, toolCall)
					}

					assistantMsg := types.ChatCompletionMessage{
						Role:      types.ChatMessageRoleAssistant,
						Content:   nil,
						ToolCalls: toolCalls,
					}
					openaiRequest.Messages = append(openaiRequest.Messages, assistantMsg)
				}
			}
			continue // 跳过默认的 append
		default:
			openaiRequest.Messages = append(openaiRequest.Messages, openaiMsg)
		}
	}

	// 处理工具定义
	if len(r.claudeRequest.Tools) > 0 {
		tools := make([]*types.ChatCompletionTool, 0)

		for _, tool := range r.claudeRequest.Tools {
			// 过滤掉 Claude 内置工具类型（反重力等渠道不支持）
			// 内置工具类型包括: computer_20241022, bash_20241022, text_editor_20241022 等
			if tool.Type != "" && tool.Type != "custom" {
				continue
			}

			// 确保有工具名称
			if tool.Name == "" {
				continue
			}

			var parameters interface{}
			if tool.InputSchema == nil {
				// 如果 InputSchema 为空，设置默认空 schema
				parameters = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
			} else {
				switch cleanMode {
				case schemaCleanFull:
					// 完全清理：移除不支持字段 + 转换 type 数组为 nullable
					parameters = r.cleanSchemaForDirectGemini(tool.InputSchema)
				case schemaCleanMetaOnly:
					// 仅清理元字段：移除 $schema 等，但保留 type 数组格式（兼容 Claude API）
					parameters = r.cleanSchemaMetaOnly(tool.InputSchema)
				default:
					// 不清理：直接使用原始的 InputSchema
					parameters = tool.InputSchema
				}
			}

			// input_schema → parameters
			openaiTool := &types.ChatCompletionTool{
				Type: "function",
				Function: types.ChatCompletionFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  parameters,
				},
			}
			tools = append(tools, openaiTool)
		}
		openaiRequest.Tools = tools

		// 处理工具选择
		if r.claudeRequest.ToolChoice != nil {
			openaiRequest.ToolChoice = r.claudeRequest.ToolChoice
		}
	}

	return openaiRequest, nil
}

// cleanSchemaForDirectGemini 专门为直接Gemini渠道清理schema
// 与VertexAI的清理逻辑分开，避免相互影响
func (r *relayClaudeOnly) cleanSchemaForDirectGemini(schema interface{}) interface{} {
	if schema == nil {
		return schema
	}

	// 创建深拷贝避免修改原始数据
	return r.deepCleanSchema(schema)
}

// cleanSchemaMetaOnly 仅清理 JSON Schema 元字段，保留 type 数组格式
func (r *relayClaudeOnly) cleanSchemaMetaOnly(schema interface{}) interface{} {
	if schema == nil {
		return schema
	}
	return r.deepCleanSchemaMetaOnly(schema)
}

// deepCleanSchemaMetaOnly 递归清理 schema 中 Antigravity 不支持的字段
func (r *relayClaudeOnly) deepCleanSchemaMetaOnly(obj interface{}) interface{} {
	switch v := obj.(type) {
	case map[string]interface{}:
		cleaned := make(map[string]interface{})
		for key, value := range v {
			if antigravityUnsupportedSchemaKeys[key] {
				continue
			}

			// 处理 type: ["string", "null"] 转换为 type: "string" + nullable: true
			// Antigravity 后端转发到 Gemini 时需要此转换
			if key == "type" {
				if typeArr, ok := value.([]interface{}); ok {
					hasNull := false
					var nonNullType string
					for _, t := range typeArr {
						if tStr, ok := t.(string); ok {
							if tStr == "null" {
								hasNull = true
							} else if nonNullType == "" {
								nonNullType = tStr
							}
						}
					}
					if nonNullType != "" {
						cleaned["type"] = nonNullType
					} else {
						cleaned["type"] = "string"
					}
					if hasNull {
						cleaned["nullable"] = true
					}
					continue
				}
			}

			cleaned[key] = r.deepCleanSchemaMetaOnly(value)
		}

		// 补充缺失的 type: "object"
		if _, hasProperties := cleaned["properties"]; hasProperties {
			if _, hasType := cleaned["type"]; !hasType {
				cleaned["type"] = "object"
			}
		}

		// 过滤 required 数组，移除 properties 中不存在的属性名
		// Gemini API 要求 required 中的属性必须在 properties 中定义
		if required, hasRequired := cleaned["required"]; hasRequired {
			if properties, hasProps := cleaned["properties"].(map[string]interface{}); hasProps {
				if requiredArr, ok := required.([]interface{}); ok {
					filteredRequired := make([]interface{}, 0)
					for _, reqItem := range requiredArr {
						if reqStr, ok := reqItem.(string); ok {
							if _, exists := properties[reqStr]; exists {
								filteredRequired = append(filteredRequired, reqItem)
							}
						}
					}
					if len(filteredRequired) > 0 {
						cleaned["required"] = filteredRequired
					} else {
						delete(cleaned, "required")
					}
				}
			} else {
				// 没有 properties 时，直接删除 required
				delete(cleaned, "required")
			}
		}

		return cleaned
	case []interface{}:
		cleaned := make([]interface{}, len(v))
		for i, item := range v {
			cleaned[i] = r.deepCleanSchemaMetaOnly(item)
		}
		return cleaned
	default:
		return obj
	}
}

// antigravityUnsupportedSchemaKeys Antigravity 渠道不支持的 JSON Schema 字段
var antigravityUnsupportedSchemaKeys = map[string]bool{
	"$schema":              true,
	"$id":                  true,
	"$ref":                 true,
	"$defs":                true,
	"definitions":          true,
	"title":                true,
	"example":              true,
	"examples":             true,
	"readOnly":             true,
	"writeOnly":            true,
	"default":              true,
	"exclusiveMaximum":     true,
	"exclusiveMinimum":     true,
	"oneOf":                true,
	"anyOf":                true,
	"allOf":                true,
	"const":                true,
	"additionalItems":      true,
	"contains":             true,
	"patternProperties":    true,
	"dependencies":         true,
	"propertyNames":        true,
	"if":                   true,
	"then":                 true,
	"else":                 true,
	"contentEncoding":      true,
	"contentMediaType":     true,
	"minLength":            true,
	"maxLength":            true,
	"minimum":              true,
	"maximum":              true,
	"minItems":             true,
	"maxItems":             true,
	"additionalProperties": true,
	"pattern":              true,
	"format":               true,
	"deprecated":           true,
}

// geminiUnsupportedSchemaKeys Gemini API 不支持的 JSON Schema 字段
var geminiUnsupportedSchemaKeys = map[string]bool{
	"$schema":              true,
	"$id":                  true,
	"$ref":                 true,
	"$defs":                true,
	"definitions":          true,
	"title":                true,
	"example":              true,
	"examples":             true,
	"readOnly":             true,
	"writeOnly":            true,
	"default":              true,
	"const":                true,
	"exclusiveMaximum":     true,
	"exclusiveMinimum":     true,
	"oneOf":                true,
	"anyOf":                true,
	"allOf":                true,
	"additionalItems":      true,
	"contains":             true,
	"additionalProperties": true,
	"patternProperties":    true,
	"dependencies":         true,
	"propertyNames":        true,
	"if":                   true,
	"then":                 true,
	"else":                 true,
	"contentEncoding":      true,
	"contentMediaType":     true,
}

// deepCleanSchema 递归清理schema中Gemini API不支持的字段
func (r *relayClaudeOnly) deepCleanSchema(obj interface{}) interface{} {
	switch v := obj.(type) {
	case map[string]interface{}:
		cleaned := make(map[string]interface{})
		for key, value := range v {
			if geminiUnsupportedSchemaKeys[key] {
				continue
			}

			// 处理 type: ["string", "null"] 转换为 type: "string" + nullable: true
			if key == "type" {
				if typeArr, ok := value.([]interface{}); ok {
					hasNull := false
					var nonNullType string
					for _, t := range typeArr {
						if tStr, ok := t.(string); ok {
							if tStr == "null" {
								hasNull = true
							} else if nonNullType == "" {
								nonNullType = tStr
							}
						}
					}
					if nonNullType != "" {
						cleaned["type"] = nonNullType
					} else {
						cleaned["type"] = "string"
					}
					if hasNull {
						cleaned["nullable"] = true
					}
					continue
				}
			}

			// 处理 format 字段：Gemini 只支持 STRING 类型的 "enum" 和 "date-time"
			if key == "format" {
				if formatStr, ok := value.(string); ok {
					if typeVal, exists := v["type"]; exists && typeVal == "string" {
						if formatStr == "enum" || formatStr == "date-time" {
							cleaned[key] = value
						}
						continue
					} else {
						cleaned[key] = r.deepCleanSchema(value)
						continue
					}
				}
			}

			cleaned[key] = r.deepCleanSchema(value)
		}

		// 补充缺失的 type: "object"
		if _, hasProperties := cleaned["properties"]; hasProperties {
			if _, hasType := cleaned["type"]; !hasType {
				cleaned["type"] = "object"
			}
		}

		// 过滤 required 数组，移除 properties 中不存在的属性名
		// Gemini API 要求 required 中的属性必须在 properties 中定义
		if required, hasRequired := cleaned["required"]; hasRequired {
			if properties, hasProps := cleaned["properties"].(map[string]interface{}); hasProps {
				if requiredArr, ok := required.([]interface{}); ok {
					filteredRequired := make([]interface{}, 0)
					for _, reqItem := range requiredArr {
						if reqStr, ok := reqItem.(string); ok {
							if _, exists := properties[reqStr]; exists {
								filteredRequired = append(filteredRequired, reqItem)
							}
						}
					}
					if len(filteredRequired) > 0 {
						cleaned["required"] = filteredRequired
					} else {
						delete(cleaned, "required")
					}
				}
			} else {
				// 没有 properties 时，直接删除 required
				delete(cleaned, "required")
			}
		}

		return cleaned
	case []interface{}:
		cleaned := make([]interface{}, len(v))
		for i, item := range v {
			cleaned[i] = r.deepCleanSchema(item)
		}
		return cleaned
	default:
		return obj
	}
}

// convertOpenAIResponseToClaude 将OpenAI响应转换为Claude格式
func (r *relayClaudeOnly) convertOpenAIResponseToClaude(openaiResponse *types.ChatCompletionResponse) *claude.ClaudeResponse {
	if openaiResponse == nil || len(openaiResponse.Choices) == 0 {
		return &claude.ClaudeResponse{
			Id:      "msg_" + openaiResponse.ID,
			Type:    "message",
			Role:    "assistant",
			Content: []claude.ResContent{},
			Model:   openaiResponse.Model,
		}
	}

	choice := openaiResponse.Choices[0]
	content := make([]claude.ResContent, 0)

	// 处理文本内容
	// 检查是否达到 max_tokens 限制
	if choice.FinishReason == "length" && (choice.Message.Content == nil || choice.Message.Content == "") {
		// 当达到 max_tokens 限制且内容为空时，添加一个默认消息
		content = append(content, claude.ResContent{
			Type: "text",
			Text: "[Response truncated due to token limit]",
		})
	} else {
		// 正常处理内容
		switch contentValue := choice.Message.Content.(type) {
		case string:
			if contentValue != "" {
				content = append(content, claude.ResContent{
					Type: "text",
					Text: contentValue,
				})
			}
		case []interface{}:
			// 处理复杂内容格式
			for _, part := range contentValue {
				if partMap, ok := part.(map[string]interface{}); ok {
					if partType, exists := partMap["type"].(string); exists && partType == "text" {
						if text, textExists := partMap["text"].(string); textExists && text != "" {
							content = append(content, claude.ResContent{
								Type: "text",
								Text: text,
							})
						}
					}
				}
			}
		case nil:
			// 内容为空，不添加任何内容
		default:
			// 尝试转换为字符串
			if str := fmt.Sprintf("%v", contentValue); str != "" && str != "<nil>" {
				content = append(content, claude.ResContent{
					Type: "text",
					Text: str,
				})
			}
		}
	}

	// 处理工具调用
	var toolCallTokens int
	if len(choice.Message.ToolCalls) > 0 {
		for _, toolCall := range choice.Message.ToolCalls {
			var input interface{}
			if toolCall.Function.Arguments != "" {
				json.Unmarshal([]byte(toolCall.Function.Arguments), &input)
			} else {
				input = map[string]interface{}{}
			}

			content = append(content, claude.ResContent{
				Type:  "tool_use",
				Id:    toolCall.Id,
				Name:  toolCall.Function.Name,
				Input: input,
			})

			// 计算工具调用的 tokens
			toolCallText := fmt.Sprintf("tool_use:%s:%s", toolCall.Function.Name, toolCall.Function.Arguments)
			toolCallTokens += common.CountTokenText(toolCallText, openaiResponse.Model)
		}
	}

	// 转换停止原因
	stopReason := convertFinishReason(choice.FinishReason)

	claudeResponse := &claude.ClaudeResponse{
		Id:           "msg_" + openaiResponse.ID,
		Type:         "message",
		Role:         "assistant",
		Content:      content,
		Model:        openaiResponse.Model,
		StopReason:   stopReason,
		StopSequence: "", // 添加缺失的字段
	}

	// 处理使用量信息
	if openaiResponse.Usage != nil {
		// 计算最终的输出 tokens
		finalOutputTokens := openaiResponse.Usage.CompletionTokens

		if finalOutputTokens == 0 {
			// 如果 OpenAI 返回的 completion_tokens 为 0，计算工具调用和文本内容的 tokens
			finalOutputTokens = toolCallTokens

			// 累加文本内容的 tokens
			if len(content) > 0 {
				var textContent strings.Builder
				for _, c := range content {
					if c.Type == "text" && c.Text != "" {
						textContent.WriteString(c.Text)
					}
				}
				if textContent.Len() > 0 {
					textTokens := common.CountTokenText(textContent.String(), openaiResponse.Model)
					finalOutputTokens += textTokens
				}
			}
		}

		claudeResponse.Usage = claude.Usage{
			InputTokens:  openaiResponse.Usage.PromptTokens,
			OutputTokens: finalOutputTokens,
		}

	}

	return claudeResponse
}

// isBackgroundTask 检测是否为背景任务（如话题分析）
// convertOpenAIStreamToClaude 将OpenAI流式响应转换为Claude格式
func (r *relayClaudeOnly) convertOpenAIStreamToClaude(stream requester.StreamReaderInterface[string]) int64 {

	r.setStreamHeaders()

	flusher, ok := r.c.Writer.(http.Flusher)
	if !ok {
		logger.SysError("Streaming unsupported")
		return 0
	}

	messageId := fmt.Sprintf("msg_%d", utils.GetTimestamp())
	model := r.modelName
	hasStarted := false
	hasTextContentStarted := false
	hasFinished := false
	contentChunks := 0
	toolCallChunks := 0
	isClosed := false
	isThinkingStarted := false
	contentIndex := 0
	processedInThisChunk := make(map[int]bool)

	// 工具调用状态管理 - 使用请求级别的局部变量，避免全局变量导致的内存泄漏
	toolCallStates := make(map[int]map[string]interface{}) // toolCallIndex -> toolCallInfo
	toolCallToContentIndex := make(map[int]int)            // toolCallIndex -> contentBlockIndex

	// 保存最后的 usage 信息，用于 EOF 时补发
	var lastUsage map[string]interface{}

	// 累积工具调用的 token 数（用于当上游不提供 usage 时的计算）
	toolCallStatesForTokens := make(map[int]map[string]string) // 用于记录工具调用状态以便最后计算 tokens

	safeClose := func() {
		if !isClosed {
			isClosed = true
		}
	}
	defer safeClose()

	var firstResponseTime int64
	isFirst := true

	dataChan, errChan := stream.Recv()

streamLoop:
	for {
		select {
		case rawLine := <-dataChan:
			if isClosed {
				break streamLoop
			}

			if isFirst {
				firstResponseTime = utils.GetTimestamp()
				isFirst = false
			}

			if !hasStarted && !isClosed && !hasFinished {
				hasStarted = true
				// 发送message_start事件（格式与demo完全一致）
				// 直接构造JSON字符串以确保字段顺序正确
				messageStartJSON := fmt.Sprintf(`{"type":"message_start","message":{"id":"%s","type":"message","role":"assistant","content":[],"model":"%s","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`, messageId, model)
				r.writeSSEEventRaw("message_start", messageStartJSON, &isClosed)
			}

			// 处理不同格式的流式数据
			var data string
			if strings.HasPrefix(rawLine, "data: ") {
				// SSE 格式: data: {...}
				data = strings.TrimPrefix(rawLine, "data: ")
				if data == "[DONE]" {
					break streamLoop
				}
			} else if strings.TrimSpace(rawLine) != "" && (strings.HasPrefix(rawLine, "{") || strings.HasPrefix(rawLine, ": OPENROUTER PROCESSING")) {
				// 直接 JSON 格式或处理标记
				if strings.HasPrefix(rawLine, ": OPENROUTER PROCESSING") {
					continue
				}
				data = rawLine
			} else {
				continue
			}

			var openaiChunk map[string]interface{}
			if err := json.Unmarshal([]byte(data), &openaiChunk); err != nil {
				continue
			}

			// 重置每个chunk的处理状态
			processedInThisChunk = make(map[int]bool)

			// 保存 usage 信息
			if usage, usageExists := openaiChunk["usage"].(map[string]interface{}); usageExists {
				lastUsage = usage
			}

			// 处理choices
			if choices, exists := openaiChunk["choices"].([]interface{}); exists && len(choices) > 0 {
				choice := choices[0].(map[string]interface{})

				// 处理delta内容
				if delta, exists := choice["delta"].(map[string]interface{}); exists {

					// 处理thinking内容
					if thinking, thinkingExists := delta["thinking"]; thinkingExists && !isClosed && !hasFinished {
						if thinkingMap, ok := thinking.(map[string]interface{}); ok {
							if !isThinkingStarted {
								contentBlockStart := map[string]interface{}{
									"type":  "content_block_start",
									"index": contentIndex,
									"content_block": map[string]interface{}{
										"type":     "thinking",
										"thinking": "",
									},
								}
								r.writeSSEEvent("content_block_start", contentBlockStart, &isClosed)
								flusher.Flush()
								isThinkingStarted = true
							}

							if signature, sigExists := thinkingMap["signature"]; sigExists {
								thinkingSignature := map[string]interface{}{
									"type":  "content_block_delta",
									"index": contentIndex,
									"delta": map[string]interface{}{
										"type":      "signature_delta",
										"signature": signature,
									},
								}
								r.writeSSEEvent("content_block_delta", thinkingSignature, &isClosed)
								flusher.Flush()

								contentBlockStop := map[string]interface{}{
									"type":  "content_block_stop",
									"index": contentIndex,
								}
								r.writeSSEEvent("content_block_stop", contentBlockStop, &isClosed)
								flusher.Flush()
								contentIndex++
							} else if content, contentExists := thinkingMap["content"]; contentExists {
								thinkingChunk := map[string]interface{}{
									"type":  "content_block_delta",
									"index": contentIndex,
									"delta": map[string]interface{}{
										"type":     "thinking_delta",
										"thinking": content,
									},
								}
								r.writeSSEEvent("content_block_delta", thinkingChunk, &isClosed)
								flusher.Flush()
							}
						}
					}
					// 处理文本内容
					if contentValue, contentExists := delta["content"]; contentExists && contentValue != nil && !isClosed && !hasFinished {
						if content, ok := contentValue.(string); ok {

							// 只有当内容不为空时才处理
							if content != "" {
								contentChunks++

								// 累积文本内容到 TextBuilder 用于 token 计算
								r.provider.GetUsage().TextBuilder.WriteString(content)

								if !hasTextContentStarted && !hasFinished {
									// 发送content_block_start事件（格式与demo一致）
									contentBlockStartJSON := fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"text","text":""}}`, contentIndex)
									r.writeSSEEventRaw("content_block_start", contentBlockStartJSON, &isClosed)
									hasTextContentStarted = true
								}

								// 发送content_block_delta事件（格式与demo一致）
								contentBytes, _ := json.Marshal(content)
								contentBlockDeltaJSON := fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"text_delta","text":%s}}`, contentIndex, string(contentBytes))
								r.writeSSEEventRaw("content_block_delta", contentBlockDeltaJSON, &isClosed)
							}
						}
					}

					// 处理工具调用
					if toolCalls, toolExists := delta["tool_calls"].([]interface{}); toolExists && !isClosed && !hasFinished {
						toolCallChunks++
						for _, toolCall := range toolCalls {
							if toolCallMap, ok := toolCall.(map[string]interface{}); ok {
								r.processToolCallDelta(toolCallMap, &contentIndex, flusher, processedInThisChunk, hasTextContentStarted, &isClosed, &hasFinished, toolCallStates, toolCallToContentIndex)

								// 累积工具调用信息（在流结束时统一计算 tokens）
								if function, funcExists := toolCallMap["function"].(map[string]interface{}); funcExists {
									toolCallIndex := 0 // 需要从 toolCallMap 中获取 index
									if idx, idxExists := toolCallMap["index"]; idxExists {
										if idxFloat, ok := idx.(float64); ok {
											toolCallIndex = int(idxFloat)
										} else if idxInt, ok := idx.(int); ok {
											toolCallIndex = idxInt
										}
									}

									// 确保索引不为负数
									if toolCallIndex < 0 {
										toolCallIndex = 0
									}

									if toolCallStatesForTokens[toolCallIndex] == nil {
										toolCallStatesForTokens[toolCallIndex] = map[string]string{
											"name":      "",
											"arguments": "",
										}
									}

									if name, nameExists := function["name"].(string); nameExists {
										toolCallStatesForTokens[toolCallIndex]["name"] = name
									}
									if args, argsExists := function["arguments"].(string); argsExists {
										toolCallStatesForTokens[toolCallIndex]["arguments"] += args
									}
								}
							}
						}
					}
				}

				// 处理finish_reason
				if finishReason, exists := choice["finish_reason"].(string); exists && finishReason != "" && !isClosed && !hasFinished {

					hasFinished = true

					// 检查是否有内容（用于调试，但不记录日志）
					if contentChunks == 0 && toolCallChunks == 0 {
						// 无内容的流响应，但这可能是正常情况（如背景任务）
					}

					// 发送content_block_stop事件 - 复刻JavaScript逻辑（格式与demo一致）
					if (hasTextContentStarted || toolCallChunks > 0) && !isClosed {
						contentBlockStopJSON := fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, contentIndex)
						r.writeSSEEventRaw("content_block_stop", contentBlockStopJSON, &isClosed)
					}

					// 转换停止原因
					claudeStopReason := "end_turn"
					switch finishReason {
					case "stop":
						claudeStopReason = "end_turn"
					case "length":
						claudeStopReason = "max_tokens"
					case "tool_calls":
						claudeStopReason = "tool_use"
					case "content_filter":
						claudeStopReason = "stop_sequence"
					}

					// 发送message_delta事件（格式与demo一致，必须包含usage字段）
					var messageDeltaJSON string
					if usage, usageExists := openaiChunk["usage"].(map[string]interface{}); usageExists {
						// 安全地获取token数量，防止类型断言失败
						inputTokens := 0
						outputTokens := 0
						if promptTokens, ok := usage["prompt_tokens"]; ok {
							if tokens, ok := promptTokens.(float64); ok {
								inputTokens = int(tokens)
							}
						}
						if completionTokens, ok := usage["completion_tokens"]; ok {
							if tokens, ok := completionTokens.(float64); ok {
								outputTokens = int(tokens)
							}
						}
						messageDeltaJSON = fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":"%s","stop_sequence":null},"usage":{"input_tokens":%d,"output_tokens":%d}}`, claudeStopReason, inputTokens, outputTokens)
					} else {
						// 如果没有usage信息，计算工具调用和文本内容的 tokens
						currentUsage := r.provider.GetUsage()

						// 计算工具调用 tokens（在流结束时统一计算）
						estimatedOutputTokens := 0
						for _, toolCallState := range toolCallStatesForTokens {
							if name, nameExists := toolCallState["name"]; nameExists {
								args := toolCallState["arguments"]
								if name != "" {
									toolCallText := fmt.Sprintf("tool_use:%s:%s", name, args)
									tokens := common.CountTokenText(toolCallText, r.modelName)
									estimatedOutputTokens += tokens
								}
							}
						}

						// 累加文本内容的 tokens
						if currentUsage.TextBuilder.Len() > 0 {
							textTokens := common.CountTokenText(currentUsage.TextBuilder.String(), r.modelName)
							estimatedOutputTokens += textTokens
						}

						// 更新 Provider 的 Usage
						currentUsage.CompletionTokens = estimatedOutputTokens
						currentUsage.TotalTokens = currentUsage.PromptTokens + estimatedOutputTokens

						messageDeltaJSON = fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":"%s","stop_sequence":null},"usage":{"input_tokens":%d,"output_tokens":%d}}`, claudeStopReason, currentUsage.PromptTokens, estimatedOutputTokens)
					}

					if !isClosed {
						r.writeSSEEventRaw("message_delta", messageDeltaJSON, &isClosed)
					}

					// 发送message_stop事件（格式与demo一致）
					if !isClosed {
						messageStopJSON := `{"type":"message_stop"}`
						r.writeSSEEventRaw("message_stop", messageStopJSON, &isClosed)
					}

					// 确保流正确结束
					safeClose()
					break streamLoop
				}
			}
		case err := <-errChan:
			if err != nil {
				if err.Error() == "EOF" {
					// 正常结束 - 确保发送完整的结束序列
					if !hasFinished && !isClosed {
						// 如果还没有发送结束事件，补发
						if hasTextContentStarted || toolCallChunks > 0 {
							contentBlockStopJSON := fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, contentIndex)
							r.writeSSEEventRaw("content_block_stop", contentBlockStopJSON, &isClosed)
						}

						// 使用保存的 usage 信息，如果没有则使用默认值
						var messageDeltaJSON string
						if lastUsage != nil {
							inputTokens := 0
							outputTokens := 0
							if promptTokens, ok := lastUsage["prompt_tokens"]; ok {
								if tokens, ok := promptTokens.(float64); ok {
									inputTokens = int(tokens)
								}
							}
							if completionTokens, ok := lastUsage["completion_tokens"]; ok {
								if tokens, ok := completionTokens.(float64); ok {
									outputTokens = int(tokens)
								}
							}
							messageDeltaJSON = fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":%d,"output_tokens":%d}}`, inputTokens, outputTokens)
						} else {
							currentUsage := r.provider.GetUsage()

							// 计算工具调用 tokens（在流结束时统一计算）
							estimatedOutputTokens := 0
							for _, toolCallState := range toolCallStatesForTokens {
								if name, nameExists := toolCallState["name"]; nameExists {
									args := toolCallState["arguments"]
									if name != "" {
										toolCallText := fmt.Sprintf("tool_use:%s:%s", name, args)
										estimatedOutputTokens += common.CountTokenText(toolCallText, r.modelName)
									}
								}
							}

							// 累加文本内容的 tokens
							if currentUsage.TextBuilder.Len() > 0 {
								textTokens := common.CountTokenText(currentUsage.TextBuilder.String(), r.modelName)
								estimatedOutputTokens += textTokens
							}

							// 更新 Provider 的 Usage
							currentUsage.CompletionTokens = estimatedOutputTokens
							currentUsage.TotalTokens = currentUsage.PromptTokens + estimatedOutputTokens

							messageDeltaJSON = fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":%d,"output_tokens":%d}}`, currentUsage.PromptTokens, estimatedOutputTokens)
						}
						r.writeSSEEventRaw("message_delta", messageDeltaJSON, &isClosed)

						messageStopJSON := `{"type":"message_stop"}`
						r.writeSSEEventRaw("message_stop", messageStopJSON, &isClosed)
					}

					safeClose()
					break streamLoop
				}
				logger.SysError("Stream read error: " + err.Error())
				safeClose()
			}
			break streamLoop
		}
	}

	return firstResponseTime
}

// processToolCallDelta 处理工具调用的增量数据
// toolCallStates和toolCallToContentIndex作为参数传入，避免全局变量导致的内存泄漏和并发问题
func (r *relayClaudeOnly) processToolCallDelta(toolCall map[string]interface{}, contentIndex *int, flusher http.Flusher, processedInThisChunk map[int]bool, hasTextContentStarted bool, isClosed *bool, hasFinished *bool, toolCallStates map[int]map[string]interface{}, toolCallToContentIndex map[int]int) {
	// 获取工具调用索引
	toolCallIndex := 0
	if index, exists := toolCall["index"].(float64); exists {
		toolCallIndex = int(index)
	}

	// 防止重复处理
	if processedInThisChunk[toolCallIndex] {
		return
	}
	processedInThisChunk[toolCallIndex] = true

	if function, exists := toolCall["function"].(map[string]interface{}); exists {
		// 检查是否是未知索引（新的工具调用）
		isUnknownIndex := false
		if _, exists := toolCallToContentIndex[toolCallIndex]; !exists {
			isUnknownIndex = true
		}

		if isUnknownIndex {
			// 计算新的内容块索引
			newContentBlockIndex := len(toolCallToContentIndex)
			if hasTextContentStarted {
				newContentBlockIndex = len(toolCallToContentIndex) + 1
			}

			// 如果不是第一个内容块，先发送前一个的 stop 事件
			if newContentBlockIndex != 0 {
				contentBlockStop := map[string]interface{}{
					"type":  "content_block_stop",
					"index": *contentIndex,
				}
				r.writeSSEEvent("content_block_stop", contentBlockStop, isClosed)
				flusher.Flush()
				*contentIndex++
			}

			// 设置索引映射
			toolCallToContentIndex[toolCallIndex] = newContentBlockIndex

			// 生成工具调用ID和名称 - 支持临时ID
			toolCallId := ""
			toolCallName := ""

			if id, idExists := toolCall["id"].(string); idExists && id != "" {
				toolCallId = id
			} else {
				toolCallId = fmt.Sprintf("call_%d_%d", utils.GetTimestamp(), toolCallIndex)
			}

			if name, nameExists := function["name"].(string); nameExists && name != "" {
				toolCallName = name
			} else {
				toolCallName = fmt.Sprintf("tool_%d", toolCallIndex)
			}

			// 发送 content_block_start 事件
			contentBlockStart := map[string]interface{}{
				"type":  "content_block_start",
				"index": *contentIndex,
				"content_block": map[string]interface{}{
					"type":  "tool_use",
					"id":    toolCallId,
					"name":  toolCallName,
					"input": map[string]interface{}{},
				},
			}
			r.writeSSEEvent("content_block_start", contentBlockStart, isClosed)
			flusher.Flush()

			// 保存工具调用状态
			toolCallStates[toolCallIndex] = map[string]interface{}{
				"id":                toolCallId,
				"name":              toolCallName,
				"arguments":         "",
				"contentBlockIndex": newContentBlockIndex,
			}
		} else if toolCall["id"] != nil && function["name"] != nil {
			// 处理ID更新
			if existingToolCall, exists := toolCallStates[toolCallIndex]; exists {
				existingId := existingToolCall["id"].(string)
				existingName := existingToolCall["name"].(string)

				// 检查是否是临时ID
				wasTemporary := strings.HasPrefix(existingId, "call_") && strings.HasPrefix(existingName, "tool_")

				if wasTemporary {
					if newId, ok := toolCall["id"].(string); ok && newId != "" {
						existingToolCall["id"] = newId
					}
					if newName, ok := function["name"].(string); ok && newName != "" {
						existingToolCall["name"] = newName
					}
				}
			}
		}

		// 处理参数增量
		if arguments, argsExists := function["arguments"].(string); argsExists && arguments != "" && !*isClosed && !*hasFinished {
			_, exists := toolCallToContentIndex[toolCallIndex]
			if !exists {
				return
			}

			// 更新累积的参数
			if currentToolCall, exists := toolCallStates[toolCallIndex]; exists {
				currentArgs := currentToolCall["arguments"].(string)
				currentToolCall["arguments"] = currentArgs + arguments

				// JSON 验证
				trimmedArgs := strings.TrimSpace(currentToolCall["arguments"].(string))
				if strings.HasPrefix(trimmedArgs, "{") && strings.HasSuffix(trimmedArgs, "}") {
					var parsedParams interface{}
					json.Unmarshal([]byte(trimmedArgs), &parsedParams)
				}
			}

			// 发送 input_json_delta 事件
			contentBlockDelta := map[string]interface{}{
				"type":  "content_block_delta",
				"index": *contentIndex,
				"delta": map[string]interface{}{
					"type":         "input_json_delta",
					"partial_json": arguments,
				},
			}
			r.writeSSEEvent("content_block_delta", contentBlockDelta, isClosed)
			flusher.Flush()
		}
	}
}

// writeSSEEvent 统一的SSE事件写入函数，支持结构化数据和原始JSON字符串
func (r *relayClaudeOnly) writeSSEEvent(eventType string, data interface{}, isClosed *bool) {
	r.writeSSEEventInternal(eventType, data, isClosed, false)
}

// writeSSEEventRaw 直接发送原始JSON字符串
func (r *relayClaudeOnly) writeSSEEventRaw(eventType, jsonData string, isClosed *bool) {
	r.writeSSEEventInternal(eventType, jsonData, isClosed, true)
}

// writeSSEEventSafe 安全的SSE事件写入（不需要isClosed参数）
func (r *relayClaudeOnly) writeSSEEventSafe(eventType string, data interface{}) {
	var closed bool
	r.writeSSEEventInternal(eventType, data, &closed, false)
}

// writeSSEEventInternal 内部统一的SSE事件写入实现
func (r *relayClaudeOnly) writeSSEEventInternal(eventType string, data interface{}, isClosed *bool, isRawJSON bool) {
	if *isClosed {
		return
	}

	defer func() {
		if r := recover(); r != nil {
			*isClosed = true
		}
	}()

	// 检查客户端连接状态
	select {
	case <-r.c.Request.Context().Done():
		// 客户端已断开连接
		*isClosed = true
		return
	default:
		// 连接正常，继续处理
	}

	var jsonData string
	if isRawJSON {
		jsonData = data.(string)
	} else {
		jsonBytes, err := json.Marshal(data)
		if err != nil {
			*isClosed = true
			return
		}
		jsonData = string(jsonBytes)
	}

	_, err := fmt.Fprintf(r.c.Writer, "event: %s\ndata: %s\n\n", eventType, jsonData)
	if err != nil {
		// 检测常见的连接关闭错误
		if strings.Contains(err.Error(), "broken pipe") ||
			strings.Contains(err.Error(), "connection reset") ||
			strings.Contains(err.Error(), "write: connection reset by peer") ||
			strings.Contains(err.Error(), "client disconnected") {
			*isClosed = true
		}
		return
	}

	// 立即flush数据，确保客户端能及时收到
	if flusher, ok := r.c.Writer.(http.Flusher); ok {
		flusher.Flush()
	}
}

// handleBackgroundTaskInSetRequest 在setRequest阶段处理背景任务

// sendVertexAIGeminiWithClaudeFormat handles VertexAI Gemini model Claude format requests
// using new transformer architecture: Claude format -> unified format -> Gemini format -> VertexAI Gemini API -> Gemini response -> unified format -> Claude format
func (r *relayClaudeOnly) sendVertexAIGeminiWithClaudeFormat() (err *types.OpenAIErrorWithStatusCode, done bool) {

	// 创建转换管理器
	transformManager := transformer.CreateClaudeToVertexGeminiManager()

	// 1. 使用转换管理器处理请求转换（暂时不使用，保持兼容性）
	// 注释掉：请求转换未实际使用，只是做错误检查但丢弃返回值
	// _, transformErr := transformManager.ProcessRequest(r.claudeRequest)
	// if transformErr != nil {
	// 	return common.ErrorWrapper(transformErr, "request_transform_failed", http.StatusInternalServerError), true
	// }

	// 内容审查
	if safetyErr := r.performContentSafety(); safetyErr != nil {
		err = safetyErr
		done = true
		return
	}

	// 2. 直接调用 VertexAI API（暂时使用现有的 provider，后续可以优化为直接 HTTP 调用）
	// 为了保持兼容性，我们先转换为 OpenAI 格式，然后使用现有的 provider
	// VertexAI 使用不清理schema的转换方法，因为后续会有专门的 CleanGeminiRequestBytes 处理
	openaiRequest, convertErr := r.convertClaudeToOpenAIForVertexAI()
	if convertErr != nil {
		return convertErr, true
	}

	openaiRequest.Model = r.modelName

	// 获取 VertexAI provider
	vertexaiProvider, ok := r.provider.(*vertexai.VertexAIProvider)
	if !ok {
		err = common.StringErrorWrapperLocal("provider is not VertexAI provider", "channel_error", http.StatusServiceUnavailable)
		done = true
		return
	}

	if r.claudeRequest.Stream {
		// 处理流式响应
		var stream requester.StreamReaderInterface[string]
		stream, err = vertexaiProvider.CreateChatCompletionStream(openaiRequest)
		if err != nil {
			return err, true
		}

		if r.heartbeat != nil {
			r.heartbeat.Stop()
		}

		// use new transformer to handle stream response
		firstResponseTime := r.convertOpenAIStreamToClaudeWithTransformer(stream, transformManager)
		r.SetFirstResponseTime(time.Unix(firstResponseTime, 0))
	} else {
		// 处理非流式响应
		var openaiResponse *types.ChatCompletionResponse
		openaiResponse, err = vertexaiProvider.CreateChatCompletion(openaiRequest)
		if err != nil {
			return err, true
		}

		if r.heartbeat != nil {
			r.heartbeat.Stop()
		}

		// use new transformer to handle non-stream response
		claudeResponse := r.convertOpenAIResponseToClaudeWithTransformer(openaiResponse, transformManager)
		openErr := responseJsonClient(r.c, claudeResponse)

		if openErr != nil {
			logger.SysLog(fmt.Sprintf("响应发送错误: %v", openErr))
		}
	}

	return err, false
}

// convertOpenAIStreamToClaudeWithTransformer uses transformer to handle stream response
func (r *relayClaudeOnly) convertOpenAIStreamToClaudeWithTransformer(stream requester.StreamReaderInterface[string], transformManager *transformer.TransformManager) int64 {

	// 设置响应头
	r.setStreamHeaders()
	r.c.Header("Access-Control-Allow-Origin", "*")
	r.c.Header("Access-Control-Allow-Headers", "Content-Type")

	flusher, ok := r.c.Writer.(http.Flusher)
	if !ok {
		logger.SysLog("ResponseWriter 不支持 Flusher")
		return time.Now().Unix()
	}

	// 创建一个模拟的 HTTP 响应来包装流数据
	pr, pw := io.Pipe()

	// 确保在函数退出时关闭PipeReader，防止goroutine泄漏
	defer pr.Close()

	// 在 goroutine 中将流数据写入管道
	go func() {
		defer pw.Close()

		dataChan, errChan := stream.Recv()

		for {
			select {
			case rawLine, ok := <-dataChan:
				if !ok {
					return
				}
				// 写入原始的 OpenAI 流数据
				fmt.Fprintf(pw, "data: %s\n\n", rawLine)

			case err, ok := <-errChan:
				if !ok {
					return
				}
				if err != nil {
					if err == io.EOF {
						return
					}
					logger.SysLog(fmt.Sprintf("流接收错误: %v", err))
					return
				}
			}
		}
	}()

	// 创建模拟的 HTTP 响应
	mockResponse := &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       pr,
	}
	mockResponse.Header.Set("Content-Type", "text/event-stream")

	// use transform manager to handle stream response
	claudeStream, err := transformManager.ProcessStreamResponse(mockResponse)
	if err != nil {
		// pr会通过defer自动关闭，这会导致pw.Close()被触发，goroutine正常退出
		return time.Now().Unix()
	}

	// 将转换后的 Claude 流式响应直接写入客户端
	defer claudeStream.Body.Close()

	scanner := bufio.NewScanner(claudeStream.Body)
	firstResponseTime := time.Now().Unix()

	for scanner.Scan() {
		line := scanner.Text()

		// forward Claude format SSE events directly
		fmt.Fprintf(r.c.Writer, "%s\n", line)
		flusher.Flush()
	}

	if err := scanner.Err(); err != nil {
		// log scan error if needed
	}

	return firstResponseTime
}

// convertOpenAIResponseToClaudeWithTransformer uses transformer to handle non-stream response
func (r *relayClaudeOnly) convertOpenAIResponseToClaudeWithTransformer(openaiResponse *types.ChatCompletionResponse, transformManager *transformer.TransformManager) *claude.ClaudeResponse {

	// 创建一个模拟的 HTTP 响应
	responseBytes, _ := json.Marshal(openaiResponse)
	mockResponse := &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(responseBytes))),
	}

	// use transform manager to handle response
	claudeResponseInterface, err := transformManager.ProcessResponse(mockResponse)
	if err != nil {
		// return error response
		return &claude.ClaudeResponse{
			Id:         "error",
			Type:       "message",
			Role:       "assistant",
			Content:    []claude.ResContent{{Type: "text", Text: "Response conversion error"}},
			Model:      r.modelName,
			StopReason: "error",
		}
	}

	claudeResponse, ok := claudeResponseInterface.(*claude.ClaudeResponse)
	if !ok {
		return &claude.ClaudeResponse{
			Id:         "error",
			Type:       "message",
			Role:       "assistant",
			Content:    []claude.ResContent{{Type: "text", Text: "Response format conversion error"}},
			Model:      r.modelName,
			StopReason: "error",
		}
	}

	return claudeResponse
}

// sendGeminiWithClaudeFormat handles Gemini channel Claude format requests
// using transformer architecture: Claude format -> OpenAI format -> Gemini API -> OpenAI response -> Claude format
func (r *relayClaudeOnly) sendGeminiWithClaudeFormat() (err *types.OpenAIErrorWithStatusCode, done bool) {

	// 将Claude请求转换为OpenAI格式
	openaiRequest, err := r.convertClaudeToOpenAI()
	if err != nil {
		return err, true
	}

	// 内容审查
	if safetyErr := r.performContentSafety(); safetyErr != nil {
		err = safetyErr
		done = true
		return
	}

	openaiRequest.Model = r.modelName

	// 获取 Gemini provider
	geminiProvider, ok := r.provider.(*gemini.GeminiProvider)
	if !ok {
		err = common.StringErrorWrapperLocal("provider is not Gemini provider", "channel_error", http.StatusServiceUnavailable)
		done = true
		return
	}

	if r.claudeRequest.Stream {
		// 处理流式响应 - 使用改进的手动转换逻辑，保持计费逻辑不变
		var stream requester.StreamReaderInterface[string]
		stream, err = geminiProvider.CreateChatCompletionStream(openaiRequest)
		if err != nil {
			return err, true
		}

		if r.heartbeat != nil {
			r.heartbeat.Stop()
		}

		// 使用与 VertexAI 相同的 Transformer 架构，彻底解决重复响应问题
		transformManager := transformer.CreateClaudeToVertexGeminiManager()
		firstResponseTime := r.convertOpenAIStreamToClaudeWithTransformer(stream, transformManager)
		r.SetFirstResponseTime(time.Unix(firstResponseTime, 0))
	} else {
		// 处理非流式响应 - 保持原有逻辑，确保计费正确
		var openaiResponse *types.ChatCompletionResponse
		openaiResponse, err = geminiProvider.CreateChatCompletion(openaiRequest)
		if err != nil {
			return err, true
		}

		if r.heartbeat != nil {
			r.heartbeat.Stop()
		}

		// 转换OpenAI响应为Claude格式 - 保持原有计费逻辑
		claudeResponse := r.convertOpenAIResponseToClaude(openaiResponse)
		openErr := responseJsonClient(r.c, claudeResponse)

		if openErr != nil {
			// 对于响应发送错误（如客户端断开连接），不应该触发重试
			// 这种错误是客户端问题，不是服务端问题

			// 不设置 err，避免触发重试机制
		}
	}

	return err, false
}

// sendAntigravityWithClaudeFormat handles Antigravity channel Claude format requests
// Claude format -> OpenAI format -> Antigravity API -> OpenAI response -> Claude format
func (r *relayClaudeOnly) sendAntigravityWithClaudeFormat() (err *types.OpenAIErrorWithStatusCode, done bool) {

	// 使用 Antigravity 专用的 schema 清理模式
	openaiRequest, err := r.convertClaudeToOpenAIForAntigravity()
	if err != nil {
		return err, true
	}

	// 内容审查
	if safetyErr := r.performContentSafety(); safetyErr != nil {
		err = safetyErr
		done = true
		return
	}

	openaiRequest.Model = r.modelName

	// 获取 Antigravity provider
	antigravityProvider, ok := r.provider.(*antigravity.AntigravityProvider)
	if !ok {
		err = common.StringErrorWrapperLocal("provider is not Antigravity provider", "channel_error", http.StatusServiceUnavailable)
		done = true
		return
	}

	// Claude 模型特殊处理：检查是否启用思考模式
	enableThinking := r.isClaudeThinkingEnabled()

	// 如果是 Claude 思考模型，需要删除 topP 参数
	if enableThinking && model_utils.ContainsCaseInsensitive(r.modelName, "claude") {
		openaiRequest.TopP = nil // 设置为 nil 表示不使用
	}

	if r.claudeRequest.Stream {
		// 处理流式响应
		var stream requester.StreamReaderInterface[string]
		stream, err = antigravityProvider.CreateChatCompletionStream(openaiRequest)
		if err != nil {
			return err, true
		}

		if r.heartbeat != nil {
			r.heartbeat.Stop()
		}

		// 使用 Transformer 架构处理流式响应
		transformManager := transformer.CreateClaudeToVertexGeminiManager()
		firstResponseTime := r.convertOpenAIStreamToClaudeWithTransformer(stream, transformManager)
		r.SetFirstResponseTime(time.Unix(firstResponseTime, 0))
	} else {
		// 处理非流式响应
		var openaiResponse *types.ChatCompletionResponse
		openaiResponse, err = antigravityProvider.CreateChatCompletion(openaiRequest)
		if err != nil {
			return err, true
		}

		if r.heartbeat != nil {
			r.heartbeat.Stop()
		}

		// 转换OpenAI响应为Claude格式
		claudeResponse := r.convertOpenAIResponseToClaude(openaiResponse)
		openErr := responseJsonClient(r.c, claudeResponse)

		if openErr != nil {
			// 对于响应发送错误（如客户端断开连接），不应该触发重试
		}
	}

	return err, false
}

// isClaudeThinkingEnabled 检查是否启用了 Claude 思考模式
func (r *relayClaudeOnly) isClaudeThinkingEnabled() bool {
	if r.claudeRequest == nil || r.claudeRequest.Thinking == nil {
		return false
	}

	// 检查 thinking 参数的 type 是否为 "enabled"
	return r.claudeRequest.Thinking.Type == "enabled"
}

// applyClaudeThinkingConstraints 应用 Claude Thinking 约束校验
// 1. tool_choice 强制工具使用时禁用 thinking（Anthropic API 限制）
// 2. 确保 max_tokens > thinking.budget_tokens
func (r *relayClaudeOnly) applyClaudeThinkingConstraints() {
	if r.claudeRequest == nil || r.claudeRequest.Thinking == nil {
		return
	}

	// 约束1: tool_choice="any"/"tool" 与 thinking 互斥
	if r.claudeRequest.ToolChoice != nil {
		toolChoiceType := r.claudeRequest.ToolChoice.Type
		if toolChoiceType == "any" || toolChoiceType == "tool" {
			r.claudeRequest.Thinking = nil
			return
		}
	}

	// 约束2: 确保 max_tokens > thinking.budget_tokens
	if r.claudeRequest.Thinking.Type != "enabled" {
		return
	}

	budgetTokens := r.claudeRequest.Thinking.BudgetTokens
	if budgetTokens <= 0 {
		return
	}

	const fallbackBuffer = 4000
	requiredMaxTokens := budgetTokens + fallbackBuffer
	if r.claudeRequest.MaxTokens < requiredMaxTokens {
		r.claudeRequest.MaxTokens = requiredMaxTokens
	}
}
