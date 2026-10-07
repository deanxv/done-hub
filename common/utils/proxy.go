package utils

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"
)

// NewDialer 返回带 connect_timeout 的基础拨号器；socks5 场景下也作为连接代理服务器的底层拨号器。
func NewDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   time.Duration(GetOrDefault("connect_timeout", 5)) * time.Second,
		KeepAlive: 30 * time.Second,
	}
}

// ConfigureTransportProxy 将代理固化到 Transport 上。
//
// 一个 Transport 只能对应一个代理地址（或直连），不得按请求动态切换代理：
// Transport 的 HTTP/2 连接池只按目标 host:port 复用连接，不区分代理，
// 共享 Transport 会导致直连/不同代理之间互相复用连接，代理被静默绕过。
// proxyAddr 为空时不做修改（直连）。
func ConfigureTransportProxy(trans *http.Transport, proxyAddr string) error {
	if proxyAddr == "" {
		return nil
	}

	proxyURL, err := url.Parse(proxyAddr)
	if err != nil {
		return fmt.Errorf("error parsing proxy address: %w", err)
	}

	switch proxyURL.Scheme {
	case "http", "https":
		trans.Proxy = http.ProxyURL(proxyURL)
	case "socks5", "socks5h":
		trans.Proxy = nil
		proxyDialer, err := proxy.FromURL(proxyURL, NewDialer())
		if err != nil {
			return fmt.Errorf("error creating proxy dialer: %w", err)
		}
		if contextDialer, ok := proxyDialer.(proxy.ContextDialer); ok {
			trans.DialContext = contextDialer.DialContext
		} else {
			trans.DialContext = func(_ context.Context, network, addr string) (net.Conn, error) {
				return proxyDialer.Dial(network, addr)
			}
		}
	default:
		return fmt.Errorf("unsupported proxy scheme: %s", proxyURL.Scheme)
	}

	return nil
}

// NewProxyHTTPClient 构建一个将代理固化在 Transport 上的 http.Client。
// 适用于 oauth2 等第三方库：它们通过 PostForm 等方式发请求，只能依赖 Transport 自身携带代理。
// proxyAddr 为空时返回默认直连的 http.Client。
func NewProxyHTTPClient(proxyAddr string) (*http.Client, error) {
	if proxyAddr == "" {
		return &http.Client{}, nil
	}

	transport := &http.Transport{}
	if err := ConfigureTransportProxy(transport, proxyAddr); err != nil {
		return nil, err
	}

	return &http.Client{Transport: transport}, nil
}
