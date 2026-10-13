package codex

import (
	_ "embed"
	"encoding/json"
	"strings"
)

// defaultCodexInstructions 是 GPT-5-Codex 系列（含旧版 codex 模型）的默认 base prompt。
//
//go:embed instructions_default.txt
var defaultCodexInstructions string

//go:embed instructions_gpt5_1.txt
var instructionsGPT51 string

//go:embed instructions_gpt5_2.txt
var instructionsGPT52 string

//go:embed instructions_gpt5_5.txt
var instructionsGPT55 string

//go:embed instructions_gpt6_astra.txt
var instructionsGPT6Astra string

// codexGPT61SolMetadata 是官方 openai/codex 仓库 codex-rs/models-manager/models.json
// 中 gpt-6.1-sol 的完整描述，用于读取 instructions_template。
//
//go:embed codex_gpt61_sol.json
var codexGPT61SolMetadata []byte

// instructionsGPT61Sol 在包初始化时解析一次，避免每个请求重复反序列化 65KB 的 JSON。
var instructionsGPT61Sol = parseInstructionsTemplate(codexGPT61SolMetadata)

func parseInstructionsTemplate(metadata []byte) string {
	var meta struct {
		ModelMessages struct {
			InstructionsTemplate string `json:"instructions_template"`
		} `json:"model_messages"`
	}
	if err := json.Unmarshal(metadata, &meta); err != nil {
		return ""
	}
	return meta.ModelMessages.InstructionsTemplate
}

// latestCodexInstructions 返回目前已知最新的 Codex base instructions（当前为 GPT-5.5）。
// 若 GPT-5.5 prompt 意外为空则回退到 defaultCodexInstructions。
func latestCodexInstructions() string {
	if v := strings.TrimSpace(instructionsGPT55); v != "" {
		return instructionsGPT55
	}
	return defaultCodexInstructions
}

// CodexBaseInstructionsForModel 按模型返回最匹配的真实 Codex base instructions：
//   - gpt-6.1-sol（含 effort/compact 变体）→ 官方 models.json instructions_template
//   - gpt-6 / gpt-6-*（astra/sol/luna 等）→ GPT-6 Astra prompt（sol/luna 官方 prompt 与之同源但不完全一致，取最接近者）
//   - 含 "codex" 的模型（gpt-5-codex / spark 等）→ GPT-5-Codex prompt
//   - gpt-5.5 系非 codex 模型 → GPT-5.5 prompt
//   - gpt-5.2 系 → GPT-5.2 prompt
//   - gpt-5.1 系 → GPT-5.1 prompt
//   - 其它（gpt-5.3 / gpt-5.4 / 裸 gpt-5 / 未知）→ 回退到最新版本（当前 GPT-5.5）
//
// 任一专用 prompt 意外为空时回退链最终落到 defaultCodexInstructions，保证返回非空。
func CodexBaseInstructionsForModel(model string) string {
	canonical := canonicalizeModelSpelling(model)
	switch {
	case isGPT61SolSpelling(canonical):
		if v := strings.TrimSpace(instructionsGPT61Sol); v != "" {
			return instructionsGPT61Sol
		}
	case canonical == "gpt-6" || strings.HasPrefix(canonical, "gpt-6-"):
		if v := strings.TrimSpace(instructionsGPT6Astra); v != "" {
			return instructionsGPT6Astra
		}
	case strings.Contains(canonical, "codex"):
		return defaultCodexInstructions
	case strings.HasPrefix(canonical, "gpt-5.5"):
		return latestCodexInstructions()
	case strings.HasPrefix(canonical, "gpt-5.2"):
		if v := strings.TrimSpace(instructionsGPT52); v != "" {
			return instructionsGPT52
		}
	case strings.HasPrefix(canonical, "gpt-5.1"):
		if v := strings.TrimSpace(instructionsGPT51); v != "" {
			return instructionsGPT51
		}
	}
	return latestCodexInstructions()
}

// canonicalizeModelSpelling 剥离供应商前缀、统一大小写和分隔符。
func canonicalizeModelSpelling(model string) string {
	model = strings.TrimSpace(model)
	if slash := strings.LastIndexByte(model, '/'); slash >= 0 {
		model = strings.TrimSpace(model[slash+1:])
	}
	model = strings.ToLower(model)
	model = strings.ReplaceAll(model, "_", "-")
	model = strings.Join(strings.Fields(model), "-")
	for strings.Contains(model, "--") {
		model = strings.ReplaceAll(model, "--", "-")
	}
	return model
}

// isGPT61SolSpelling 识别 gpt-6.1-sol 及其 effort/compact 变体。
func isGPT61SolSpelling(canonical string) bool {
	if canonical == "gpt-6.1-sol" {
		return true
	}
	suffix, ok := strings.CutPrefix(canonical, "gpt-6.1-sol-")
	if !ok {
		return false
	}
	switch suffix {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max", "openai-compact":
		return true
	}
	return false
}
