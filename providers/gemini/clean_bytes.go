package gemini

import (
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// 客户端 round-trip 回来的合法 Gemini thoughtSignature 是 base64 不透明值，长度 >> 50。
// 用作判断"是否值得透传"的下限——更短的字符串大概率是损坏值或 Anthropic 的 thinking_signature
// 混入，对官方 Gemini API 没有意义。
const minThoughtSignatureLength = 50

// CleanGeminiRequestBytes 在字节层面清理 Gemini 请求数据中的不兼容字段
// 使用 gjson/sjson 直接操作字节，避免对含 base64 图片的大请求做完整 json.Unmarshal/Marshal
func CleanGeminiRequestBytes(data []byte, isVertexAI bool) ([]byte, error) {
	var err error

	// 单次遍历完成 contents 的所有清洗（原 step 1/2/3 合并）
	data, err = cleanContentsBytes(data)
	if err != nil {
		return nil, err
	}

	// tools 清洗（独立 key，数据量小，无需合并）
	data, err = cleanToolsBytes(data, isVertexAI)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// setOp 表示一个 sjson.SetBytes 操作
type setOp struct {
	path  string
	value string
}

// setRawOp 表示一个 sjson.SetRawBytes 操作
type setRawOp struct {
	path  string
	value []byte
}

// cleanContentsBytes 单次遍历完成所有 contents 清洗
// Collect-Then-Apply 模式：一次 gjson.GetBytes 解析，收集所有变更路径，最后批量 sjson 写入
//
// 合并了以下三个原独立函数：
//   - validateAndFixFunctionCallSequenceBytes（step 1）
//   - deleteFunctionIdsBytes（step 2）
//   - ensureContentRolesBytes（step 3）
//
// 历史上还有 step 5：给缺失 thoughtSignature 的 model functionCall part 注入哨兵值
// "skip_thought_signature_validator"。该哨兵仅对 Antigravity 网关有效，官方 Gemini / Vertex
// 会判定为非法签名并返回 400 INVALID_ARGUMENT。Antigravity 路径在
// providers/antigravity/chat.go 中有自己的 applyThinkingSignatureSentinel 注入，
// 因此此处不再统一注入；客户端提供的合法签名按原样透传。
func cleanContentsBytes(data []byte) ([]byte, error) {
	contents := gjson.GetBytes(data, "contents")
	if !contents.Exists() {
		return data, nil
	}

	contentsArr := contents.Array()
	n := len(contentsArr)

	// 收集所有待执行的变更
	var pathsToDelete []string
	var pathsToSet []setOp
	var pathsToSetRaw []setRawOp
	fixedTurns := make(map[int]bool) // step1 整体替换 parts 的 turn，step2 跳过其 parts

	for i := 0; i < n; i++ {
		content := contentsArr[i]
		roleResult := content.Get("role")
		role := roleResult.String()

		// ── Step 3: 确保 role 存在 ──
		if !roleResult.Exists() {
			pathsToSet = append(pathsToSet, setOp{
				path:  fmt.Sprintf("contents.%d.role", i),
				value: "user",
			})
		}

		// ── Step 1: 验证函数调用序列（仅 model turn，且非最后一个 turn） ──
		if role == "model" && i < n-1 {
			var callNames []string
			for _, part := range content.Get("parts").Array() {
				for _, field := range []string{"functionCall", "function_call"} {
					if name := part.Get(field + ".name").String(); name != "" {
						callNames = append(callNames, name)
					}
				}
			}

			if len(callNames) > 0 {
				next := contentsArr[i+1]
				if next.Get("role").String() != "model" {
					if fix := buildFunctionCallFix(callNames, next, i+1); fix != nil {
						pathsToSetRaw = append(pathsToSetRaw, *fix)
						fixedTurns[i+1] = true // 标记：该 turn 的 parts 将被整体替换，step2 跳过
					}
				}
			}
		}

		// ── Step 2: 遍历 parts 删除 functionCall/functionResponse 的 id ──
		// 跳过被 step1 整体替换 parts 的 turn（收集的 id 路径会被覆盖）
		if fixedTurns[i] {
			continue
		}

		parts := content.Get("parts")
		if !parts.Exists() {
			continue
		}

		for j, part := range parts.Array() {
			for _, field := range []string{"functionCall", "function_call", "functionResponse", "function_response"} {
				if part.Get(field + ".id").Exists() {
					pathsToDelete = append(pathsToDelete,
						fmt.Sprintf("contents.%d.parts.%d.%s.id", i, j, field))
				}
			}
		}
	}

	// ── Batch Apply：批量执行所有变更 ──
	// 执行顺序：SetRaw（step1 整体替换 parts）→ Delete（step2 删 id）→ Set（step3 role）
	var err error

	for _, op := range pathsToSetRaw {
		data, err = sjson.SetRawBytes(data, op.path, op.value)
		if err != nil {
			return nil, err
		}
	}

	for _, path := range pathsToDelete {
		data, _ = sjson.DeleteBytes(data, path)
	}

	for _, op := range pathsToSet {
		data, err = sjson.SetBytes(data, op.path, op.value)
		if err != nil {
			return nil, err
		}
	}

	return data, nil
}

// buildFunctionCallFix 检查 model turn 的 functionCall 与下一个 turn 的 functionResponse 是否匹配
// 匹配则返回 nil（无需修复）；不匹配则构建修复后的 parts 数据
func buildFunctionCallFix(callNames []string, next gjson.Result, turnIndex int) *setRawOp {
	// 提取 functionResponse names
	var respNames []string
	for _, part := range next.Get("parts").Array() {
		for _, field := range []string{"functionResponse", "function_response"} {
			if name := part.Get(field + ".name").String(); name != "" {
				respNames = append(respNames, name)
			}
		}
	}

	// 构建频次 map 并检查是否匹配
	callFreq := make(map[string]int)
	for _, name := range callNames {
		callFreq[name]++
	}
	respFreq := make(map[string]int)
	for _, name := range respNames {
		respFreq[name]++
	}

	matched := true
	for name, cnt := range callFreq {
		if respFreq[name] != cnt {
			matched = false
			break
		}
	}
	if matched {
		for name, cnt := range respFreq {
			if callFreq[name] != cnt {
				matched = false
				break
			}
		}
	}
	if matched {
		return nil
	}

	// 不匹配 → 仅 unmarshal 下一个 turn 的 parts（小对象，不含图片）
	partsRaw := next.Get("parts").Raw
	if partsRaw == "" {
		return nil
	}
	var partsData []interface{}
	if err := json.Unmarshal([]byte(partsRaw), &partsData); err != nil {
		return nil
	}

	// 裁剪：移除没有对应 call 的多余 response
	trimCallFreq := make(map[string]int)
	for k, v := range callFreq {
		trimCallFreq[k] = v
	}
	var fixedParts []interface{}
	for _, part := range partsData {
		if partMap, ok := part.(map[string]interface{}); ok {
			if name, ok := getFunctionResponseName(partMap); ok {
				if trimCallFreq[name] > 0 {
					trimCallFreq[name]--
					fixedParts = append(fixedParts, part)
				}
				continue
			}
		}
		fixedParts = append(fixedParts, part)
	}

	// 补齐：为缺少 response 的 call 补充空响应
	fieldName := detectResponseFieldStyle(fixedParts)
	for _, callName := range callNames {
		if trimCallFreq[callName] > 0 {
			trimCallFreq[callName]--
			fixedParts = append(fixedParts, map[string]interface{}{
				fieldName: map[string]interface{}{
					"name": callName,
					"response": map[string]interface{}{
						"output": "",
					},
				},
			})
		}
	}

	// marshal 修复后的 parts
	fixedPartsBytes, err := json.Marshal(fixedParts)
	if err != nil {
		return nil
	}

	return &setRawOp{
		path:  fmt.Sprintf("contents.%d.parts", turnIndex),
		value: fixedPartsBytes,
	}
}

// CleanToolsBytesOnly 仅执行 tools 数组的清理步骤
// 用于跨 provider 重试的增量清理：当已有 Gemini-cleaned bytes 需要适配 VertexAI 时，
// 无需重新从 raw bytes 执行全部清理，只需增量执行此步骤即可
// （contents 清洗与 isVertexAI 无关，已在首次清理中完成）
func CleanToolsBytesOnly(data []byte, isVertexAI bool) ([]byte, error) {
	return cleanToolsBytes(data, isVertexAI)
}

// cleanToolsBytes 清理 tools 数组中 Gemini API 不支持的字段
// tools 数组很小（无 base64），直接 unmarshal → 清理 → marshal → sjson 写回
func cleanToolsBytes(data []byte, isVertexAI bool) ([]byte, error) {
	tools := gjson.GetBytes(data, "tools")
	if !tools.Exists() || !tools.IsArray() {
		return data, nil
	}

	var toolsArr []interface{}
	if err := json.Unmarshal([]byte(tools.Raw), &toolsArr); err != nil {
		return data, nil
	}

	var validTools []interface{}
	for _, tool := range toolsArr {
		toolMap, ok := tool.(map[string]interface{})
		if !ok {
			continue
		}

		if isVertexAI {
			delete(toolMap, "tool_type")
			delete(toolMap, "toolType")
			delete(toolMap, "type")
		}

		if functionDeclarations, ok := toolMap["functionDeclarations"].([]interface{}); ok {
			for _, funcDecl := range functionDeclarations {
				if funcDeclMap, ok := funcDecl.(map[string]interface{}); ok {
					delete(funcDeclMap, "strict")
					if parameters, ok := funcDeclMap["parameters"].(map[string]interface{}); ok {
						delete(parameters, "$schema")
						cleanSchemaRecursively(parameters)
					}
				}
			}

			if len(functionDeclarations) == 0 {
				continue
			}
		}

		hasValidContent := false
		for key, value := range toolMap {
			if key == "functionDeclarations" {
				if arr, ok := value.([]interface{}); ok && len(arr) > 0 {
					hasValidContent = true
					break
				}
			} else if value != nil {
				hasValidContent = true
				break
			}
		}

		if hasValidContent {
			validTools = append(validTools, toolMap)
		}
	}

	if len(validTools) == 0 {
		data, _ = sjson.DeleteBytes(data, "tools")
	} else {
		cleanedToolsBytes, err := json.Marshal(validTools)
		if err != nil {
			return data, nil
		}
		data, err = sjson.SetRawBytes(data, "tools", cleanedToolsBytes)
		if err != nil {
			return nil, err
		}
	}

	return data, nil
}
