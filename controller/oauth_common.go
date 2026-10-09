package controller

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"
)

// generateNonce generates a cryptographically random nonce for CSP.
// Returns a base64-encoded random string, or empty string on failure.
// Callers must treat an empty return as a hard failure — never fall back to a
// predictable value such as a timestamp.
func generateNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(b)
}

// truncateErrorDescription bounds an OAuth error_description to at most 200
// Unicode code points. It performs NO escaping — HTML escaping is done centrally
// in the render functions, which is the single trusted point for output encoding.
func truncateErrorDescription(input string) string {
	runes := []rune(input)
	if len(runes) > 200 {
		return string(runes[:200])
	}
	return input
}

// generateRandomProjectID returns a placeholder project ID used when auto-detection
// fails. Format: "projects/random-{hex8}/locations/global".
func generateRandomProjectID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// 降级：从时间戳生成 4 字节随机数
		n := uint32(time.Now().UnixNano() & 0xFFFFFFFF)
		b = []byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
	}
	return fmt.Sprintf("projects/random-%x/locations/global", b)
}
