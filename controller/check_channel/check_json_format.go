package check_channel

import (
	"done-hub/types"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

type CheckJsonFormatProcess struct {
	ModelName string
}

func CreateCheckJsonFormatProcess(modelName string) *CheckJsonFormatProcess {
	return &CheckJsonFormatProcess{ModelName: modelName}
}

const jsonProbeKey = "json"

func (c *CheckJsonFormatProcess) GetKey() string {
	return jsonProbeKey
}

const jsonExpectedAnswer = -3.75

func (c *CheckJsonFormatProcess) GetRequest() *types.ChatCompletionRequest {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"steps": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"explanation": map[string]any{"type": "string"},
						"output":      map[string]any{"type": "string"},
					},
					"required":             []string{"explanation", "output"},
					"additionalProperties": false,
				},
			},
			"final_answer": map[string]any{"type": "string"},
		},
		"required":             []string{"steps", "final_answer"},
		"additionalProperties": false,
	}

	return &types.ChatCompletionRequest{
		Model: c.ModelName,
		Messages: []types.ChatCompletionMessage{
			{Role: types.ChatMessageRoleSystem, Content: "You are a helpful math tutor. Guide the user through the solution step by step."},
			{Role: types.ChatMessageRoleUser, Content: "Solve 8x + 7 = -23. Put only the value of x in final_answer."},
		},
		ResponseFormat: &types.ChatCompletionResponseFormat{
			Type: "json_schema",
			JsonSchema: &types.FormatJsonSchema{
				Name:   "math_reasoning",
				Schema: schema,
				Strict: true,
			},
		},
	}
}

var jsonCodeFence = regexp.MustCompile("(?s)^```(?:json)?\\s*(.*?)\\s*```$")
var decimalPattern = regexp.MustCompile(`-?\d+(?:\.\d+)?(?:/\d+)?`)

type mathReasoning struct {
	Steps []struct {
		Explanation string `json:"explanation"`
		Output      string `json:"output"`
	} `json:"steps"`
	FinalAnswer *string `json:"final_answer"`
}

// claudeJSONNote 解释 Claude 转换路径下的 JSON 结果：转换会丢弃 response_format，
// 结果反映的是 done-hub 的转换而不是上游能力
const claudeJSONNote = "此渠道经 done-hub 转为 Claude 原生请求，response_format 不会传给上游（与 Anthropic 官方 OpenAI 兼容层一致），本项不代表上游能力"

func (c *CheckJsonFormatProcess) Check(ctx CheckContext, _ *types.ChatCompletionRequest, resp *types.ChatCompletionResponse, openaiErr *types.OpenAIError) []*CheckResult {
	if openaiErr != nil {
		return failedResponse(openaiErr)
	}

	result := &CheckResult{Name: "JSON Schema", Status: CheckStatusFailed}
	content, ok := firstChoiceContent(resp)
	if !ok {
		result.Remark = "未返回响应内容"
		return []*CheckResult{result}
	}

	content = strings.TrimSpace(content)
	fenced := false
	if m := jsonCodeFence.FindStringSubmatch(content); m != nil {
		content, fenced = m[1], true
	}

	var parsed mathReasoning
	err := json.Unmarshal([]byte(content), &parsed)
	if ctx.ClaudeConversion && (err != nil || parsed.FinalAnswer == nil || len(parsed.Steps) == 0) {
		// 上游根本没收到 schema 约束，不按 schema 作答是预期行为，不能判失败
		return []*CheckResult{{Name: "JSON Schema", Status: CheckStatusSkipped, Remark: claudeJSONNote}}
	}
	if err != nil {
		result.Remark = "返回内容不是合法 JSON：" + truncate(content, 120)
		return []*CheckResult{result}
	}
	if parsed.FinalAnswer == nil || len(parsed.Steps) == 0 {
		result.Remark = "JSON 缺少 schema 要求的 steps 或 final_answer 字段"
		return []*CheckResult{result}
	}
	if !answerEquals(*parsed.FinalAnswer, jsonExpectedAnswer) {
		result.Status = CheckStatusWarning
		result.Remark = fmt.Sprintf("结构正确，但答案 %s 与期望 %v 不符", truncate(*parsed.FinalAnswer, 60), jsonExpectedAnswer)
		return []*CheckResult{result}
	}

	result.Status = CheckStatusSuccess
	result.Remark = fmt.Sprintf("符合 schema，%d 个步骤，答案正确", len(parsed.Steps))
	if ctx.ClaudeConversion {
		// 模型自发输出了 JSON，但没有 schema 约束，不能据此认定渠道支持结构化输出
		result.Status = CheckStatusWarning
		result.Remark += "。" + claudeJSONNote
		return []*CheckResult{result}
	}
	if fenced {
		// 原生 json_schema 不会包代码块，出现代码块说明上游是靠提示词模拟的
		result.Status = CheckStatusWarning
		result.Remark += "（被代码块包裹，上游可能不支持原生 json_schema）"
	}
	return []*CheckResult{result}
}

// answerEquals 接受 -3.75、-15/4、x = -3.75 等写法
func answerEquals(answer string, want float64) bool {
	for _, m := range decimalPattern.FindAllString(answer, -1) {
		var v float64
		if num, den, isFrac := strings.Cut(m, "/"); isFrac {
			n, err1 := strconv.ParseFloat(num, 64)
			d, err2 := strconv.ParseFloat(den, 64)
			if err1 != nil || err2 != nil || d == 0 {
				continue
			}
			v = n / d
		} else {
			f, err := strconv.ParseFloat(m, 64)
			if err != nil {
				continue
			}
			v = f
		}
		if math.Abs(v-want) < 1e-9 {
			return true
		}
	}
	return false
}
