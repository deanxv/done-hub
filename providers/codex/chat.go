package codex

import (
	"done-hub/common"
	"done-hub/common/logger"
	"done-hub/common/requester"
	"done-hub/types"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// CreateChatCompletion 创建聊天完成（非流式）
func (p *CodexProvider) CreateChatCompletion(request *types.ChatCompletionRequest) (*types.ChatCompletionResponse, *types.OpenAIErrorWithStatusCode) {
	// Codex API 只支持流式请求（stream 必须为 true）
	// 所以对于非流式请求，我们需要：
	// 1. 发送流式请求到 Codex（强制 stream = true）
	// 2. 收集完整的流式响应
	// 3. 转换为非流式格式返回

	// 转换为 Responses 格式
	responsesRequest := p.chatToResponsesRequest(request)

	// 强制设置为流式（Codex API 要求）
	responsesRequest.Stream = true

	// 获取请求
	req, errWithCode := p.getResponsesRequest(responsesRequest)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	// 发送流式请求
	resp, errWithCode := p.Requester.SendRequestRaw(req)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 创建流式处理器
	chatHandler := &CodexStreamHandler{
		Usage:   p.Usage,
		Request: request,
		Context: p.Context,
	}

	// 获取流式响应
	stream, errWithCode := requester.RequestStream(p.Requester, resp, chatHandler.HandlerStream)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 收集完整响应
	fullResponse, errWithCode := p.collectStreamResponse(stream, request)
	if errWithCode != nil {
		return nil, errWithCode
	}

	return fullResponse, nil
}

// CreateChatCompletionStream 创建聊天完成（流式）
func (p *CodexProvider) CreateChatCompletionStream(request *types.ChatCompletionRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	// 转换为 Responses 格式
	responsesRequest := p.chatToResponsesRequest(request)

	// 强制设置为流式（Codex API 要求）
	responsesRequest.Stream = true

	// 获取请求
	req, errWithCode := p.getResponsesRequest(responsesRequest)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	// 发送请求
	resp, errWithCode := p.Requester.SendRequestRaw(req)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 使用 OpenAI 标准的流处理器
	chatHandler := &CodexStreamHandler{
		Usage:   p.Usage,
		Request: request,
		Context: p.Context,
	}

	return requester.RequestStream(p.Requester, resp, chatHandler.HandlerStream)
}

// chatToResponsesRequest 将 ChatCompletionRequest 转换为 OpenAIResponsesRequest，
// 转换后走与 /v1/responses 同一份 Codex 请求规整逻辑，避免两条链路出现行为漂移。
func (p *CodexProvider) chatToResponsesRequest(request *types.ChatCompletionRequest) *types.OpenAIResponsesRequest {
	responsesRequest := request.ToResponsesRequest()
	p.prepareCodexRequest(responsesRequest)
	return responsesRequest
}

// applyDefaultHeaders 应用 Codex 默认请求头（只补充缺失的头，不覆盖已有的）
// 注意：这个方法的优先级最低，不会覆盖客户端透传的头或 ModelHeaders
func (p *CodexProvider) applyDefaultHeaders(headers map[string]string) {
	// 设置 Host（必需，但不覆盖已有的）
	if _, exists := headers["Host"]; !exists {
		headers["Host"] = "chatgpt.com"
	}

	// 设置 User-Agent（如果没有从客户端透传或 ModelHeaders 设置）
	if _, exists := headers["User-Agent"]; !exists {
		// 尝试从 Other 字段读取自定义 UA
		if p.Channel.Other != "" {
			var config map[string]string
			if err := json.Unmarshal([]byte(p.Channel.Other), &config); err == nil {
				if userAgent, exists := config["user_agent"]; exists && userAgent != "" {
					headers["User-Agent"] = userAgent
					return
				}
			}
		}
		headers["User-Agent"] = DefaultCodexUserAgent
	}

	// 设置 Accept（如果没有设置）
	if _, exists := headers["Accept"]; !exists {
		headers["Accept"] = "application/json"
	}
}

// CodexStreamHandler Codex 流式响应处理器
type CodexStreamHandler struct {
	Usage   *types.Usage
	Request *types.ChatCompletionRequest
	Context *gin.Context
}

// HandlerStream 处理流式响应（将 Responses 格式转换为 Chat 格式）
func (h *CodexStreamHandler) HandlerStream(rawLine *[]byte, dataChan chan string, errChan chan error) {
	// 如果没有数据，直接返回
	if rawLine == nil || len(*rawLine) == 0 {
		return
	}

	line := string(*rawLine)

	// 跳过空行和 event: 行
	if !strings.HasPrefix(line, "data:") {
		return
	}

	// 移除 "data: " 前缀
	data := strings.TrimPrefix(line, "data:")
	data = strings.TrimSpace(data)

	// 跳过 [DONE] 标记
	if data == "[DONE]" {
		return
	}

	// 解析 Responses 格式的流式响应
	var responsesStream types.OpenAIResponsesStreamResponses
	if err := json.Unmarshal([]byte(data), &responsesStream); err != nil {
		logger.SysError("Failed to unmarshal Codex stream response: " + err.Error())
		return
	}

	// 处理 response.completed 事件（包含 usage 信息）
	if responsesStream.Type == "response.completed" && responsesStream.Response != nil {
		if responsesStream.Response.Usage != nil {
			h.Usage.PromptTokens = responsesStream.Response.Usage.InputTokens
			h.Usage.CompletionTokens = responsesStream.Response.Usage.OutputTokens
			h.Usage.TotalTokens = responsesStream.Response.Usage.TotalTokens
		}
		return
	}

	// 处理 response.output_text.delta 事件（文本增量）
	if responsesStream.Type == "response.output_text.delta" {
		delta, ok := responsesStream.Delta.(string)
		if !ok {
			return
		}

		// 转换为 Chat 格式的流式响应
		chatResponse := h.convertResponsesStreamToChatStream(&responsesStream, delta)
		if chatResponse != nil {
			responseBody, err := json.Marshal(chatResponse)
			if err != nil {
				logger.SysError("Failed to marshal Chat stream response: " + err.Error())
				return
			}
			dataChan <- string(responseBody)
		}
	}
}

// collectStreamResponse 收集流式响应并转换为非流式格式
func (p *CodexProvider) collectStreamResponse(stream requester.StreamReaderInterface[string], request *types.ChatCompletionRequest) (*types.ChatCompletionResponse, *types.OpenAIErrorWithStatusCode) {
	var fullContent strings.Builder
	var responseID string
	var model string = request.Model
	var finishReason string = "stop"

	// 获取数据和错误通道
	dataChan, errChan := stream.Recv()

	// 读取所有流式数据
	for {
		select {
		case data, ok := <-dataChan:
			if !ok {
				// 通道已关闭，所有数据已读取完毕
				goto buildResponse
			}

			// 解析流式响应
			var chatStream types.ChatCompletionStreamResponse
			if err := json.Unmarshal([]byte(data), &chatStream); err != nil {
				logger.SysError("Failed to unmarshal stream response: " + err.Error())
				continue
			}

			// 提取响应 ID
			if responseID == "" && chatStream.ID != "" {
				responseID = chatStream.ID
			}

			// 提取模型
			if chatStream.Model != "" {
				model = chatStream.Model
			}

			// 收集内容
			if len(chatStream.Choices) > 0 {
				choice := chatStream.Choices[0]
				if choice.Delta.Content != "" {
					fullContent.WriteString(choice.Delta.Content)
				}
				if choice.FinishReason != nil {
					if fr, ok := choice.FinishReason.(string); ok && fr != "" {
						finishReason = fr
					}
				}
			}

		case err, ok := <-errChan:
			if !ok {
				// 错误通道已关闭
				continue
			}
			if err != nil {
				// EOF 是正常的流结束信号，不是错误
				if err.Error() == "EOF" {
					goto buildResponse
				}
				logger.SysError("Stream error: " + err.Error())
				return nil, common.ErrorWrapper(err, "stream_read_failed", http.StatusInternalServerError)
			}
		}
	}

buildResponse:
	// 构建完整的非流式响应
	response := &types.ChatCompletionResponse{
		ID:      responseID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []types.ChatCompletionChoice{
			{
				Index: 0,
				Message: types.ChatCompletionMessage{
					Role:    "assistant",
					Content: fullContent.String(),
				},
				FinishReason: finishReason,
			},
		},
		Usage: &types.Usage{
			PromptTokens:     p.Usage.PromptTokens,
			CompletionTokens: p.Usage.CompletionTokens,
			TotalTokens:      p.Usage.TotalTokens,
		},
	}

	return response, nil
}

// convertResponsesStreamToChatStream 将 Responses 流式响应转换为 Chat 流式响应
func (h *CodexStreamHandler) convertResponsesStreamToChatStream(responsesStream *types.OpenAIResponsesStreamResponses, delta string) *types.ChatCompletionStreamResponse {
	// 获取响应 ID
	responseID := ""
	if responsesStream.Response != nil {
		responseID = responsesStream.Response.ID
	}

	// 获取响应中应该使用的模型名称
	model := h.Request.Model
	if h.Context != nil {
		if provider, exists := h.Context.Get("provider"); exists {
			if p, ok := provider.(*CodexProvider); ok {
				model = p.GetResponseModelName(h.Request.Model)
			}
		}
	}

	response := &types.ChatCompletionStreamResponse{
		ID:      responseID,
		Object:  "chat.completion.chunk",
		Created: 0,
		Model:   model,
		Choices: []types.ChatCompletionStreamChoice{
			{
				Index: 0,
				Delta: types.ChatCompletionStreamChoiceDelta{
					Content: delta,
				},
			},
		},
	}

	return response
}

// convertResponsesToChatCompletion 将 Responses 格式转换为 Chat 格式
func (p *CodexProvider) convertResponsesToChatCompletion(responsesResp *types.OpenAIResponsesResponses, model string) *types.ChatCompletionResponse {
	// 提取文本内容
	content := ""
	if len(responsesResp.Output) > 0 {
		for _, output := range responsesResp.Output {
			if output.Type == types.ContentTypeOutputText {
				content += output.StringContent()
			}
		}
	}

	// 构建 Chat 格式响应
	chatResponse := &types.ChatCompletionResponse{
		ID:      responsesResp.ID,
		Object:  "chat.completion",
		Created: responsesResp.CreatedAt,
		Model:   p.GetResponseModelName(model),
		Choices: []types.ChatCompletionChoice{
			{
				Index: 0,
				Message: types.ChatCompletionMessage{
					Role:    types.ChatMessageRoleAssistant,
					Content: content,
				},
				FinishReason: types.ConvertResponsesStatusToChat(responsesResp.Status),
			},
		},
		Usage: &types.Usage{
			PromptTokens:     p.Usage.PromptTokens,
			CompletionTokens: p.Usage.CompletionTokens,
			TotalTokens:      p.Usage.TotalTokens,
		},
	}

	return chatResponse
}
