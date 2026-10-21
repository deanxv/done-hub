package types

import (
	"errors"
	"fmt"
)

type EmbeddingRequest struct {
	Model          string `json:"model" binding:"required"`
	Input          any    `json:"input" binding:"required"`
	EncodingFormat string `json:"encoding_format,omitempty"`
	Dimensions     int    `json:"dimensions,omitempty"`
	User           string `json:"user,omitempty"`
}

type Embedding struct {
	Object    string `json:"object"`
	Embedding any    `json:"embedding"`
	Index     int    `json:"index"`
}

type EmbeddingResponse struct {
	Object string      `json:"object"`
	Data   []Embedding `json:"data"`
	Model  string      `json:"model"`
	Usage  *Usage      `json:"usage,omitempty"`
}

// ParseInput 解析 embeddings 请求的 input，仅支持字符串和字符串数组。
// token 数组（[]int）不支持：只接受文本的上游渠道据此返回 400，其余渠道应透传原始 input。
func (r EmbeddingRequest) ParseInput() ([]string, error) {
	if r.Input == nil {
		return nil, errors.New("input is required")
	}

	switch input := r.Input.(type) {
	case string:
		return []string{input}, nil
	case []any:
		if len(input) == 0 {
			return nil, errors.New("invalid input: empty array")
		}
		texts := make([]string, 0, len(input))
		for i, item := range input {
			str, ok := item.(string)
			if !ok {
				if isNumber(item) {
					return nil, fmt.Errorf("invalid input[%d]: expected string, got %T (token arrays are not supported)", i, item)
				}
				return nil, fmt.Errorf("invalid input[%d]: expected string, got %T", i, item)
			}
			texts = append(texts, str)
		}
		return texts, nil
	default:
		return nil, fmt.Errorf("invalid input: expected string or array of strings, got %T", r.Input)
	}
}

// ParseInputString 返回 input 解析后的第一段文本，供只接受单个字符串的上游使用。
func (r EmbeddingRequest) ParseInputString() (string, error) {
	texts, err := r.ParseInput()
	if err != nil {
		return "", err
	}
	return texts[0], nil
}

// isNumber 判断元素是否为 JSON 数字（解码后为 float64）。
func isNumber(v any) bool {
	_, ok := v.(float64)
	return ok
}
