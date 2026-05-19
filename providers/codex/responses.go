package codex

import (
	"done-hub/common"
	"done-hub/common/requester"
	"done-hub/types"
	"encoding/json"
	"net/http"
	"strings"
)

// CodexResponsesStreamHandler Codex Responses 流式响应处理器
type CodexResponsesStreamHandler struct {
	Usage       *types.Usage
	eventBuffer strings.Builder
	eventType   string
}

// CreateResponsesCompaction 显式拒绝 compact 请求。
//
// CodexProvider 内嵌了 openai.OpenAIProvider，会自动通过方法集提升获得 OpenAIProvider 的 CreateResponsesCompaction，
// 但 Codex 的上游路径是 /backend-api/codex/responses（非 OpenAI 的 /v1/responses），
// 继承来的实现会把 URL 拼成 /backend-api/codex/responses/compact，多数情况上游会 404，
// 且会跳过 Codex 必需的请求适配（prepareCodexRequest / adaptCodexCLI / 强制流式等）。
// 这里直接覆盖为返回 503，让错误信息明确指向"渠道不支持"而非晦涩的上游错误。
func (p *CodexProvider) CreateResponsesCompaction(request *types.OpenAIResponsesRequest) (*types.OpenAIResponsesResponses, *types.OpenAIErrorWithStatusCode) {
	return nil, common.StringErrorWrapperLocal("codex channel does not support /v1/responses/compact", "channel_error", http.StatusServiceUnavailable)
}

// CreateResponses 创建 Responses 完成（非流式）
func (p *CodexProvider) CreateResponses(request *types.OpenAIResponsesRequest) (*types.OpenAIResponsesResponses, *types.OpenAIErrorWithStatusCode) {
	// Codex API 特定参数设置
	p.prepareCodexRequest(request)

	// Codex API 只支持流式请求，所以强制设置 stream = true
	request.Stream = true

	// 获取请求
	req, errWithCode := p.getResponsesRequest(request)
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
	handler := &CodexResponsesStreamHandler{
		Usage: p.Usage,
	}

	// 获取流式响应
	stream, errWithCode := requester.RequestNoTrimStream(p.Requester, resp, handler.HandlerResponsesStream)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 收集完整响应
	response, errWithCode := p.collectResponsesStreamResponse(stream)
	if errWithCode != nil {
		return nil, errWithCode
	}

	return response, nil
}

// CreateResponsesStream 创建 Responses 完成（流式）
func (p *CodexProvider) CreateResponsesStream(request *types.OpenAIResponsesRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	// Codex API 特定参数设置
	p.prepareCodexRequest(request)

	// 强制设置为流式（Codex API 要求）
	request.Stream = true

	// 获取请求
	req, errWithCode := p.getResponsesRequest(request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	// 发送请求
	resp, errWithCode := p.Requester.SendRequestRaw(req)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 创建流式处理器
	handler := &CodexResponsesStreamHandler{
		Usage: p.Usage,
	}

	// 使用 RequestNoTrimStream 保持原始格式（包括 event: 行）
	return requester.RequestNoTrimStream(p.Requester, resp, handler.HandlerResponsesStream)
}

// prepareCodexRequest 准备 Codex 请求参数
func (p *CodexProvider) prepareCodexRequest(request *types.OpenAIResponsesRequest) {
	// 1. 模型名称规范化：gpt-5-* 系列统一为 gpt-5
	if len(request.Model) > 6 && request.Model[:6] == "gpt-5-" && request.Model != "gpt-5-codex" {
		request.Model = "gpt-5"
	}

	// 2. Codex API 要求 store 参数必须设置为 false
	storeFalse := false
	request.Store = &storeFalse

	// 3. 处理 temperature 和 top_p 冲突
	// 当两者都存在时，优先保留 temperature，删除 top_p
	// 这是因为某些 API 不允许同时设置这两个参数
	if request.Temperature != nil && request.TopP != nil {
		request.TopP = nil
	}

	// 4. 适配 Codex CLI 格式
	// 注意：metadata 字段处理（参考 Demo 的 delete processedBody.metadata）
	// Go 通过结构体定义自动过滤：OpenAIResponsesRequest 中未定义 metadata 字段，
	// 因此在 JSON 序列化时会自动忽略，效果等同于 Demo 的显式删除
	p.adaptCodexCLI(request)
}

// adaptCodexCLI 适配 Codex CLI 格式
func (p *CodexProvider) adaptCodexCLI(request *types.OpenAIResponsesRequest) {
	// 检测是否为 Codex CLI 请求（通过 instructions 字段判断）
	isCodexCLI := false
	if request.Instructions != "" {
		instructions := request.Instructions
		isCodexCLI = len(instructions) > 50 && (len(instructions) >= len("You are a coding agent running in the Codex CLI") &&
			instructions[:len("You are a coding agent running in the Codex CLI")] == "You are a coding agent running in the Codex CLI" ||
			len(instructions) >= len("You are Codex") &&
				instructions[:len("You are Codex")] == "You are Codex")
	}

	// 如果不是 Codex CLI 请求，则进行适配
	if !isCodexCLI {
		// 移除不兼容的请求体字段
		request.Temperature = nil
		request.TopP = nil
		request.MaxOutputTokens = 0

		// 设置固定的 Codex CLI instructions
		request.Instructions = CodexCLIInstructions
	}
}

// collectResponsesStreamResponse 收集流式响应并转换为非流式格式
func (p *CodexProvider) collectResponsesStreamResponse(stream requester.StreamReaderInterface[string]) (*types.OpenAIResponsesResponses, *types.OpenAIErrorWithStatusCode) {
	var response *types.OpenAIResponsesResponses

	dataChan, errChan := stream.Recv()

	for {
		select {
		case data, ok := <-dataChan:
			if !ok {
				goto buildResponse
			}

			if strings.TrimSpace(data) == "" {
				continue
			}

			// 解析 SSE 格式，提取 data: 行中的 JSON
			jsonData := extractJSONFromSSE(data)
			if jsonData == "" {
				continue
			}

			// 解析流式响应
			var streamResp types.OpenAIResponsesStreamResponses
			if err := json.Unmarshal([]byte(jsonData), &streamResp); err != nil {
				continue
			}

			// 提取完整响应（response.completed 事件）
			if streamResp.Type == "response.completed" && streamResp.Response != nil {
				response = streamResp.Response
				if response.Usage != nil {
					p.Usage.PromptTokens = response.Usage.InputTokens
					p.Usage.CompletionTokens = response.Usage.OutputTokens
					p.Usage.TotalTokens = response.Usage.TotalTokens
				}
			}

		case err, ok := <-errChan:
			if !ok {
				continue
			}
			if err != nil {
				// EOF 是正常的流结束信号
				if err.Error() == "EOF" {
					goto buildResponse
				}
				return nil, common.ErrorWrapper(err, "stream_read_failed", http.StatusInternalServerError)
			}
		}
	}

buildResponse:
	if response == nil {
		return nil, common.StringErrorWrapperLocal("no response received", "no_response", http.StatusInternalServerError)
	}

	return response, nil
}

// extractJSONFromSSE 从 SSE 格式中提取 JSON 数据
func extractJSONFromSSE(sseData string) string {
	// SSE 格式示例：
	// event: response.created
	//
	// data: {"type":"response.created",...}
	//
	// 我们需要提取 data: 后面的 JSON

	lines := strings.Split(sseData, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data: ") {
			return strings.TrimPrefix(line, "data: ")
		}
	}
	return ""
}

// getResponsesRequest 构建 Responses 请求
func (p *CodexProvider) getResponsesRequest(request *types.OpenAIResponsesRequest) (*http.Request, *types.OpenAIErrorWithStatusCode) {
	// 获取完整的请求 URL
	fullRequestURL := p.GetFullRequestURL(p.Config.Responses, request.Model)

	// 获取请求头（使用内部方法以获取错误信息）
	headers, err := p.getRequestHeadersInternal()
	if err != nil {
		return nil, p.handleTokenError(err)
	}

	// 应用 Codex 默认请求头（在透传的请求头基础上补充）
	p.applyDefaultHeaders(headers)

	if request.Stream {
		headers["Accept"] = "text/event-stream"
	} else {
		headers["Accept"] = "application/json"
	}

	// 使用 Requester 创建请求
	req, err := p.Requester.NewRequest(http.MethodPost, fullRequestURL, p.Requester.WithBody(request), p.Requester.WithHeader(headers))
	if err != nil {
		return nil, common.ErrorWrapper(err, "new_request_failed", http.StatusInternalServerError)
	}

	return req, nil
}

// HandlerResponsesStream 处理 Responses 流式响应（完全透传）
func (h *CodexResponsesStreamHandler) HandlerResponsesStream(rawLine *[]byte, dataChan chan string, errChan chan error) {
	rawStr := string(*rawLine)

	// 处理 SSE 事件格式
	if strings.HasPrefix(rawStr, "event: ") {
		// 开始新的事件，保存事件类型
		h.eventType = strings.TrimPrefix(rawStr, "event: ")
		h.eventBuffer.Reset()
		h.eventBuffer.WriteString(rawStr)
		h.eventBuffer.WriteString("\n")
		return
	}

	// 如果 rawLine 前缀不为 data:，则添加到缓冲区
	if !strings.HasPrefix(rawStr, "data: ") {
		if h.eventBuffer.Len() > 0 {
			h.eventBuffer.WriteString(rawStr)
			h.eventBuffer.WriteString("\n")
		} else {
			// 没有事件类型的行，直接转发
			dataChan <- rawStr
		}
		return
	}

	// 处理 data: 行
	dataLine := strings.TrimPrefix(rawStr, "data: ")
	dataLine = strings.TrimSpace(dataLine)

	// 跳过 [DONE] 标记
	if dataLine == "[DONE]" {
		// 如果有缓冲的事件，先发送
		if h.eventBuffer.Len() > 0 {
			dataChan <- h.eventBuffer.String()
			h.eventBuffer.Reset()
			h.eventType = ""
		}
		return
	}

	// 解析 JSON 以提取 usage 信息（但不修改响应）
	var responsesEvent types.OpenAIResponsesStreamResponses
	if err := json.Unmarshal([]byte(dataLine), &responsesEvent); err == nil {
		// 提取 usage 信息
		if responsesEvent.Type == "response.completed" && responsesEvent.Response != nil {
			if responsesEvent.Response.Usage != nil {
				h.Usage.PromptTokens = responsesEvent.Response.Usage.InputTokens
				h.Usage.CompletionTokens = responsesEvent.Response.Usage.OutputTokens
				h.Usage.TotalTokens = responsesEvent.Response.Usage.TotalTokens
			}
		}
	}

	// 完全透传：将原始数据添加到缓冲区或直接发送
	if h.eventBuffer.Len() > 0 {
		// 有事件类型，添加 data 行到缓冲区
		h.eventBuffer.WriteString(rawStr)
		h.eventBuffer.WriteString("\n")

		// 检查是否是完整的事件（以空行结束）
		if strings.HasSuffix(h.eventBuffer.String(), "\n\n") {
			// 发送完整的事件
			dataChan <- h.eventBuffer.String()
			h.eventBuffer.Reset()
			h.eventType = ""
		}
	} else {
		// 没有事件类型，直接转发 data 行
		dataChan <- rawStr
	}
}
