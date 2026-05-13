package config

import (
	"encoding/json"
	"strings"
)

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
