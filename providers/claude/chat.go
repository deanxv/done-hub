package claude

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/image"
	"done-hub/common/requester"
	"done-hub/common/utils"
	"done-hub/providers/base"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
)

const (
	StreamTollsNone = 0
	StreamTollsUse  = 1
	StreamTollsArg  = 2
)

type ClaudeStreamHandler struct {
	Usage       *types.Usage
	Request     *types.ChatCompletionRequest
	StreamTolls int
	Prefix      string
}

func (p *ClaudeProvider) CreateChatCompletion(request *types.ChatCompletionRequest) (*types.ChatCompletionResponse, *types.OpenAIErrorWithStatusCode) {
	request.OneOtherArg = p.GetOtherArg()
	claudeRequest, errWithCode := ConvertFromChatOpenai(request)
	if errWithCode != nil {
		return nil, errWithCode
	}

	req, errWithCode := p.getChatRequest(claudeRequest)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	claudeResponse := &ClaudeResponse{}
	// 发送请求
	_, errWithCode = p.Requester.SendRequest(req, claudeResponse, false)
	if errWithCode != nil {
		return nil, errWithCode
	}

	return ConvertToChatOpenai(p, claudeResponse, request)
}

func (p *ClaudeProvider) CreateChatCompletionStream(request *types.ChatCompletionRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	request.OneOtherArg = p.GetOtherArg()
	claudeRequest, errWithCode := ConvertFromChatOpenai(request)
	if errWithCode != nil {
		return nil, errWithCode
	}

	req, errWithCode := p.getChatRequest(claudeRequest)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	// 发送请求
	resp, errWithCode := p.Requester.SendRequestRaw(req)
	if errWithCode != nil {
		return nil, errWithCode
	}

	chatHandler := &ClaudeStreamHandler{
		Usage:   p.Usage,
		Request: request,
		Prefix:  `data: {"type"`,
	}

	eventstream.NewDecoder()

	return requester.RequestStream(p.Requester, resp, chatHandler.HandlerStream)
}

func (p *ClaudeProvider) getChatRequest(claudeRequest *ClaudeRequest) (*http.Request, *types.OpenAIErrorWithStatusCode) {
	url, errWithCode := p.GetSupportedAPIUri(config.RelayModeChatCompletions)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 获取请求地址
	fullRequestURL := p.GetFullRequestURL(url)
	if fullRequestURL == "" {
		return nil, common.ErrorWrapperLocal(nil, "invalid_claude_config", http.StatusInternalServerError)
	}

	headers := p.GetRequestHeaders()
	if claudeRequest.Stream {
		headers["Accept"] = "text/event-stream"
	}

	if strings.HasPrefix(claudeRequest.Model, "claude-3-5-sonnet") {
		headers["anthropic-beta"] = "max-tokens-3-5-sonnet-2024-07-15"
	}

	if strings.HasPrefix(claudeRequest.Model, "claude-3-7-sonnet") {
		headers["anthropic-beta"] = "output-128k-2025-02-19"
	}

	// 创建请求
	req, err := p.Requester.NewRequest(http.MethodPost, fullRequestURL, p.Requester.WithBody(claudeRequest), p.Requester.WithHeader(headers))
	if err != nil {
		return nil, common.ErrorWrapperLocal(err, "new_request_failed", http.StatusInternalServerError)
	}

	return req, nil
}

func ConvertFromChatOpenai(request *types.ChatCompletionRequest) (*ClaudeRequest, *types.OpenAIErrorWithStatusCode) {
	claudeRequest := ClaudeRequest{
		Model:         request.Model,
		Messages:      make([]Message, 0),
		System:        "",
		MaxTokens:     request.MaxTokens,
		StopSequences: nil,
		Temperature:   request.Temperature,
		TopP:          request.TopP,
		Stream:        request.Stream,
	}

	if request.Stop != nil {
		stopBytes, err := json.Marshal(request.Stop)
		if err == nil {
			var stopSequences []string
			if err := json.Unmarshal(stopBytes, &stopSequences); err == nil {
				claudeRequest.StopSequences = stopSequences
			} else if stop, ok := request.Stop.(string); ok {
				claudeRequest.StopSequences = []string{stop}
			}
		}
	}

	systemMessage := ""
	mgsLen := len(request.Messages) - 1
	isThink := request.OneOtherArg == "thinking" || request.Reasoning != nil

	for index, msg := range request.Messages {
		if isThink && index == mgsLen && (msg.Role == types.ChatMessageRoleAssistant || msg.Role == types.ChatMessageRoleSystem) {
			msg.Role = types.ChatMessageRoleUser
		}

		if msg.Role == types.ChatMessageRoleSystem {
			systemMessage += msg.StringContent()
			continue
		}
		messageContent, err := convertMessageContent(&msg)
		if err != nil {
			return nil, common.ErrorWrapper(err, "conversion_error", http.StatusBadRequest)
		}
		if messageContent != nil {
			claudeRequest.Messages = append(claudeRequest.Messages, *messageContent)
		}
	}

	if systemMessage != "" {
		claudeRequest.System = systemMessage
	}

	for _, tool := range request.Tools {
		tool := Tools{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: tool.Function.Parameters,
		}
		claudeRequest.Tools = append(claudeRequest.Tools, tool)
	}

	if request.ToolChoice != nil {
		toolType, toolFunc := request.ParseToolChoice()
		claudeRequest.ToolChoice = ConvertToolChoice(toolType, toolFunc)
	}

	if claudeRequest.MaxTokens == 0 {
		claudeRequest.MaxTokens = config.ClaudeSettingsInstance.GetDefaultMaxTokens(request.Model)
	}

	// 如果是3-7 默认开启thinking
	if request.OneOtherArg == "thinking" || request.Reasoning != nil {
		var opErr *types.OpenAIErrorWithStatusCode
		claudeRequest.MaxTokens, claudeRequest.Thinking, opErr = getThinking(claudeRequest.MaxTokens, request.Reasoning)

		if opErr != nil {
			return nil, opErr
		}

		claudeRequest.TopP = nil
	}

	return &claudeRequest, nil
}

func getThinking(maxTokens int, reasoning *types.ChatReasoning) (newMaxtokens int, thinking *Thinking, err *types.OpenAIErrorWithStatusCode) {
	newMaxtokens = maxTokens
	thinking = &Thinking{
		Type: "enabled",
	}

	if reasoning == nil || (reasoning.MaxTokens == 0 && reasoning.Effort == "") {
		thinking.BudgetTokens = int(float64(maxTokens) * config.ClaudeSettingsInstance.BudgetTokensPercentage)
	} else if reasoning.MaxTokens > 0 {
		if reasoning.MaxTokens < 1024 {
			err = common.StringErrorWrapper("budget_token must be greater than 1024", "budget_tokens_too_small", http.StatusBadRequest)
			return
		}

		if reasoning.MaxTokens > maxTokens {
			err = common.StringErrorWrapper(fmt.Sprintf("budget_token cannot be greater than the max_token, max_token: %d, budget_token: %d", maxTokens, reasoning.MaxTokens), "budget_tokens_too_large", http.StatusBadRequest)
			return
		}
		thinking.BudgetTokens = reasoning.MaxTokens
	} else {
		switch reasoning.Effort {
		case "low":
			thinking.BudgetTokens = int(float64(maxTokens) * 0.2)
		case "medium":
			thinking.BudgetTokens = int(float64(maxTokens) * 0.5)
		default:
			thinking.BudgetTokens = int(float64(maxTokens) * 0.8)
		}
	}

	// 如果低于1024,则设置为1024
	if thinking.BudgetTokens < 1024 {
		thinking.BudgetTokens = 1024
	}

	if newMaxtokens <= thinking.BudgetTokens {
		newMaxtokens = 1280
	}

	return
}

func ConvertToolChoice(toolType, toolFunc string) *ToolChoice {
	choice := &ToolChoice{Type: "auto"}

	switch toolType {
	case types.ToolChoiceTypeFunction:
		choice.Type = "tool"
		choice.Name = toolFunc
	case types.ToolChoiceTypeRequired:
		choice.Type = "any"
	}

	return choice
}

func convertMessageContent(msg *types.ChatCompletionMessage) (*Message, error) {
	message := Message{
		Role: convertRole(msg.Role),
	}

	content := make([]MessageContent, 0)

	if msg.ToolCalls != nil {
		for _, toolCall := range msg.ToolCalls {
			inputParam := make(map[string]any)
			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &inputParam); err != nil {
				return nil, err
			}
			content = append(content, MessageContent{
				Type:  ContentTypeToolUes,
				Id:    toolCall.Id,
				Name:  toolCall.Function.Name,
				Input: inputParam,
			})
		}

		message.Content = content
		return &message, nil
	}

	if msg.Role == types.ChatMessageRoleTool {
		content = append(content, MessageContent{
			Type:      ContentTypeToolResult,
			Content:   msg.StringContent(),
			ToolUseId: msg.ToolCallID,
		})

		message.Content = content
		return &message, nil
	}

	openaiContent := msg.ParseContent()
	for _, part := range openaiContent {
		if part.Type == types.ContentTypeText {
			content = append(content, MessageContent{
				Type: "text",
				Text: part.Text,
			})
			continue
		}
		if part.Type == types.ContentTypeImageURL {
			mimeType, data, err := image.GetImageFromUrl(part.ImageURL.URL)
			if err != nil {
				return nil, common.ErrorWrapper(err, "image_url_invalid", http.StatusBadRequest)
			}
			claudeType := "image"

			if mimeType == "application/pdf" {
				claudeType = "document"
			}
			content = append(content, MessageContent{
				Type: claudeType,
				Source: &ContentSource{
					Type:      "base64",
					MediaType: mimeType,
					Data:      data,
				},
			})
		}
	}

	message.Content = content

	return &message, nil
}

func ConvertToChatOpenai(provider base.ProviderInterface, response *ClaudeResponse, request *types.ChatCompletionRequest) (openaiResponse *types.ChatCompletionResponse, errWithCode *types.OpenAIErrorWithStatusCode) {
	aiError := errorHandle(response.Error)
	if aiError != nil {
		errWithCode = &types.OpenAIErrorWithStatusCode{
			OpenAIError: *aiError,
			StatusCode:  http.StatusBadRequest,
		}
		return
	}

	choices := make([]types.ChatCompletionChoice, 0)
	isThinking := false
	thinkingContent := ""

	for _, content := range response.Content {
		switch content.Type {
		case ContentTypeToolUes:
			if len(choices) == 0 {
				choice := types.ChatCompletionChoice{
					Index: 0,
					Message: types.ChatCompletionMessage{
						Role:    response.Role,
						Content: "",
					},
				}
				choices = append(choices, choice)
			}

			index := len(choices) - 1
			lastChoice := choices[index]

			if lastChoice.Message.ToolCalls == nil {
				lastChoice.Message.ToolCalls = make([]*types.ChatCompletionToolCalls, 0)
			}
			lastChoice.Message.ToolCalls = append(lastChoice.Message.ToolCalls, content.ToOpenAITool())
			lastChoice.FinishReason = types.FinishReasonToolCalls
			choices[index] = lastChoice
		case ContentTypeThinking, ContentTypeRedactedThinking:
			if content.Type == ContentTypeRedactedThinking {
				continue
			}
			isThinking = true
			thinkingContent = content.Thinking
		default:
			choice := types.ChatCompletionChoice{
				Index: 0,
				Message: types.ChatCompletionMessage{
					Role:    response.Role,
					Content: content.Text,
				},
				FinishReason: stopReasonClaude2OpenAI(response.StopReason),
			}

			if isThinking {
				choice.Message.ReasoningContent = thinkingContent
			}

			choices = append(choices, choice)
		}

	}

	if len(choices) == 0 {
		// 如果没有内容，则返回一个空的响应
		choices = append(choices, types.ChatCompletionChoice{
			Index: 0,
			Message: types.ChatCompletionMessage{
				Role:    response.Role,
				Content: "",
			},
			FinishReason: stopReasonClaude2OpenAI(response.StopReason),
		})
	}

	openaiResponse = &types.ChatCompletionResponse{
		ID:      response.Id,
		Object:  "chat.completion",
		Created: utils.GetTimestamp(),
		Choices: choices,
		Model:   request.Model,
		Usage: &types.Usage{
			CompletionTokens: 0,
			PromptTokens:     0,
			TotalTokens:      0,
		},
	}

	usage := provider.GetUsage()
	isOk := ClaudeUsageToOpenaiUsage(&response.Usage, usage)
	if !isOk {
		usage.CompletionTokens = ClaudeOutputUsage(response)
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}

	openaiResponse.Usage = usage

	return openaiResponse, nil
}

// 转换为OpenAI聊天流式请求体
func (h *ClaudeStreamHandler) HandlerStream(rawLine *[]byte, dataChan chan string, errChan chan error) {
	// 如果rawLine 前缀不为data:，则直接返回
	if !strings.HasPrefix(string(*rawLine), h.Prefix) {
		*rawLine = nil
		return
	}

	if strings.HasPrefix(string(*rawLine), "data: ") {
		// 去除前缀
		*rawLine = (*rawLine)[6:]
	}

	var claudeResponse ClaudeStreamResponse
	err := json.Unmarshal(*rawLine, &claudeResponse)
	if err != nil {
		errChan <- common.ErrorToOpenAIError(err)
		return
	}

	aiError := errorHandle(claudeResponse.Error)
	if aiError != nil {
		errChan <- aiError
		return
	}

	if claudeResponse.Type == "message_stop" {
		errChan <- io.EOF
		*rawLine = requester.StreamClosed
		return
	}

	switch claudeResponse.Type {
	case "message_start":
		h.convertToOpenaiStream(&claudeResponse, dataChan)
		h.Usage.PromptTokens = claudeResponse.Message.Usage.InputTokens

	case "message_delta":
		h.convertToOpenaiStream(&claudeResponse, dataChan)
		h.Usage.CompletionTokens = claudeResponse.Usage.OutputTokens
		h.Usage.TotalTokens = h.Usage.PromptTokens + h.Usage.CompletionTokens

	case "content_block_delta":
		h.convertToOpenaiStream(&claudeResponse, dataChan)
		h.Usage.TextBuilder.WriteString(claudeResponse.Delta.Text)
	case "content_block_start":
		h.convertToOpenaiStream(&claudeResponse, dataChan)

	default:
		return
	}
}

func (h *ClaudeStreamHandler) convertToOpenaiStream(claudeResponse *ClaudeStreamResponse, dataChan chan string) {
	choice := types.ChatCompletionStreamChoice{
		Index: claudeResponse.Index,
		Delta: types.ChatCompletionStreamChoiceDelta{
			Role:    claudeResponse.Message.Role,
			Content: claudeResponse.Delta.Text,
		},
	}

	if claudeResponse.ContentBlock.Text != "" {
		choice.Delta.Content = claudeResponse.ContentBlock.Text
	}

	var toolCalls []*types.ChatCompletionToolCalls

	if claudeResponse.ContentBlock.Type == ContentTypeToolUes {
		toolCalls = append(toolCalls, &types.ChatCompletionToolCalls{
			Id:   claudeResponse.ContentBlock.Id,
			Type: types.ChatMessageRoleFunction,
			Function: &types.ChatCompletionToolCallsFunction{
				Name:      claudeResponse.ContentBlock.Name,
				Arguments: "",
			},
		})
		h.StreamTolls = StreamTollsUse
	}

	switch claudeResponse.Delta.Type {
	case ContentStreamTypeInputJsonDelta:
		if claudeResponse.Delta.PartialJson == "" {
			return
		}
		toolCalls = append(toolCalls, &types.ChatCompletionToolCalls{
			Type: types.ChatMessageRoleFunction,
			Function: &types.ChatCompletionToolCallsFunction{
				Arguments: claudeResponse.Delta.PartialJson,
			},
		})
		h.StreamTolls = StreamTollsArg
	case ContentStreamTypeSignatureDelta:
		// 加密的不处理
		choice.Delta.ReasoningContent = "\n"
	case ContentStreamTypeThinking:
		choice.Delta.ReasoningContent = claudeResponse.Delta.Thinking
	}

	if claudeResponse.ContentBlock.Type != ContentTypeToolUes && claudeResponse.Delta.Type != "input_json_delta" && h.StreamTolls != StreamTollsNone {
		if h.StreamTolls == StreamTollsUse {
			toolCalls = append(toolCalls, &types.ChatCompletionToolCalls{
				Type: types.ChatMessageRoleFunction,
				Function: &types.ChatCompletionToolCallsFunction{
					Arguments: "{}",
				},
			})
		}

		h.StreamTolls = StreamTollsNone
	}

	if toolCalls != nil {
		choice.Delta.ToolCalls = toolCalls
	}

	finishReason := stopReasonClaude2OpenAI(claudeResponse.Delta.StopReason)
	if finishReason != "" {
		choice.FinishReason = &finishReason
	}
	chatCompletion := types.ChatCompletionStreamResponse{
		ID:      fmt.Sprintf("chatcmpl-%s", utils.GetUUID()),
		Object:  "chat.completion.chunk",
		Created: utils.GetTimestamp(),
		Model:   h.Request.Model,
		Choices: []types.ChatCompletionStreamChoice{choice},
	}

	responseBody, _ := json.Marshal(chatCompletion)
	dataChan <- string(responseBody)
}
