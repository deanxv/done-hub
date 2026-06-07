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
	// 缓存创建：扁平与嵌套 ephemeral_*_input_tokens 互斥（Anthropic 二选一上报），
	// 由 GetCacheCreationTotalTokens 统一取实际有值那一形式的总数。谁的总数大就
	// 整体覆盖（扁平字段与嵌套指针一起搬，避免只搬一边丢字段）。
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
	usage.PromptTokensDetails.CachedWriteTokens = cacheCreationTokens
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
