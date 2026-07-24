package requester

import (
	"crypto/tls"
	"done-hub/common/logger"
	"done-hub/common/utils"
	"fmt"
	"net/http"
	"time"
)

var HTTPClient *http.Client
var relayRequestTimeout time.Duration

// streamIdleTimeout 流式空闲超时：每收到一段数据就重置，上游静默超过该时长即中止。
// 与墙钟总超时（HTTPClient.Timeout）互补——墙钟是硬总封顶，本值精准处理"卡死流"。
// 设为 0 禁用（回到旧的纯阻塞读行为）。
var streamIdleTimeout time.Duration

func InitHttpClient() {
	// TLS 握手超时配置，默认 30 秒，可通过环境变量 TLS_HANDSHAKE_TIMEOUT 配置
	tlsHandshakeSeconds := utils.GetOrDefault("tls_handshake_timeout", 30)
	tlsHandshakeTimeout := time.Duration(tlsHandshakeSeconds) * time.Second
	// 响应头超时配置，默认 120 秒，防止请求体发送完成后上游长时间不返回响应头
	responseHeaderSeconds := utils.GetOrDefault("response_header_timeout", 120)
	responseHeaderTimeout := time.Duration(responseHeaderSeconds) * time.Second

	// TLS 证书验证配置，默认 false，设为 true 可跳过证书验证（用于 IP 直连等场景）
	tlsInsecureSkipVerify := utils.GetOrDefault("tls_insecure_skip_verify", false)

	// 连接池容量：MaxConnsPerHost 是会阻塞而非报错的硬上限，多租户 / 多渠道共享同一上游
	// host 时极易撞顶。设 0 表示不限。
	maxConnsPerHost := utils.GetOrDefault("max_conns_per_host", 0)
	maxIdleConnsPerHost := utils.GetOrDefault("max_idle_conns_per_host", 200)
	maxIdleConns := utils.GetOrDefault("max_idle_conns", 1000)

	trans := &http.Transport{
		DialContext: utils.Socks5ProxyFunc,
		Proxy:       utils.ProxyFunc,

		MaxIdleConns:        maxIdleConns,
		MaxIdleConnsPerHost: maxIdleConnsPerHost,
		MaxConnsPerHost:     maxConnsPerHost,
		IdleConnTimeout:     90 * time.Second,

		// 超时配置
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ExpectContinueTimeout: 1 * time.Second,

		ResponseHeaderTimeout: responseHeaderTimeout,

		// 连接复用优化
		DisableKeepAlives:  false,
		DisableCompression: false,
		ForceAttemptHTTP2:  true,
	}

	if tlsInsecureSkipVerify {
		trans.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	HTTPClient = &http.Client{
		Transport: trans,
		Timeout:   0,
	}

	// 全局请求超时，默认 600 秒（10 分钟），覆盖整个请求生命周期（含流式 body 读取），设为 0 可禁用
	relayTimeout := utils.GetOrDefault("relay_timeout", 600)
	if relayTimeout > 0 {
		HTTPClient.Timeout = time.Duration(relayTimeout) * time.Second
	}

	// 非流式请求独立超时，默认 300 秒（5 分钟），可通过 RELAY_REQUEST_TIMEOUT 配置，设为 0 禁用
	requestTimeout := utils.GetOrDefault("relay_request_timeout", 300)
	if requestTimeout > 0 {
		relayRequestTimeout = time.Duration(requestTimeout) * time.Second
	}

	// 流式空闲超时，默认 300 秒，可通过 STREAM_IDLE_TIMEOUT 配置，设为 0 禁用。
	// 上游流式响应期间每收到数据即重置；静默超过该时长则中止流，避免卡死流拖满墙钟总超时。
	streamIdleSeconds := utils.GetOrDefault("stream_idle_timeout", 300)
	if streamIdleSeconds > 0 {
		streamIdleTimeout = time.Duration(streamIdleSeconds) * time.Second
	}

	logger.SysLog(fmt.Sprintf("HTTP Client: relay_timeout=%ds, response_header_timeout=%ds, relay_request_timeout=%ds, stream_idle_timeout=%ds, tls_handshake_timeout=%ds, tls_insecure_skip_verify=%v, max_conns_per_host=%d, max_idle_conns_per_host=%d, max_idle_conns=%d",
		relayTimeout, responseHeaderSeconds, requestTimeout, streamIdleSeconds, tlsHandshakeSeconds, tlsInsecureSkipVerify, maxConnsPerHost, maxIdleConnsPerHost, maxIdleConns))
}
