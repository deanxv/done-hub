package model

import (
	"done-hub/common/config"
	"strings"
)

func (channel *Channel) GetNeed2ResponseModels() string {
	if channel.Need2ResponseModels == nil {
		return ""
	}

	return *channel.Need2ResponseModels
}

// ShouldUseResponsesForModel 判断模型是否应走 chat→Responses 兼容路径。
//
// 渠道自定义列表非空时，按该列表全量覆盖路由（不与默认表合并）；
// 未配置（nil）或留空时，继承全局默认表 DefaultNeed2ResponseModels，
// 与改造前写死的 need2Response 行为一致，保证既有渠道零迁移成本。
func (channel *Channel) ShouldUseResponsesForModel(model string) bool {
	modelName := strings.TrimSpace(model)
	if modelName == "" {
		return false
	}

	if custom := channel.GetNeed2ResponseModels(); strings.TrimSpace(custom) != "" {
		modelSet := config.BuildNeed2ResponseModelSet(config.ParseNeed2ResponseModels(custom))
		_, ok := modelSet[modelName]
		return ok
	}

	_, ok := config.DefaultNeed2ResponseModels[modelName]
	return ok
}
