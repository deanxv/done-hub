package claudecode

import (
	"crypto/sha256"
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/requester"
	"done-hub/providers/claude"
	"done-hub/types"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// CreateChatCompletion 创建聊天完成
func (p *ClaudeCodeProvider) CreateChatCompletion(request *types.ChatCompletionRequest) (*types.ChatCompletionResponse, *types.OpenAIErrorWithStatusCode) {
	request.OneOtherArg = p.GetOtherArg()
	claudeRequest, errWithCode := claude.ConvertFromChatOpenai(request)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 应用 ClaudeCode 兼容性处理
	p.applyClaudeCodeCompatibility(claudeRequest)

	req, errWithCode := p.getChatRequest(claudeRequest)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	claudeResponse := &claude.ClaudeResponse{}
	_, errWithCode = p.Requester.SendRequest(req, claudeResponse, false)
	if errWithCode != nil {
		return nil, errWithCode
	}

	return claude.ConvertToChatOpenai(&p.ClaudeProvider, claudeResponse, request)
}

// CreateChatCompletionStream 创建流式聊天完成
func (p *ClaudeCodeProvider) CreateChatCompletionStream(request *types.ChatCompletionRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	request.OneOtherArg = p.GetOtherArg()
	claudeRequest, errWithCode := claude.ConvertFromChatOpenai(request)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 应用 ClaudeCode 兼容性处理
	p.applyClaudeCodeCompatibility(claudeRequest)

	req, errWithCode := p.getChatRequest(claudeRequest)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	resp, errWithCode := p.Requester.SendRequestRaw(req)
	if errWithCode != nil {
		return nil, errWithCode
	}

	chatHandler := &claude.ClaudeStreamHandler{
		Usage:   p.Usage,
		Request: request,
		Prefix:  "data:",
		Context: p.Context,
	}

	return requester.RequestStream(p.Requester, resp, chatHandler.HandlerStream)
}

// getChatRequest 获取聊天请求
func (p *ClaudeCodeProvider) getChatRequest(claudeRequest *claude.ClaudeRequest) (*http.Request, *types.OpenAIErrorWithStatusCode) {
	url, errWithCode := p.GetSupportedAPIUri(config.RelayModeChatCompletions)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 获取请求地址
	fullRequestURL := p.GetFullRequestURL(url)
	if fullRequestURL == "" {
		return nil, common.ErrorWrapperLocal(nil, "invalid_claudecode_config", http.StatusInternalServerError)
	}

	// 获取请求头
	headers := p.GetRequestHeaders()

	// 检查 token 是否获取成功
	if _, hasAuth := headers["Authorization"]; !hasAuth {
		// Token 获取失败，返回详细错误信息
		token, err := p.GetToken()
		if err != nil {
			return nil, p.handleTokenError(err)
		}
		// 如果 GetToken 成功但 headers 中没有 Authorization，手动添加
		headers["Authorization"] = "Bearer " + token
	}

	// 应用 ClaudeCode 默认请求头
	p.applyDefaultHeaders(headers)

	if claudeRequest.Stream {
		headers["Accept"] = "text/event-stream"
	}

	// 使用BaseProvider的统一方法创建请求，支持额外参数处理
	req, errWithCode := p.NewRequestWithCustomParams(http.MethodPost, fullRequestURL, claudeRequest, headers, claudeRequest.Model)
	if errWithCode != nil {
		return nil, errWithCode
	}

	return req, nil
}

// applyDefaultHeaders 应用 ClaudeCode 默认请求头
func (p *ClaudeCodeProvider) applyDefaultHeaders(headers map[string]string) {
	// 如果没有 anthropic-beta，设置默认值
	if _, exists := headers["anthropic-beta"]; !exists {
		headers["anthropic-beta"] = "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14"
	}

	// 如果没有 user-agent，设置默认值
	if _, exists := headers["user-agent"]; !exists {
		headers["user-agent"] = "claude-cli/1.0.81 (external, cli)"
	}

	// 添加 ClaudeCode 必需的 x-stainless-* 头部
	if _, exists := headers["x-stainless-retry-count"]; !exists {
		headers["x-stainless-retry-count"] = "0"
	}
	if _, exists := headers["x-stainless-timeout"]; !exists {
		headers["x-stainless-timeout"] = "60"
	}
	if _, exists := headers["x-stainless-lang"]; !exists {
		headers["x-stainless-lang"] = "js"
	}
	if _, exists := headers["x-stainless-package-version"]; !exists {
		headers["x-stainless-package-version"] = "0.55.1"
	}
	if _, exists := headers["x-stainless-os"]; !exists {
		headers["x-stainless-os"] = "Windows"
	}
	if _, exists := headers["x-stainless-arch"]; !exists {
		headers["x-stainless-arch"] = "x64"
	}
	if _, exists := headers["x-stainless-runtime"]; !exists {
		headers["x-stainless-runtime"] = "node"
	}
	if _, exists := headers["x-stainless-runtime-version"]; !exists {
		headers["x-stainless-runtime-version"] = "v20.19.2"
	}

	// 添加其他必需的头部
	if _, exists := headers["x-app"]; !exists {
		headers["x-app"] = "cli"
	}
	if _, exists := headers["anthropic-dangerous-direct-browser-access"]; !exists {
		headers["anthropic-dangerous-direct-browser-access"] = "true"
	}
	if _, exists := headers["accept-language"]; !exists {
		headers["accept-language"] = "*"
	}
	if _, exists := headers["sec-fetch-mode"]; !exists {
		headers["sec-fetch-mode"] = "cors"
	}
}

// generateClaudeCodeUserId 生成 ClaudeCode 格式的 user_id
// 格式: user_{64位十六进制}_account__session_{uuid}
func generateClaudeCodeUserId() string {
	// 生成一个随机的64位十六进制字符串
	hash := sha256.New()
	sessionUUID := uuid.New().String()
	hash.Write([]byte(sessionUUID))
	userHash := hex.EncodeToString(hash.Sum(nil))

	// 生成session UUID
	sessionID := uuid.New().String()

	// 组合成 ClaudeCode 格式
	return fmt.Sprintf("user_%s_account__session_%s", userHash, sessionID)
}

// extractMetadataFromOriginalRequest 从原始请求体中提取 metadata 字段
func (p *ClaudeCodeProvider) extractMetadataFromOriginalRequest(claudeRequest *claude.ClaudeRequest) {
	if p.Context == nil {
		return
	}

	// 从 gin.Context 中获取原始请求体
	rawBody, exists := p.Context.Get(config.GinRequestBodyKey)
	if !exists {
		return
	}

	bodyBytes, ok := rawBody.([]byte)
	if !ok {
		return
	}

	// 解析原始请求体为 map
	var requestMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &requestMap); err != nil {
		return
	}

	// 提取 metadata 字段
	if metadataInterface, exists := requestMap["metadata"]; exists {
		if metadataMap, ok := metadataInterface.(map[string]interface{}); ok {
			// 提取 user_id
			if userID, ok := metadataMap["user_id"].(string); ok && userID != "" {
				if claudeRequest.Metadata == nil {
					claudeRequest.Metadata = &claude.ClaudeMetadata{}
				}
				claudeRequest.Metadata.UserId = userID
			}
		}
	}
}

// applyClaudeCodeCompatibility 应用 ClaudeCode 兼容性处理
// 确保 system 字段中包含必需的 Claude Code 指令，并添加 metadata.user_id
func (p *ClaudeCodeProvider) applyClaudeCodeCompatibility(claudeRequest *claude.ClaudeRequest) {
	p.extractMetadataFromOriginalRequest(claudeRequest)
	p.ensureClaudeCodeSystemInstruction(claudeRequest)
	p.ensureMetadataUserId(claudeRequest)
}

// ensureClaudeCodeSystemInstruction 确保 system 字段包含 Claude Code 指令
func (p *ClaudeCodeProvider) ensureClaudeCodeSystemInstruction(claudeRequest *claude.ClaudeRequest) {
	const claudeCodeInstructionText = "You are Claude Code, Anthropic's official CLI for Claude."

	requiredCacheItem := claude.MessageContent{
		Type: "text",
		Text: claudeCodeInstructionText,
		CacheControl: map[string]string{
			"type": "ephemeral",
		},
	}

	// system 为空时直接设置
	if claudeRequest.System == nil || claudeRequest.System == "" {
		claudeRequest.System = []claude.MessageContent{requiredCacheItem}
		return
	}

	// system 是字符串
	if systemStr, ok := claudeRequest.System.(string); ok {
		if strings.TrimSpace(systemStr) == "" {
			claudeRequest.System = []claude.MessageContent{requiredCacheItem}
			return
		}
		if strings.HasPrefix(strings.TrimSpace(systemStr), claudeCodeInstructionText) {
			return
		}
		claudeRequest.System = []claude.MessageContent{
			requiredCacheItem,
			{Type: "text", Text: systemStr},
		}
		return
	}

	// system 是 []interface{}
	if systemArray, ok := claudeRequest.System.([]interface{}); ok {
		systemContents := p.convertSystemArrayToContents(systemArray, claudeCodeInstructionText)
		claudeRequest.System = systemContents
		return
	}

	// system 是 []MessageContent
	if systemArray, ok := claudeRequest.System.([]claude.MessageContent); ok {
		if len(systemArray) > 0 && systemArray[0].Type == "text" && systemArray[0].Text == claudeCodeInstructionText {
			return
		}
		if !p.hasClaudeCodeInstruction(systemArray, claudeCodeInstructionText) {
			claudeRequest.System = append([]claude.MessageContent{requiredCacheItem}, systemArray...)
		}
	}
}

// convertSystemArrayToContents 将 []interface{} 转换为 []MessageContent
func (p *ClaudeCodeProvider) convertSystemArrayToContents(systemArray []interface{}, instructionText string) []claude.MessageContent {
	requiredCacheItem := claude.MessageContent{
		Type: "text",
		Text: instructionText,
		CacheControl: map[string]string{
			"type": "ephemeral",
		},
	}

	var systemContents []claude.MessageContent
	hasRequired := false

	for _, item := range systemArray {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		if itemType != "text" {
			continue
		}
		text, _ := itemMap["text"].(string)
		if text == "" {
			continue
		}

		if text == instructionText {
			if cacheControl, exists := itemMap["cache_control"].(map[string]interface{}); exists {
				if cacheType, _ := cacheControl["type"].(string); cacheType == "ephemeral" {
					hasRequired = true
				}
			}
		}

		content := claude.MessageContent{Type: "text", Text: text}
		if cacheControl, exists := itemMap["cache_control"].(map[string]interface{}); exists {
			cacheControlMap := make(map[string]string)
			for k, v := range cacheControl {
				if strVal, ok := v.(string); ok {
					cacheControlMap[k] = strVal
				}
			}
			content.CacheControl = cacheControlMap
		}
		systemContents = append(systemContents, content)
	}

	if !hasRequired {
		systemContents = append([]claude.MessageContent{requiredCacheItem}, systemContents...)
	}
	return systemContents
}

// hasClaudeCodeInstruction 检查是否已包含 Claude Code 指令
func (p *ClaudeCodeProvider) hasClaudeCodeInstruction(contents []claude.MessageContent, instructionText string) bool {
	for _, item := range contents {
		if item.Type == "text" && item.Text == instructionText {
			if item.CacheControl != nil {
				if cacheControlMap, ok := item.CacheControl.(map[string]string); ok {
					if cacheControlMap["type"] == "ephemeral" {
						return true
					}
				}
				if cacheControlMap, ok := item.CacheControl.(map[string]interface{}); ok {
					if cacheType, _ := cacheControlMap["type"].(string); cacheType == "ephemeral" {
						return true
					}
				}
			}
		}
	}
	return false
}

// ensureMetadataUserId 确保 metadata.user_id 存在
func (p *ClaudeCodeProvider) ensureMetadataUserId(claudeRequest *claude.ClaudeRequest) {
	if claudeRequest.Metadata == nil {
		claudeRequest.Metadata = &claude.ClaudeMetadata{
			UserId: generateClaudeCodeUserId(),
		}
	} else if claudeRequest.Metadata.UserId == "" {
		claudeRequest.Metadata.UserId = generateClaudeCodeUserId()
	}
}
