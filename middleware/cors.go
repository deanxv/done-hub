package middleware

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func CORS() gin.HandlerFunc {
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	config.AllowCredentials = true
	config.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"*"}
	// 添加 Vary 头部以防止 CDN 缓存问题
	// 限流相关响应头不在 CORS 安全响应头名单里，不显式暴露的话跨域调用方读不到，
	// 也就无法按 Retry-After 退避
	config.ExposeHeaders = []string{
		"Vary", "Cache-Control", "Retry-After",
		"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset",
	}
	return cors.New(config)
}
