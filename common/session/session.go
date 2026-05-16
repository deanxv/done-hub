package session

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

// sessionIDPattern 用于从 metadata.user_id 中提取 session ID 的正则表达式
var sessionIDPattern = regexp.MustCompile(`session_([a-f0-9-]{36})`)

// ExtractSessionIDFromMetadata 从 metadata.user_id 字符串中提取 session ID
// 仅处理旧字符串格式: user_{64位字符串}_account__session_{uuid}
func ExtractSessionIDFromMetadata(userID string) string {
	if userID == "" {
		return ""
	}
	matches := sessionIDPattern.FindStringSubmatch(userID)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// ExtractSessionIDFromMetadataValue 从 metadata.user_id 值中提取 session ID，
// 同时兼容新旧两种 claude-cli 格式：
//  1. 旧格式（字符串）：user_{hex}_account__session_{uuid}
//  2. 新格式（对象）：{"device_id":"...","account_uuid":"...","session_id":"<uuid>"}
func ExtractSessionIDFromMetadataValue(userID interface{}) string {
	switch v := userID.(type) {
	case string:
		return ExtractSessionIDFromMetadata(v)
	case map[string]interface{}:
		if sid, ok := v["session_id"].(string); ok {
			return strings.TrimSpace(sid)
		}
	}
	return ""
}

// HashContent 对内容进行 SHA256 hash 并返回前32个字符
func HashContent(content string) string {
	if content == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])[:32]
}

// GenerateGeminiCliSessionHash 生成 GeminiCli 的 session hash
// 完全复刻 code-relay-demo 的实现逻辑
// 基于 User-Agent、IP 和 API Key 前缀生成 hash
func GenerateGeminiCliSessionHash(c *gin.Context) string {
	userAgent := c.GetHeader("User-Agent")
	clientIP := c.ClientIP()
	apiKey := c.GetString("token")

	// 提取 API Key 前缀（前20个字符，与 demo 保持一致）
	apiKeyPrefix := ""
	if len(apiKey) >= 20 {
		apiKeyPrefix = apiKey[:20]
	} else {
		apiKeyPrefix = apiKey
	}

	// 生成 session hash
	return GenerateGeminiCliSessionHashFromParts(userAgent, clientIP, apiKeyPrefix)
}

// GenerateGeminiCliSessionHashFromParts 从 User-Agent、IP 和 API Key 前缀生成 GeminiCli session hash
// 用于在不依赖 gin.Context 的情况下生成 hash
func GenerateGeminiCliSessionHashFromParts(userAgent, clientIP, apiKeyPrefix string) string {
	// 拼接字符串，使用 ":" 作为分隔符（与 code-relay-demo 保持一致）
	// 过滤掉空字符串（等同于 JavaScript 的 filter(Boolean)）
	var parts []string
	if userAgent != "" {
		parts = append(parts, userAgent)
	}
	if clientIP != "" {
		parts = append(parts, clientIP)
	}
	if apiKeyPrefix != "" {
		parts = append(parts, apiKeyPrefix)
	}

	combined := strings.Join(parts, ":")

	// 生成 SHA256 hash
	hash := sha256.Sum256([]byte(combined))
	return hex.EncodeToString(hash[:])
}

// GenerateCodexSessionHash 生成 Codex 的 session hash
// 完全复刻 code-relay-demo 的实现逻辑
// 从请求头中提取 session_id 或 x-session-id
func GenerateCodexSessionHash(c *gin.Context) string {
	// 优先使用 session_id，其次使用 x-session-id
	sessionID := c.GetHeader("session_id")
	if sessionID == "" {
		sessionID = c.GetHeader("x-session-id")
	}

	return GenerateCodexSessionHashFromSessionID(sessionID)
}

// GenerateCodexSessionHashFromSessionID 从 session ID 生成 Codex session hash
// 用于在不依赖 gin.Context 的情况下生成 hash
func GenerateCodexSessionHashFromSessionID(sessionID string) string {
	if sessionID == "" {
		return ""
	}

	// 对 session_id 进行 SHA256 hash
	hash := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(hash[:])
}
