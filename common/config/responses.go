package config

import (
	"encoding/json"
	"strings"
)

// DefaultNeed2ResponseModels 是兜底路由表。
// 渠道 Need2ResponseModels 为 nil 或空串时均继承此表——
// 因为前端保存时会把 null 归一成 ""，所以两者在实现上等价，
// 用"留空 = 继承默认"可保证既有渠道零迁移成本。
// 只有渠道设置了非空列表时，才全量覆盖此表（不合并）。
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
