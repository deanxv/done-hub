package gemini

import (
	"bytes"
	"done-hub/common/logger"
	"done-hub/common/requester"
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
	"strings"
	"time"
)

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

	return &GeminiProvider{
		OpenAIProvider: openai.OpenAIProvider{
			BaseProvider: base.BaseProvider{
				Config:    getConfig(version),
				Channel:   channel,
				Requester: requester.NewHTTPRequester(channel.GetProxy(), RequestErrorHandle(channel.Key)),
			},
			SupportStreamOptions: true,
		},
		UseOpenaiAPI:     useOpenaiAPI,
		UseCodeExecution: useCodeExecution,
	}
}

type GeminiProvider struct {
	openai.OpenAIProvider
	UseOpenaiAPI     bool
	UseCodeExecution bool
}

func getConfig(version string) base.ProviderConfig {
	return base.ProviderConfig{
		BaseURL:           "https://generativelanguage.googleapis.com",
		ChatCompletions:   fmt.Sprintf("/%s/chat/completions", version),
		ModelList:         "/models",
		ImagesGenerations: "1",
	}
}

// 正则表达式匹配 "Please retry in Xs" 格式的重试时间
var retryInRegex = regexp.MustCompile(`Please retry in ([0-9.]+)s`)

// 正则表达式匹配每日配额限制（Quota exceeded + per_day + limit: 0）
var perDayQuotaRegex = regexp.MustCompile(`Quota exceeded for metric:.*per_day.*limit: 0`)

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
			openAIError := errorHandle(geminiError, key)

			// 解析 429 错误的冻结时间
			if openAIError != nil && geminiError.ErrorInfo != nil && geminiError.ErrorInfo.Code == http.StatusTooManyRequests {
				parseRateLimitResetTime(openAIError, geminiError.ErrorInfo)
			}

			return openAIError
		} else {
			geminiErrors := &GeminiErrors{}
			if err := json.Unmarshal(bodyBytes, geminiErrors); err == nil {
				geminiError := geminiErrors.Error()
				openAIError := errorHandle(geminiError, key)

				// 解析 429 错误的冻结时间
				if openAIError != nil && geminiError.ErrorInfo.Code == http.StatusTooManyRequests {
					parseRateLimitResetTime(openAIError, geminiError.ErrorInfo)
				}

				return openAIError
			}
		}

		return nil
	}
}

// parseRateLimitResetTime 从错误消息中解析冻结时间
func parseRateLimitResetTime(openAIError *types.OpenAIError, errorInfo *GeminiError) {
	// 方式1: 匹配 "Please retry in Xs" 格式
	if matches := retryInRegex.FindStringSubmatch(errorInfo.Message); len(matches) == 2 {
		if duration, err := time.ParseDuration(matches[1] + "s"); err == nil {
			// 向上取整，确保冻结时间足够
			resetTimestamp := time.Now().Unix() + int64(math.Ceil(duration.Seconds()))
			openAIError.RateLimitResetAt = resetTimestamp
			logger.SysLog(fmt.Sprintf("[Gemini] Rate limit detected, retry in: %ss, reset at: %s",
				matches[1], time.Unix(resetTimestamp, 0).Format(time.RFC3339)))
			return
		}
	}

	// 方式2: 每日配额限制，冻结到太平洋时间次日 0:05（冗余5分钟）
	if perDayQuotaRegex.MatchString(errorInfo.Message) {
		pst, err := time.LoadLocation("America/Los_Angeles")
		if err != nil {
			return
		}
		now := time.Now().In(pst)
		nextReset := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 5, 0, 0, pst)
		openAIError.RateLimitResetAt = nextReset.Unix()
		logger.SysLog(fmt.Sprintf("[Gemini] Daily quota exceeded, reset at: %s (Pacific Time)",
			nextReset.Format(time.RFC3339)))
	}
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

	if p.Channel.Other != "" {
		version = p.Channel.Other
	}

	inputVersion := p.Context.Param("version")
	if inputVersion != "" {
		version = inputVersion
	}

	return fmt.Sprintf("%s/%s/models/%s:%s", baseURL, version, modelName, requestURL)

}

// 获取请求头
func (p *GeminiProvider) GetRequestHeaders() (headers map[string]string) {
	headers = make(map[string]string)
	p.CommonRequestHeaders(headers)
	headers["x-goog-api-key"] = p.Channel.Key

	return headers
}
