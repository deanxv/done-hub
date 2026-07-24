package bedrockmessages

import "strings"

// bedrockMantleModelMap 把常用别名映射为 mantle 端点要求的 model id 格式：
// `anthropic.` 前缀 + 无 ARN / 无 -v1:0 后缀 / 无跨区前缀（mantle 走 global 动态路由）。
// 官方支持列表见 platform.claude.com/docs/en/build-with-claude/claude-in-amazon-bedrock。
var bedrockMantleModelMap = map[string]string{
	"claude-fable-5":   "anthropic.claude-fable-5",
	"claude-opus-5":    "anthropic.claude-opus-5",
	"claude-opus-4-8":  "anthropic.claude-opus-4-8",
	"claude-opus-4-7":  "anthropic.claude-opus-4-7",
	"claude-sonnet-5":  "anthropic.claude-sonnet-5",
	"claude-haiku-4-5": "anthropic.claude-haiku-4-5",
}

// resolveMantleModelName 归一化上游 model id。
// 已带 `anthropic.` 前缀的原样返回；命中别名表的做映射；其余原样返回（允许用户直接填全名）。
func resolveMantleModelName(modelName string) string {
	if strings.HasPrefix(modelName, "anthropic.") {
		return modelName
	}
	if mapped, ok := bedrockMantleModelMap[modelName]; ok {
		return mapped
	}
	return modelName
}
