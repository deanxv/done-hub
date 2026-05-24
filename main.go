package main

import (
	"context"
	"done-hub/cli"
	"done-hub/common"
	"done-hub/common/cache"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/notify"
	"done-hub/common/oidc"
	"done-hub/common/redis"
	"done-hub/common/requester"
	"done-hub/common/search"
	"done-hub/common/storage"
	"done-hub/common/telegram"
	"done-hub/controller"
	"done-hub/cron"
	"done-hub/middleware"
	"done-hub/model"
	"done-hub/relay/task"
	"done-hub/router"
	"done-hub/safty"
	"embed"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

//go:embed web/build
var buildFS embed.FS

//go:embed web/build/index.html
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
	logger.SysLog("Done Hub " + config.Version + " started")

	// Initialize user token
	err := common.InitUserToken()
	if err != nil {
		logger.FatalLog("failed to initialize user token: " + err.Error())
	}

	// Initialize SQL Database
	model.SetupDB()
	defer model.CloseDB()
	// Initialize Redis
	redis.InitRedisClient()
	cache.InitCacheManager()
	// Initialize invite code lock system
	model.InitInviteCodeLock()
	// Initialize options
	model.InitOptionMap()
	// Initialize oidc
	oidc.InitOIDCConfig()
	model.NewPricing()
	model.HandleOldTokenMaxId()

	initMemoryCache()
	initSync()

	common.InitTokenEncoders()
	requester.InitHttpClient()
	initMemoryMonitor()
	// Initialize Telegram bot
	telegram.InitTelegramBot()

	controller.InitMidjourneyTask()
	task.InitTask()
	notify.InitNotifier()
	cron.InitCron()
	storage.InitStorage()
	search.InitSearcher()
	// 初始化安全检查器
	safty.InitSaftyTools()
	// 初始化账单数据
	if config.UserInvoiceMonth {
		logger.SysLog("Enable User Invoice Monthly Data")
		go model.InsertStatisticsMonth()
	}
	initHttpServer()
}

func initMemoryCache() {
	if viper.GetBool("memory_cache_enabled") {
		config.MemoryCacheEnabled = true
	}

	if !config.MemoryCacheEnabled {
		return
	}

	syncFrequency := viper.GetInt("sync_frequency")
	model.TokenCacheSeconds = syncFrequency

	logger.SysLog("memory cache enabled")
	logger.SysLog(fmt.Sprintf("sync frequency: %d seconds", syncFrequency))
	go model.SyncOptions(syncFrequency)
	go SyncChannelCache(syncFrequency)
}

func initSync() {
	// go controller.AutomaticallyUpdateChannels(viper.GetInt("channel.update_frequency"))
	go controller.AutomaticallyTestChannels(viper.GetInt("channel.test_frequency"))
}

func initHttpServer() {
	if viper.GetString("gin_mode") != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}

	server := gin.New()
	server.Use(gin.Recovery())
	server.Use(middleware.RequestId())
	middleware.SetUpLogger(server)

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

	router.SetRouter(server, buildFS, indexPage)
	port := viper.GetString("port")

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: server,
	}

	serverErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	select {
	case err := <-serverErr:
		if err != nil {
			logger.FatalLog("failed to start HTTP server: " + err.Error())
		}
	case sig := <-quit:
		logger.SysLog(fmt.Sprintf("received signal %s, shutting down...", sig))
		// 注意：srv.Shutdown 不会等已 hijack 的 WebSocket 连接（net/http 不 track），
		// 长流式 / WS 会话由后面的 WaitTrackedGoroutines 通过 timeout 兜底
		// 默认 30s 以容纳长流式响应（LLM 流式补全单次常 30-120s）
		// 部署侧需配套 docker-compose stop_grace_period >= shutdown_timeout + flush 余量
		shutdownTimeout := viper.GetInt("shutdown_timeout")
		if shutdownTimeout <= 0 {
			shutdownTimeout = 30
		}
		deadline := time.Now().Add(time.Duration(shutdownTimeout) * time.Second)

		// 1) 停止接受新请求并等待 in-flight HTTP 请求完成
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		if err := srv.Shutdown(ctx); err != nil {
			logger.SysError("HTTP server shutdown error: " + err.Error())
		}
		cancel()

		// 2) 等待 handler 派生的 tracked goroutine（realtime / task 的 Consume）跑完，
		//    否则它们的 RecordConsumeLog 会塞进 batch 队列后无人 flush
		remaining := time.Until(deadline)
		// 即使 srv.Shutdown 把预算耗光，也至少给 tracked goroutine 1s 收尾，
		// 否则刚跑完 Shutdown 就立即超时退出，必丢这部分数据。
		// 极端情况（shutdown_timeout=1）下，真实等待会比配置多 ~1s
		if remaining < time.Second {
			remaining = time.Second
		}
		if !common.WaitTrackedGoroutines(remaining) {
			logger.SysError(fmt.Sprintf("tracked goroutines did not finish within %s, data may be lost", remaining))
		}

		// 3) 停 batch updater 后台 ticker，避免它和主线程同时 batchUpdate
		//    引发"swap 后未写完"窗口数据丢失
		// 4) flush batch 队列：必须在前三步完成之后，确保没有新数据再进队列
		//    注意：StopBatchUpdater + FlushAllBatches 内部是同步 DB 调用，不受
		//    shutdown_timeout 约束；DB 慢时这一步可能超出总退出时长，由 docker
		//    stop_grace_period 兜底
		if config.BatchUpdateEnabled {
			logger.SysLog("stopping batch updater before flush")
			model.StopBatchUpdater()
			logger.SysLog("flushing batch updates before exit")
			model.FlushAllBatches()
		}
		logger.SysLog("shutdown complete")
	}
}

func SyncChannelCache(frequency int) {
	// 只有 从 服务器端获取数据的时候才会用到
	if config.IsMasterNode {
		logger.SysLog("master node does't synchronize the channel")
		return
	}
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		logger.SysLog("syncing channels from database")
		model.ChannelGroup.Load()
		model.PricingInstance.Init()
		model.ModelOwnedBysInstance.Load()
		model.GlobalUserGroupRatio.Load()
	}
}

// initMemoryMonitor 初始化内存监控，定期记录内存使用情况
func initMemoryMonitor() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)

			heapMB := memStats.HeapAlloc / 1024 / 1024
			sysMB := memStats.Sys / 1024 / 1024
			numGoroutines := runtime.NumGoroutine()

			logger.SysLog(fmt.Sprintf("Memory: Heap=%dMB, Sys=%dMB, Goroutines=%d", heapMB, sysMB, numGoroutines))
		}
	}()
}
