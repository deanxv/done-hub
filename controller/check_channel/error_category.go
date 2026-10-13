package check_channel

import "strings"

// classifyError 把上游错误归类，便于管理员一眼看出失败原因。
// 错误分类规则按顺序匹配，先命中先返回。
func classifyError(statusCode int, errType, message string) string {
	text := strings.ToLower(errType + " " + message)
	switch {
	case statusCode == 401 || containsAny(text, "unauthorized", "invalid api key", "invalid_api_key", "incorrect api key", "authentication"):
		return "鉴权失败"
	case containsAny(text, "insufficient balance", "insufficient_quota", "insufficient quota", "quota exceeded", "billing hard limit", "run out of credits", "余额不足"):
		return "余额或额度不足"
	case containsAny(text, "content policy", "content_policy", "safety", "moderation"):
		return "内容审核拦截"
	case containsAny(text, "does not exist", "model_not_found", "model not found", "unsupported model", "model not supported", "no available channel", "no such model"):
		return "模型不可用"
	case containsAny(text, "context window", "context length", "maximum context", "too many tokens"):
		return "超出上下文"
	case statusCode == 429 || containsAny(text, "rate limit", "rate_limit", "overloaded", "high demand", "capacity", "concurrency limit"):
		return "限流或过载"
	case statusCode == 408 || statusCode == 504 || containsAny(text, "timeout", "timed out", "deadline exceeded", "gateway time-out"):
		return "超时"
	case containsAny(text, "connection reset", "connection refused", "no such host", "tls", "eof"):
		return "网络错误"
	case statusCode == 403:
		return "无权限"
	case statusCode == 404:
		return "接口不存在"
	case statusCode == 400 || containsAny(text, "invalid_request", "invalid request", "invalid parameter", "unsupported parameter", "not supported"):
		return "请求不被支持"
	case statusCode >= 500:
		return "上游服务错误"
	}
	return ""
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
