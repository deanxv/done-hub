package common

import (
	"done-hub/types"
	"strings"
)

// FilterEmptyContentMessages 过滤掉content为空的消息
// 这是一个公共工具函数，各个渠道可以根据需要选择是否使用
// 参数:
//   - messages: 原始消息列表
//
// 返
// 返回值:
//   - []types.ChatCompletionMessage: 过滤后的消息列表
func FilterEmptyContentMessages(messages []types.ChatCompletionMessage) []types.ChatCompletionMessage {
	if len(messages) == 0 {
		return messages
	}

	filteredMessages := make([]types.ChatCompletionMessage, 0, len(messages))

	for _, message := range messages {
		if !IsMessageContentEmpty(message) {
			filteredMessages = append(filteredMessages, message)
		}
	}

	return filteredMessages
}

func IsMessageContentEmpty(message types.ChatCompletionMessage) bool {
	// 如果消息有tool_calls、function_call等重要字段，不认为是空消息
	if message.ToolCalls != nil || message.FunctionCall != nil || message.ToolCallID != "" {
		return false
	}

	// 如果content为nil，认为是空的
	if message.Content == nil {
		return true
	}

	// 如果是字符串类型的content
	if contentStr, ok := message.Content.(string); ok {
		// 去除空白字符后检查是否为空
		return strings.TrimSpace(contentStr) == ""
	}

	// 如果是数组类型的content（多模态消息）
	contentParts := message.ParseContent()
	if len(contentParts) == 0 {
		return true
	}

	// 检查所有content part是否都为空
	hasNonEmptyContent := false
	for _, part := range contentParts {
		if part.Type == types.ContentTypeText {
			if strings.TrimSpace(part.Text) != "" {
				hasNonEmptyContent = true
				break
			}
		} else {
			// 非文本类型的content（如图片、音频等）认为是有效的
			hasNonEmptyContent = true
			break
		}
	}

	return !hasNonEmptyContent
}

// NormalizeNullContentWithToolCalls 将 assistant 消息中 content: null + tool_calls 的情况规范化为 content: ""
// OpenAI 规范允许 content: null，但部分下游（如 Anthropic）不接受空 text block
func NormalizeNullContentWithToolCalls(messages []types.ChatCompletionMessage) {
	for i := range messages {
		if messages[i].Content == nil && messages[i].ToolCalls != nil {
			messages[i].Content = ""
		}
	}
}

func FilterEmptyContentParts(messages []types.ChatCompletionMessage) []types.ChatCompletionMessage {
	if len(messages) == 0 {
		return messages
	}

	processedMessages := make([]types.ChatCompletionMessage, 0, len(messages))

	for _, message := range messages {
		processedMessage := message // 复制消息

		// 如果是字符串类型的content，检查是否为空
		if contentStr, ok := message.Content.(string); ok {
			if strings.TrimSpace(contentStr) == "" {
				processedMessage.Content = nil // 将空字符串设为nil
			}
		} else if message.Content != nil {
			// 如果是数组类型的content，过滤空的部分
			contentParts := message.ParseContent()
			if len(contentParts) > 0 {
				filteredParts := make([]types.ChatMessagePart, 0, len(contentParts))
				for _, part := range contentParts {
					if part.Type == types.ContentTypeText {
						if strings.TrimSpace(part.Text) != "" {
							filteredParts = append(filteredParts, part)
						}

						// 非文本类型的content保留
						filteredParts = append(filteredParts, part)
					}
				}

				// 如果过滤后还有内容，更新消息的content
				if len(filteredParts) > 0 {
					// 这里需要将filteredParts转换回原始格式
					// 简化处理：如果只有一个文本部分，转为字符串
					if len(filteredParts) == 1 && filteredParts[0].Type == types.ContentTypeText {
						processedMessage.Content = filteredParts[0].Text
					}
					// 否则保持原样（复杂的多模态内容处理需要更精细的逻辑）
				} else {
					processedMessage.Content = nil
				}
			}
		}

		processedMessages = append(processedMessages, processedMessage)
	}

	return processedMessages
}
