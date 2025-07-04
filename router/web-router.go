package router

import (
	"done-hub/controller"
	"done-hub/middleware"
	"embed"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

func SetWebRouter(router *gin.Engine, buildFS embed.FS, indexPage []byte) {
	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.Use(middleware.GlobalWebRateLimit())
	router.Use(middleware.Cache())
	// 特别处理favicon.ico请求
	router.GET("/favicon.ico", controller.Favicon(buildFS))

	embedFS, err := static.EmbedFolder(buildFS, "web/build")
	if err != nil {
		// 处理错误，可以选择记录日志或者 panic
		panic("无法创建嵌入文件系统: " + err.Error())
	}
	router.Use(static.Serve("/", embedFS))

	router.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.RequestURI, "/v1") || strings.HasPrefix(c.Request.RequestURI, "/api") {
			controller.RelayNotFound(c)
			return
		}
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexPage)
	})
}
