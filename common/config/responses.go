package config

import (
	"encoding/json"
	"strings"
)

// DefaultNeed2ResponseModels 是兜底路由表。
// 渠道未设置 Need2ResponseModels（nil）时生效；
// 渠道显式设置为空串时，该渠道不做 chat→Responses 转换。
var DefaultNeed2ResponseModels = map[string]struct{}{
	"o3-pro-2025-06-10":                {},
	"o3-pro":                           {},
	"o1-pro-2025-03-19":                {},
	"o1-pro":                           {},
	"o3-deep-research-2025-06-26":      {},
	"o3-deep-research":                 {},
	"o4-mini-deep-research-2025-06-26": {},
	"o4-mini-deep-research":            {},
	"codex-mini-latest":                {},
}

func BuildNeed2ResponseModelSet(models []string) map[string]struct{} {
	modelSet := make(map[string]struct{}, len(models))
	for _, model := range models {
		modelName := strings.TrimSpace(model)
		if modelName == "" {
			continue
		}
		modelSet[modelName] = struct{}{}
	}

	return modelSet
}

func ParseNeed2ResponseModels(data string) []string {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return []string{}
	}

	if strings.HasPrefix(trimmed, "[") {
		var modelList []string
		if err := json.Unmarshal([]byte(trimmed), &modelList); err == nil {
			return modelList
		}
	}

	normalized := strings.ReplaceAll(trimmed, "\r\n", "\n")
	parts := strings.FieldsFunc(normalized, func(r rune) bool {
		return r == '\n' || r == ','
	})

	return parts
}
