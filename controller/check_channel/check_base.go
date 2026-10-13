package check_channel

import (
	"done-hub/common/config"
	"done-hub/types"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// 快照后缀：2024-08-06 或 0613 这类日期
var snapshotSuffixPattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}|\d{4})$`)

type CheckBaseProcess struct {
	ModelName string
	challenge challenge
}

func CreateCheckBaseProcess(modelName string) *CheckBaseProcess {
	return &CheckBaseProcess{ModelName: modelName, challenge: generateChallenge()}
}

const baseProbeKey = "base"

func (c *CheckBaseProcess) GetKey() string {
	return baseProbeKey
}

func (c *CheckBaseProcess) GetRequest() *types.ChatCompletionRequest {
	return &types.ChatCompletionRequest{
		Model: c.ModelName,
		Messages: []types.ChatCompletionMessage{
			{Role: types.ChatMessageRoleUser, Content: c.challenge.Prompt},
		},
	}
}

func (c *CheckBaseProcess) Check(_ CheckContext, req *types.ChatCompletionRequest, resp *types.ChatCompletionResponse, openaiErr *types.OpenAIError) []*CheckResult {
	if openaiErr != nil {
		return failedResponse(openaiErr)
	}
	if resp == nil {
		return []*CheckResult{{Name: "响应", Status: CheckStatusFailed, Remark: "未返回响应内容"}}
	}

	results := []*CheckResult{c.checkChallenge(resp), checkUsage(resp)}
	// 用映射后的上游模型名推断官方格式：渠道别名推断不出上游类型
	channelType := getChannelTypeByModelName(req.Model)
	results = append(results, checkResponseModel(channelType, req.Model, resp.Model), checkResponseID(channelType, resp.ID))
	return results
}

// checkChallenge 是唯一能证明「真实调用了可用模型」的项：题目随机，伪造响应无法答对
func (c *CheckBaseProcess) checkChallenge(resp *types.ChatCompletionResponse) *CheckResult {
	result := &CheckResult{Name: "算术校验", Status: CheckStatusFailed}
	answer, ok := firstChoiceContent(resp)
	switch {
	case !ok:
		result.Remark = "未返回响应内容"
	case validateChallenge(answer, c.challenge.Expected):
		result.Status = CheckStatusSuccess
		result.Remark = fmt.Sprintf("期望 %s，回答正确", c.challenge.Expected)
	default:
		result.Remark = fmt.Sprintf("期望 %s，实际回答：%s", c.challenge.Expected, truncate(answer, 200))
	}
	return result
}

func checkUsage(resp *types.ChatCompletionResponse) *CheckResult {
	if resp.Usage == nil || resp.Usage.PromptTokens <= 0 || resp.Usage.CompletionTokens <= 0 {
		return &CheckResult{Name: "用量", Status: CheckStatusWarning, Remark: "上游未返回有效用量，计费将依赖本地估算"}
	}
	return &CheckResult{
		Name:   "用量",
		Status: CheckStatusSuccess,
		Remark: fmt.Sprintf("输入 %d tokens，输出 %d tokens", resp.Usage.PromptTokens, resp.Usage.CompletionTokens),
	}
}

// 响应模型名和 ID 都是中转可任意改写的字段，只作参考，不判定失败
func checkResponseModel(channelType int, requestModel, responseModel string) *CheckResult {
	result := &CheckResult{Name: "响应模型", Status: CheckStatusWarning}
	if responseModel == "" {
		result.Remark = "响应中没有模型名"
		return result
	}

	consistent := responseModel == requestModel
	if !consistent && channelType == config.ChannelTypeOpenAI {
		// OpenAI 对不带版本号的别名会返回带日期的快照名，如 gpt-4o -> gpt-4o-2024-08-06。
		// 只接受「别名-日期」，避免 gpt-4o-mini 冒充 gpt-4o
		if suffix, ok := strings.CutPrefix(responseModel, requestModel+"-"); ok {
			consistent = snapshotSuffixPattern.MatchString(suffix)
		}
	}

	if consistent {
		result.Status = CheckStatusSuccess
		result.Remark = responseModel
	} else {
		result.Remark = fmt.Sprintf("请求 %s，响应 %s（Azure、模型映射或上游别名都可能导致不一致）", requestModel, responseModel)
	}
	return result
}

// Codex 及 chat 转 responses 的 OpenAI 渠道返回的是 Responses API 的 resp_ ID
var officialIDPrefixes = map[int][]string{
	config.ChannelTypeOpenAI:    {"chatcmpl-", "resp_"},
	config.ChannelTypeAnthropic: {"msg_"},
}

func checkResponseID(channelType int, id string) *CheckResult {
	prefixes, ok := officialIDPrefixes[channelType]
	switch {
	case id == "":
		return &CheckResult{Name: "响应 ID", Status: CheckStatusWarning, Remark: "响应中没有 ID，部分上游或中转不返回"}
	case !ok || slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(id, p) }):
		return &CheckResult{Name: "响应 ID", Status: CheckStatusSuccess, Remark: id}
	default:
		return &CheckResult{Name: "响应 ID", Status: CheckStatusWarning, Remark: fmt.Sprintf("%s（官方 ID 以 %s 开头）", id, strings.Join(prefixes, " 或 "))}
	}
}

// checkLatency：成功但耗时超过阈值视为响应慢
func checkLatency(latency time.Duration) *CheckResult {
	if latency >= slowThreshold {
		return &CheckResult{Name: "耗时", Status: CheckStatusWarning, Remark: fmt.Sprintf("%.2f 秒，响应较慢", latency.Seconds())}
	}
	return &CheckResult{Name: "耗时", Status: CheckStatusSuccess, Remark: fmt.Sprintf("%.2f 秒", latency.Seconds())}
}

func truncate(s string, limit int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

// getChannelTypeByModelName 按模型名推断官方上游，用于判断响应字段的官方格式
func getChannelTypeByModelName(modelName string) int {
	switch {
	case strings.HasPrefix(modelName, "gpt-"), strings.HasPrefix(modelName, "chatgpt-"),
		strings.HasPrefix(modelName, "o1"), strings.HasPrefix(modelName, "o3"), strings.HasPrefix(modelName, "o4"):
		// -all / -realtime / -instruct 是第三方或非 chat 变体，响应格式不按官方判定
		if strings.Contains(modelName, "-all") || strings.Contains(modelName, "-realtime") || strings.Contains(modelName, "-instruct") {
			return 0
		}
		return config.ChannelTypeOpenAI
	case strings.HasPrefix(modelName, "claude-"):
		return config.ChannelTypeAnthropic
	case strings.HasPrefix(modelName, "gemini-"):
		return config.ChannelTypeGemini
	}
	return 0
}
