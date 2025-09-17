package router

import (
	"go-template/middleware"
	"net/http"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// SetWebRouter 使用内嵌的极简页面（不依赖 web/build）
func SetWebRouter(engine *gin.Engine, indexPage []byte) {
	engine.Use(gzip.Gzip(gzip.DefaultCompression))
	engine.Use(middleware.GlobalWebRateLimit())

	engine.GET("/", func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache")
		if len(indexPage) == 0 {
			c.Data(http.StatusOK, "text/html; charset=utf-8", []byte("<!doctype html><html><head><meta charset=\"utf-8\"><title>Go Template</title></head><body><h1>Go Template</h1><p>Server is running.</p></body></html>"))
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexPage)
	})

	// 简单的 favicon 处理（200 空内容，避免 404）
	engine.GET("/favicon.ico", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=3600")
		c.Status(http.StatusNoContent)
	})
}
