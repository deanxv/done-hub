package transformer

import (
	"bufio"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// VertexToolCallState represents the state of a tool call in VertexGemini transformer
type VertexToolCallState struct {
	ID              string
	Name            string
	Arguments       string
	Sent            bool  // whether the complete tool call has been sent
	LastSentArgsLen int   // length of arguments when last sent, used for calculating increments
	MergedIndexes   []int // indexes that have been merged to avoid duplicate processing of same ID
}

// VertexGeminiTransformer handles format conversion for VertexAI Gemini
type VertexGeminiTransformer struct {
	name          string
	toolCalls     map[int]*VertexToolCallState    // manage tool call state by index
	toolCallsByID map[string]*VertexToolCallState // manage tool call state by ID for merging same ID calls
	isClosed      bool                            // whether the stream is closed
}

// NewVertexGeminiTransformer creates a new VertexGemini transformer
func NewVertexGeminiTransformer() *VertexGeminiTransformer {
	return &VertexGeminiTransformer{
		name:          "vertex-gemini",
		toolCalls:     make(map[int]*VertexToolCallState),
		toolCallsByID: make(map[string]*VertexToolCallState),
		isClosed:      false,
	}
}

// GetName returns transformer name
func (t *VertexGeminiTransformer) GetName() string {
	return t.name
}

// TransformRequestOut converts VertexGemini response to unified format (not implemented)
func (t *VertexGeminiTransformer) TransformRequestOut(request interface{}) (*UnifiedChatRequest, error) {
	return nil, fmt.Errorf("VertexGemini transformer does not support TransformRequestOut")
}

// TransformRequestIn converts unified format to VertexGemini request format
func (t *VertexGeminiTransformer) TransformRequestIn(request *UnifiedChatRequest) (interface{}, error) {

	geminiRequest := map[string]interface{}{
		"generationConfig": map[string]interface{}{},
		"safetySettings": []map[string]interface{}{
			{"category": "HARM_CATEGORY_HARASSMENT", "threshold": "BLOCK_NONE"},
			{"category": "HARM_CATEGORY_HATE_SPEECH", "threshold": "BLOCK_NONE"},
			{"category": "HARM_CATEGORY_SEXUALLY_EXPLICIT", "threshold": "BLOCK_NONE"},
			{"category": "HARM_CATEGORY_DANGEROUS_CONTENT", "threshold": "BLOCK_NONE"},
			{"category": "HARM_CATEGORY_CIVIC_INTEGRITY", "threshold": "BLOCK_NONE"},
		},
	}

	// set generation config
	generationConfig := geminiRequest["generationConfig"].(map[string]interface{})
	if request.MaxTokens > 0 {
		generationConfig["maxOutputTokens"] = request.MaxTokens
	}
	if request.Temperature != nil {
		generationConfig["temperature"] = *request.Temperature
	}

	// convert messages
	var contents []map[string]interface{}
	for _, msg := range request.Messages {
		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}

		var parts []map[string]interface{}

		// 处理文本内容
		if msg.Content != nil {
			if contentStr, ok := msg.Content.(string); ok && contentStr != "" {
				parts = append(parts, map[string]interface{}{
					"text": contentStr,
				})
			} else if contentArray, ok := msg.Content.([]interface{}); ok {
				for _, item := range contentArray {
					if itemMap, ok := item.(map[string]interface{}); ok {
						if itemMap["type"] == "text" && itemMap["text"] != nil {
							parts = append(parts, map[string]interface{}{
								"text": itemMap["text"],
							})
						}
					}
				}
			}
		}

		// 处理工具调用
		if len(msg.ToolCalls) > 0 {
			for _, toolCall := range msg.ToolCalls {
				// 确保工具名称不为空
				toolName := toolCall.Function.Name
				if toolName == "" {
					continue
				}

				var args map[string]interface{}
				if toolCall.Function.Arguments != "" {
					if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
						args = map[string]interface{}{}
					}
				} else {
					args = map[string]interface{}{}
				}

				parts = append(parts, map[string]interface{}{
					"functionCall": map[string]interface{}{
						"name": toolName,
						"args": args,
					},
				})
			}
		}

		if len(parts) > 0 {
			contents = append(contents, map[string]interface{}{
				"role":  role,
				"parts": parts,
			})
		}
	}

	geminiRequest["contents"] = contents

	// 转换工具
	if len(request.Tools) > 0 {
		var functionDeclarations []map[string]interface{}
		for _, tool := range request.Tools {
			functionDeclarations = append(functionDeclarations, map[string]interface{}{
				"name":        tool.Function.Name,
				"description": tool.Function.Description,
				"parameters":  tool.Function.Parameters,
			})
		}

		geminiRequest["tools"] = []map[string]interface{}{
			{
				"functionDeclarations": functionDeclarations,
			},
		}
	}

	return geminiRequest, nil
}

// TransformResponseOut 将 VertexGemini 响应转换为统一格式
func (t *VertexGeminiTransformer) TransformResponseOut(response *http.Response) (*UnifiedChatResponse, error) {

	if response.Body == nil {
		return nil, fmt.Errorf("response body is nil")
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}
	response.Body.Close()

	var responseData map[string]interface{}
	if err := json.Unmarshal(body, &responseData); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	// 检查是否是 OpenAI 格式（已经被 VertexAI provider 转换过）
	if _, hasChoices := responseData["choices"].([]interface{}); hasChoices {
		return t.transformOpenAIResponse(responseData)
	}

	// 检查是否是原始 Gemini 格式
	if _, hasCandidates := responseData["candidates"].([]interface{}); hasCandidates {
		return t.transformGeminiResponse(responseData)
	}

	return nil, fmt.Errorf("unknown response format: neither OpenAI nor Gemini format")
}

// transformOpenAIResponse 转换 OpenAI 格式响应
func (t *VertexGeminiTransformer) transformOpenAIResponse(responseData map[string]interface{}) (*UnifiedChatResponse, error) {
	choices, _ := responseData["choices"].([]interface{})
	if len(choices) == 0 {
		return nil, fmt.Errorf("no choices in OpenAI response")
	}

	choice := choices[0].(map[string]interface{})
	message, _ := choice["message"].(map[string]interface{})

	var textContent string
	var toolCalls []UnifiedToolCall

	// 处理文本内容
	if content, exists := message["content"]; exists && content != nil {
		textContent = content.(string)
	}

	// 处理工具调用
	if toolCallsData, exists := message["tool_calls"].([]interface{}); exists {
		for _, tc := range toolCallsData {
			tcMap := tc.(map[string]interface{})
			function := tcMap["function"].(map[string]interface{})

			toolCalls = append(toolCalls, UnifiedToolCall{
				Id:   tcMap["id"].(string),
				Type: tcMap["type"].(string),
				Function: UnifiedToolCallFunction{
					Name:      function["name"].(string),
					Arguments: function["arguments"].(string),
				},
			})
		}
	}

	// 转换结束原因
	finishReason := ""
	if reason, exists := choice["finish_reason"]; exists && reason != nil {
		finishReason = reason.(string)
	}

	// 处理 usage 信息
	var usage *types.Usage
	if usageData, exists := responseData["usage"].(map[string]interface{}); exists {
		usage = &types.Usage{
			PromptTokens:     int(usageData["prompt_tokens"].(float64)),
			CompletionTokens: int(usageData["completion_tokens"].(float64)),
			TotalTokens:      int(usageData["total_tokens"].(float64)),
		}

	}

	unifiedMessage := &UnifiedMessage{
		Role:    "assistant",
		Content: textContent,
	}

	if len(toolCalls) > 0 {
		unifiedMessage.ToolCalls = toolCalls
	}

	unified := &UnifiedChatResponse{
		Id:      responseData["id"].(string),
		Object:  responseData["object"].(string),
		Created: int64(responseData["created"].(float64)),
		Model:   responseData["model"].(string),
		Choices: []UnifiedChoice{
			{
				Index:        0,
				Message:      unifiedMessage,
				FinishReason: finishReason,
			},
		},
		Usage: usage,
	}

	return unified, nil
}

// transformGeminiResponse 转换原始 Gemini 格式响应
func (t *VertexGeminiTransformer) transformGeminiResponse(responseData map[string]interface{}) (*UnifiedChatResponse, error) {
	// 提取候选项
	candidates, _ := responseData["candidates"].([]interface{})

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no candidates in Gemini response")
	}

	candidate := candidates[0].(map[string]interface{})
	content, _ := candidate["content"].(map[string]interface{})
	parts, _ := content["parts"].([]interface{})

	var textContent string
	var toolCalls []UnifiedToolCall

	// 处理响应内容
	for _, part := range parts {
		partMap := part.(map[string]interface{})

		// 处理文本内容
		if text, exists := partMap["text"]; exists {
			textContent += text.(string)
		}

		// 处理工具调用
		if functionCall, exists := partMap["functionCall"]; exists {
			fcMap, ok := functionCall.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := fcMap["name"].(string)
			args, _ := fcMap["args"].(map[string]interface{})

			// 确保 args 不为 nil，避免 json.Marshal 返回 "null"
			if args == nil {
				args = map[string]interface{}{}
			}

			argsBytes, _ := json.Marshal(args)

			toolCalls = append(toolCalls, UnifiedToolCall{
				Id:   fmt.Sprintf("call_%d", time.Now().UnixNano()),
				Type: "function",
				Function: UnifiedToolCallFunction{
					Name:      name,
					Arguments: string(argsBytes),
				},
			})
		}
	}

	// 转换结束原因
	finishReason := ""
	if reason, exists := candidate["finishReason"]; exists {
		switch reason.(string) {
		case "STOP":
			finishReason = "stop"
		case "MAX_TOKENS", "CONTINUATION":
			// CONTINUATION：Long Decode 单次 hop 到达输出上限、仍可续写，对 OpenAI 协议即长度截断
			finishReason = "length"
		case "SAFETY":
			finishReason = "content_filter"
		default:
			if len(toolCalls) > 0 {
				finishReason = "tool_calls"
			} else {
				finishReason = "stop"
			}
		}
	}

	// 处理 usage 信息
	var usage *types.Usage
	if usageMetadata, exists := responseData["usageMetadata"]; exists {
		usageMap := usageMetadata.(map[string]interface{})
		usage = &types.Usage{
			PromptTokens:     int(usageMap["promptTokenCount"].(float64)),
			CompletionTokens: int(usageMap["candidatesTokenCount"].(float64)),
			TotalTokens:      int(usageMap["totalTokenCount"].(float64)),
		}

	}

	message := &UnifiedMessage{
		Role:    "assistant",
		Content: textContent,
	}

	if len(toolCalls) > 0 {
		message.ToolCalls = toolCalls
	}

	unified := &UnifiedChatResponse{
		Id:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   "gemini-pro", // 可以从请求中获取
		Choices: []UnifiedChoice{
			{
				Index:        0,
				Message:      message,
				FinishReason: finishReason,
			},
		},
		Usage: usage,
	}

	return unified, nil
}

// TransformResponseIn 将统一格式转换为 VertexGemini 响应格式（不需要实现）
func (t *VertexGeminiTransformer) TransformResponseIn(response *UnifiedChatResponse) (interface{}, error) {
	return nil, fmt.Errorf("VertexGemini transformer does not support TransformResponseIn")
}

// TransformStreamResponseOut converts VertexGemini stream response to unified format stream
func (t *VertexGeminiTransformer) TransformStreamResponseOut(response *http.Response) (*http.Response, error) {
	if response.Body == nil {
		return response, nil
	}

	// reset tool call state
	t.toolCalls = make(map[int]*VertexToolCallState)
	t.toolCallsByID = make(map[string]*VertexToolCallState)
	t.isClosed = false

	// create pipe
	pr, pw := io.Pipe()

	go func() {
		defer func() {
			pw.Close()
		}()
		defer response.Body.Close()

		scanner := bufio.NewScanner(response.Body)
		// set buffer size limit to prevent memory leaks (1MB limit like demo)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)
		chunkId := 0

		for scanner.Scan() {
			line := scanner.Text()

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")

			if data == "" || data == "[DONE]" {
				continue
			}

			var chunkData map[string]interface{}
			if err := json.Unmarshal([]byte(data), &chunkData); err != nil {
				continue
			}

			// 检查是否是 OpenAI 格式（已经被 VertexAI provider 转换过）
			if choices, hasChoices := chunkData["choices"].([]interface{}); hasChoices {
				// 直接处理 OpenAI 格式
				if len(choices) == 0 {
					continue
				}
				choice := choices[0].(map[string]interface{})
				delta, _ := choice["delta"].(map[string]interface{})

				var textContent string
				var toolCalls []UnifiedToolCall

				// 处理文本内容
				if content, exists := delta["content"]; exists && content != nil {
					textContent = content.(string)
				}

				// 处理工具调用
				if toolCallsData, exists := delta["tool_calls"].([]interface{}); exists {
					for _, tc := range toolCallsData {
						tcMap := tc.(map[string]interface{})
						function, _ := tcMap["function"].(map[string]interface{})

						// 安全地获取字段值
						var toolCallId string
						if id, exists := tcMap["id"]; exists && id != nil {
							toolCallId = id.(string)
						} else {
							// 如果没有 id，生成一个
							toolCallId = fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), chunkId)
						}

						var toolCallType string
						if tcType, exists := tcMap["type"]; exists && tcType != nil {
							toolCallType = tcType.(string)
						} else {
							toolCallType = "function"
						}

						var functionName string
						if name, exists := function["name"]; exists && name != nil {
							functionName = name.(string)
						}

						var functionArgs string
						if args, exists := function["arguments"]; exists && args != nil {
							functionArgs = args.(string)
						} else {
							functionArgs = "{}"
						}

						// 获取 index
						toolCallIndex := 0
						if idx, exists := tcMap["index"]; exists && idx != nil {
							if idxFloat, ok := idx.(float64); ok {
								toolCallIndex = int(idxFloat)
							}
						}

						// 检查是否已经存在相同ID的工具调用（用于合并）
						var toolCallState *VertexToolCallState
						var exists bool

						if toolCallId != "" {
							// prioritize ID lookup for merging
							if existingState, found := t.toolCallsByID[toolCallId]; found {
								// check if this index has already been processed
								alreadyProcessed := false
								for _, idx := range existingState.MergedIndexes {
									if idx == toolCallIndex {
										alreadyProcessed = true
										break
									}
								}
								if alreadyProcessed {
									continue // skip processing this tool call
								}

								toolCallState = existingState
								exists = true
								// record this index as processed
								toolCallState.MergedIndexes = append(toolCallState.MergedIndexes, toolCallIndex)
							}
						}

						if !exists {
							// 通过索引查找
							toolCallState, exists = t.toolCalls[toolCallIndex]
						}

						if !exists {
							// 创建新的工具调用状态
							if toolCallId == "" {
								toolCallId = fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), toolCallIndex)
							}
							toolCallState = &VertexToolCallState{
								ID:              toolCallId,
								Name:            functionName,
								Arguments:       "",
								Sent:            false,
								LastSentArgsLen: 0,
								MergedIndexes:   []int{toolCallIndex},
							}
							t.toolCalls[toolCallIndex] = toolCallState
							if toolCallId != "" {
								t.toolCallsByID[toolCallId] = toolCallState
							}
						} else if toolCallId != "" && functionName != "" {
							// update existing tool call ID and name if it was temporary
							wasTemporary := strings.HasPrefix(toolCallState.ID, "call_") && strings.HasPrefix(toolCallState.Name, "tool_")
							if wasTemporary {
								// remove from old ID mapping
								if toolCallState.ID != "" {
									delete(t.toolCallsByID, toolCallState.ID)
								}
								toolCallState.ID = toolCallId
								toolCallState.Name = functionName
								// add to new ID mapping
								t.toolCallsByID[toolCallId] = toolCallState
							}
						}

						// update tool call state
						if toolCallId != "" && toolCallState.ID == "" {
							toolCallState.ID = toolCallId
						}
						if functionName != "" && toolCallState.Name == "" {
							toolCallState.Name = functionName
						}
						// check for new argument increments
						hasNewArgs := functionArgs != "" && functionArgs != "{}" && !t.isClosed
						if hasNewArgs {
							toolCallState.Arguments += functionArgs
						}

						// determine whether to send tool call
						isFirstTime := !toolCallState.Sent && toolCallState.Name != ""
						hasArgsIncrement := len(toolCallState.Arguments) > toolCallState.LastSentArgsLen

						shouldInclude := toolCallState.Name != "" && (isFirstTime || hasArgsIncrement)
						if shouldInclude {
							// 安全地扩展数组
							if toolCallIndex >= len(toolCalls) {
								// 扩展到需要的大小
								newSize := toolCallIndex + 1
								newToolCalls := make([]UnifiedToolCall, newSize)
								copy(newToolCalls, toolCalls)
								toolCalls = newToolCalls
							}

							// calculate argument increment to send
							var argsToSend string
							if isFirstTime {
								// first time sending, send complete arguments
								argsToSend = toolCallState.Arguments
							} else if hasArgsIncrement {
								// send argument increment with boundary check
								if toolCallState.LastSentArgsLen < len(toolCallState.Arguments) {
									argsToSend = toolCallState.Arguments[toolCallState.LastSentArgsLen:]
								}
							}

							// add or update tool call
							toolCalls[toolCallIndex] = UnifiedToolCall{
								Id:   toolCallState.ID,
								Type: toolCallType,
								Function: UnifiedToolCallFunction{
									Name:      toolCallState.Name,
									Arguments: argsToSend,
								},
							}

							// update state
							if isFirstTime {
								toolCallState.Sent = true
							}
							toolCallState.LastSentArgsLen = len(toolCallState.Arguments)
						}
					}
				}

				// 转换结束原因
				finishReason := ""
				if reason, exists := choice["finish_reason"]; exists && reason != nil {
					finishReason = reason.(string)
				}

				// 处理 usage 信息
				var usage *types.Usage
				if usageData, exists := chunkData["usage"].(map[string]interface{}); exists {
					usage = &types.Usage{
						PromptTokens:     int(usageData["prompt_tokens"].(float64)),
						CompletionTokens: int(usageData["completion_tokens"].(float64)),
						TotalTokens:      int(usageData["total_tokens"].(float64)),
					}
				}

				deltaMsg := &UnifiedMessage{
					Role: "assistant",
				}

				if textContent != "" {
					deltaMsg.Content = textContent
				}

				if len(toolCalls) > 0 {
					deltaMsg.ToolCalls = toolCalls
				}

				unified := &UnifiedChatResponse{
					Id:      chunkData["id"].(string),
					Object:  "chat.completion.chunk",
					Created: int64(chunkData["created"].(float64)),
					Model:   chunkData["model"].(string),
					Choices: []UnifiedChoice{
						{
							Index:        0,
							Delta:        deltaMsg,
							FinishReason: finishReason,
						},
					},
					Usage: usage,
				}

				// 写入统一格式的流数据
				unifiedBytes, _ := json.Marshal(unified)
				unifiedData := fmt.Sprintf("data: %s\n\n", string(unifiedBytes))
				fmt.Fprintf(pw, "%s", unifiedData)

				chunkId++
				continue
			}

			// 检查是否是原始 Gemini 格式
			candidates, ok := chunkData["candidates"].([]interface{})
			if !ok || len(candidates) == 0 {
				continue
			}
			// 处理原始 Gemini 格式（这部分保持原有逻辑）

			candidate := candidates[0].(map[string]interface{})
			content, _ := candidate["content"].(map[string]interface{})
			parts, _ := content["parts"].([]interface{})

			var textContent string
			var toolCalls []UnifiedToolCall

			// 处理响应内容
			for _, part := range parts {
				partMap := part.(map[string]interface{})

				// 处理文本内容
				if text, exists := partMap["text"]; exists {
					textContent += text.(string)
				}

				// 处理工具调用
				if functionCall, exists := partMap["functionCall"]; exists {
					fcMap, ok := functionCall.(map[string]interface{})
					if !ok {
						continue
					}
					name, _ := fcMap["name"].(string)
					args, _ := fcMap["args"].(map[string]interface{})

					// 确保 args 不为 nil，避免 json.Marshal 返回 "null"
					if args == nil {
						args = map[string]interface{}{}
					}

					argsBytes, _ := json.Marshal(args)

					toolCalls = append(toolCalls, UnifiedToolCall{
						Id:   fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), chunkId),
						Type: "function",
						Function: UnifiedToolCallFunction{
							Name:      name,
							Arguments: string(argsBytes),
						},
					})
				}
			}

			// 转换结束原因
			finishReason := ""
			if reason, exists := candidate["finishReason"]; exists {
				switch reason.(string) {
				case "STOP":
					finishReason = "stop"
				case "MAX_TOKENS", "CONTINUATION":
					// CONTINUATION：Long Decode 单次 hop 到达输出上限、仍可续写，对 OpenAI 协议即长度截断
					finishReason = "length"
				case "SAFETY":
					finishReason = "content_filter"
				default:
					if len(toolCalls) > 0 {
						finishReason = "tool_calls"
					}
				}
			}

			// 处理 usage 信息
			var usage *types.Usage
			if usageMetadata, exists := chunkData["usageMetadata"]; exists {
				usageMap := usageMetadata.(map[string]interface{})
				usage = &types.Usage{
					PromptTokens:     int(usageMap["promptTokenCount"].(float64)),
					CompletionTokens: int(usageMap["candidatesTokenCount"].(float64)),
					TotalTokens:      int(usageMap["totalTokenCount"].(float64)),
				}

			}

			delta := &UnifiedMessage{
				Role: "assistant",
			}

			if textContent != "" {
				delta.Content = textContent
			}

			if len(toolCalls) > 0 {
				delta.ToolCalls = toolCalls
			}

			unified := &UnifiedChatResponse{
				Id:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   "gemini-pro",
				Choices: []UnifiedChoice{
					{
						Index:        0,
						Delta:        delta,
						FinishReason: finishReason,
					},
				},
				Usage: usage,
			}

			// write unified format stream data
			unifiedBytes, _ := json.Marshal(unified)
			unifiedData := fmt.Sprintf("data: %s\n\n", string(unifiedBytes))
			fmt.Fprintf(pw, "%s", unifiedData)

			chunkId++
		}

		if err := scanner.Err(); err != nil {
			// log scan error if needed
		}

		// send end marker
		fmt.Fprintf(pw, "data: [DONE]\n\n")

		// clean up tool call state to prevent memory leaks
		t.isClosed = true
		t.toolCalls = make(map[int]*VertexToolCallState)
		t.toolCallsByID = make(map[string]*VertexToolCallState)
	}()

	// 创建新的响应
	newResponse := &http.Response{
		Status:        response.Status,
		StatusCode:    response.StatusCode,
		Proto:         response.Proto,
		ProtoMajor:    response.ProtoMajor,
		ProtoMinor:    response.ProtoMinor,
		Header:        make(http.Header),
		Body:          pr,
		ContentLength: -1,
	}

	// 复制头部
	for k, v := range response.Header {
		newResponse.Header[k] = v
	}

	// 设置流式响应头部
	newResponse.Header.Set("Content-Type", "text/event-stream")
	newResponse.Header.Set("Cache-Control", "no-cache")
	newResponse.Header.Set("Connection", "keep-alive")

	return newResponse, nil
}

// TransformStreamResponseIn 将统一格式流转换为 VertexGemini 流式响应格式（不需要实现）
func (t *VertexGeminiTransformer) TransformStreamResponseIn(response *http.Response) (*http.Response, error) {
	return nil, fmt.Errorf("VertexGemini transformer does not support TransformStreamResponseIn")
}
