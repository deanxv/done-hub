package gemini

import (
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// CleanGeminiRequestBytes 在字节层面清理 Gemini 请求数据中的不兼容字段
// 使用 gjson/sjson 直接操作字节，避免对含 base64 图片的大请求做完整 json.Unmarshal/Marshal
func CleanGeminiRequestBytes(data []byte, isVertexAI bool) ([]byte, error) {
	var err error

	// 1. 验证和修复函数调用序列
	data, err = validateAndFixFunctionCallSequenceBytes(data)
	if err != nil {
		return nil, err
	}

	// 2. 删除 functionCall/functionResponse 中的 id 字段
	data, err = deleteFunctionIdsBytes(data)
	if err != nil {
		return nil, err
	}

	// 3. 确保每个 content 都有 role 字段
	data, err = ensureContentRolesBytes(data)
	if err != nil {
		return nil, err
	}

	// 4. 清理 tools 数组（小对象，无 base64）
	data, err = cleanToolsBytes(data, isVertexAI)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// validateAndFixFunctionCallSequenceBytes 验证和修复函数调用序列
// 用 gjson 读取 functionCall/functionResponse name（不为 base64 分配内存）
// 已匹配（常见情况）→ 跳过，零开销
// 不匹配 → 仅 unmarshal 下一个 turn 的 parts（小，不含图片），修复后写回
func validateAndFixFunctionCallSequenceBytes(data []byte) ([]byte, error) {
	contents := gjson.GetBytes(data, "contents")
	if !contents.Exists() {
		return data, nil
	}

	contentsArr := contents.Array()
	n := len(contentsArr)

	// 注意：contentsArr 是原始 data 的快照。循环中 sjson 修改 data 不影响 contentsArr 的读取，
	// 因为被修改的 turn（i+1）role != "model"，不会在后续迭代中作为 model turn 被重新检查。
	for i := 0; i < n-1; i++ {
		content := contentsArr[i]
		role := content.Get("role").String()
		if role != "model" {
			continue
		}

		// 提取 functionCall names
		var callNames []string
		for _, part := range content.Get("parts").Array() {
			for _, field := range []string{"functionCall", "function_call"} {
				name := part.Get(field + ".name").String()
				if name != "" {
					callNames = append(callNames, name)
				}
			}
		}

		if len(callNames) == 0 {
			continue
		}

		// 检查下一个 turn
		next := contentsArr[i+1]
		nextRole := next.Get("role").String()
		if nextRole == "model" {
			continue
		}

		// 提取 functionResponse names
		var respNames []string
		for _, part := range next.Get("parts").Array() {
			for _, field := range []string{"functionResponse", "function_response"} {
				name := part.Get(field + ".name").String()
				if name != "" {
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
			continue
		}

		// 不匹配 → 仅 unmarshal 下一个 turn 的 parts（小对象，不含图片）
		partsRaw := next.Get("parts").Raw
		if partsRaw == "" {
			continue
		}
		var partsData []interface{}
		if err := json.Unmarshal([]byte(partsRaw), &partsData); err != nil {
			continue
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

		// marshal 修复后的 parts 并写回
		fixedPartsBytes, err := json.Marshal(fixedParts)
		if err != nil {
			continue
		}

		path := fmt.Sprintf("contents.%d.parts", i+1)
		data, err = sjson.SetRawBytes(data, path, fixedPartsBytes)
		if err != nil {
			return nil, err
		}
	}

	return data, nil
}

// deleteFunctionIdsBytes 删除 contents 中 functionCall/functionResponse 的 id 字段
// 先用 gjson 收集需要删除的路径，再用 sjson 批量删除
func deleteFunctionIdsBytes(data []byte) ([]byte, error) {
	contents := gjson.GetBytes(data, "contents")
	if !contents.Exists() {
		return data, nil
	}

	// 第一遍：收集所有需要删除的路径
	var pathsToDelete []string
	for i, content := range contents.Array() {
		parts := content.Get("parts")
		if !parts.Exists() {
			continue
		}
		for j, part := range parts.Array() {
			for _, field := range []string{"functionCall", "function_call", "functionResponse", "function_response"} {
				if part.Get(field + ".id").Exists() {
					pathsToDelete = append(pathsToDelete, fmt.Sprintf("contents.%d.parts.%d.%s.id", i, j, field))
				}
			}
		}
	}

	// 第二遍：执行删除
	for _, path := range pathsToDelete {
		data, _ = sjson.DeleteBytes(data, path)
	}
	return data, nil
}

// ensureContentRolesBytes 确保每个 content 都有 role 字段，缺少时设为 "user"
func ensureContentRolesBytes(data []byte) ([]byte, error) {
	contents := gjson.GetBytes(data, "contents")
	if !contents.Exists() {
		return data, nil
	}

	for i, content := range contents.Array() {
		if !content.Get("role").Exists() {
			path := fmt.Sprintf("contents.%d.role", i)
			var err error
			data, err = sjson.SetBytes(data, path, "user")
			if err != nil {
				return nil, err
			}
		}
	}
	return data, nil
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
