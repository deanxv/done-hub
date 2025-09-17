package middleware

import (
	"go-template/common/logger"
	"go-template/common/utils"

	"github.com/gin-gonic/gin"
)

// abortWithMessage is currently unused; keeping a minimal, generic helper.
func abortWithMessage(c *gin.Context, statusCode int, message string) {
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"message": utils.MessageWithRequestId(message, c.GetString(logger.RequestIdKey)),
			"type":    "go_template_error",
		},
	})
	c.Abort()
	logger.LogError(c.Request.Context(), message)
}
