package requester

import (
	"done-hub/common/logger"
	"done-hub/common/utils"
	"fmt"
	"net/http"
	"time"
)

var HTTPClient *http.Client
var relayRequestTimeout time.Duration

func InitHttpClient() {
	// TLS 握手超时配置，默认 30 秒，可通过环境变量 TLS_HANDSHAKE_TIMEOUT 配置
	tlsHandshakeSeconds := utils.GetOrDefault("tls_handshake_timeout", 30)
	tlsHandshakeTimeout := time.Duration(tlsHandshakeSeconds) * time.Second
	// 响应头超时配置，默认 120 秒，防止请求体发送完成后上游长时间不返回响应头
	responseHeaderSeconds := utils.GetOrDefault("response_header_timeout", 120)
	responseHeaderTimeout := time.Duration(responseHeaderSeconds) * time.Second

	trans := &http.Transport{
		DialContext: utils.Socks5ProxyFunc,
		Proxy:       utils.ProxyFunc,

		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 50,
		MaxConnsPerHost:     100,
		IdleConnTimeout:     60 * time.Second,

		// 超时配置
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: responseHeaderTimeout,

		// 连接复用优化
		DisableKeepAlives:  false,
		DisableCompression: false,
		ForceAttemptHTTP2:  true,
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

	logger.SysLog(fmt.Sprintf("HTTP Client: relay_timeout=%ds, response_header_timeout=%ds, relay_request_timeout=%ds, tls_handshake_timeout=%ds",
		relayTimeout, responseHeaderSeconds, requestTimeout, tlsHandshakeSeconds))
}
