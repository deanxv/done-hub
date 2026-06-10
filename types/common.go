package types

import (
	"done-hub/common/config"
	"encoding/json"
	"fmt"
	"strings"
)

type Usage struct {
	PromptTokens            int                     `json:"prompt_tokens"`
	CompletionTokens        int                     `json:"completion_tokens"`
	TotalTokens             int                     `json:"total_tokens"`
	PromptTokensDetails     PromptTokensDetails     `json:"prompt_tokens_details"`
	CompletionTokensDetails CompletionTokensDetails `json:"completion_tokens_details"`

	// Anthropic-style top-level cache fields returned by some OpenAI-compatible
	// gateways when proxying Anthropic models (e.g. cache_creation_input_tokens).
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`

	ExtraTokens  map[string]int          `json:"-"`
	ExtraBilling map[string]ExtraBilling `json:"-"`
	TextBuilder  strings.Builder         `json:"-"`
}

type ExtraBilling struct {
	Type      string `json:"type"`
	CallCount int    `json:"call_count"`
}

func (u *Usage) GetExtraTokens() map[string]int {
	if u.ExtraTokens == nil {
		u.ExtraTokens = make(map[string]int)
	}

	// 组装，已有的数据

	// 缓存数据
	if u.PromptTokensDetails.CachedTokens > 0 && u.ExtraTokens[config.UsageExtraCache] == 0 {
		u.ExtraTokens[config.UsageExtraCache] = u.PromptTokensDetails.CachedTokens
	}

	// Anthropic-style top-level cache fields (from OpenAI-compatible gateways
	// proxying Anthropic models). Only used as fallback when the standard
	// prompt_tokens_details fields are not already populated.
	// 顶层扁平字段不携带 5m/1h TTL 占比，全部计入 CachedWriteTokens 按 5m 倍率计费——
	// 偏保守（少算 1h 部分的 0.75x），与 sub2api/new-api 兜底策略一致。
	// 原生 Anthropic 路径在 providers/claude/common.go 按嵌套字段拆 1h 桶。
	if u.CacheCreationInputTokens > 0 && u.PromptTokensDetails.CachedWriteTokens == 0 {
		u.PromptTokensDetails.CachedWriteTokens = u.CacheCreationInputTokens
	}
	if u.CacheReadInputTokens > 0 && u.PromptTokensDetails.CachedReadTokens == 0 {
		u.PromptTokensDetails.CachedReadTokens = u.CacheReadInputTokens
	}

	// 输入音频
	if u.PromptTokensDetails.AudioTokens > 0 && u.ExtraTokens[config.UsageExtraInputAudio] == 0 {
		u.ExtraTokens[config.UsageExtraInputAudio] = u.PromptTokensDetails.AudioTokens
	}

	// 输入文字
	if u.PromptTokensDetails.TextTokens > 0 && u.ExtraTokens[config.UsageExtraInputTextTokens] == 0 {
		u.ExtraTokens[config.UsageExtraInputTextTokens] = u.PromptTokensDetails.TextTokens
	}

	// 缓存写入（5m）
	if u.PromptTokensDetails.CachedWriteTokens > 0 && u.ExtraTokens[config.UsageExtraCachedWrite] == 0 {
		u.ExtraTokens[config.UsageExtraCachedWrite] = u.PromptTokensDetails.CachedWriteTokens
	}

	// 缓存写入（1h）
	if u.PromptTokensDetails.CachedWrite1hTokens > 0 && u.ExtraTokens[config.UsageExtraCachedWrite1h] == 0 {
		u.ExtraTokens[config.UsageExtraCachedWrite1h] = u.PromptTokensDetails.CachedWrite1hTokens
	}

	// 缓存读取
	if u.PromptTokensDetails.CachedReadTokens > 0 && u.ExtraTokens[config.UsageExtraCachedRead] == 0 {
		u.ExtraTokens[config.UsageExtraCachedRead] = u.PromptTokensDetails.CachedReadTokens
	}

	// 输入图像
	if u.PromptTokensDetails.ImageTokens > 0 && u.ExtraTokens[config.UsageExtraInputImageTokens] == 0 {
		u.ExtraTokens[config.UsageExtraInputImageTokens] = u.PromptTokensDetails.ImageTokens
	}

	// 输出图像
	if u.CompletionTokensDetails.ImageTokens > 0 && u.ExtraTokens[config.UsageExtraOutputImageTokens] == 0 {
		u.ExtraTokens[config.UsageExtraOutputImageTokens] = u.CompletionTokensDetails.ImageTokens
	}

	// 输出音频
	if u.CompletionTokensDetails.AudioTokens > 0 && u.ExtraTokens[config.UsageExtraOutputAudio] == 0 {
		u.ExtraTokens[config.UsageExtraOutputAudio] = u.CompletionTokensDetails.AudioTokens
	}

	// 输出文字
	if u.CompletionTokensDetails.TextTokens > 0 && u.ExtraTokens[config.UsageExtraOutputTextTokens] == 0 {
		u.ExtraTokens[config.UsageExtraOutputTextTokens] = u.CompletionTokensDetails.TextTokens
	}

	// 推理
	if u.CompletionTokensDetails.ReasoningTokens > 0 && u.ExtraTokens[config.UsageExtraReasoning] == 0 {
		u.ExtraTokens[config.UsageExtraReasoning] = u.CompletionTokensDetails.ReasoningTokens
	}

	return u.ExtraTokens
}

func (u *Usage) SetExtraTokens(key string, value int) {
	if u.ExtraTokens == nil {
		u.ExtraTokens = make(map[string]int)
	}

	u.ExtraTokens[key] = value
}

type PromptTokensDetails struct {
	AudioTokens          int `json:"audio_tokens,omitempty"`
	CachedTokens         int `json:"cached_tokens,omitempty"`
	TextTokens           int `json:"text_tokens,omitempty"`
	ImageTokens          int `json:"image_tokens,omitempty"`
	CachedTokensInternal int `json:"cached_tokens_internal,omitempty"`

	CachedWriteTokens   int `json:"-"`
	CachedWrite1hTokens int `json:"-"`
	CachedReadTokens    int `json:"-"`
}

type CompletionTokensDetails struct {
	AudioTokens              int `json:"audio_tokens,omitempty"`
	TextTokens               int `json:"text_tokens,omitempty"`
	ReasoningTokens          int `json:"reasoning_tokens"`
	AcceptedPredictionTokens int `json:"accepted_prediction_tokens"`
	RejectedPredictionTokens int `json:"rejected_prediction_tokens"`
	ImageTokens              int `json:"image_tokens,omitempty"`
}

func (i *PromptTokensDetails) Merge(other *PromptTokensDetails) {
	if other == nil {
		return
	}

	i.AudioTokens += other.AudioTokens
	i.CachedTokens += other.CachedTokens
	i.TextTokens += other.TextTokens
}

func (o *CompletionTokensDetails) Merge(other *CompletionTokensDetails) {
	if other == nil {
		return
	}

	o.AudioTokens += other.AudioTokens
	o.TextTokens += other.TextTokens
}

type OpenAIError struct {
	Code       any    `json:"code,omitempty"`
	Message    string `json:"message"`
	Param      string `json:"param,omitempty"`
	Type       string `json:"type,omitempty"`
	InnerError any    `json:"innererror,omitempty"`

	// RateLimitResetAt 限流重置时间（Unix 时间戳，秒）
	// 用于存储从响应头/响应体中解析的冻结时间（如 anthropic-ratelimit-unified-reset、Gemini retryDelay）。
	//
	// 约定：provider **仅在拿到上游精确的 Retry-After 信号时**才应设置此字段，不要凭状态码或
	// 经验值伪造。relay/main.go:shouldCooldowns 把它作为"上游已明确告诉何时再来"的信号，
	// 优先级高于管理员配置的 RetryCooldownPerStatus 和全局 RetryCooldownSeconds。
	// 任何状态码都会读取该字段——若 provider 误填，会导致非 429 路径也基于错的时间冷却。
	RateLimitResetAt int64 `json:"-"`
}

func (e *OpenAIError) Error() string {
	response := &OpenAIErrorResponse{
		Error: *e,
	}

	// 转换为JSON
	bytes, _ := json.Marshal(response)

	fmt.Println("e", string(bytes))
	return string(bytes)
}

type OpenAIErrorWithStatusCode struct {
	OpenAIError
	StatusCode int  `json:"status_code"`
	LocalError bool `json:"-"`
}

type OpenAIErrorResponse struct {
	Error OpenAIError `json:"error,omitempty"`
}

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

func (u *Usage) IncExtraBilling(key string, bType string) {
	if u.ExtraBilling == nil {
		u.ExtraBilling = make(map[string]ExtraBilling)
		if _, ok := u.ExtraTokens[key]; !ok {
			u.ExtraBilling[key] = ExtraBilling{
				Type:      bType,
				CallCount: 0,
			}
		}
	}

	billing := u.ExtraBilling[key]
	billing.CallCount++
	u.ExtraBilling[key] = billing
}
