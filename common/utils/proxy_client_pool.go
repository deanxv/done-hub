package utils

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// ProxyClientPool 按代理地址缓存 http.Client，每个代理地址独享一个 http.Transport（连接池物理隔离）。
//
// 不能用"共享 Transport + 按请求注入代理"的方式：HTTP/2 连接池只按目标 host:port 复用连接，
// 直连与各代理的请求会互相复用连接，代理被静默绕过。
//
// 代理 client 闲置超过 idleTTL 后被回收（关闭空闲连接），避免代理地址变更后旧 Transport 常驻。
type ProxyClientPool struct {
	base    func() *http.Client // 直连 client，同时作为代理 client 的参数模板（超时 / TLS / 连接池）
	idleTTL time.Duration

	mu        sync.RWMutex
	clients   map[string]*proxyClientEntry
	lastSweep atomic.Int64
}

type proxyClientEntry struct {
	client   *http.Client
	lastUsed atomic.Int64
}

// NewProxyClientPool base 返回直连 client；其 Transport 须为 *http.Transport 才能继承参数。
func NewProxyClientPool(base func() *http.Client, idleTTL time.Duration) *ProxyClientPool {
	return &ProxyClientPool{
		base:    base,
		idleTTL: idleTTL,
		clients: make(map[string]*proxyClientEntry),
	}
}

// Get 返回 proxyAddr 对应的 client；proxyAddr 为空返回直连 client。
func (p *ProxyClientPool) Get(proxyAddr string) *http.Client {
	if proxyAddr == "" {
		return p.base()
	}

	now := time.Now().UnixNano()
	p.sweep(now)

	p.mu.RLock()
	entry, ok := p.clients[proxyAddr]
	p.mu.RUnlock()
	if ok {
		entry.lastUsed.Store(now)
		return entry.client
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok = p.clients[proxyAddr]
	if !ok {
		entry = &proxyClientEntry{client: p.build(proxyAddr)}
		p.clients[proxyAddr] = entry
	}
	entry.lastUsed.Store(now)
	return entry.client
}

func (p *ProxyClientPool) build(proxyAddr string) *http.Client {
	base := p.base()

	var trans *http.Transport
	if baseTrans, ok := base.Transport.(*http.Transport); ok {
		trans = baseTrans.Clone()
	} else {
		trans = &http.Transport{DialContext: NewDialer().DialContext, ForceAttemptHTTP2: true}
	}

	if err := ConfigureTransportProxy(trans, proxyAddr); err != nil {
		// 不能降级为直连：那会让配置了代理的渠道静默绕过代理，必须让请求失败暴露问题
		return &http.Client{Transport: errRoundTripper{err: err}}
	}

	return &http.Client{
		Transport:     trans,
		Timeout:       base.Timeout,
		CheckRedirect: base.CheckRedirect,
	}
}

// sweep 回收闲置超过 idleTTL 的代理 client，最多每 idleTTL/2 执行一次，不起后台 goroutine。
// 已被调用方取走的 client 仍可正常完成在途请求，CloseIdleConnections 只关闭空闲连接。
func (p *ProxyClientPool) sweep(now int64) {
	if p.idleTTL <= 0 {
		return
	}
	last := p.lastSweep.Load()
	if now-last < int64(p.idleTTL/2) || !p.lastSweep.CompareAndSwap(last, now) {
		return
	}

	deadline := now - int64(p.idleTTL)
	p.mu.Lock()
	defer p.mu.Unlock()
	for addr, entry := range p.clients {
		if entry.lastUsed.Load() < deadline {
			entry.client.CloseIdleConnections()
			delete(p.clients, addr)
		}
	}
}

// errRoundTripper 对所有请求返回固定错误，用于代理配置非法的场景。
type errRoundTripper struct{ err error }

func (e errRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("invalid proxy config: %w", e.err)
}
