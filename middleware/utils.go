package middleware

import (
	"done-hub/common/logger"
	"done-hub/common/utils"

	"github.com/gin-gonic/gin"
)

// abortWithMessage is currently unused; keeping a minimal, generic helper.
func abortWithMessage(c *gin.Context, statusCode int, message string) {
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"message": utils.MessageWithRequestId(message, c.GetString(logger.RequestIdKey)),
			"type":    "one_hub_error",
		},
	})
	c.Abort()
	logger.LogError(c.Request.Context(), message)
}
