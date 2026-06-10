package claude

import (
	"done-hub/common"
	"done-hub/types"
	"strconv"
	"strings"
)

func StringErrorWrapper(err string, code string, statusCode int, localError bool) *ClaudeErrorWithStatusCode {
	claudeError := ClaudeError{
		Type: "one_hub_error",
		ErrorInfo: ClaudeErrorInfo{
			Type:    code,
			Message: err,
		},
	}

	return &ClaudeErrorWithStatusCode{
		LocalError:  localError,
		StatusCode:  statusCode,
		ClaudeError: claudeError,
	}
}

func OpenaiErrToClaudeErr(err *types.OpenAIErrorWithStatusCode) *ClaudeErrorWithStatusCode {
	if err == nil {
		return nil
	}

	var typeStr string

	switch v := err.Code.(type) {
	case string:
		typeStr = v
	case int:
		typeStr = strconv.Itoa(v)
	default:
		typeStr = "unknown"
	}

	return &ClaudeErrorWithStatusCode{
		LocalError: err.LocalError,
		StatusCode: err.StatusCode,
		ClaudeError: ClaudeError{
			Type: typeStr,
			ErrorInfo: ClaudeErrorInfo{
				Type:    err.Type,
				Message: err.Message,
			},
		},
	}
}

func ErrorToClaudeErr(err error) *ClaudeError {
	if err == nil {
		return nil
	}
	return &ClaudeError{
		Type: "one_hub_error",
		ErrorInfo: ClaudeErrorInfo{
			Type:    "internal_error",
			Message: err.Error(),
		},
	}
}

func ClaudeUsageMerge(usage *Usage, mergeUsage *Usage) {
	if usage == nil || mergeUsage == nil {
		return
	}
	if mergeUsage.InputTokens > usage.InputTokens {
		usage.InputTokens = mergeUsage.InputTokens
	}
	if mergeUsage.OutputTokens > usage.OutputTokens {
		usage.OutputTokens = mergeUsage.OutputTokens
	}
	// 缓存创建：扁平字段为权威总数，嵌套仅用于推断 1h 占比。流式 message_start /
	// message_delta 可能各报一次，按 GetCacheCreationTotalTokens 取大者整体覆盖
	// （扁平与嵌套指针一起搬，避免只搬一边导致 1h 占比错位）。
	if mergeUsage.GetCacheCreationTotalTokens() > usage.GetCacheCreationTotalTokens() {
		usage.CacheCreationInputTokens = mergeUsage.CacheCreationInputTokens
		usage.CacheCreation = mergeUsage.CacheCreation
	}
	if mergeUsage.CacheReadInputTokens > usage.CacheReadInputTokens {
		usage.CacheReadInputTokens = mergeUsage.CacheReadInputTokens
	}
}

func ClaudeUsageToOpenaiUsage(cUsage *Usage, usage *types.Usage) bool {
	if usage == nil || cUsage == nil {
		return false
	}

	if cUsage.InputTokens == 0 && cUsage.OutputTokens == 0 {
		return false
	}

	cacheCreationTokens := cUsage.GetCacheCreationTotalTokens()
	// 扁平 cache_creation_input_tokens 作为缓存写入总数的权威来源。嵌套 ephemeral_*
	// 仅用作 1h 占比的信号：取嵌套 1h 并 cap 到总数，剩余全部归入更便宜的 5m 桶。
	// 这样即使嵌套字段缺失、为 0 或与扁平不一致，总数也始终等于扁平字段，不会少算/多算。
	// 流式 message_delta 复用同一 usage 时本函数被多次调用，下面两个赋值即覆盖，
	// 不会保留上一帧的 1h 残值。
	tokens1h := 0
	if cUsage.CacheCreation != nil {
		tokens1h = cUsage.CacheCreation.Ephemeral1hInputTokens
	}
	if tokens1h > cacheCreationTokens {
		tokens1h = cacheCreationTokens
	}
	usage.PromptTokensDetails.CachedWriteTokens = cacheCreationTokens - tokens1h
	usage.PromptTokensDetails.CachedWrite1hTokens = tokens1h
	usage.PromptTokensDetails.CachedReadTokens = cUsage.CacheReadInputTokens

	usage.PromptTokens = cUsage.InputTokens + cacheCreationTokens + cUsage.CacheReadInputTokens
	usage.CompletionTokens = cUsage.OutputTokens
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	return true
}

func ClaudeOutputUsage(response *ClaudeResponse) int {
	var textMsg strings.Builder

	for _, c := range response.Content {
		if c.Type == "text" {
			textMsg.WriteString(c.Text + "\n")
		}
	}

	return common.CountTokenText(textMsg.String(), response.Model)
}
