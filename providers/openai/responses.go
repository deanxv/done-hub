package openai

import (
	"bytes"
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/requester"
	"done-hub/common/utils"
	"done-hub/providers/base"
	"done-hub/types"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type OpenAIResponsesStreamHandler struct {
	Usage     *types.Usage
	Prefix    string
	Model     string
	MessageID string
	Context   *gin.Context

	searchType string
	toolIndex  int

	// SSE 事件缓冲
	eventBuffer strings.Builder
	eventType   string
}

func (p *OpenAIProvider) CreateResponses(request *types.OpenAIResponsesRequest) (openaiResponse *types.OpenAIResponsesResponses, errWithCode *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getResponsesRequest(request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	response := &types.OpenAIResponsesResponses{}
	// 发送请求
	_, errWithCode = p.Requester.SendRequest(req, response, false)
	if errWithCode != nil {
		return nil, errWithCode
	}

	if response.Usage == nil || response.Usage.OutputTokens == 0 {
		response.Usage = &types.ResponsesUsage{
			InputTokens:  p.Usage.PromptTokens,
			OutputTokens: 0,
			TotalTokens:  0,
		}
		// // 那么需要计算
		response.Usage.OutputTokens = common.CountTokenText(response.GetContent(), request.Model)
		response.Usage.TotalTokens = response.Usage.InputTokens + response.Usage.OutputTokens
	}

	*p.Usage = *response.Usage.ToOpenAIUsage()

	getResponsesExtraBilling(response, p.Usage)

	return response, nil
}

func (p *OpenAIProvider) CreateResponsesStream(request *types.OpenAIResponsesRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
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

	chatHandler := OpenAIResponsesStreamHandler{
		Usage:  p.Usage,
		Prefix: `data: `,
		// chat→responses 兼容路径（HandlerChatStream）合成的 chat chunk 用 h.Model 当响应模型名，
		// 这里直接解析成用户原始请求模型名；原生 responses 路径不读 h.Model，走 response.model 字节改写。
		Model:   p.GetResponseModelName(request.Model),
		Context: p.Context,
	}

	if request.ConvertChat {
		return requester.RequestStream(p.Requester, resp, chatHandler.HandlerChatStream)
	}

	return requester.RequestNoTrimStream(p.Requester, resp, chatHandler.HandlerResponsesStream)
}

// getResponsesRequest 构造发往上游 /v1/responses 的请求。
// 优先字节透传：把客户端原始请求体直接转发给上游，仅在模型映射改名时按字段级最小补丁。
// 这样可以避免反序列化到 OpenAIResponsesRequest 时丢弃结构体未定义的字段
// （如 prompt_cache_key / prompt_cache_retention / safety_identifier / service_tier /
//
//	background / conversation / metadata / stream_options 等）。
//
// 当客户端走的是 chat→responses 兼容路径（ConvertChat=true）或拿不到原始字节时，
// 回退到结构体序列化路径 GetRequestTextBody。
func (p *OpenAIProvider) getResponsesRequest(request *types.OpenAIResponsesRequest) (*http.Request, *types.OpenAIErrorWithStatusCode) {
	if !request.ConvertChat {
		if patched, ok := p.patchResponsesRequestBody(request); ok {
			url, errWithCode := p.GetSupportedAPIUri(config.RelayModeResponses)
			if errWithCode != nil {
				return nil, errWithCode
			}
			fullRequestURL := p.GetFullRequestURL(url, request.Model)
			headers := p.GetRequestHeaders()
			if request.Stream {
				headers["Accept"] = "text/event-stream"
			}
			return p.NewRequestWithCustomParamsBytes(http.MethodPost, fullRequestURL, patched, headers, request.Model)
		}
	}

	return p.GetRequestTextBody(config.RelayModeResponses, request.Model, request)
}

// patchResponsesRequestBody 读 gin 缓存里的原始 /v1/responses 请求体，
// 仅对 model 做字段级最小修改（模型映射会改写 request.Model），其它一律按字节保留。
// 返回 (字节, true) 表示透传成功；返回 (nil, false) 表示透传不可用、调用方应回退。
//
// 与 providers/claude/relay.go:patchClaudeRequestBody 同款实现思路：
//   - 保留所有未知字段（OpenAI 后续新增的字段不需要再改代码）
//   - 保留字段【值】的原始字节（数值精度、cache key 字节）
//   - 保留顶层 key 顺序（缓存 prefix 命中对字节敏感）
func (p *OpenAIProvider) patchResponsesRequestBody(request *types.OpenAIResponsesRequest) ([]byte, bool) {
	if p.Context == nil {
		return nil, false
	}
	rawBody, err := common.ReadBodyRaw(p.Context)
	if err != nil || len(rawBody) == 0 {
		return nil, false
	}

	// 必须看起来像 /v1/responses 原生请求（含 model 字段），
	// 否则可能是 chat→responses 兼容路径走错了入口，直接放弃透传。
	if !gjson.GetBytes(rawBody, "model").Exists() {
		return nil, false
	}

	out := rawBody

	// 模型重写：done-hub 的模型映射会改 request.Model，仅在实际变化时才回写。
	if request.Model != "" && gjson.GetBytes(out, "model").String() != request.Model {
		patched, err := sjson.SetBytes(out, "model", request.Model)
		if err != nil {
			return nil, false
		}
		out = patched
	}

	if p.ResponsesBodyPatch != nil {
		out = p.ResponsesBodyPatch(request.Model, out)
	}

	return out, true
}

// CreateResponsesCompaction 处理 POST /v1/responses/compact。
// compact 端点永远是非流式响应，请求体结构是 /v1/responses 的子集
// （model + input + instructions + previous_response_id），实现上完全复用字节透传逻辑。
func (p *OpenAIProvider) CreateResponsesCompaction(request *types.OpenAIResponsesRequest) (*types.OpenAIResponsesResponses, *types.OpenAIErrorWithStatusCode) {
	// 强制非流式：覆盖客户端可能误传的 stream:true，确保结构体回退路径不会带出 stream 字段；
	// 字节透传路径由 patchResponsesCompactRequestBody 删除 body 里的 stream。
	request.Stream = false

	req, errWithCode := p.getResponsesCompactRequest(request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	response := &types.OpenAIResponsesResponses{}
	_, errWithCode = p.Requester.SendRequest(req, response, false)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 与 CreateResponses 同款兜底：上游漏返 usage 或 output_tokens 时，
	// 用本地预扣的 PromptTokens + 对响应文本做 token 估算补齐，避免计费归零。
	if response.Usage == nil || response.Usage.OutputTokens == 0 {
		response.Usage = &types.ResponsesUsage{
			InputTokens:  p.Usage.PromptTokens,
			OutputTokens: 0,
			TotalTokens:  0,
		}
		response.Usage.OutputTokens = common.CountTokenText(response.GetContent(), request.Model)
		response.Usage.TotalTokens = response.Usage.InputTokens + response.Usage.OutputTokens
	}

	*p.Usage = *response.Usage.ToOpenAIUsage()

	return response, nil
}

// getResponsesCompactRequest 构造发往上游 /v1/responses/compact 的请求。
// URL 在 responses URL 末尾追加 /compact；body 优先字节透传，否则回退结构体序列化。
func (p *OpenAIProvider) getResponsesCompactRequest(request *types.OpenAIResponsesRequest) (*http.Request, *types.OpenAIErrorWithStatusCode) {
	url, errWithCode := p.GetSupportedAPIUri(config.RelayModeResponses)
	if errWithCode != nil {
		return nil, errWithCode
	}
	fullRequestURL := p.GetFullRequestURL(url+"/compact", request.Model)
	headers := p.GetRequestHeaders()

	if patched, ok := p.patchResponsesCompactRequestBody(request); ok {
		return p.NewRequestWithCustomParamsBytes(http.MethodPost, fullRequestURL, patched, headers, request.Model)
	}

	return p.NewRequestWithCustomParams(http.MethodPost, fullRequestURL, request, headers, request.Model)
}

// patchResponsesCompactRequestBody 与 patchResponsesRequestBody 同款逻辑，
// 额外强制删除 body 里的 stream 字段（compact 端点不支持流式，客户端误传 stream:true 时会让上游回 SSE）。
// sjson.DeleteBytes 对不存在的字段是 no-op，无需先用 gjson 探测。
func (p *OpenAIProvider) patchResponsesCompactRequestBody(request *types.OpenAIResponsesRequest) ([]byte, bool) {
	out, ok := p.patchResponsesRequestBody(request)
	if !ok {
		return nil, false
	}

	patched, err := sjson.DeleteBytes(out, "stream")
	if err != nil {
		return nil, false
	}

	return patched, true
}

func (h *OpenAIResponsesStreamHandler) HandlerResponsesStream(rawLine *[]byte, dataChan chan string, errChan chan error) {
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

	// 如果rawLine 前缀不为data:，则添加到缓冲区
	if !strings.HasPrefix(rawStr, h.Prefix) {
		if h.eventBuffer.Len() > 0 {
			h.eventBuffer.WriteString(rawStr)
			h.eventBuffer.WriteString("\n")
		} else {
			// 没有事件类型的行，直接转发
			dataChan <- rawStr
		}
		return
	}

	noSpaceLine := bytes.TrimSpace(*rawLine)
	if strings.HasPrefix(string(noSpaceLine), "data: ") {
		// 去除前缀
		noSpaceLine = noSpaceLine[6:]
	}

	var openaiResponse types.OpenAIResponsesStreamResponses
	err := json.Unmarshal(noSpaceLine, &openaiResponse)
	if err != nil {
		errChan <- common.ErrorToOpenAIError(err)
		return
	}

	// 统一请求响应模型：model 仅出现在 response.created / response.completed 等信封事件的
	// response.model（文本增量事件不含该字段，helper 自动 no-op）。在剥离前缀的纯 JSON 上
	// 字节级改写后回填 rawStr，保留 data: 前缀与字段顺序；下游写入 eventBuffer 的各出口都复用。
	if patched, changed := base.UnifyModelInJSONBytes(h.Context, noSpaceLine, "response.model"); changed {
		rawStr = strings.Replace(rawStr, string(noSpaceLine), string(patched), 1)
	}

	switch openaiResponse.Type {
	case "response.created":
		if len(openaiResponse.Response.Tools) > 0 {
			for _, tool := range openaiResponse.Response.Tools {
				if tool.Type == types.APITollTypeWebSearchPreview {
					h.searchType = "medium"
					if tool.SearchContextSize != "" {
						h.searchType = tool.SearchContextSize
					}
				}
			}
		}
	case "response.output_text.delta":
		delta, ok := openaiResponse.Delta.(string)
		if ok {
			h.Usage.TextBuilder.WriteString(delta)
		}
	case "response.output_item.added":
		if openaiResponse.Item != nil {
			switch openaiResponse.Item.Type {
			case types.InputTypeWebSearchCall:
				if h.searchType == "" {
					h.searchType = "medium"
				}
				h.Usage.IncExtraBilling(types.APITollTypeWebSearchPreview, h.searchType)
			case types.InputTypeCodeInterpreterCall:
				h.Usage.IncExtraBilling(types.APITollTypeCodeInterpreter, "")
			case types.InputTypeFileSearchCall:
				h.Usage.IncExtraBilling(types.APITollTypeFileSearch, "")
			}
		}
	case "response.completed", "response.failed", "response.incomplete":
		// 处理流结束事件 - 先处理usage，再结束流
		if openaiResponse.Response != nil && openaiResponse.Response.Usage != nil {
			usage := openaiResponse.Response.Usage
			*h.Usage = *usage.ToOpenAIUsage()
			getResponsesExtraBilling(openaiResponse.Response, h.Usage)
		}

		// 添加数据行到缓冲区
		h.eventBuffer.WriteString(rawStr)
		h.eventBuffer.WriteString("\n")

		// 发送完整的 SSE 事件块
		dataChan <- h.eventBuffer.String()

		// 发送EOF信号结束流
		errChan <- io.EOF

		// 标记流已关闭
		*rawLine = requester.StreamClosed
		return
	}

	// 添加数据行到缓冲区
	h.eventBuffer.WriteString(rawStr)
	h.eventBuffer.WriteString("\n")

	// 发送完整的 SSE 事件块
	dataChan <- h.eventBuffer.String()

	// 重置缓冲区为下一个事件做准备
	h.eventBuffer.Reset()
	h.eventType = ""
}

func (h *OpenAIResponsesStreamHandler) HandlerChatStream(rawLine *[]byte, dataChan chan string, errChan chan error) {
	// 如果rawLine 前缀不为data:，则直接返回
	if !strings.HasPrefix(string(*rawLine), h.Prefix) {
		*rawLine = nil
		return
	}

	// 去除前缀
	*rawLine = (*rawLine)[6:]

	var openaiResponse types.OpenAIResponsesStreamResponses
	err := json.Unmarshal(*rawLine, &openaiResponse)
	if err != nil {
		errChan <- common.ErrorToOpenAIError(err)
		return
	}

	chatRes := types.ChatCompletionStreamResponse{
		ID:      h.MessageID,
		Object:  "chat.completion.chunk",
		Created: utils.GetTimestamp(),
		Model:   h.Model,
		Choices: make([]types.ChatCompletionStreamChoice, 0),
	}
	needOutput := false

	switch openaiResponse.Type {
	case "response.created":
		if openaiResponse.Response != nil {
			if h.MessageID == "" {
				h.MessageID = openaiResponse.Response.ID
				chatRes.ID = h.MessageID
			}
		}
		if len(openaiResponse.Response.Tools) > 0 {
			for _, tool := range openaiResponse.Response.Tools {
				if tool.Type == types.APITollTypeWebSearchPreview {
					h.searchType = "medium"
					if tool.SearchContextSize != "" {
						h.searchType = tool.SearchContextSize
					}
				}
			}
		}
		chatRes.Choices = append(chatRes.Choices, types.ChatCompletionStreamChoice{
			Index: 0,
			Delta: types.ChatCompletionStreamChoiceDelta{},
		})
		needOutput = true
	case "response.output_text.delta": // 处理文本输出的增量
		delta, ok := openaiResponse.Delta.(string)
		if ok {
			h.Usage.TextBuilder.WriteString(delta)
		}
		chatRes.Choices = append(chatRes.Choices, types.ChatCompletionStreamChoice{
			Index: 0,
			Delta: types.ChatCompletionStreamChoiceDelta{
				Content: delta,
			},
		})
		needOutput = true
	case "response.reasoning_summary_text.delta": // 处理文本输出的增量
		delta, ok := openaiResponse.Delta.(string)
		if ok {
			h.Usage.TextBuilder.WriteString(delta)
		}
		chatRes.Choices = append(chatRes.Choices, types.ChatCompletionStreamChoice{
			Index: 0,
			Delta: types.ChatCompletionStreamChoiceDelta{
				ReasoningContent: delta,
			},
		})
		needOutput = true
	case "response.function_call_arguments.delta": // 处理函数调用参数的增量
		delta, ok := openaiResponse.Delta.(string)
		if ok {
			h.Usage.TextBuilder.WriteString(delta)
		}
		chatRes.Choices = append(chatRes.Choices, types.ChatCompletionStreamChoice{
			Index: 0,
			Delta: types.ChatCompletionStreamChoiceDelta{
				Role: types.ChatMessageRoleAssistant,
				ToolCalls: []*types.ChatCompletionToolCalls{
					{
						Index: h.toolIndex,
						Function: &types.ChatCompletionToolCallsFunction{
							Arguments: delta,
						},
					},
				},
			},
		})
		needOutput = true
	case "response.function_call_arguments.done":
		h.toolIndex++
	case "response.output_item.added":
		if openaiResponse.Item != nil {
			switch openaiResponse.Item.Type {
			case types.InputTypeWebSearchCall:
				if h.searchType == "" {
					h.searchType = "medium"
				}
				h.Usage.IncExtraBilling(types.APITollTypeWebSearchPreview, h.searchType)
			case types.InputTypeCodeInterpreterCall:
				h.Usage.IncExtraBilling(types.APITollTypeCodeInterpreter, "")
			case types.InputTypeFileSearchCall:
				h.Usage.IncExtraBilling(types.APITollTypeFileSearch, "")

			case types.InputTypeMessage, types.InputTypeReasoning:
				chatRes.Choices = append(chatRes.Choices, types.ChatCompletionStreamChoice{
					Index: 0,
					Delta: types.ChatCompletionStreamChoiceDelta{
						Role:    types.ChatMessageRoleAssistant,
						Content: "",
					},
				})
				needOutput = true
			case types.InputTypeFunctionCall:
				chatRes.Choices = append(chatRes.Choices, types.ChatCompletionStreamChoice{
					Index: 0,
					Delta: types.ChatCompletionStreamChoiceDelta{
						Role: types.ChatMessageRoleAssistant,
						ToolCalls: []*types.ChatCompletionToolCalls{
							{
								Index: h.toolIndex,
								Id:    openaiResponse.Item.CallID,
								Type:  "function",
								Function: &types.ChatCompletionToolCallsFunction{
									Name:      openaiResponse.Item.Name,
									Arguments: openaiResponse.Item.ArgumentsString(),
								},
							},
						},
					},
				})
				needOutput = true
			}
		}
	default:
		if openaiResponse.Response != nil && openaiResponse.Response.Usage != nil {
			usage := openaiResponse.Response.Usage
			*h.Usage = *usage.ToOpenAIUsage()

			getResponsesExtraBilling(openaiResponse.Response, h.Usage)
			chatRes.Choices = append(chatRes.Choices, types.ChatCompletionStreamChoice{
				Index:        0,
				Delta:        types.ChatCompletionStreamChoiceDelta{},
				FinishReason: types.ConvertResponsesStatusToChat(openaiResponse.Response.Status),
			})
			needOutput = true

		}
	}

	if needOutput {
		jsonData, err := json.Marshal(chatRes)
		if err != nil {
			errChan <- common.ErrorToOpenAIError(err)
			return
		}
		dataChan <- string(jsonData)

		return
	}

	*rawLine = nil
}

func getResponsesExtraBilling(response *types.OpenAIResponsesResponses, usage *types.Usage) {
	if usage == nil {
		return
	}

	if len(response.Output) > 0 {
		for _, output := range response.Output {
			switch output.Type {
			case types.InputTypeWebSearchCall:
				searchType := "medium"
				if len(response.Tools) > 0 {
					for _, tool := range response.Tools {
						if tool.Type == types.APITollTypeWebSearchPreview {
							if tool.SearchContextSize != "" {
								searchType = tool.SearchContextSize
							}
						}
					}
				}
				usage.IncExtraBilling(types.APITollTypeWebSearchPreview, searchType)
			case types.InputTypeCodeInterpreterCall:
				usage.IncExtraBilling(types.APITollTypeCodeInterpreter, "")
			case types.InputTypeFileSearchCall:
				usage.IncExtraBilling(types.APITollTypeFileSearch, "")
			case types.InputTypeImageGenerationCall:
				imageType := output.Quality + "-" + output.Size
				usage.IncExtraBilling(types.APITollTypeImageGeneration, imageType)
			}
		}
	}
}
