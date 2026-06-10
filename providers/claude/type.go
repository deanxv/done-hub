package claude

import (
	"done-hub/types"
	"encoding/json"
	"math"
)

const (
	FinishReasonEndTurn = "end_turn"
	FinishReasonToolUse = "tool_use"
)

const (
	ContentTypeText             = "text"
	ContentTypeImage            = "image"
	ContentTypeToolUes          = "tool_use"
	ContentTypeToolResult       = "tool_result"
	ContentTypeThinking         = "thinking"
	ContentTypeRedactedThinking = "redacted_thinking"

	ContentStreamTypeThinking       = "thinking_delta"
	ContentStreamTypeSignatureDelta = "signature_delta"
	ContentStreamTypeInputJsonDelta = "input_json_delta"
)

type ClaudeError struct {
	Type      string          `json:"type"`
	ErrorInfo ClaudeErrorInfo `json:"error"`
}

func (e *ClaudeError) Error() string {
	bytes, _ := json.Marshal(e)
	return string(bytes) + "\n"
}

type ClaudeErrorWithStatusCode struct {
	ClaudeError
	StatusCode int  `json:"status_code"`
	LocalError bool `json:"-"`
}

func (e *ClaudeErrorWithStatusCode) ToOpenAiError() *types.OpenAIErrorWithStatusCode {
	return &types.OpenAIErrorWithStatusCode{
		StatusCode: e.StatusCode,
		OpenAIError: types.OpenAIError{
			Type:    e.Type,
			Message: e.ErrorInfo.Message,
		},
		LocalError: e.LocalError,
	}
}

type ClaudeErrorInfo struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// ClaudeMetadata.UserId 用 json.RawMessage 保存，以同时兼容两种 claude-cli 报文格式：
//  1. 旧格式：字符串 `"user_<hex>_account__session_<uuid>"`
//  2. 新格式：对象 `{"device_id":"<hex>","account_uuid":"...","session_id":"<uuid>"}`
//
// 若仍按 string 反序列化，新格式会让 done-hub 在入口处 400 拒绝整个请求。
type ClaudeMetadata struct {
	UserId json.RawMessage `json:"user_id,omitempty"`
}

type ResContent struct {
	Text       string `json:"text,omitempty"`
	Type       string `json:"type"`
	Name       string `json:"name,omitempty"`
	Input      any    `json:"input,omitempty"`
	Id         string `json:"id,omitempty"`
	Thinking   string `json:"thinking,omitempty"`
	Signature  string `json:"signature,omitempty"`
	Delta      string `json:"delta,omitempty"`
	Citations  any    `json:"citations,omitempty"`
	Content    any    `json:"content,omitempty"`
	ToolUseId  string `json:"tool_use_id,omitempty"`
	ServerName string `json:"server_name,omitempty"`
	IsError    *bool  `json:"is_error,omitempty"`
	FileId     string `json:"file_id,omitempty"`
	Data       string `json:"data,omitempty"`
}

func (g *ResContent) ToOpenAITool() *types.ChatCompletionToolCalls {
	args, _ := json.Marshal(g.Input)

	return &types.ChatCompletionToolCalls{
		Id:    g.Id,
		Type:  types.ChatMessageRoleFunction,
		Index: 0,
		Function: &types.ChatCompletionToolCallsFunction{
			Name:      g.Name,
			Arguments: string(args),
		},
	}
}

type ContentSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	Url       string `json:"url,omitempty"`
}

type MessageContent struct {
	Type         string         `json:"type"`
	Text         string         `json:"text,omitempty"`
	Source       *ContentSource `json:"source,omitempty"`
	Id           string         `json:"id,omitempty"`
	Name         string         `json:"name,omitempty"`
	Input        any            `json:"input,omitempty"`
	Content      any            `json:"content,omitempty"`
	IsError      *bool          `json:"is_error,omitempty"`
	ToolUseId    string         `json:"tool_use_id,omitempty"`
	CacheControl any            `json:"cache_control,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type ClaudeRequest struct {
	Model         string          `json:"model,omitempty"`
	System        any             `json:"system,omitempty"`
	Messages      []Message       `json:"messages"`
	MaxTokens     int             `json:"max_tokens"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Temperature   *float64        `json:"temperature,omitempty"`
	TopP          *float64        `json:"top_p,omitempty"`
	TopK          *int            `json:"top_k,omitempty"`
	Tools         []Tools         `json:"tools,omitempty"`
	ToolChoice    *ToolChoice     `json:"tool_choice,omitempty"`
	Thinking      *Thinking       `json:"thinking,omitempty"`
	McpServers    any             `json:"mcp_servers,omitempty"`
	Metadata      *ClaudeMetadata `json:"metadata,omitempty"`
	Stream        bool            `json:"stream"`
}

type Thinking struct {
	Type         string `json:"type,omitempty"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}
type ToolChoice struct {
	Type                   string `json:"type,omitempty"`
	Name                   string `json:"name,omitempty"`
	DisableParallelToolUse bool   `json:"disable_parallel_tool_use,omitempty"`
}

type Tools struct {
	Type            string `json:"type,omitempty"`
	CacheControl    any    `json:"cache_control,omitempty"`
	Name            string `json:"name,omitempty"`
	Description     string `json:"description,omitempty"`
	InputSchema     any    `json:"input_schema,omitempty"`
	DisplayHeightPx int    `json:"display_height_px,omitempty"`
	DisplayWidthPx  int    `json:"display_width_px,omitempty"`
	DisplayNumber   int    `json:"display_number,omitempty"`
}

type Usage struct {
	InputTokens              int                 `json:"input_tokens,omitempty"`
	OutputTokens             int                 `json:"output_tokens,omitempty"`
	CacheCreationInputTokens int                 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int                 `json:"cache_read_input_tokens,omitempty"`
	CacheCreation            *CacheCreationUsage `json:"cache_creation,omitempty"`

	ServerToolUse *ServerToolUse `json:"server_tool_use,omitempty"`
}

// CacheCreationUsage 对应 Anthropic usage.cache_creation 嵌套对象。
// Anthropic 协议：扁平 cache_creation_input_tokens == Ephemeral5m + Ephemeral1h 之和，
// 两者**同时返回**而非互斥。但嵌套字段不被信任为权威——计费侧以扁平字段为准（见
// ClaudeUsageToOpenaiUsage），嵌套仅用于推断 1h 占比；扁平为 0 时回退到嵌套之和
// 兼容仅返回嵌套的第三方网关。
// 注意：Anthropic 后续若新增 TTL 桶（如 30s、1d），需同步更新 ClaudeUsageToOpenaiUsage
// 的拆桶分支以及 UnmarshalJSON 里 cache_creation 的字段归一化列表。
type CacheCreationUsage struct {
	Ephemeral5mInputTokens int `json:"ephemeral_5m_input_tokens,omitempty"`
	Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens,omitempty"`
}

// GetCacheCreationTotalTokens 返回缓存创建总 token 数。
// 扁平字段为权威来源；缺失时回退到嵌套 ephemeral_*_input_tokens 之和兼容仅返回嵌套的第三方网关。
func (u *Usage) GetCacheCreationTotalTokens() int {
	if u == nil {
		return 0
	}
	if u.CacheCreationInputTokens > 0 {
		return u.CacheCreationInputTokens
	}
	if u.CacheCreation == nil {
		return 0
	}
	return u.CacheCreation.Ephemeral5mInputTokens + u.CacheCreation.Ephemeral1hInputTokens
}

// UnmarshalJSON 自定义 JSON 解析，兼容浮点数 token 值
func (u *Usage) UnmarshalJSON(data []byte) error {
	// 先用 map[string]interface{} 解析所有字段
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// 创建一个新的 map 来存储处理后的数据
	processedData := make(map[string]interface{})
	for k, v := range raw {
		processedData[k] = v // 默认保持原值
	}

	// 辅助函数：将 interface{} 转换为 int，处理浮点数（向上取整）
	convertToInt := func(v interface{}) int {
		switch val := v.(type) {
		case float64:
			return int(math.Ceil(val)) // 向上取整
		case int:
			return val
		case json.Number:
			if f, err := val.Float64(); err == nil {
				return int(math.Ceil(f))
			}
		}
		return 0
	}

	// 处理需要转换为整数的字段
	tokenFields := []string{"input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens"}
	for _, field := range tokenFields {
		if v, ok := raw[field]; ok {
			processedData[field] = convertToInt(v)
		}
	}

	// 同样规则下沉到嵌套的 cache_creation：上游若把 ephemeral_*_input_tokens
	// 序列化成浮点，标准 unmarshal 到 int 字段会整体失败，导致本请求的 usage 全部丢失。
	if ccRaw, ok := raw["cache_creation"].(map[string]interface{}); ok {
		ccNormalized := make(map[string]interface{}, len(ccRaw))
		for k, v := range ccRaw {
			ccNormalized[k] = v
		}
		for _, field := range []string{"ephemeral_5m_input_tokens", "ephemeral_1h_input_tokens"} {
			if v, ok := ccRaw[field]; ok {
				ccNormalized[field] = convertToInt(v)
			}
		}
		processedData["cache_creation"] = ccNormalized
	}

	// 将处理后的数据重新序列化
	processedJSON, err := json.Marshal(processedData)
	if err != nil {
		return err
	}

	// 使用标准的 Unmarshal 将处理后的 JSON 解析到结构体
	type UsageTemp Usage
	var temp UsageTemp
	if err := json.Unmarshal(processedJSON, &temp); err != nil {
		return err
	}

	*u = Usage(temp)
	return nil
}

type ServerToolUse struct {
	WebSearchRequests int `json:"web_search_requests,omitempty"`
}
type ClaudeResponse struct {
	Id           string       `json:"id"`
	Type         string       `json:"type"`
	Role         string       `json:"role"`
	Content      []ResContent `json:"content"`
	Model        string       `json:"model"`
	StopReason   string       `json:"stop_reason,omitempty"`
	StopSequence string       `json:"stop_sequence,omitempty"`
	Usage        Usage        `json:"usage,omitempty"`
	Error        *ClaudeError `json:"error,omitempty"`

	Container any `json:"container,omitempty"`
}

type Delta struct {
	Type         string `json:"type,omitempty"`
	Text         string `json:"text,omitempty"`
	PartialJson  string `json:"partial_json,omitempty"`
	StopReason   string `json:"stop_reason,omitempty"`
	StopSequence string `json:"stop_sequence,omitempty"`
	Thinking     string `json:"thinking,omitempty"`
	Signature    string `json:"signature,omitempty"`
	Citations    any    `json:"citations,omitempty"`
}

type ClaudeStreamResponse struct {
	Type         string         `json:"type"`
	Message      ClaudeResponse `json:"message,omitempty"`
	Index        int            `json:"index,omitempty"`
	Delta        Delta          `json:"delta,omitempty"`
	ContentBlock ContentBlock   `json:"content_block,omitempty"`
	Usage        Usage          `json:"usage,omitempty"`
	Error        *ClaudeError   `json:"error,omitempty"`
}

type ContentBlock struct {
	Type  string `json:"type"`
	Id    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`
	Text  string `json:"text,omitempty"`
}

type ModelListResponse struct {
	Data []Model `json:"data"`
}

type Model struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
