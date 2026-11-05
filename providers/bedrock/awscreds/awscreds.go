// Package awscreds 统一解析 AWS 系渠道（Bedrock / Bedrock Messages）的渠道 Key，并提供
// 可直接用于 SigV4 签名或 AWS SDK 的凭证提供者。
//
// 支持三种凭证形态：
//   - Bearer:     Region|BearerToken
//   - 静态 SigV4: Region|AccessKeyID|SecretAccessKey[|SessionToken]
//   - AssumeRole: Region|AccessKeyID|SecretAccessKey|RoleArn
//     角色链:     Region|AccessKeyID|SecretAccessKey|SessionToken|RoleArn
//
// AssumeRole 形态是 AWS 推荐的最小权限做法：长期 AK/SK 只保留 sts:AssumeRole 权限，
// 真正的 Bedrock 权限挂在目标角色上。done-hub 在运行时调 STS 换取临时凭证，并按
// (Key, 代理) 全局缓存、到期前自动续期，不会每个请求都调一次 STS。
package awscreds

import (
	"context"
	"strings"
	"sync"
	"time"

	"done-hub/common/requester"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

const (
	// roleSessionName 出现在 CloudTrail 的 AssumeRole 记录里，便于审计时区分调用方。
	roleSessionName = "done-hub"
	// assumeRoleDuration 临时凭证有效期。IAM 角色 MaxSessionDuration 最小即 1 小时，
	// 故 1 小时对任何角色都合法。
	//
	// 角色链（五段 Key：源 AK/SK 本身就是 STS 临时凭证，再 AssumeRole 到目标角色）已支持，
	// 但 STS 要求目标会话时长不超过源会话剩余时间：源会话只剩 30 分钟时请求 3600s 会被拒，
	// 表现为间歇性签名失败。此处固定 1 小时是有意选择——按剩余时间动态缩短会让缓存条目
	// 随源凭证剩余时长漂移，而源凭证本就由调用方负责在耗尽前轮换；
	// 确实长期遇到该场景时再改为渠道级可配。
	assumeRoleDuration = time.Hour
	// refreshWindow 提前续期窗口：凭证剩余有效期不足该值即触发刷新，
	// 避免签名瞬间凭证刚好过期被上游拒绝。
	refreshWindow = 5 * time.Minute
	// assumeRoleTimeout 单次 STS AssumeRole 调用的墙钟上限。缓存刷新期间所有并发请求
	// 都在等同一次 STS 调用，没有上限时 STS 网络卡住会拖住该渠道全部请求。
	assumeRoleTimeout = 30 * time.Second
)

// Config 是从渠道 Key 解析出的凭证配置。字段均为字符串，可作为缓存 key 直接比较。
type Config struct {
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	// RoleArn 非空时，用上面的 AK/SK 调 STS AssumeRole 换临时凭证，再用临时凭证签名。
	RoleArn string
	// APIToken 非空表示 Bearer 模式（Bedrock API key），不走 SigV4。
	APIToken string
}

// ParseKey 解析渠道 Key。返回 ok=false 表示段数不足、无法识别，或段数超出约定。
//
// 第四段形如 IAM 角色 ARN（见 isRoleArn）视为 RoleArn，否则视为 SessionToken
// （STS 临时凭证的 token 不会以 arn: 开头）。
//
// 五段形态专门表达角色链：源凭证本身是 STS 临时凭证，同时又需要 AssumeRole 到目标角色。
// 此时第四段必须是 SessionToken、第五段必须是 RoleArn——两者必须同时存在，
// 所以不能沿用四段那种「按形态推断」的写法。段数超过五段一律 ok=false 而不是静默截断，
// 避免多写的段被悄悄丢弃、用户以为配置已生效。
func ParseKey(key string) (cfg Config, ok bool) {
	keys := strings.Split(key, "|")
	if len(keys) < 2 || len(keys) > 5 {
		return Config{}, false
	}
	cfg.Region = keys[0]
	if len(keys) == 2 {
		cfg.APIToken = keys[1]
		return cfg, true
	}
	cfg.AccessKeyID = keys[1]
	cfg.SecretAccessKey = keys[2]

	if len(keys) == 5 {
		if !isRoleArn(keys[4]) {
			// 五段时第五段位置固定为 RoleArn；不是 ARN 说明用户写错了字段顺序，
			// 交回调用方让渠道配置报错，而不是把 RoleArn 当成 SessionToken 用。
			return Config{}, false
		}
		cfg.SessionToken = keys[3]
		cfg.RoleArn = keys[4]
		return cfg, true
	}

	if len(keys) == 4 && keys[3] != "" {
		if isRoleArn(keys[3]) {
			cfg.RoleArn = keys[3]
		} else {
			cfg.SessionToken = keys[3]
		}
	}
	return cfg, true
}

// isRoleArn 判断字符串是否为 IAM 角色 ARN（覆盖 aws / aws-cn / aws-us-gov 分区）。
func isRoleArn(s string) bool {
	return strings.HasPrefix(s, "arn:") && strings.Contains(s, ":role/")
}

// Provider 返回可供 AWS SDK 使用的凭证提供者。
//   - 无 RoleArn：静态凭证（与原行为一致）。
//   - 有 RoleArn：按 (cfg, proxy) 全局缓存的 AssumeRole 提供者，临时凭证自动续期；
//     并发首次取用时由 CredentialsCache 合并成一次 STS 调用。
//
// proxy 为渠道代理地址：STS 调用与 Bedrock 调用走同一出口。
func Provider(cfg Config, proxy string) aws.CredentialsProvider {
	if cfg.RoleArn == "" {
		return aws.NewCredentialsCache(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken),
		)
	}
	return assumeRoleCache.get(cfg, proxy)
}

// Retrieve 取当前可用于 SigV4 签名的凭证；AssumeRole 模式下为（缓存中的）临时凭证。
// 内部使用独立的超时 context：缓存刷新跨请求共享，不应被单个客户端断开所取消，
// 但也不能无限等待 STS。
func Retrieve(cfg Config, proxy string) (aws.Credentials, error) {
	if cfg.RoleArn == "" {
		return aws.Credentials{
			AccessKeyID:     cfg.AccessKeyID,
			SecretAccessKey: cfg.SecretAccessKey,
			SessionToken:    cfg.SessionToken,
		}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), assumeRoleTimeout)
	defer cancel()
	return assumeRoleCache.get(cfg, proxy).Retrieve(ctx)
}

// newSTSClient 构造 STS 客户端；测试中可替换为返回假客户端。
var newSTSClient = func(cfg Config, proxy string) stscreds.AssumeRoleAPIClient {
	return sts.New(sts.Options{
		Region: cfg.Region,
		Credentials: aws.NewCredentialsCache(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken),
		),
		HTTPClient: requester.GetHTTPClient(proxy),
	})
}

type cacheKey struct {
	cfg   Config
	proxy string
}

// providerCache 以渠道 Key + 代理为键缓存 AssumeRole 提供者。
// 渠道改 Key 会自然落到新条目；旧条目极小（几个字符串 + 一份凭证），不做淘汰。
type providerCache struct {
	mu      sync.Mutex
	entries map[cacheKey]*aws.CredentialsCache
}

var assumeRoleCache = &providerCache{entries: map[cacheKey]*aws.CredentialsCache{}}

func (c *providerCache) get(cfg Config, proxy string) *aws.CredentialsCache {
	k := cacheKey{cfg: cfg, proxy: proxy}
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.entries[k]; ok {
		return p
	}
	p := aws.NewCredentialsCache(
		stscreds.NewAssumeRoleProvider(newSTSClient(cfg, proxy), cfg.RoleArn, func(o *stscreds.AssumeRoleOptions) {
			o.RoleSessionName = roleSessionName
			o.Duration = assumeRoleDuration
		}),
		func(o *aws.CredentialsCacheOptions) {
			o.ExpiryWindow = refreshWindow
		},
	)
	c.entries[k] = p
	return p
}
