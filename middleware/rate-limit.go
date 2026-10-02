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
	"strconv"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

func isLoopbackIP(remoteIP string) bool {
	ip := net.ParseIP(remoteIP)
	return ip != nil && ip.IsLoopback()
}

// rateLimitWhitelist 是免限流的来源，取自 global.rate_limit_whitelist，
// 面向管理脚本 / 监控探针这类调用量天然高于人类用户、又无法靠 IP 桶区分的可信来源。
//
// 只豁免 global.* 总量限流，不豁免 CriticalRateLimit：后者护着登录、注册、改密、
// OAuth 回调等 19 个端点，是口令爆破的唯一节流点。若这里也放行，一条 192.168.0.0/16
// 就会把整个内网的爆破防护静默关掉，而配置项名字看不出这层影响。Upload/Download 同理。
var (
	rateLimitWhitelist     []*net.IPNet
	rateLimitWhitelistOnce sync.Once
)

func isRateLimitWhitelisted(ip net.IP) bool {
	rateLimitWhitelistOnce.Do(func() {
		rateLimitWhitelist = config.ParseCIDRList(viper.GetStringSlice("global.rate_limit_whitelist"), "global.rate_limit_whitelist")
	})
	if ip == nil {
		return false
	}
	for _, cidr := range rateLimitWhitelist {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// rateLimitBucket 是本次请求要占用的一格配额。name 用于拼 Redis key 与内存 key，
// limit 是这个桶自己的上限：每个桶各带自己的阈值，因为用户桶与 IP 桶额度不同。
// name 同时决定阈值归属，务必保证不同 limit 的计数落在不同 name 上（见 rateLimitBuckets）。
type rateLimitBucket struct {
	name  string
	limit int
}

// sessionUserID 取出会话里已登录的用户 id。
// 不用 sessions.Default：它内部是 MustGet，未挂 session 中间件的 Engine 上会 panic。
// RelayOnly 模式下 relay / dashboard 路由就是这种情况。
//
// 刻意只认服务端签名的会话，不采纳请求头里的 access token：此中间件跑在鉴权之前，
// 未经校验的凭据一旦用于分桶，攻击者只要不断更换 token 值就能无限开桶。
func sessionUserID(c *gin.Context) (int, bool) {
	raw, exists := c.Get(sessions.DefaultKey)
	if !exists {
		return 0, false
	}
	session, ok := raw.(sessions.Session)
	if !ok {
		return 0, false
	}
	id, ok := session.Get("id").(int)
	if !ok || id <= 0 {
		return 0, false
	}
	return id, true
}

// rateLimitBuckets 返回本次请求要占用的桶，全部都要过、任一满即拒。
//
// 识别到登录用户时是「用户配额 + 放宽的 IP 天花板」两个桶：只按用户计，一台机器靠多
// 注册账号就能线性放大配额（注册本身只被 CriticalRateLimit 限速，挡的是建号速度而非
// 建号后的收益）；只按 IP 计，则同一 NAT / 公司出口后面的用户互相挤占，一人刷满全员 429。
// 此时 IP 上限刻意高于单用户配额——同一条出口后面可能坐着几十个合法用户，若两者相等，
// 先进来的一个用户就能把整条出口打满，等于把误伤挪到了更低的位置。
//
// 识别不到用户时只有 IP 一个桶，限额必须保持 maxRequestNum 原值、不得放宽：
// 此时它是这条路径上唯一的总量闸门。按 token 鉴权的 /dashboard、未登录的匿名访问
// 都走这一支，若跟着放宽 rateLimitIPToleranceFactor 倍，等于把闸门整体抬高数倍。
//
// 两种情形的 IP 计数刻意用不同的 key（ip: 与 ipc:），因为滑动窗口的阈值必须与计数器
// 一一对应，同一个 key 上按请求轮流换限额会让判定结果自相矛盾。分开后两者各由自己的
// 人群填充、互不干扰：匿名流量不会把带用户请求的天花板顶掉，反过来也一样。
//
// perUserQuota 为 false 时强制只按 IP：CriticalRateLimit 等敏感端点如果认用户桶，
// 攻击者带一个自己的有效 session 去打别人的账号，就换到了一个空桶。
func rateLimitBuckets(c *gin.Context, clientIP string, maxRequestNum int, perUserQuota bool) []rateLimitBucket {
	if perUserQuota {
		if id, ok := sessionUserID(c); ok {
			return []rateLimitBucket{
				{name: "u:" + strconv.Itoa(id), limit: maxRequestNum},
				{name: "ipc:" + clientIP, limit: maxRequestNum * rateLimitIPToleranceFactor},
			}
		}
	}
	return []rateLimitBucket{{name: "ip:" + clientIP, limit: maxRequestNum}}
}

// abortWithTooManyRequests 返回 429，带 Retry-After（秒）与可展示的 JSON 错误体。
// 原实现只写状态码、响应体为空，客户端既不知道该等多久也拿不到错误信息，只能立刻重试
// 从而把窗口一直顶满。
//
// 不复用 abortWithMessage：它会写一条 error 日志并占用 logger 的 1000 条环形缓冲
// （后台日志页面靠它）。429 是高频出口，被刷时那条路径会放大磁盘写入并把其他诊断
// 记录冲干净，而状态码本身已经进了 GIN 访问日志。request id 仍按同样格式带上，
// 它只是字符串拼接、不触发日志，缺了用户报障时无从定位。
func abortWithTooManyRequests(c *gin.Context, bucket rateLimitBucket, retryAfterSeconds int64) {
	if retryAfterSeconds < 1 {
		retryAfterSeconds = 1
	}
	retryAfter := strconv.FormatInt(retryAfterSeconds, 10)
	c.Header("Retry-After", retryAfter)
	// 限流配额头，取 X-RateLimit-* 这一业界事实标准写法（GitHub、OpenAI 等均如此；
	// IETF 的 ratelimit-headers 草案则用不带前缀的 RateLimit-*，尚未定稿）。
	// 本项目对外是 OpenAI 兼容接口，沿用 X- 前缀与其风格一致，客户端也早已普遍适配。
	// Remaining 恒为 0：能走到这里就是这个桶已经满了。
	c.Header("X-RateLimit-Limit", strconv.Itoa(bucket.limit))
	c.Header("X-RateLimit-Remaining", "0")
	// Reset 这里给的是「还需等待的秒数」，与 Retry-After 同义，不取 Unix 时间戳。
	// 两者都保留是因为各自有既成用法：Retry-After 是 HTTP 标准头、通用客户端认它，
	// X-RateLimit-Reset 供已按限流头实现的 SDK 读取。
	c.Header("X-RateLimit-Reset", retryAfter)
	c.JSON(http.StatusTooManyRequests, gin.H{
		"error": gin.H{
			"message": utils.MessageWithRequestId(
				"请求过于频繁，请在 "+retryAfter+" 秒后重试。",
				c.GetString(logger.RequestIdKey),
			),
			"type": "one_hub_error",
		},
	})
	c.Abort()
}

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
// 限流只是辅助，鉴权/配额在后续 middleware；把瞬时 Redis 故障翻译成 500 会让上游
// 误以为模型挂了并触发重试，反而放大问题。
// context.Canceled 是客户端中途断开（父 ctx 取消），不是 Redis 故障，过滤掉避免误告警；
// context.DeadlineExceeded 保留为真 Redis 慢的信号。
func degradeAllow(where string, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	logger.SysError("rate limit degraded, allowing request (" + where + "): " + err.Error())
}

// rateLimitIPToleranceFactor 是 IP 天花板相对单用户配额的倍率，
// 仅在已识别出登录用户、IP 桶退居第二道防线时生效（见 rateLimitBuckets）。
// 取 4 是因为同一条 NAT / 公司出口后面通常坐着几十个合法用户：若 IP 上限等于
// 单用户配额，先进来的一个用户就能把整条出口打满，等于把误伤挪到了更低的位置；
// 放宽后 IP 桶只负责拦住「单机靠账号数线性放大配额」这类异常。
const rateLimitIPToleranceFactor = 4

// All duration's unit is seconds
// Shouldn't larger then RateLimitKeyExpirationDuration
var (
	GlobalApiRateLimitNum            = 300
	GlobalApiRateLimitDuration int64 = 3 * 60

	GlobalWebRateLimitNum            = 300
	GlobalWebRateLimitDuration int64 = 3 * 60

	UploadRateLimitNum            = 10
	UploadRateLimitDuration int64 = 60

	DownloadRateLimitNum            = 10
	DownloadRateLimitDuration int64 = 60

	CriticalRateLimitNum            = 200
	CriticalRateLimitDuration int64 = 20 * 60
)

// rateLimitScript 在 Redis 侧原子完成「清理过期 + 判定 + 记账」。
//
// 换掉原先的 LLen/LIndex/LPush/Expire 多次往返有三个理由：
//   - 原子性。原实现读到 LLen 与写入 LPush 之间没有互斥，并发请求会一起判定为「未满」
//     而全部放行，配额被击穿；单次 EVALSHA 天然串行。
//   - 往返次数。每请求 3-4 次 RTT 降到 1 次。
//   - 时间基准。原实现把时间存成 "2006-01-02T15:04:05.000Z" 字符串：Format 用本地时区
//     生成、Parse 又按 UTC 解读，在非 UTC 部署下 elapsed 会偏掉整个时区差
//     （Asia/Shanghai 实测 -8h），窗口判定与 Retry-After 一起失准。这里统一传 Unix 秒。
//
// 队列顺序 [新 --> 旧]（LPUSH 进、尾部最旧），与内存实现相反，沿用原 Redis 实现的方向。
// 返回 {allowed, retryAfterSeconds}。
var rateLimitScript = goredis.NewScript(`
local key      = KEYS[1]
local maxReq   = tonumber(ARGV[1])
local duration = tonumber(ARGV[2])
local now      = tonumber(ARGV[3])
local ttl      = tonumber(ARGV[4])

-- 从尾部清掉已滑出窗口的记录；tonumber 失败的是旧版字符串格式，一并丢弃（自愈）
while true do
  local oldest = redis.call('LINDEX', key, -1)
  if not oldest then break end
  local ts = tonumber(oldest)
  if ts == nil or now - ts >= duration then
    redis.call('RPOP', key)
  else
    break
  end
end

if redis.call('LLEN', key) < maxReq then
  redis.call('LPUSH', key, now)
  redis.call('EXPIRE', key, ttl)
  return {1, 0}
end

-- 已满：最旧的一条还要多久滑出窗口，届时才腾出配额
local oldest = tonumber(redis.call('LINDEX', key, -1))
local retry = math.ceil(oldest + duration - now)
if retry < 1 then retry = 1 end
if retry > duration then retry = duration end
redis.call('EXPIRE', key, ttl)
return {0, retry}
`)

// redisRateLimiter 依次扣减每个桶的配额，任一桶满即 429。
// 桶之间不是原子事务：先扣的桶成功、后扣的桶失败时，前面的扣减不会回滚。
// 这是可接受的——多扣的那一次只占掉一格本来就给同一来源的配额，
// 而跨桶回滚需要 MULTI/watch，代价远高于这点偏差。
func redisRateLimiter(c *gin.Context, duration int64, mark string, buckets []rateLimitBucket) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), getRateLimitTimeout())
	defer cancel()

	ttl := int64(config.RateLimitKeyExpirationDuration.Seconds())
	now := time.Now().Unix()
	for _, bucket := range buckets {
		key := "rateLimit:" + mark + bucket.name
		res, err := rateLimitScript.Run(ctx, redis.RDB, []string{key}, bucket.limit, duration, now, ttl).Int64Slice()
		if err != nil {
			degradeAllow("eval", err)
			return
		}
		if len(res) != 2 {
			degradeAllow("eval", errors.New("unexpected script result"))
			return
		}
		if res[0] == 0 {
			abortWithTooManyRequests(c, bucket, res[1])
			return
		}
	}
}

func memoryRateLimiter(c *gin.Context, duration int64, mark string, buckets []rateLimitBucket) {
	for _, bucket := range buckets {
		allowed, retryAfter := inMemoryRateLimiter.Request(mark+bucket.name, bucket.limit, duration)
		if !allowed {
			abortWithTooManyRequests(c, bucket, retryAfter)
			return
		}
	}
}

// rateLimitFactory 构造限流中间件。
// allowWhitelist：是否让 global.rate_limit_whitelist 豁免本限流器。
// perUserQuota：已登录请求是否额外享有独立的用户配额，同时保留放宽的 IP 天花板。
// 两者都只对 global.* 总量限流开启，敏感端点一律按 IP 照常计数。
func rateLimitFactory(maxRequestNum int, duration int64, mark string, allowWhitelist, perUserQuota bool) func(c *gin.Context) {
	var limiter func(c *gin.Context, buckets []rateLimitBucket)
	if config.RedisEnabled {
		limiter = func(c *gin.Context, buckets []rateLimitBucket) {
			redisRateLimiter(c, duration, mark, buckets)
		}
	} else {
		// It's safe to call multi times.
		inMemoryRateLimiter.Init(config.RateLimitKeyExpirationDuration)
		limiter = func(c *gin.Context, buckets []rateLimitBucket) {
			memoryRateLimiter(c, duration, mark, buckets)
		}
	}
	return func(c *gin.Context) {
		// 豁免与分桶都用 ClientIP()（即可能来自可信反代的 X-Forwarded-For），
		// 而不是 socket 对端。其可信度由 main.go 的 SetTrustedProxies 保证：来自不可信
		// 对端的转发头会被 gin 直接忽略，只有可信反代追加的地址才会被采纳，因此伪造头
		// 在公网直连时换不到新桶、也换不到豁免。
		// 反过来说不能改看 socket 对端：同机反代（Nginx 与本体同宿主机）的对端恒为
		// 127.0.0.1，那样会把所有流量都当成回环请求豁免掉，限流整个失效。
		clientIP := c.ClientIP()
		if isLoopbackIP(clientIP) {
			return
		}
		if allowWhitelist && isRateLimitWhitelisted(net.ParseIP(clientIP)) {
			return
		}
		limiter(c, rateLimitBuckets(c, clientIP, maxRequestNum, perUserQuota))
	}
}

func GlobalWebRateLimit() func(c *gin.Context) {
	return rateLimitFactory(utils.GetOrDefault("global.web_rate_limit", GlobalWebRateLimitNum), GlobalWebRateLimitDuration, "GW", true, true)
}

func GlobalAPIRateLimit() func(c *gin.Context) {
	return rateLimitFactory(utils.GetOrDefault("global.api_rate_limit", GlobalApiRateLimitNum), GlobalApiRateLimitDuration, "GA", true, true)
}

func CriticalRateLimit() func(c *gin.Context) {
	return rateLimitFactory(CriticalRateLimitNum, CriticalRateLimitDuration, "CT", false, false)
}

func DownloadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(DownloadRateLimitNum, DownloadRateLimitDuration, "DW", false, false)
}

func UploadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(UploadRateLimitNum, UploadRateLimitDuration, "UP", false, false)
}
