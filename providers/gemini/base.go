package gemini

import (
	"bytes"
	"context"
	"done-hub/common/cache"
	"done-hub/common/logger"
	"done-hub/common/requester"
	"done-hub/common/utils"
	"done-hub/model"
	"done-hub/providers/base"
	"done-hub/providers/openai"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const geminiSATokenCacheKey = "api_token:gemini_sa"
const geminiSAScope = "https://www.googleapis.com/auth/generative-language"

type GeminiProviderFactory struct{}

// 创建 GeminiProvider
func (f GeminiProviderFactory) Create(channel *model.Channel) base.ProviderInterface {
	useOpenaiAPI := false
	useCodeExecution := false

	if channel.Plugin != nil {
		plugin := channel.Plugin.Data()
		if pWeb, ok := plugin["code_execution"]; ok {
			if enable, ok := pWeb["enable"].(bool); ok && enable {
				useCodeExecution = true
			}
		}

		if pWeb, ok := plugin["use_openai_api"]; ok {
			if enable, ok := pWeb["enable"].(bool); ok && enable {
				useOpenaiAPI = true
			}
		}
	}

	version := "v1beta"
	if channel.Other != "" {
		version = channel.Other
	}

	// Key 是服务账号 JSON 时改用 OAuth Bearer 认证（见 GetRequestHeaders）。
	// use_openai_api 会绕到 OpenAIProvider 的 Bearer <Key> 逻辑（把整段 JSON 当 key），
	// 对服务账号无效，这里强制关闭；原生路径同样接受 OpenAI 格式请求并转换，功能等价。
	saEmail, useServiceAccount := parseServiceAccountEmail(channel.Key)
	if useServiceAccount {
		useOpenaiAPI = false
	}

	return &GeminiProvider{
		OpenAIProvider: openai.OpenAIProvider{
			BaseProvider: base.BaseProvider{
				Config:    getConfig(version),
				Channel:   channel,
				Requester: requester.NewHTTPRequester(channel.GetProxy(), RequestErrorHandle(channel.Key)),
			},
			SupportStreamOptions: true,
		},
		UseOpenaiAPI:        useOpenaiAPI,
		UseCodeExecution:    useCodeExecution,
		ServiceAccountEmail: saEmail,
	}
}

type GeminiProvider struct {
	openai.OpenAIProvider
	UseOpenaiAPI     bool
	UseCodeExecution bool
	// ServiceAccountEmail 非空表示 Key 是 GCP 服务账号 JSON，认证走 OAuth Bearer token。
	ServiceAccountEmail string
}

func getConfig(version string) base.ProviderConfig {
	return base.ProviderConfig{
		BaseURL:           "https://generativelanguage.googleapis.com",
		ChatCompletions:   fmt.Sprintf("/%s/chat/completions", version),
		ModelList:         "/models",
		ImagesGenerations: "1",
	}
}

// 正则表达式匹配 "Please retry in <Go duration>" 格式（支持 30s、30.5s、5m30s、11h4m11.239163367s 等）
var retryInRegex = regexp.MustCompile(`Please retry in ([\dhm.]+s)`)

// 兜底：精确正则 retryInRegex 失配时（"in " 后出现非 [\dhm.] 字符）退化为允许 "in " 与
// 首个数字之间出现至多 10 个非数字字符，只抓整数秒作为下界。
//
// 设计动机是防御性的——即便当前热路径已经把 parseRateLimitResetTime 放在 cleaningError 之前调用、
// 不会再让本地脱敏污染 message，仍然可能在以下场景遇到 "xxxxx" 类噪声夹在 duration 字符中：
//   - 上游或中间层 proxy 自身做了脱敏后转发
//   - 未来重构万一调换调用顺序
//   - 重放历史日志做诊断
//
// 上限 10 字符的取舍：cleaningError 替换串 "xxxxx" 是 5 字符（base.go cleaningError 实现），
// 留余量到 10 足以覆盖；同时拒绝 "Please retry in N/A, error code 503" 这类 16 字符前缀的
// 异常 message，避免 503 被误当冻结秒数（参见 TestParseRateLimitResetTime_RejectsLongPrefix）。
var retryInFallbackRegex = regexp.MustCompile(`Please retry in [^\d]{0,10}(\d+)`)

// Google RPC RetryInfo 的 @type 标识，详见 https://cloud.google.com/apis/design/errors#retry_info
const googleRPCRetryInfoType = "type.googleapis.com/google.rpc.RetryInfo"

// Google API 通常用 lowerCamelCase，但部分 SDK 序列化为 snake_case，两手准备
var retryDelayMetadataKeys = []string{"retryDelay", "retry_delay"}

// rateLimitResetBufferSeconds 在秒级恢复时间上额外冗余的秒数，仅用于吸收时钟漂移。
// per_day 路径有自己的 5 分钟冗余（次日 0:05 太平洋时间硬编码），不走这里。
// 取舍：5s 偏激进，若上游实际恢复滞后于声明值会导致渠道二次撞墙再冻结一轮；
// 现阶段优先快恢复，若线上出现抖动再调到 15~30s。
const rateLimitResetBufferSeconds int64 = 5

// 正则表达式匹配每日配额限制（Quota exceeded + per_day），不限制 limit 具体数值
var perDayQuotaRegex = regexp.MustCompile(`Quota exceeded for metric:.*per_day`)

// perMinuteRateLimitRegex 匹配 Google 对"每分钟请求限流（RPM）"的标准错误文本，例：
//
//	"Quota exceeded for quota metric 'API requests' and limit 'Request limit per minute for a region' ..."
//
// 触发场景：客户端 RPM 超过当前模型 quota（free tier ~15 RPM、paid 1k+ RPM）。
// 这条错误上游通常**不会**附带 RetryInfo 或 "Please retry in" 文本——既然 RPM 一分钟内一定刷新，
// Google 没必要回精确时间——所以 message 启发式是唯一可用兜底。否则会落到下面的 keyword/code/type
// 检查，配了 "Quota exceeded" 等宽 keyword 的部署会被永久禁渠道（per-minute 实际只该冻 60s）。
var perMinuteRateLimitRegex = regexp.MustCompile(`Request limit per minute`)

// perMinuteRateLimitCooldownSeconds per-minute 限流兜底冻结时长（秒）。
// quota 在每个分钟边界刷新，worst-case 等下一个边界即恢复。60s 是稳妥上界；
// 偏长比偏短安全：偏短会立即撞同 channel 再翻一轮，偏长只是这一分钟内换别的渠道服务。
const perMinuteRateLimitCooldownSeconds int64 = 60

// CachedContentNotFoundMsg 是 Google 对 "cachedContent 引用失效" 这一类错误的统一返回串。
// 触发场景包括：cache TTL 过期、cache 不属于当前 key、跨项目 / 跨区域引用。
// Google 故意把"不存在"与"无权访问"合并成同一句返回（防缓存名枚举），所以这一条本质是
// "请求体里 cachedContent 字段当前不可用"，跟渠道 key 自身是否有效无关。
// 跨包共用：controller.ShouldDisableChannel 用它做"不要禁用渠道"决策，
// relay/gemini 用它做"剥字段透明重试"决策——两个决策独立但判定串相同，集中在此一份。
const CachedContentNotFoundMsg = "CachedContent not found (or permission denied)"

// 请求错误处理
func RequestErrorHandle(key string) requester.HttpErrorHandler {
	return func(resp *http.Response) *types.OpenAIError {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil
		}
		resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		geminiError := &GeminiErrorResponse{}
		if err := json.NewDecoder(resp.Body).Decode(geminiError); err == nil {
			return buildOpenAIErrorFromGemini(geminiError, key)
		}
		geminiErrors := &GeminiErrors{}
		// 长度判空：GeminiErrors.Error() 内部直接 (*e)[0]，空切片会 panic
		if err := json.Unmarshal(bodyBytes, geminiErrors); err == nil && len(*geminiErrors) > 0 {
			return buildOpenAIErrorFromGemini(geminiErrors.Error(), key)
		}

		return nil
	}
}

// buildOpenAIErrorFromGemini 把 Gemini 错误响应转成统一的 OpenAIError。
// 必须先调用 parseRateLimitResetTime（读未脱敏的 message / 结构化 Details），
// 再走 errorHandle（内部 cleaningError 会把 message 中的 key 替换为 "xxxxx"）。
// 顺序反了会让 429 限流被误判为永久错误。
func buildOpenAIErrorFromGemini(geminiError *GeminiErrorResponse, key string) *types.OpenAIError {
	var resetAt int64
	if geminiError.ErrorInfo != nil && geminiError.ErrorInfo.Code == http.StatusTooManyRequests {
		if v, ok := parseRateLimitResetTime(geminiError.ErrorInfo); ok {
			resetAt = v
		}
	}
	openAIError := errorHandle(geminiError, key)
	if openAIError != nil && resetAt > 0 {
		openAIError.RateLimitResetAt = resetAt
	}
	return openAIError
}

// parseRateLimitResetTime 从 Gemini 错误中解析冻结结束的 Unix 时间戳。
//
// 必须在 cleaningError 之前调用，否则 message 中的 API key 会被替换成 "xxxxx"，
// 导致 "Please retry in <duration>" 文本被破坏（key 子串可能跨越 duration 字符），
// 使后续两条 message 正则全部失配，渠道被误判为永久错误。
//
// 优先级：结构化 Details (RetryInfo) > message 精确正则 > message 兜底正则 > per_day 启发式。
// 返回 (0, false) 表示无法判定，调用方应回退到默认冷却策略而非永久禁用。
func parseRateLimitResetTime(errorInfo *GeminiError) (int64, bool) {
	// 方式1（最稳）: 从 Google RPC RetryInfo 结构化字段抽 retryDelay
	// 这条不依赖 message 文本，因此不受 cleaningError 影响。
	if delay := extractRetryDelayFromDetails(errorInfo.Details); delay > 0 {
		resetTimestamp := time.Now().Unix() + int64(math.Ceil(delay.Seconds())) + rateLimitResetBufferSeconds
		logger.SysLog(fmt.Sprintf("[Gemini] Rate limit detected via RetryInfo, retry in: %s (+%ds buffer), reset at: %s",
			delay, rateLimitResetBufferSeconds, time.Unix(resetTimestamp, 0).Format(time.RFC3339)))
		return resetTimestamp, true
	}

	// 方式2: 精确正则匹配 "Please retry in <Go duration>"
	if matches := retryInRegex.FindStringSubmatch(errorInfo.Message); len(matches) == 2 {
		if duration, err := time.ParseDuration(matches[1]); err == nil {
			resetTimestamp := time.Now().Unix() + int64(math.Ceil(duration.Seconds())) + rateLimitResetBufferSeconds
			logger.SysLog(fmt.Sprintf("[Gemini] Rate limit detected, retry in: %s (+%ds buffer), reset at: %s",
				matches[1], rateLimitResetBufferSeconds, time.Unix(resetTimestamp, 0).Format(time.RFC3339)))
			return resetTimestamp, true
		}
	}

	// 方式2兜底: 数字段被脱敏（如 "xxxxx1.604730737s" / "46.4xxxxx9107644s"），只抓整数秒作为下界
	if matches := retryInFallbackRegex.FindStringSubmatch(errorInfo.Message); len(matches) == 2 {
		if seconds, err := strconv.Atoi(matches[1]); err == nil {
			resetTimestamp := time.Now().Unix() + int64(seconds) + rateLimitResetBufferSeconds
			logger.SysLog(fmt.Sprintf("[Gemini] Rate limit detected (fallback, %ds + %ds buffer), reset at: %s",
				seconds, rateLimitResetBufferSeconds, time.Unix(resetTimestamp, 0).Format(time.RFC3339)))
			return resetTimestamp, true
		}
	}

	// 方式3: 每日配额限制，冻结到太平洋时间次日 0:05（冗余5分钟）
	if perDayQuotaRegex.MatchString(errorInfo.Message) {
		pst, err := time.LoadLocation("America/Los_Angeles")
		if err != nil {
			// tzdata 缺失（如最小化容器镜像）会让 per_day 兜底完全失效，
			// 显式打日志便于排查，避免静默吞错。
			logger.SysError(fmt.Sprintf("[Gemini] LoadLocation(America/Los_Angeles) failed, per_day cooldown unavailable: %v", err))
			return 0, false
		}
		now := time.Now().In(pst)
		nextReset := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 5, 0, 0, pst)
		logger.SysLog(fmt.Sprintf("[Gemini] Daily quota exceeded, reset at: %s (Pacific Time)",
			nextReset.Format(time.RFC3339)))
		return nextReset.Unix(), true
	}

	// 方式4: 每分钟请求限流（RPM），冻结 60s 与配额刷新窗口对齐
	// 上游既无 RetryInfo Details，message 也没有 "Please retry in" 文本时，仅能从限流类型本身推断。
	// 优先级最低：放在结构化 / 精确文本 / per_day 之后，让带精确时间的路径先匹配。
	if perMinuteRateLimitRegex.MatchString(errorInfo.Message) {
		resetTimestamp := time.Now().Unix() + perMinuteRateLimitCooldownSeconds + rateLimitResetBufferSeconds
		logger.SysLog(fmt.Sprintf("[Gemini] Per-minute rate limit detected (heuristic, %ds + %ds buffer), reset at: %s",
			perMinuteRateLimitCooldownSeconds, rateLimitResetBufferSeconds, time.Unix(resetTimestamp, 0).Format(time.RFC3339)))
		return resetTimestamp, true
	}

	return 0, false
}

// extractRetryDelayFromDetails 在 details 数组里找 RetryInfo 项并解析 retryDelay。
// Google 返回的 retryDelay 既可能在顶层（少见，但 SDK 反序列化时常见），也可能在 metadata 里，
// 因此两处都试。返回 0 表示没找到或解析失败。
func extractRetryDelayFromDetails(details []GeminiErrorDetails) time.Duration {
	for i := range details {
		if details[i].Type != googleRPCRetryInfoType {
			continue
		}
		if details[i].RetryDelay != "" {
			if d, err := parseRetryDelayString(details[i].RetryDelay); err == nil && d > 0 {
				return d
			}
		}
		for _, key := range retryDelayMetadataKeys {
			if v, ok := details[i].Metadata[key]; ok {
				if s, ok := v.(string); ok {
					if d, err := parseRetryDelayString(s); err == nil && d > 0 {
						return d
					}
				}
			}
		}
	}
	return 0
}

// parseRetryDelayString 接受 Go duration 字符串（"1.6s"）或纯秒数字符串（"1.6"），
// 后者来自部分 Google API 响应里 retryDelay 不带单位的情况。
func parseRetryDelayString(s string) (time.Duration, error) {
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	if secs, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Duration(secs * float64(time.Second)), nil
	}
	return 0, fmt.Errorf("unrecognized retryDelay format: %q", s)
}

// 错误处理
func errorHandle(geminiError *GeminiErrorResponse, key string) *types.OpenAIError {
	if geminiError.ErrorInfo == nil || geminiError.ErrorInfo.Message == "" {
		return nil
	}

	cleaningError(geminiError.ErrorInfo, key)

	return &types.OpenAIError{
		Message: geminiError.ErrorInfo.Message,
		Type:    "gemini_error",
		Param:   geminiError.ErrorInfo.Status,
		Code:    geminiError.ErrorInfo.Code,
	}
}

func cleaningError(errorInfo *GeminiError, key string) {
	if key == "" {
		return
	}
	message := strings.Replace(errorInfo.Message, key, "xxxxx", 1)

	// 截断 base64 数据，避免日志过长
	message = truncateBase64InMessage(message)

	errorInfo.Message = message
}

// truncateBase64InMessage 截断错误消息中的 base64 数据
func truncateBase64InMessage(message string) string {
	const maxBase64Length = 50 // 只保留前50个字符

	result := message
	offset := 0

	// 循环处理所有的 base64 数据
	for {
		// 在当前偏移位置查找下一个 base64 数据
		idx := strings.Index(result[offset:], ";base64,")
		if idx == -1 {
			break
		}

		// 计算实际位置
		actualIdx := offset + idx
		start := actualIdx + 8 // ";base64," 的长度

		// 查找 base64 数据的结束位置（通常是引号、空格或其他分隔符）
		end := start
		for end < len(result) && isBase64Char(result[end]) {
			end++
		}

		if end-start > maxBase64Length {
			// 截断 base64 数据
			result = result[:start+maxBase64Length] + "...[truncated]" + result[end:]
			// 更新偏移位置，继续查找下一个
			offset = start + maxBase64Length + len("...[truncated]")
		} else {
			// 如果这个 base64 数据不需要截断，移动到下一个位置
			offset = end
		}
	}

	return result
}

// isBase64Char 检查字符是否是 base64 字符
func isBase64Char(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '='
}

func (p *GeminiProvider) GetFullRequestURL(requestURL string, modelName string) string {
	baseURL := strings.TrimSuffix(p.GetBaseURL(), "/")
	version := "v1beta"

	inputVersion := p.Context.Param("version")
	if inputVersion != "" {
		version = inputVersion
	}

	if p.Channel.Other != "" {
		version = p.Channel.Other
	}

	return fmt.Sprintf("%s/%s/models/%s:%s", baseURL, version, modelName, requestURL)

}

// parseServiceAccountEmail 解析 GCP 服务账号 JSON 并返回 client_email。
// 若 key 不是服务账号凭证（如普通的 AIza API Key），ok 返回 false。
func parseServiceAccountEmail(key string) (email string, ok bool) {
	if !strings.HasPrefix(strings.TrimSpace(key), "{") {
		return "", false
	}
	var sa struct {
		Type        string `json:"type"`
		ClientEmail string `json:"client_email"`
	}
	if err := json.Unmarshal([]byte(key), &sa); err != nil || sa.Type != "service_account" {
		return "", false
	}
	return sa.ClientEmail, true
}

// GetToken 用服务账号凭证换取调用 Gemini API 的 OAuth token，带缓存。
// 复刻自 vertexai.GetToken，区别仅在于 scope 用 generative-language 而非 cloud-platform。
func (p *GeminiProvider) GetToken() (string, error) {
	cacheKey := fmt.Sprintf("%s:%d", geminiSATokenCacheKey, p.Channel.Id)
	token, err := cache.GetCache[string](cacheKey)
	if err != nil {
		logger.SysError("Failed to get token from cache: " + err.Error())
	}

	if token != "" {
		return token, nil
	}

	config, err := google.JWTConfigFromJSON([]byte(p.Channel.Key), geminiSAScope)
	if err != nil {
		return "", fmt.Errorf("failed to parse credentials: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	httpClient, err := utils.NewProxyHTTPClient(p.Channel.GetProxy())
	if err != nil {
		return "", fmt.Errorf("failed to create proxy http client: %w", err)
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, httpClient)

	tok, err := config.TokenSource(ctx).Token()
	if err != nil {
		return "", fmt.Errorf("failed to get access token: %w", err)
	}

	duration := time.Until(tok.Expiry) - 5*time.Minute
	if duration <= 0 {
		duration = 30 * time.Second
	}
	cache.SetCache(cacheKey, tok.AccessToken, duration)

	return tok.AccessToken, nil
}

// 获取请求头
func (p *GeminiProvider) GetRequestHeaders() (headers map[string]string) {
	headers = make(map[string]string)
	p.CommonRequestHeaders(headers)

	if p.ServiceAccountEmail != "" {
		token, err := p.GetToken()
		if err != nil {
			logger.SysError("[Gemini] failed to get service account token: " + err.Error())
			return headers
		}
		headers["Authorization"] = "Bearer " + token
		return headers
	}

	headers["x-goog-api-key"] = p.Channel.Key
	return headers
}
