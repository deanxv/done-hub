package antigravity

import (
	"bytes"
	"done-hub/common"
	"done-hub/common/logger"
	"done-hub/common/requester"
	"done-hub/providers/base"
	"done-hub/providers/gemini"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// CreateGeminiChat 创建Gemini格式的聊天（非流式）
func (p *AntigravityProvider) CreateGeminiChat(request *gemini.GeminiChatRequest) (*gemini.GeminiChatResponse, *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getChatRequest(request, false, true)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	// 使用包装的响应结构
	antigravityResponse := &AntigravityResponse{}
	// 发送请求
	_, errWithCode = p.Requester.SendRequest(req, antigravityResponse, false)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 提取实际的 Gemini 响应
	if antigravityResponse.Response == nil {
		return nil, common.StringErrorWrapper("no response in upstream response", "no_response", http.StatusInternalServerError)
	}

	geminiResponse := antigravityResponse.Response

	// 只有非 countTokens 请求才检查 candidates
	if request.Action != "countTokens" && len(geminiResponse.Candidates) == 0 {
		return nil, common.StringErrorWrapper("no candidates", "no_candidates", http.StatusInternalServerError)
	}

	usage := p.GetUsage()
	*usage = gemini.ConvertOpenAIUsage(geminiResponse.UsageMetadata)

	return geminiResponse, nil
}

// CreateGeminiChatStream 创建Gemini格式的聊天（流式）
func (p *AntigravityProvider) CreateGeminiChatStream(request *gemini.GeminiChatRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getChatRequest(request, true, true)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	channel := p.GetChannel()

	// 使用 Antigravity 专用的 Relay 流处理器
	chatHandler := &AntigravityRelayStreamHandler{
		Usage:     p.Usage,
		ModelName: request.Model,
		Prefix:    `data: `,
		Context:   p.Context,
		Key:       channel.Key,
	}

	// 发送请求
	resp, errWithCode := p.Requester.SendRequestRaw(req)
	if errWithCode != nil {
		return nil, errWithCode
	}

	stream, errWithCode := requester.RequestNoTrimStream(p.Requester, resp, chatHandler.HandlerStream)
	if errWithCode != nil {
		return nil, errWithCode
	}

	return stream, nil
}

// AntigravityRelayStreamHandler Antigravity Relay 流式响应处理器
type AntigravityRelayStreamHandler struct {
	Usage     *types.Usage
	Prefix    string
	ModelName string
	Context   *gin.Context
	Key       string
}

// HandlerStream 处理流式响应
func (h *AntigravityRelayStreamHandler) HandlerStream(rawLine *[]byte, dataChan chan string, errChan chan error) {
	rawStr := string(*rawLine)

	if !strings.HasPrefix(rawStr, h.Prefix) {
		return
	}

	// 去除 "data: " 前缀
	noSpaceLine := bytes.TrimSpace(*rawLine)
	noSpaceLine = noSpaceLine[6:] // 去除 "data: "

	// 解析包装的响应
	var antigravityResponse AntigravityResponse
	err := json.Unmarshal(noSpaceLine, &antigravityResponse)
	if err != nil {
		logger.SysError(fmt.Sprintf("Failed to unmarshal Antigravity relay stream response: %s", err.Error()))
		// 添加超时保护，防止 goroutine 永久阻塞
		select {
		case errChan <- gemini.ErrorToGeminiErr(err):
		case <-time.After(1000 * time.Millisecond):
			logger.SysError("Failed to send unmarshal error to errChan: timeout")
		}
		return
	}

	// 提取实际的 Gemini 响应
	if antigravityResponse.Response == nil {
		select {
		case dataChan <- rawStr:
		case <-time.After(1000 * time.Millisecond):
			logger.SysError("Failed to send raw response to dataChan: timeout")
		}
		return
	}

	geminiResponse := antigravityResponse.Response

	// 检查错误
	if geminiResponse.ErrorInfo != nil {
		cleaningError(geminiResponse.ErrorInfo, h.Key)
		select {
		case errChan <- geminiResponse.ErrorInfo:
		case <-time.After(1000 * time.Millisecond):
			logger.SysError("Failed to send error info to errChan: timeout")
		}
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

	// 统一请求响应模型：本 handler 已把响应反序列化成结构体并整体重新序列化，
	// 直接改 ModelVersion / Model 两个字段即可（与非流式 relay.unifyResponseModel 的 Gemini 分支一致）。
	// 仅在原值非空时改写，避免给本不含该字段的响应凭空注入。
	if geminiResponse.ModelVersion != "" {
		geminiResponse.ModelVersion = base.GetResponseModelNameFromContext(h.Context, geminiResponse.ModelVersion)
	}
	if geminiResponse.Model != "" {
		geminiResponse.Model = base.GetResponseModelNameFromContext(h.Context, geminiResponse.Model)
	}

	// 重新序列化实际的 Gemini 响应并转发
	responseJSON, err := json.Marshal(geminiResponse)
	if err != nil {
		logger.SysError(fmt.Sprintf("Failed to marshal Gemini response: %s", err.Error()))
		select {
		case errChan <- gemini.ErrorToGeminiErr(err):
		case <-time.After(1000 * time.Millisecond):
			logger.SysError("Failed to send marshal error to errChan: timeout")
		}
		return
	}

	// 添加超时保护，防止在客户端断开时永久阻塞
	select {
	case dataChan <- fmt.Sprintf("data: %s\n\n", string(responseJSON)):
	case <-time.After(1000 * time.Millisecond):
		logger.SysError("Failed to send response data to dataChan: timeout")
	}
}
