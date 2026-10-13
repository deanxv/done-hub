package check_channel

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
)

// 随机算术 challenge：用 few-shot 模板让模型完成一道简单算术题。
// 每次题目随机，伪造响应字段的中转无法通过，只有真实调用了可用模型才能答对。
const challengePromptTemplate = `Calculate and respond with ONLY the number, nothing else.

Q: 3 + 5 = ?
A: 8

Q: 12 - 7 = ?
A: 5

Q: %d %s %d = ?
A:`

const (
	challengeMin = 1
	challengeMax = 50
)

var challengeNumberRegex = regexp.MustCompile(`-?\d+`)

type challenge struct {
	Prompt   string
	Expected string
}

func generateChallenge() challenge {
	a := challengeMin + rand.IntN(challengeMax-challengeMin+1)
	b := challengeMin + rand.IntN(challengeMax-challengeMin+1)
	if rand.IntN(2) == 0 {
		return challenge{Prompt: fmt.Sprintf(challengePromptTemplate, a, "+", b), Expected: strconv.Itoa(a + b)}
	}
	// 减法用大减小，保证结果非负
	if b > a {
		a, b = b, a
	}
	return challenge{Prompt: fmt.Sprintf(challengePromptTemplate, a, "-", b), Expected: strconv.Itoa(a - b)}
}

// validateChallenge 取回答中最后一个整数比对，容忍模型多输出了少量文字。
// 回显题目的中转会带上 few-shot 答案和操作数，可能碰巧等于期望值：含题目问号的回答直接判错，且不做任意位置命中
func validateChallenge(answer, expected string) bool {
	if expected == "" || strings.Contains(answer, "= ?") {
		return false
	}
	matches := challengeNumberRegex.FindAllString(answer, -1)
	return len(matches) > 0 && matches[len(matches)-1] == expected
}
