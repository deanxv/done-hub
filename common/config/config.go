package config

import (
	"net"
	"strings"
	"time"

	"done-hub/common/logger"
	"done-hub/common/utils"

	"github.com/spf13/viper"
)

func InitConf() {
	defaultConfig()
	setEnv()
	Language = viper.GetString("language")
	IsMasterNode = viper.GetString("node_type") != "slave"
	RelayOnly = viper.GetBool("relay_only")
	RequestInterval = time.Duration(viper.GetInt("polling_interval")) * time.Second
	SessionSecret = utils.GetOrDefault("session_secret", SessionSecret)
	UserInvoiceMonth = viper.GetBool("user_invoice_month")
	GitHubProxy = viper.GetString("github_proxy")
	MCP_ENABLE = viper.GetBool("mcp.enable") != false
	UPTIMEKUMA_ENABLE = viper.GetBool("uptime_kuma.enable") != false
	UPTIMEKUMA_DOMAIN = viper.GetString("uptime_kuma.domain")
	UPTIMEKUMA_STATUS_PAGE_NAME = viper.GetString("uptime_kuma.status_page_name")
}

func setEnv() {
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
}

// DefaultTrustedProxies 是默认信任的反代来源：loopback + RFC1918 私网 + IPv6 ULA。
// 涵盖 docker-compose / k8s / 宿主机 Nginx 这些常见部署，运维零改动即可继续按
// X-Forwarded-For 解析真实客户端 IP；而公网直连的实例不在此列，伪造的 XFF 会被忽略。
var DefaultTrustedProxies = []string{
	"127.0.0.0/8", "::1/128",
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
	"fc00::/7",
}

// TrustedProxyNone 是「不信任任何代理」的哨兵值。
// 不能用空值表达：viper 在环境变量为空时会回落到默认值，TRUSTED_PROXIES="" 得到的
// 仍是默认私网列表。因此要显式写 none（大小写不敏感）。
const TrustedProxyNone = "none"

// TrustedProxies 返回可信反代列表，供 gin 的 SetTrustedProxies 使用。
// 返回空切片表示谁都不信，此时 ClientIP() 只取 socket 对端地址（见 TrustedProxyNone）。
func TrustedProxies() []string {
	proxies := SplitCommaList(viper.GetStringSlice("trusted_proxies"))
	for _, proxy := range proxies {
		if strings.EqualFold(proxy, TrustedProxyNone) {
			return nil
		}
	}
	return proxies
}

// SplitCommaList 归一化「列表或逗号分隔字符串」两种写法。
// 走环境变量时（AutomaticEnv + "." -> "_"）整个值是一个字符串，viper 的 GetStringSlice
// 只按空白切分，逗号分隔会退化成单个条目；这里统一再按逗号切一次，使 yaml 列表与
// "a,b c" 形式的环境变量表现一致。
func SplitCommaList(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		for _, part := range strings.Split(item, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// ParseCIDRList 把 IP / CIDR 混合列表编译成网段。裸 IP 按单机处理（/32、/128）。
// 预编译而不是逐请求解析字符串：省掉热路径上的 ParseCIDR，IPv6 的书写形式不再要紧
// （2001:0db8::1 与 2001:db8::1 按二进制相等），非法条目在启动时就报出来。
//
// 非法条目跳过并告警：宁可少放行一个来源（表现为被限流，可见），也不要让运维
// 以为某台机器已生效、实际每次请求都还在计数（不可见）。
func ParseCIDRList(raw []string, optionName string) []*net.IPNet {
	items := SplitCommaList(raw)
	out := make([]*net.IPNet, 0, len(items))
	for _, item := range items {
		if !strings.Contains(item, "/") {
			ip := net.ParseIP(item)
			if ip == nil {
				logger.SysError(optionName + " 条目非法，已忽略: " + item)
				continue
			}
			bits := 128
			if ip.To4() != nil {
				bits = 32
			}
			out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		if _, ipNet, err := net.ParseCIDR(item); err == nil {
			out = append(out, ipNet)
		} else {
			logger.SysError(optionName + " 条目非法，已忽略: " + item)
		}
	}
	return out
}

func defaultConfig() {
	viper.SetDefault("port", "3000")
	viper.SetDefault("gin_mode", "release")
	viper.SetDefault("log_dir", "./logs")
	viper.SetDefault("sqlite_path", "done-hub.db")
	viper.SetDefault("sqlite_busy_timeout", 3000)
	viper.SetDefault("sync_frequency", 600)
	viper.SetDefault("batch_update_interval", 5)
	viper.SetDefault("shutdown_timeout", 30)
	viper.SetDefault("redis_pool_size", 100)
	viper.SetDefault("redis_min_idle_conns", 10)
	viper.SetDefault("redis_pool_timeout", 5)
	viper.SetDefault("redis_read_timeout", 2)
	viper.SetDefault("redis_write_timeout", 2)
	viper.SetDefault("global.api_rate_limit", 300)
	viper.SetDefault("global.web_rate_limit", 300)
	// 免限流来源（IP 或 CIDR），供管理脚本 / 监控探针等高频可信调用方使用。
	viper.SetDefault("global.rate_limit_whitelist", []string{})
	viper.SetDefault("trusted_proxies", DefaultTrustedProxies)
	viper.SetDefault("connect_timeout", 5)
	viper.SetDefault("auto_price_updates", false)
	viper.SetDefault("auto_price_updates_mode", "system")
	viper.SetDefault("auto_price_updates_interval", 1440)
	viper.SetDefault("update_price_service", "https://raw.githubusercontent.com/MartialBE/one-api/prices/prices.json")
	viper.SetDefault("language", "zh_CN")
	viper.SetDefault("favicon", "")
	viper.SetDefault("user_invoice_month", false)
	viper.SetDefault("mcp.enable", false)
	viper.SetDefault("uptime_kuma.enable", false)
	viper.SetDefault("uptime_kuma.domain", "")
	viper.SetDefault("uptime_kuma.status_page_name", "")
}
