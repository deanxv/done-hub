package config

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

var defaultNeed2ResponseModels = []string{
	"o3-pro-2025-06-10",
	"o3-pro",
	"o1-pro-2025-03-19",
	"o1-pro",
	"o3-deep-research-2025-06-26",
	"o3-deep-research",
	"o4-mini-deep-research-2025-06-26",
	"o4-mini-deep-research",
	"codex-mini-latest",
}

type ResponsesSettings struct {
	mutex               sync.RWMutex
	need2ResponseModels map[string]struct{}
}

var ResponsesSettingsInstance = NewResponsesSettings(defaultNeed2ResponseModels)

func init() {
	GlobalOption.RegisterCustom("Need2ResponseModels", func() string {
		return ResponsesSettingsInstance.GetNeed2ResponseModelsString()
	}, func(value string) error {
		ResponsesSettingsInstance.SetNeed2ResponseModels(value)
		return nil
	}, "")
}

func NewResponsesSettings(models []string) *ResponsesSettings {
	settings := &ResponsesSettings{}
	settings.setNeed2ResponseModels(models)
	return settings
}

func (c *ResponsesSettings) ShouldUseResponsesForModel(model string) bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	_, ok := c.need2ResponseModels[model]
	return ok
}

func (c *ResponsesSettings) SetNeed2ResponseModels(data string) {
	c.setNeed2ResponseModels(parseNeed2ResponseModels(data))
}

func (c *ResponsesSettings) GetNeed2ResponseModelsString() string {
	c.mutex.RLock()
	models := make([]string, 0, len(c.need2ResponseModels))
	for model := range c.need2ResponseModels {
		models = append(models, model)
	}
	c.mutex.RUnlock()

	sort.Strings(models)
	return strings.Join(models, "\n")
}

func (c *ResponsesSettings) setNeed2ResponseModels(models []string) {
	modelSet := make(map[string]struct{}, len(models))
	for _, model := range models {
		modelName := strings.TrimSpace(model)
		if modelName == "" {
			continue
		}
		modelSet[modelName] = struct{}{}
	}

	c.mutex.Lock()
	c.need2ResponseModels = modelSet
	c.mutex.Unlock()
}

func parseNeed2ResponseModels(data string) []string {
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

func ShouldUseResponsesForModel(model string) bool {
	return ResponsesSettingsInstance.ShouldUseResponsesForModel(model)
}
