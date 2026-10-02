package common

import (
	"context"
	"hash/fnv"
	"sync"
	"time"
)

const (
	// 分片数量，必须是2的幂
	shardCount = 32
)

// rateLimitShard 单个分片
type rateLimitShard struct {
	store map[string]*[]int64
	mutex sync.Mutex
}

// InMemoryRateLimiter 基于分片锁的内存限流器
// 使用分片锁减少高并发场景下的锁竞争
type InMemoryRateLimiter struct {
	shards             [shardCount]*rateLimitShard
	expirationDuration time.Duration
	ctx                context.Context
	cancel             context.CancelFunc
	initOnce           sync.Once
}

// getShard 根据key获取对应的分片
func (l *InMemoryRateLimiter) getShard(key string) *rateLimitShard {
	h := fnv.New32a()
	h.Write([]byte(key))
	return l.shards[h.Sum32()&(shardCount-1)]
}

// Init 初始化限流器
func (l *InMemoryRateLimiter) Init(expirationDuration time.Duration) {
	l.initOnce.Do(func() {
		l.ctx, l.cancel = context.WithCancel(context.Background())
		l.expirationDuration = expirationDuration

		// 初始化所有分片
		for i := 0; i < shardCount; i++ {
			l.shards[i] = &rateLimitShard{
				store: make(map[string]*[]int64),
			}
		}

		// 启动清理goroutine
		if expirationDuration > 0 {
			go l.clearExpiredItems()
		}
	})
}

// Stop 停止限流器，释放资源
func (l *InMemoryRateLimiter) Stop() {
	if l.cancel != nil {
		l.cancel()
	}
}

// clearExpiredItems 定期清理过期项
func (l *InMemoryRateLimiter) clearExpiredItems() {
	ticker := time.NewTicker(l.expirationDuration)
	defer ticker.Stop()

	for {
		select {
		case <-l.ctx.Done():
			return
		case <-ticker.C:
			l.doCleanup()
		}
	}
}

// doCleanup 执行清理逻辑
func (l *InMemoryRateLimiter) doCleanup() {
	now := time.Now().Unix()
	expirationSeconds := int64(l.expirationDuration.Seconds())

	for i := 0; i < shardCount; i++ {
		shard := l.shards[i]
		shard.mutex.Lock()

		for key, queue := range shard.store {
			// 队列顺序：[旧 --> 新]，从头部清理过期元素
			cutoff := 0
			for j := 0; j < len(*queue); j++ {
				if now-(*queue)[j] > expirationSeconds {
					cutoff = j + 1
				} else {
					// 后面的都是新的，不用再查了
					break
				}
			}
			// 移除过期元素
			if cutoff > 0 {
				*queue = (*queue)[cutoff:]
			}
			// 队列空了才删key
			if len(*queue) == 0 {
				delete(shard.store, key)
			}
		}

		shard.mutex.Unlock()
	}
}

// queueInitialCap 是新 key 的队列初始容量上限。
// 不按 maxRequestNum 预分配：它可能是放宽后的 IP 天花板（上千），而绝大多数 key 只会
// 落几个请求就过期。按上限预分配会让每个新 key 立刻占掉 maxRequestNum*8 字节，
// 而 key 数量由来源 IP 决定、并无上界，扫段流量下内存随之线性放大。
// slice 自身按摊还成本增长，给个小起点即可。
const queueInitialCap = 16

// Request 检查并记录请求，duration单位为秒
// 第一个返回值为true表示允许请求，false表示被限流；
// 被限流时第二个返回值是最旧请求滑出窗口所需的秒数，供调用方回填 Retry-After，允许时为 0。
func (l *InMemoryRateLimiter) Request(key string, maxRequestNum int, duration int64) (bool, int64) {
	shard := l.getShard(key)
	shard.mutex.Lock()
	defer shard.mutex.Unlock()

	now := time.Now().Unix()

	queue, ok := shard.store[key]
	if !ok {
		// 新key，创建队列
		capHint := maxRequestNum
		if capHint > queueInitialCap {
			capHint = queueInitialCap
		}
		s := make([]int64, 0, capHint)
		s = append(s, now)
		shard.store[key] = &s
		return true, 0
	}

	// 队列顺序：[旧 --> 新]
	if len(*queue) < maxRequestNum {
		// 未达到限制，直接追加
		*queue = append(*queue, now)
		return true, 0
	}

	// 达到限制，检查最旧的请求是否过期
	// (*queue)[0] 是最旧的时间戳
	if now-(*queue)[0] >= duration {
		// 最旧的请求已过期，移除并添加新请求
		*queue = (*queue)[1:]
		*queue = append(*queue, now)
		return true, 0
	}

	// 限流：最旧的一次请求还要 duration-elapsed 秒才滑出窗口，届时才会腾出配额
	return false, duration - (now - (*queue)[0])
}
