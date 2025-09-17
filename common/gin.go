package common

import (
	"bytes"
	"fmt"
	"go-template/common/config"
	"go-template/common/logger"
	"io"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

func UnmarshalBodyReusable(c *gin.Context, v any) error {
	requestBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return err
	}
	err = c.Request.Body.Close()
	if err != nil {
		return err
	}
	c.Set(config.GinRequestBodyKey, requestBody)

	c.Request.Body = io.NopCloser(bytes.NewBuffer(requestBody))
	err = c.ShouldBind(v)
	if err != nil {
		if errs, ok := err.(validator.ValidationErrors); ok {
			// 返回第一个错误字段的名称
			return fmt.Errorf("field %s is required", errs[0].Field())
		}
		return err
	}

	// c.Request.Body = io.NopCloser(bytes.NewBuffer(requestBody))
	return nil
}

func AbortWithMessage(c *gin.Context, statusCode int, message string) {
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "go_template_error",
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
