package requester

import (
	"net/http"
	"strings"
)

// upstreamRequestIDHeaders 上游 request-id 候选响应头，按语义专属度排序：
// 厂商专属头（几乎不会被中间层注入）优先，通用头兜底，避免上游前置的
// CDN / 网关注入的 x-request-id 压过真正的厂商 ID。
// cf-ray 属 CDN 层标识，语义不同，不纳入。
var upstreamRequestIDHeaders = []string{
	"x-amzn-requestid",  // AWS
	"apim-request-id",   // Azure OpenAI (APIM)
	"x-ms-request-id",   // Azure
	"xai-request-id",    // xAI
	"x-goog-request-id", // Google (Vertex / 部分 Gemini 端点)
	"request-id",        // Anthropic
	"x-request-id",      // OpenAI 及大量兼容网关
}

// ExtractUpstreamRequestID 从上游响应头中提取 request-id，取候选表中第一个非空值。
// best-effort：上游不返回任何候选头时为空串，调用方不应依赖其存在。
func ExtractUpstreamRequestID(header http.Header) string {
	for _, name := range upstreamRequestIDHeaders {
		if v := strings.TrimSpace(header.Get(name)); v != "" {
			return v
		}
	}
	return ""
}
