package openai

import (
	"done-hub/common/config"
	"net/http"
	"strings"
)

// openaiUpstreamHeaderExcluded 传输层头，绝不透传（key 全小写）。
var openaiUpstreamHeaderExcluded = map[string]struct{}{
	"content-length":    {},
	"content-type":      {},
	"content-encoding":  {},
	"transfer-encoding": {},
	"connection":        {},
	"keep-alive":        {},
}

// openaiUpstreamHeaderPrefixes 前缀白名单（全小写）：限流指纹头，
// 覆盖 x-ratelimit-limit/remaining/reset-{requests,tokens} 及 images 等变体。
var openaiUpstreamHeaderPrefixes = []string{
	"x-ratelimit-",
}

// openaiUpstreamHeaderExact 精确白名单（全小写）：退避指令。
// 注意：retry-after / x-should-retry 是上游 429 错误响应才带的头，而错误路径由
// SendRequest 的 HandleErrorResp 提前接管，不会走到本过滤器——故当前仅在成功响应可达。
// 保留在白名单是零成本的前瞻：若将来打通错误响应头透传，此处即自动生效。
var openaiUpstreamHeaderExact = map[string]struct{}{
	"retry-after":    {},
	"x-should-retry": {},
}

// filterOpenAIUpstreamHeaders 从上游 OpenAI 响应头中挑出可透传给客户端的指纹头，
// 目的是让 done-hub 中转的响应尽量贴近直连 OpenAI（携带 x-ratelimit-* / retry-after 等）。
// request-id / x-request-id 不直透（避免覆盖本地追踪 ID），单独提取为字符串返回，
// 由 relay 层以 X-Upstream-Request-Id 回写。大小写不敏感；多值 header 全部保留；
// 无命中时返回的 http.Header 为 nil。
func filterOpenAIUpstreamHeaders(src http.Header) (http.Header, string) {
	if len(src) == 0 {
		return nil, ""
	}
	out := http.Header{}
	var requestID string
	for name, values := range src {
		lower := strings.ToLower(name)
		if _, excluded := openaiUpstreamHeaderExcluded[lower]; excluded {
			continue
		}
		if lower == "request-id" || lower == "x-request-id" {
			if requestID == "" && len(values) > 0 {
				requestID = values[0]
			}
			continue
		}
		matched := false
		if _, ok := openaiUpstreamHeaderExact[lower]; ok {
			matched = true
		} else {
			for _, prefix := range openaiUpstreamHeaderPrefixes {
				if strings.HasPrefix(lower, prefix) {
					matched = true
					break
				}
			}
		}
		if !matched {
			continue
		}
		for _, v := range values {
			out.Add(name, v)
		}
	}
	if len(out) == 0 {
		out = nil
	}
	return out, requestID
}

// storeOpenAIUpstreamHeaders 过滤上游响应头并暂存到 gin.Context，供 relay 层透传写出。
// 流式与非流式共用；与响应体字节透传解耦，仅受全局 FingerprintPassThroughEnabled 开关控制。
func (p *OpenAIProvider) storeOpenAIUpstreamHeaders(header http.Header) {
	if !config.FingerprintPassThroughEnabled || p.Context == nil {
		return
	}
	headers, requestID := filterOpenAIUpstreamHeaders(header)
	if headers != nil {
		p.Context.Set(config.GinPassThroughHeaders, headers)
	}
	if requestID != "" {
		p.Context.Set(config.GinUpstreamRequestIdKey, requestID)
	}
}
