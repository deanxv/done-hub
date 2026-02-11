package geminicli

import (
	"bytes"
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/requester"
	"done-hub/common/utils"
	"done-hub/providers/base"
	"done-hub/providers/gemini"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// CreateChatCompletion 创建聊天补全（非流式）
func (p *GeminiCliProvider) CreateChatCompletion(request *types.ChatCompletionRequest) (*types.ChatCompletionResponse, *types.OpenAIErrorWithStatusCode) {
	// 转换为Gemini格式
	geminiRequest, errWithCode := gemini.ConvertFromChatOpenai(request)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 构建内部API请求
	req, errWithCode := p.getChatRequest(geminiRequest, false, false)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	// 使用包装的响应结构
	cliResponse := &GeminiCliResponse{}
	// 发送请求
	_, errWithCode = p.Requester.SendRequest(req, cliResponse, false)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 提取实际的 Gemini 响应
	if cliResponse.Response == nil {
		return nil, common.StringErrorWrapper("no response in upstream response", "no_response", http.StatusInternalServerError)
	}

	return gemini.ConvertToChatOpenai(p, cliResponse.Response, request)
}

// CreateChatCompletionStream 创建聊天补全（流式）
func (p *GeminiCliProvider) CreateChatCompletionStream(request *types.ChatCompletionRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	// 转换为Gemini格式
	geminiRequest, errWithCode := gemini.ConvertFromChatOpenai(request)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 构建内部API请求
	req, errWithCode := p.getChatRequest(geminiRequest, true, false)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	// 发送请求
	resp, errWithCode := p.Requester.SendRequestRaw(req)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 使用 GeminiCli 专用的流处理器
	chatHandler := &GeminiCliStreamHandler{
		Usage:   p.Usage,
		Request: request,
		Context: p.Context,
	}

	return requester.RequestStream(p.Requester, resp, chatHandler.HandlerStream)
}

// getChatRequest 构建内部API请求
func (p *GeminiCliProvider) getChatRequest(geminiRequest *gemini.GeminiChatRequest, isStream bool, isRelay bool) (*http.Request, *types.OpenAIErrorWithStatusCode) {
	// 确定请求URL
	action := "generateContent"
	if isStream {
		action = "streamGenerateContent"
	}

	fullRequestURL := p.GetFullRequestURL(action, geminiRequest.Model)

	// 获取请求头
	headers, err := p.getRequestHeadersInternal()
	if err != nil {
		return nil, p.handleTokenError(err)
	}

	// 只有在 relay 模式下才清理数据（与 gemini provider 保持一致）
	var geminiRequestBody any
	if isRelay {
		rawMap, _, exists := p.GetProcessedBody()
		if !exists {
			if preMap, ok := p.GetRawMapBody(); ok {
				rawMap = preMap
			} else if rawData, rawExists := p.GetRawBody(); rawExists {
				rawMap = make(map[string]interface{})
				if err := json.Unmarshal(rawData, &rawMap); err != nil {
					return nil, common.ErrorWrapper(err, "unmarshal_request_failed", http.StatusInternalServerError)
				}
			} else {
				return nil, common.StringErrorWrapperLocal("request body not found", "request_body_not_found", http.StatusInternalServerError)
			}

			// 清理 contents 中的 role 和 id 字段
			if contents, ok := rawMap["contents"].([]interface{}); ok {
				for _, content := range contents {
					if contentMap, ok := content.(map[string]interface{}); ok {
						if _, hasRole := contentMap["role"]; !hasRole {
							contentMap["role"] = "user"
						}

						// 清理 parts 中的 function_call 和 function_response 的 id 字段
						if parts, ok := contentMap["parts"].([]interface{}); ok {
							for _, part := range parts {
								if partMap, ok := part.(map[string]interface{}); ok {
									for _, fn := range []string{"functionCall", "function_call"} {
										if fc, ok := partMap[fn].(map[string]interface{}); ok {
											delete(fc, "id")
										}
									}
									for _, fn := range []string{"functionResponse", "function_response"} {
										if fr, ok := partMap[fn].(map[string]interface{}); ok {
											delete(fr, "id")
										}
									}
								}
							}
						}
					}
				}
			}

			delete(rawMap, "model")
			p.SetProcessedBody(rawMap, false)
			p.Context.Set(config.GinRawMapBodyKey, nil)
		}

		geminiRequestBody = rawMap
	} else {
		geminiRequestBody = geminiRequest
	}

	if isStream {
		fullRequestURL += "?alt=sse"
	}

	// 处理额外参数
	customParams, err := p.CustomParameterHandler()
	if err != nil {
		return nil, common.ErrorWrapper(err, "custom_parameter_error", http.StatusInternalServerError)
	}

	// 如果有额外参数，将其合并到 Gemini 请求体中
	var finalGeminiRequest any = geminiRequestBody
	if customParams != nil {
		// 将 Gemini 请求体转换为 map，以便添加额外参数
		var geminiRequestMap map[string]interface{}

		// 检查 geminiRequestBody 是否已经是 map 类型
		if rawMap, ok := geminiRequestBody.(map[string]interface{}); ok {
			geminiRequestMap = rawMap
		} else {
			// 否则进行 JSON 编码
			requestBytes, err := json.Marshal(geminiRequestBody)
			if err != nil {
				return nil, common.ErrorWrapper(err, "marshal_request_failed", http.StatusInternalServerError)
			}

			err = json.Unmarshal(requestBytes, &geminiRequestMap)
			if err != nil {
				return nil, common.ErrorWrapper(err, "unmarshal_request_failed", http.StatusInternalServerError)
			}
		}

		// 处理自定义额外参数
		geminiRequestMap = p.MergeCustomParams(geminiRequestMap, customParams, geminiRequest.Model)
		finalGeminiRequest = geminiRequestMap
	}

	// 包装为 GeminiCliRequest 格式
	requestBody := map[string]interface{}{
		"model":   geminiRequest.Model,
		"project": p.ProjectID,
		"request": finalGeminiRequest,
	}

	// 创建请求
	req, err := p.Requester.NewRequest(http.MethodPost, fullRequestURL, p.Requester.WithBody(requestBody), p.Requester.WithHeader(headers))
	if err != nil {
		return nil, common.ErrorWrapper(err, "create_request_failed", http.StatusInternalServerError)
	}

	return req, nil
}

// GeminiCliStreamHandler GeminiCli 流式响应处理器
type GeminiCliStreamHandler struct {
	Usage   *types.Usage
	Request *types.ChatCompletionRequest
	Context *gin.Context
}

// HandlerStream 处理流式响应
func (h *GeminiCliStreamHandler) HandlerStream(rawLine *[]byte, dataChan chan string, errChan chan error) {
	rawStr := string(*rawLine)

	// 如果不是 data: 开头，直接返回
	if !strings.HasPrefix(rawStr, "data: ") {
		return
	}

	// 去除 "data: " 前缀
	noSpaceLine := bytes.TrimSpace(*rawLine)
	noSpaceLine = noSpaceLine[6:] // 去除 "data: "

	// 解析包装的响应
	var cliResponse GeminiCliResponse
	err := json.Unmarshal(noSpaceLine, &cliResponse)
	if err != nil {
		logger.SysError(fmt.Sprintf("Failed to unmarshal GeminiCli stream response: %s", err.Error()))
		errChan <- common.ErrorToOpenAIError(err)
		return
	}

	// 提取实际的 Gemini 响应
	if cliResponse.Response == nil {
		logger.SysError("GeminiCli stream response has no 'response' field")
		return
	}

	geminiResponse := cliResponse.Response

	// 检查错误
	if geminiResponse.ErrorInfo != nil {
		errChan <- geminiResponse.ErrorInfo
		return
	}

	// 更新 usage
	if geminiResponse.UsageMetadata != nil {
		h.Usage.PromptTokens = geminiResponse.UsageMetadata.PromptTokenCount

		// 计算 completion tokens，确保不为负数
		completionTokens := geminiResponse.UsageMetadata.CandidatesTokenCount + geminiResponse.UsageMetadata.ThoughtsTokenCount
		if completionTokens < 0 {
			completionTokens = 0
		}
		h.Usage.CompletionTokens = completionTokens
		h.Usage.CompletionTokensDetails.ReasoningTokens = geminiResponse.UsageMetadata.ThoughtsTokenCount

		// 如果 TotalTokenCount 为 0 但有 PromptTokenCount，则计算总数
		totalTokens := geminiResponse.UsageMetadata.TotalTokenCount
		if totalTokens == 0 && geminiResponse.UsageMetadata.PromptTokenCount > 0 {
			totalTokens = geminiResponse.UsageMetadata.PromptTokenCount + completionTokens
		}
		h.Usage.TotalTokens = totalTokens
	}

	// 转换为 OpenAI 流式响应
	h.convertToOpenaiStream(geminiResponse, dataChan)
}

// convertToOpenaiStream 将 Gemini 响应转换为 OpenAI 流式格式
func (h *GeminiCliStreamHandler) convertToOpenaiStream(geminiResponse *gemini.GeminiChatResponse, dataChan chan string) {
	// 获取响应中应该使用的模型名称
	responseModel := h.Request.Model
	if h.Context != nil {
		responseModel = base.GetResponseModelNameFromContext(h.Context, h.Request.Model)
	}

	streamResponse := types.ChatCompletionStreamResponse{
		ID:      geminiResponse.ResponseId,
		Object:  "chat.completion.chunk",
		Created: utils.GetTimestamp(),
		Model:   responseModel,
	}

	choices := make([]types.ChatCompletionStreamChoice, 0, len(geminiResponse.Candidates))

	isStop := false
	for _, candidate := range geminiResponse.Candidates {
		if candidate.FinishReason != nil && *candidate.FinishReason == "STOP" {
			isStop = true
			candidate.FinishReason = nil
		}
		choices = append(choices, candidate.ToOpenAIStreamChoice(h.Request))
	}

	if len(choices) > 0 && (choices[0].Delta.ToolCalls != nil || choices[0].Delta.FunctionCall != nil) {
		choices := choices[0].ConvertOpenaiStream()
		for _, choice := range choices {
			chatCompletionCopy := streamResponse
			chatCompletionCopy.Choices = []types.ChatCompletionStreamChoice{choice}
			responseBody, _ := json.Marshal(chatCompletionCopy)
			dataChan <- string(responseBody)
		}
	} else {
		streamResponse.Choices = choices
		responseBody, _ := json.Marshal(streamResponse)
		dataChan <- string(responseBody)
	}

	if isStop {
		streamResponse.Choices = []types.ChatCompletionStreamChoice{
			{
				FinishReason: types.FinishReasonStop,
				Delta: types.ChatCompletionStreamChoiceDelta{
					Role: types.ChatMessageRoleAssistant,
				},
			},
		}
		responseBody, _ := json.Marshal(streamResponse)
		dataChan <- string(responseBody)
	}
}
