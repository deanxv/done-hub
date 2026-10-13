package check_channel

import (
	"done-hub/types"
	"fmt"
	"strings"
)

type CheckErrorProcess struct {
	ModelName string
}

func CreateCheckErrorProcess(modelName string) *CheckErrorProcess {
	return &CheckErrorProcess{ModelName: modelName}
}

const errorProbeKey = "relay"

func (c *CheckErrorProcess) GetKey() string {
	return errorProbeKey
}

// GetRequest 故意发送非法角色，触发上游报错
func (c *CheckErrorProcess) GetRequest() *types.ChatCompletionRequest {
	return &types.ChatCompletionRequest{
		Model: c.ModelName,
		Messages: []types.ChatCompletionMessage{
			{Role: types.ChatMessageRoleUser, Content: "hi"},
			{Role: "user11", Content: "hi"},
		},
	}
}

// Check 统计错误信息中 one-api 系程序追加的 request id 数量。
// 这一项只是线索：中转可以改写错误信息，也可以在转发前清洗请求，所以永远不判失败，也不参与模型总体结论。
// 数到 0 个判通过，数到 1 个以上判提示，让「疑似经过中转」被标出来。
func (c *CheckErrorProcess) Check(ctx CheckContext, _ *types.ChatCompletionRequest, _ *types.ChatCompletionResponse, openaiErr *types.OpenAIError) []*CheckResult {
	if ctx.RewritesUnknownRole {
		// done-hub 转 Claude / Gemini 时会把非法角色当成 user，请求本身就是合法的，触发不了上游报错
		return []*CheckResult{{Name: "request id", Status: CheckStatusSkipped, Remark: "此渠道经 done-hub 转为 Claude / Gemini 原生请求，非法角色会被转换成 user，无法触发上游报错"}}
	}
	if openaiErr == nil {
		return []*CheckResult{{Name: "request id", Status: CheckStatusWarning, Remark: "非法请求未报错，上游或中转可能清洗了请求"}}
	}

	count := strings.Count(strings.ToLower(openaiErr.Message), "request id:")
	if count == 0 {
		return []*CheckResult{{Name: "request id", Status: CheckStatusSuccess, Remark: "错误信息中没有 request id，未发现 one-api 系中转（可被改写，仅供参考）"}}
	}
	return []*CheckResult{{
		Name:   "request id",
		Status: CheckStatusWarning,
		Remark: fmt.Sprintf("错误信息中有 %d 个 request id，约等于经过的 one-api 系中转层数（可被改写，仅供参考）", count),
	}}
}
