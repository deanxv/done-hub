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

func (channel *Channel) ShouldUseResponsesForModel(model string) bool {
	modelName := strings.TrimSpace(model)
	if modelName == "" {
		return false
	}

	modelSet := config.BuildNeed2ResponseModelSet(
		config.ParseNeed2ResponseModels(channel.GetNeed2ResponseModels()),
	)
	_, ok := modelSet[modelName]
	return ok
}
