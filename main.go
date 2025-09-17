package main

// @title Done Hub Minimal API
// @version 1.0
// @description Minimal backend framework APIs (users, auth, options, payment, order, status).
// @BasePath /api

import (
	"done-hub/cli"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/oidc"
	"done-hub/common/redis"
	"done-hub/cron"
	_ "done-hub/docs" // swagger docs
	"done-hub/middleware"
	"done-hub/model"
	"done-hub/router"
	"net/http"
	"os"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// Note: indexPage left empty; router will serve a simple fallback page when empty.
var indexPage []byte

func main() {
	if tz := os.Getenv("TZ"); tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			time.Local = loc
		}
	} else {
		if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
			time.Local = loc
		}
	}

	cli.InitCli()
	config.InitConf()
	if viper.GetString("log_level") == "debug" {
		config.Debug = true
	}

	logger.SetupLogger()
	logger.SysLog("Framework started: " + config.Version)

	// Initialize SQL Database
	model.SetupDB()
	defer model.CloseDB()
	// Initialize Redis (optional)
	redis.InitRedisClient()
	// Initialize options
	model.InitOptionMap()
	// Initialize OIDC (if enabled)
	oidc.InitOIDCConfig()

	// Initialize Cron (mock tasks kept for extension)
	cron.InitCron()

	initHttpServer()
}

func initHttpServer() {
	if viper.GetString("gin_mode") != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}

	server := gin.New()
	server.Use(gin.Recovery())
	server.Use(middleware.RequestId())
	middleware.SetUpLogger(server)

	// 可选：反向代理可信IP头部
	trustedHeader := viper.GetString("trusted_header")
	if trustedHeader != "" {
		server.TrustedPlatform = trustedHeader
	}

	store := cookie.NewStore([]byte(config.SessionSecret))

	// 检测是否在 HTTPS 环境下运行
	isHTTPS := viper.GetBool("https") || viper.GetString("trusted_header") == "CF-Connecting-IP"

	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   2592000, // 30 days
		HttpOnly: true,
		Secure:   isHTTPS,                 // 在 HTTPS 环境下启用 Secure
		SameSite: http.SameSiteStrictMode, // 改为 Lax 模式，兼容 CDN 环境
	})

	server.Use(sessions.Sessions("session", store))

	router.SetRouter(server, indexPage)
	port := viper.GetString("port")

	err := server.Run(":" + port)
	if err != nil {
		logger.FatalLog("failed to start HTTP server: " + err.Error())
	}
}
