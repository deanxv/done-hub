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

// ShouldUseResponsesForModel 判断模型是否应走 chat→Responses 兼容路径。三值语义：
//   - Need2ResponseModels == nil：渠道未配置，继承全局默认表 DefaultNeed2ResponseModels（对齐改造前的 need2Response 行为）
//   - Need2ResponseModels == ""：渠道显式清空，该渠道不做转换
//   - 其他：按渠道自定义列表路由（全量覆盖，不与默认表合并）
func (channel *Channel) ShouldUseResponsesForModel(model string) bool {
	modelName := strings.TrimSpace(model)
	if modelName == "" {
		return false
	}

	if channel.Need2ResponseModels == nil {
		_, ok := config.DefaultNeed2ResponseModels[modelName]
		return ok
	}

	if strings.TrimSpace(*channel.Need2ResponseModels) == "" {
		return false
	}

	modelSet := config.BuildNeed2ResponseModelSet(
		config.ParseNeed2ResponseModels(*channel.Need2ResponseModels),
	)
	_, ok := modelSet[modelName]
	return ok
}
