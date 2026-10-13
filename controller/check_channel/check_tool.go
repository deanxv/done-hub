package check_channel

import (
	"done-hub/types"
	"encoding/json"
	"fmt"
	"strings"
)

type CheckToolProcess struct {
	ModelName string
}

func CreateCheckToolProcess(modelName string) *CheckToolProcess {
	return &CheckToolProcess{ModelName: modelName}
}

const toolProbeKey = "tool"

func (c *CheckToolProcess) GetKey() string {
	return toolProbeKey
}

func intPairSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"a": map[string]any{"type": "integer"},
			"b": map[string]any{"type": "integer"},
		},
		"required": []string{"a", "b"},
	}
}

func (c *CheckToolProcess) GetRequest() *types.ChatCompletionRequest {
	return &types.ChatCompletionRequest{
		Model: c.ModelName,
		Messages: []types.ChatCompletionMessage{
			{Role: types.ChatMessageRoleUser, Content: "What is 3 * 12, and what is 11 + 49? You must use the provided tools; do not compute the answers yourself."},
		},
		Tools: []*types.ChatCompletionTool{
			{Type: "function", Function: types.ChatCompletionFunction{Name: "add", Description: "Adds a and b.", Parameters: intPairSchema()}},
			{Type: "function", Function: types.ChatCompletionFunction{Name: "multiply", Description: "Multiplies a and b.", Parameters: intPairSchema()}},
		},
	}
}

type expectedCall struct {
	name string
	a, b int
}

func (e expectedCall) String() string {
	return fmt.Sprintf("%s(%d, %d)", e.name, e.a, e.b)
}

var expectedToolCalls = []expectedCall{{"multiply", 3, 12}, {"add", 11, 49}}

func (c *CheckToolProcess) Check(_ CheckContext, _ *types.ChatCompletionRequest, resp *types.ChatCompletionResponse, openaiErr *types.OpenAIError) []*CheckResult {
	if openaiErr != nil {
		return failedResponse(openaiErr)
	}

	result := &CheckResult{Name: "工具调用", Status: CheckStatusFailed}
	if resp == nil || len(resp.Choices) == 0 {
		result.Remark = "未返回响应内容"
		return []*CheckResult{result}
	}
	calls := resp.Choices[0].Message.ToolCalls
	if len(calls) == 0 {
		result.Remark = "没有调用任何函数"
		return []*CheckResult{result}
	}

	got := make(map[expectedCall]bool, len(calls))
	gotNames := make([]string, 0, len(calls))
	for _, call := range calls {
		if call == nil || call.Function == nil {
			continue
		}
		// 用 float64 接收：部分上游（如 Gemini 转换）会把整数参数输出成 3.0
		var args struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			gotNames = append(gotNames, call.Function.Name+"(参数无法解析)")
			continue
		}
		ec := expectedCall{name: call.Function.Name, a: int(args.A), b: int(args.B)}
		gotNames = append(gotNames, ec.String())
		// 加法和乘法可交换，add(49, 11) 同样正确
		if ec.a > ec.b {
			ec.a, ec.b = ec.b, ec.a
		}
		got[ec] = true
	}

	matched := 0
	for _, want := range expectedToolCalls {
		if got[want] {
			matched++
		}
	}
	summary := strings.Join(gotNames, "、")
	switch matched {
	case len(expectedToolCalls):
		result.Status = CheckStatusSuccess
		result.Remark = "并行调用正确：" + summary
	case 0:
		result.Remark = "函数名或参数不正确：" + summary
	default:
		// 只调了一个也算支持函数调用，只是没有并行
		result.Status = CheckStatusWarning
		result.Remark = fmt.Sprintf("只正确调用了 %d/%d 个函数（不支持并行调用）：%s", matched, len(expectedToolCalls), summary)
	}
	return []*CheckResult{result}
}
