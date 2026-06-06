package common

import (
	"bytes"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
)

// readBody 读取请求体，使用读穿缓存保证全链路只读一次 c.Request.Body
// 首次调用从 HTTP 流读取并缓存到 GinRequestBodyKey；后续调用直接返回缓存
func readBody(c *gin.Context) ([]byte, error) {
	if cached, exists := c.Get(config.GinRequestBodyKey); exists {
		if data, ok := cached.([]byte); ok && data != nil {
			return data, nil
		}
	}

	size := c.Request.ContentLength
	if size <= 0 || size > 100<<20 {
		size = 512
	}
	buf := bytes.NewBuffer(make([]byte, 0, size))
	if _, err := buf.ReadFrom(c.Request.Body); err != nil {
		return nil, err
	}
	c.Request.Body.Close()

	data := buf.Bytes()
	c.Set(config.GinRequestBodyKey, data)
	return data, nil
}

// ReadBodyRaw 只读取请求体原始 bytes 并缓存到 context，不做 JSON 反序列化
// 适用于大 payload 场景（如含 base64 图片的 Gemini 请求），避免 json.Unmarshal 对所有字符串的内存分配
func ReadBodyRaw(c *gin.Context) ([]byte, error) {
	return readBody(c)
}

func UnmarshalBodyReusable(c *gin.Context, v any) error {
	requestBody, err := readBody(c)
	if err != nil {
		return err
	}

	// JSON 请求：直接从 []byte 反序列化，避免创建中间 bytes.Buffer
	contentType := c.ContentType()
	if contentType == "" || strings.Contains(contentType, "json") {
		if err = json.Unmarshal(requestBody, v); err != nil {
			return err
		}
		if err = binding.Validator.ValidateStruct(v); err != nil {
			if errs, ok := err.(validator.ValidationErrors); ok {
				return fmt.Errorf("field %s is required", errs[0].Field())
			}
			return err
		}
		return nil
	}

	// 非 JSON（multipart、form 等）：回退到 ShouldBind
	c.Request.Body = io.NopCloser(bytes.NewBuffer(requestBody))
	err = c.ShouldBind(v)
	if err != nil {
		if errs, ok := err.(validator.ValidationErrors); ok {
			return fmt.Errorf("field %s is required", errs[0].Field())
		}
		return err
	}
	return nil
}

// 已知安全的网络错误短标识：仅追加这些固定短词到客户端可见消息，避免泄露 URL/IP/Key
var safeNetErrPatterns = []string{
	"no such host",
	"connection refused",
	"connection reset by peer",
	"context deadline exceeded",
	"TLS handshake timeout",
	"handshake failure",
	"unrecognized name",
	"tunnel failed",
	"Bad Gateway",
	"i/o timeout",
	"use of closed network connection",
	"PROTOCOL_ERROR",
	"internal error",
	"EOF",
}

func ErrorWrapper(err error, code string, statusCode int) *types.OpenAIErrorWithStatusCode {
	errString := "error"
	if err != nil {
		errString = err.Error()
	}

	if strings.Contains(errString, "Post") || strings.Contains(errString, "dial") {
		logger.SysError(fmt.Sprintf("error: %s", errString))
		errString = "请求上游地址失败"
		for _, p := range safeNetErrPatterns {
			if strings.Contains(err.Error(), p) {
				errString = "请求上游地址失败: " + p
				break
			}
		}
	}

	return StringErrorWrapper(errString, code, statusCode)
}

func ErrorWrapperLocal(err error, code string, statusCode int) *types.OpenAIErrorWithStatusCode {
	openaiErr := ErrorWrapper(err, code, statusCode)
	openaiErr.LocalError = true
	return openaiErr
}

func ErrorToOpenAIError(err error) *types.OpenAIError {
	return &types.OpenAIError{
		Code:    "system error",
		Message: err.Error(),
		Type:    "one_hub_error",
	}
}

func StringErrorWrapper(err string, code string, statusCode int) *types.OpenAIErrorWithStatusCode {
	openAIError := types.OpenAIError{
		Message: err,
		Type:    "one_hub_error",
		Code:    code,
	}
	return &types.OpenAIErrorWithStatusCode{
		OpenAIError: openAIError,
		StatusCode:  statusCode,
	}
}

func StringErrorWrapperLocal(err string, code string, statusCode int) *types.OpenAIErrorWithStatusCode {
	openaiErr := StringErrorWrapper(err, code, statusCode)
	openaiErr.LocalError = true
	return openaiErr

}

// UpstreamUnavailableError 构造"渠道整体不可用"类的 LocalError，专供 FilterOpenAIErr 坍缩使用。
//
// 之所以单独封装：StringErrorWrapper 把 Type 硬编码为 one_hub_error，第二参数赋给 Code；
// 而 FilterOpenAIErr 的 collapse 路由依赖 Type == "upstream_unavailable" 作为内部 sentinel。
// 调用方手工补一行 openaiErr.OpenAIError.Type = "upstream_unavailable" 容易漏写或拼错，
// 因此把 sentinel 字符串和赋值步骤封进这里，唯一入口。
//
// 客户端永远看不到 Type=upstream_unavailable —— 要么被 collapse 改写成 system_error +
// service_unavailable/rate_limit_exceeded，要么走 fall-through 段的 type 改写兜底。
// Code 这里给 service_unavailable 是防御性默认值（万一某天有路径绕过 collapse），
// 行业语义安全，不暴露上游身份。
func UpstreamUnavailableError(msg string) *types.OpenAIErrorWithStatusCode {
	e := StringErrorWrapperLocal(msg, "service_unavailable", http.StatusServiceUnavailable)
	e.OpenAIError.Type = "upstream_unavailable"
	return e
}

// ModelNotFoundError 构造 OpenAI 标准 model_not_found 错误（404 / invalid_request_error）。
// 文案对齐 OpenAI 官方，不暴露内部分组结构。Type 非 upstream_unavailable，不触发 FilterOpenAIErr collapse。
func ModelNotFoundError(modelName string) *types.OpenAIErrorWithStatusCode {
	msg := fmt.Sprintf("The model `%s` does not exist or you do not have access to it.", modelName)
	e := StringErrorWrapperLocal(msg, "model_not_found", http.StatusNotFound)
	e.OpenAIError.Type = "invalid_request_error"
	return e
}

func AbortWithMessage(c *gin.Context, statusCode int, message string) {
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "one_hub_error",
		},
	})
	c.Abort()
	logger.LogError(c.Request.Context(), message)
}

func AbortWithErr(c *gin.Context, statusCode int, err error) {
	c.JSON(statusCode, err)
	c.Abort()
	logger.LogError(c.Request.Context(), err.Error())
}

func APIRespondWithError(c *gin.Context, status int, err error) {
	c.JSON(status, gin.H{
		"success": false,
		"message": err.Error(),
	})
}

func StringRerankErrorWrapper(err string, code string, statusCode int) *types.RerankErrorWithStatusCode {
	rerankError := types.RerankError{
		Detail: err,
	}
	return &types.RerankErrorWithStatusCode{
		RerankError: rerankError,
		StatusCode:  statusCode,
	}
}

func StringRerankErrorWrapperLocal(err string, code string, statusCode int) *types.RerankErrorWithStatusCode {
	rerankError := StringRerankErrorWrapper(err, code, statusCode)
	rerankError.LocalError = true
	return rerankError

}

func OpenAIErrorToRerankError(err *types.OpenAIErrorWithStatusCode) *types.RerankErrorWithStatusCode {
	return &types.RerankErrorWithStatusCode{
		RerankError: types.RerankError{
			Detail: err.Message,
		},
		StatusCode: err.StatusCode,
		LocalError: err.LocalError,
	}
}
