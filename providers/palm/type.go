package palm

import "done-hub/types"

type PaLMChatMessage struct {
	Author  string `json:"author"`
	Content string `json:"content"`
}

type PaLMFilter struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

type PaLMPrompt struct {
	Messages []PaLMChatMessage `json:"messages"`
}

type PaLMChatRequest struct {
	Prompt         PaLMPrompt `json:"prompt"`
	Temperature    *float64   `json:"temperature,omitempty"`
	CandidateCount *int       `json:"candidateCount,omitempty"`
	TopP           *float64   `json:"topP,omitempty"`
	TopK           int        `json:"topK,omitempty"`
}

type PaLMError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

type PaLMErrorResponse struct {
	Error PaLMError `json:"error,omitempty"`
}

type PaLMChatResponse struct {
	Candidates []PaLMChatMessage             `json:"candidates"`
	Messages   []types.ChatCompletionMessage `json:"messages"`
	Filters    []PaLMFilter                  `json:"filters"`
	Usage      *types.Usage                  `json:"usage,omitempty"`
	Model      string                        `json:"model,omitempty"`
	PaLMErrorResponse
}
