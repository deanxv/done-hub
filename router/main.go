package router

import (
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/controller"
	"embed"
	"fmt"
	"net/http"
	"net/http/pprof"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

func SetRouter(router *gin.Engine, buildFS embed.FS, indexPage []byte) {
	// 纯 relay 网关模式：仅暴露转发接口与健康检查，其余(前端、/api 管理接口、
	// dashboard、MCP、pprof、前端跳转)一律不注册，未匹配路由统一返回 404。
	// 用于从节点只对外提供 relay 能力，同时避免泄露主域名等信息。
	if config.RelayOnly {
		router.GET("/health", controller.Health)
		SetRelayRouter(router)
		router.NoRoute(controller.RelayNotFound)
		logger.SysLog("RELAY_ONLY mode enabled: web pages & management API are disabled")
		return
	}

	SetApiRouter(router)
	SetDashboardRouter(router)
	SetRelayRouter(router)
	// 初始化MCP服务器与Gin集成
	if config.MCP_ENABLE {
		logger.SysLog("Enable MCP Server")
		SetMcpRouter(router)
	}
	// 启用 pprof 调试端点
	if viper.GetBool("pprof_enabled") {
		logger.SysLog("Enable pprof debug endpoints at /debug/pprof/")
		SetPprofRouter(router)
	}
	frontendBaseUrl := viper.GetString("frontend_base_url")
	if config.IsMasterNode && frontendBaseUrl != "" {
		frontendBaseUrl = ""
		logger.SysLog("FRONTEND_BASE_URL is ignored on master node")
	}
	if frontendBaseUrl == "" {
		SetWebRouter(router, buildFS, indexPage)
	} else {
		frontendBaseUrl = strings.TrimSuffix(frontendBaseUrl, "/")
		router.NoRoute(func(c *gin.Context) {
			c.Redirect(http.StatusMovedPermanently, fmt.Sprintf("%s%s", frontendBaseUrl, c.Request.RequestURI))
		})
	}
}

// SetPprofRouter 设置 pprof 调试路由
func SetPprofRouter(router *gin.Engine) {
	pprofGroup := router.Group("/debug/pprof")
	{
		pprofGroup.GET("/", gin.WrapF(pprof.Index))
		pprofGroup.GET("/cmdline", gin.WrapF(pprof.Cmdline))
		pprofGroup.GET("/profile", gin.WrapF(pprof.Profile))
		pprofGroup.GET("/symbol", gin.WrapF(pprof.Symbol))
		pprofGroup.POST("/symbol", gin.WrapF(pprof.Symbol))
		pprofGroup.GET("/trace", gin.WrapF(pprof.Trace))
		pprofGroup.GET("/allocs", gin.WrapH(pprof.Handler("allocs")))
		pprofGroup.GET("/block", gin.WrapH(pprof.Handler("block")))
		pprofGroup.GET("/goroutine", gin.WrapH(pprof.Handler("goroutine")))
		pprofGroup.GET("/heap", gin.WrapH(pprof.Handler("heap")))
		pprofGroup.GET("/mutex", gin.WrapH(pprof.Handler("mutex")))
		pprofGroup.GET("/threadcreate", gin.WrapH(pprof.Handler("threadcreate")))
	}
}
