package middleware

import (
	"context"
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/redis"
	"done-hub/common/utils"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

func isLoopbackIP(remoteIP string) bool {
	ip := net.ParseIP(remoteIP)
	return ip != nil && ip.IsLoopback()
}

var timeFormat = "2006-01-02T15:04:05.000Z"

var inMemoryRateLimiter common.InMemoryRateLimiter

// rate-limit 的 Redis 操作超时跟随 redis_read_timeout 配置，但热路径上每请求调一次
// viper.GetInt 走 RWMutex.RLock+reflect 不划算，init-once 缓存（与 redis.go 的
// stickySessionOpTimeout 同思路）。
var (
	rateLimitTimeout     time.Duration
	rateLimitTimeoutOnce sync.Once
)

func getRateLimitTimeout() time.Duration {
	rateLimitTimeoutOnce.Do(func() {
		rateLimitTimeout = time.Duration(viper.GetInt("redis_read_timeout")) * time.Second
		if rateLimitTimeout <= 0 {
			rateLimitTimeout = 2 * time.Second
		}
	})
	return rateLimitTimeout
}

// degradeAllow 把 rate-limit 路径上的 Redis 错误降级为放行，并过滤 context.Canceled 不打日志。
// 限流只是辅助，鉴权/配额在后续 middleware；把瞬时 Redis 故障翻译成 500 会让上游（newapi 等）
// 误以为模型挂了并触发重试，反而放大问题。
// context.Canceled 是客户端中途断开（父 ctx 取消），不是 Redis 故障，过滤掉避免误告警；
// context.DeadlineExceeded 保留为真 Redis 慢的信号。
func degradeAllow(where string, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	logger.SysError("rate limit degraded, allowing request (" + where + "): " + err.Error())
}

// All duration's unit is seconds
// Shouldn't larger then RateLimitKeyExpirationDuration
var (
	GlobalApiRateLimitNum            = 300
	GlobalApiRateLimitDuration int64 = 3 * 60

	GlobalWebRateLimitNum            = 180
	GlobalWebRateLimitDuration int64 = 3 * 60

	UploadRateLimitNum            = 10
	UploadRateLimitDuration int64 = 60

	DownloadRateLimitNum            = 10
	DownloadRateLimitDuration int64 = 60

	CriticalRateLimitNum            = 200
	CriticalRateLimitDuration int64 = 20 * 60
)

func redisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), getRateLimitTimeout())
	defer cancel()
	rdb := redis.RDB
	key := "rateLimit:" + mark + c.ClientIP()
	listLength, err := rdb.LLen(ctx, key).Result()
	if err != nil {
		degradeAllow("LLen", err)
		return
	}
	if listLength < int64(maxRequestNum) {
		rdb.LPush(ctx, key, time.Now().Format(timeFormat))
		rdb.Expire(ctx, key, config.RateLimitKeyExpirationDuration)
	} else {
		oldTimeStr, err := rdb.LIndex(ctx, key, -1).Result()
		if err != nil {
			degradeAllow("LIndex", err)
			return
		}
		oldTime, err := time.Parse(timeFormat, oldTimeStr)
		if err != nil {
			degradeAllow("parse old", err)
			return
		}
		nowTimeStr := time.Now().Format(timeFormat)
		nowTime, err := time.Parse(timeFormat, nowTimeStr)
		if err != nil {
			degradeAllow("parse now", err)
			return
		}
		// time.Since will return negative number!
		// See: https://stackoverflow.com/questions/50970900/why-is-time-since-returning-negative-durations-on-windows
		if int64(nowTime.Sub(oldTime).Seconds()) < duration {
			rdb.Expire(ctx, key, config.RateLimitKeyExpirationDuration)
			c.Status(http.StatusTooManyRequests)
			c.Abort()
			return
		} else {
			rdb.LPush(ctx, key, time.Now().Format(timeFormat))
			rdb.LTrim(ctx, key, 0, int64(maxRequestNum-1))
			rdb.Expire(ctx, key, config.RateLimitKeyExpirationDuration)
		}
	}
}

func memoryRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	key := mark + c.ClientIP()
	if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
		c.Status(http.StatusTooManyRequests)
		c.Abort()
		return
	}
}

func rateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	var limiter func(c *gin.Context)
	if config.RedisEnabled {
		limiter = func(c *gin.Context) {
			redisRateLimiter(c, maxRequestNum, duration, mark)
		}
	} else {
		// It's safe to call multi times.
		inMemoryRateLimiter.Init(config.RateLimitKeyExpirationDuration)
		limiter = func(c *gin.Context) {
			memoryRateLimiter(c, maxRequestNum, duration, mark)
		}
	}
	return func(c *gin.Context) {
		if isLoopbackIP(c.ClientIP()) {
			return
		}
		limiter(c)
	}
}

func GlobalWebRateLimit() func(c *gin.Context) {
	return rateLimitFactory(utils.GetOrDefault("global.web_rate_limit", GlobalWebRateLimitNum), GlobalWebRateLimitDuration, "GW")
}

func GlobalAPIRateLimit() func(c *gin.Context) {
	return rateLimitFactory(utils.GetOrDefault("global.api_rate_limit", GlobalApiRateLimitNum), GlobalApiRateLimitDuration, "GA")
}

func CriticalRateLimit() func(c *gin.Context) {
	return rateLimitFactory(CriticalRateLimitNum, CriticalRateLimitDuration, "CT")
}

func DownloadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(DownloadRateLimitNum, DownloadRateLimitDuration, "DW")
}

func UploadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(UploadRateLimitNum, UploadRateLimitDuration, "UP")
}
