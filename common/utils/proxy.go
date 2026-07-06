package utils

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

type ContextKey string

const ProxyHTTPAddrKey ContextKey = "proxyHttpAddr"
const ProxySock5AddrKey ContextKey = "proxySock5Addr"
const ProxyAddrKey ContextKey = "proxyAddr"

func ProxyFunc(req *http.Request) (*url.URL, error) {
	proxyAddr := req.Context().Value(ProxyHTTPAddrKey)
	if proxyAddr == nil {
		return nil, nil
	}

	proxyURL, err := url.Parse(proxyAddr.(string))
	if err != nil {
		return nil, fmt.Errorf("error parsing proxy address: %w", err)
	}

	switch proxyURL.Scheme {
	case "http", "https":
		return proxyURL, nil
	}

	return nil, fmt.Errorf("unsupported proxy scheme: %s", proxyURL.Scheme)
}

func Socks5ProxyFunc(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   time.Duration(GetOrDefault("connect_timeout", 5)) * time.Second,
		KeepAlive: 30 * time.Second,
	}

	proxyAddr, ok := ctx.Value(ProxySock5AddrKey).(string)
	if !ok {
		return dialer.DialContext(ctx, network, addr)
	}

	proxyURL, err := url.Parse(proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("error parsing proxy address: %w", err)
	}

	proxyDialer, err := proxy.FromURL(proxyURL, dialer)
	if err != nil {
		return nil, fmt.Errorf("error creating proxy dialer: %w", err)
	}

	// 尝试使用 ContextDialer 以支持超时控制
	if contextDialer, ok := proxyDialer.(proxy.ContextDialer); ok {
		return contextDialer.DialContext(ctx, network, addr)
	}

	// 降级到普通 Dial (不支持 context 超时)
	return proxyDialer.Dial(network, addr)
}

func SetProxy(proxyAddr string, ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}

	if proxyAddr == "" {
		return ctx
	}

	key := ProxyHTTPAddrKey

	// 如果是以 socks5:// 开头的地址，那么使用 socks5 代理
	if strings.HasPrefix(proxyAddr, "socks5") {
		key = ProxySock5AddrKey
	}

	// 否则使用 http 代理
	return context.WithValue(ctx, key, proxyAddr)
}

// NewProxyHTTPClient 构建一个将代理固化在 Transport 上的 http.Client。
// 适用于 oauth2 等第三方库：它们通过 PostForm 等方式发请求，不会透传 SetProxy 注入到 context 的代理地址，
// 只能依赖 Transport 自身携带代理。proxyAddr 为空时返回默认直连的 http.Client。
func NewProxyHTTPClient(proxyAddr string) (*http.Client, error) {
	if proxyAddr == "" {
		return &http.Client{}, nil
	}

	proxyURL, err := url.Parse(proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("error parsing proxy address: %w", err)
	}

	transport := &http.Transport{}
	switch proxyURL.Scheme {
	case "http", "https":
		transport.Proxy = http.ProxyURL(proxyURL)
	case "socks5", "socks5h":
		proxyDialer, err := proxy.FromURL(proxyURL, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("error creating proxy dialer: %w", err)
		}
		if contextDialer, ok := proxyDialer.(proxy.ContextDialer); ok {
			transport.DialContext = contextDialer.DialContext
		} else {
			transport.Dial = proxyDialer.Dial
		}
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", proxyURL.Scheme)
	}

	return &http.Client{Transport: transport}, nil
}
