package common

import (
	"bytes"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
)

// readBody 读取请求体并缓存，使用 Content-Length 预分配以减少 growSlice 开销
func readBody(c *gin.Context) ([]byte, error) {
	size := c.Request.ContentLength
	if size <= 0 || size > 100<<20 {
		size = 512
	}
	buf := bytes.NewBuffer(make([]byte, 0, size))
	if _, err := buf.ReadFrom(c.Request.Body); err != nil {
		return nil, err
	}
	c.Request.Body.Close()
	return buf.Bytes(), nil
}

// ReadBodyRaw 只读取请求体原始 bytes 并缓存到 context，不做 JSON 反序列化
// 适用于大 payload 场景（如含 base64 图片的 Gemini 请求），避免 json.Unmarshal 对所有字符串的内存分配
func ReadBodyRaw(c *gin.Context) ([]byte, error) {
	rawBody, err := readBody(c)
	if err != nil {
		return nil, err
	}
	c.Set(config.GinRequestBodyKey, rawBody)
	return rawBody, nil
}

func UnmarshalBodyReusable(c *gin.Context, v any) error {
	requestBody, err := readBody(c)
	if err != nil {
		return err
	}
	c.Set(config.GinRequestBodyKey, requestBody)

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

func ErrorWrapper(err error, code string, statusCode int) *types.OpenAIErrorWithStatusCode {
	errString := "error"
	if err != nil {
		errString = err.Error()
	}

	if strings.Contains(errString, "Post") || strings.Contains(errString, "dial") {
		logger.SysError(fmt.Sprintf("error: %s", errString))
		errString = "请求上游地址失败"
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
