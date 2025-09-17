package router

import (
	"done-hub/common/config"
	"done-hub/common/logger"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// SetRouter 仅保留 API 与最小 Web 路由，支持前端重定向
func SetRouter(engine *gin.Engine, indexPage []byte) {
	SetApiRouter(engine)

	frontendBaseUrl := viper.GetString("frontend_base_url")
	if frontendBaseUrl != "" {
		// 可选：主节点时是否忽略前端重定向（与原有行为一致）
		if config.IsMasterNode {
			logger.SysLog("FRONTEND_BASE_URL is ignored on master node")
		} else {
			frontendBaseUrl = strings.TrimSuffix(frontendBaseUrl, "/")
			engine.NoRoute(func(c *gin.Context) {
				// API 与静态资源不重定向
				if strings.HasPrefix(c.Request.RequestURI, "/api") {
					c.Status(http.StatusNotFound)
					return
				}
				c.Redirect(http.StatusMovedPermanently, fmt.Sprintf("%s%s", frontendBaseUrl, c.Request.RequestURI))
			})
			return
		}
	}

	SetWebRouter(engine, indexPage)

	// Swagger UI
	engine.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
}
