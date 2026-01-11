package services

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

type proxyRuntimeConfig struct {
	ProxyAddress string
	ProxyType    string
	ProxyClaude  bool
	ProxyCodex   bool
	ProxyGemini  bool
	ProxyCustom  bool
}

type transportCacheKey struct {
	UseProxy     bool
	ProxyAddress string
	ProxyType    string
}

var (
	proxyConfigMu sync.RWMutex
	proxyConfig   = proxyRuntimeConfig{
		ProxyType: "http",
	}

	transportCacheMu sync.Mutex
	transportCache   = make(map[transportCacheKey]*http.Transport)
)

// UpdateProxyConfigFromAppSettings 将 AppSettings 中的代理设置同步到运行时。
// 约定：
// - 只要 ProxyAddress 为空，就不会启用代理（即使某渠道开关为 true）
// - 代理类型默认为 http
// - 渠道开关控制该渠道的所有网络流量（监控 + 流量转发）是否走代理
func UpdateProxyConfigFromAppSettings(settings AppSettings) {
	newCfg := proxyRuntimeConfig{
		ProxyAddress: strings.TrimSpace(settings.ProxyAddress),
		ProxyType:    strings.ToLower(strings.TrimSpace(settings.ProxyType)),
		ProxyClaude:  settings.ProxyClaude,
		ProxyCodex:   settings.ProxyCodex,
		ProxyGemini:  settings.ProxyGemini,
		ProxyCustom:  settings.ProxyCustom,
	}
	if newCfg.ProxyType == "" {
		newCfg.ProxyType = "http"
	}

	proxyConfigMu.Lock()
	unchanged := newCfg == proxyConfig
	if !unchanged {
		proxyConfig = newCfg
	}
	proxyConfigMu.Unlock()

	if unchanged {
		return
	}

	// 配置变更：关闭旧连接池并清空缓存（新请求会按需重建）
	transportCacheMu.Lock()
	for _, tr := range transportCache {
		tr.CloseIdleConnections()
	}
	clear(transportCache)
	transportCacheMu.Unlock()
}

// GetHTTPClientForKind 返回符合“分渠道代理策略”的 HTTP 客户端。
// 说明：客户端本身是轻量对象，连接池由 Transport 复用。
func GetHTTPClientForKind(kind string) *http.Client {
	return &http.Client{Transport: GetHTTPTransportForKind(kind)}
}

// GetHTTPTransportForKind 返回符合“分渠道代理策略”的 Transport（带连接池）。
// 注意：Transport 会被缓存并复用；配置变更由 UpdateProxyConfigFromAppSettings 触发清空缓存。
func GetHTTPTransportForKind(kind string) *http.Transport {
	cfg := getProxyRuntimeConfig()

	useProxy := shouldUseProxyForKind(kind, cfg)
	key := transportCacheKey{
		UseProxy:     useProxy,
		ProxyAddress: cfg.ProxyAddress,
		ProxyType:    cfg.ProxyType,
	}
	if !useProxy {
		// direct 模式下，不需要把 address/type 纳入 key，避免不必要的 cache miss
		key.ProxyAddress = ""
		key.ProxyType = ""
	}

	transportCacheMu.Lock()
	if tr, ok := transportCache[key]; ok {
		transportCacheMu.Unlock()
		return tr
	}

	var (
		tr  *http.Transport
		err error
	)
	if !useProxy {
		tr = createDirectTransport()
	} else {
		tr, err = createProxyTransport(cfg.ProxyType, cfg.ProxyAddress)
		if err != nil {
			// 代理配置异常时降级为直连，避免影响核心功能
			fmt.Printf("⚠️  代理配置无效，已降级为直连（kind=%s, type=%s, addr=%s）：%v\n",
				kind, cfg.ProxyType, cfg.ProxyAddress, err)
			tr = createDirectTransport()
		}
	}

	transportCache[key] = tr
	transportCacheMu.Unlock()
	return tr
}

func getProxyRuntimeConfig() proxyRuntimeConfig {
	proxyConfigMu.RLock()
	defer proxyConfigMu.RUnlock()
	return proxyConfig
}

func normalizeProxyKind(kind string) string {
	k := strings.ToLower(strings.TrimSpace(kind))
	if strings.HasPrefix(k, "custom:") {
		return "custom"
	}
	switch k {
	case "claude", "claude-code", "claude_code":
		return "claude"
	case "codex":
		return "codex"
	case "gemini":
		return "gemini"
	default:
		return k
	}
}

func shouldUseProxyForKind(kind string, cfg proxyRuntimeConfig) bool {
	if strings.TrimSpace(cfg.ProxyAddress) == "" {
		return false
	}

	switch normalizeProxyKind(kind) {
	case "claude":
		return cfg.ProxyClaude
	case "codex":
		return cfg.ProxyCodex
	case "gemini":
		return cfg.ProxyGemini
	case "custom":
		return cfg.ProxyCustom
	default:
		return false
	}
}

func createDirectTransport() *http.Transport {
	return &http.Transport{
		Proxy: nil, // 直连：不使用环境变量代理，避免“禁用代理”时仍走系统代理
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
	}
}

func createProxyTransport(proxyType string, proxyAddr string) (*http.Transport, error) {
	switch strings.ToLower(strings.TrimSpace(proxyType)) {
	case "", "http", "https":
		return createHTTPProxyTransport(proxyAddr)
	case "socks5":
		return createSOCKS5ProxyTransport(proxyAddr)
	default:
		return nil, fmt.Errorf("不支持的代理类型: %s", proxyType)
	}
}

func createHTTPProxyTransport(proxyAddr string) (*http.Transport, error) {
	raw := strings.TrimSpace(proxyAddr)
	if raw == "" {
		return nil, fmt.Errorf("代理地址为空")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("解析代理地址失败: %w", err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("代理地址缺少 host: %s", proxyAddr)
	}

	return &http.Transport{
		Proxy: http.ProxyURL(u),
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
	}, nil
}

func createSOCKS5ProxyTransport(proxyAddr string) (*http.Transport, error) {
	raw := strings.TrimSpace(proxyAddr)
	if raw == "" {
		return nil, fmt.Errorf("代理地址为空")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		parsed = &url.URL{Scheme: "socks5", Host: raw}
	}

	socksAddr := parsed.Host
	if socksAddr == "" {
		socksAddr = raw
	}

	baseDialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, baseDialer)
	if err != nil {
		return nil, fmt.Errorf("创建 SOCKS5 拨号器失败: %w", err)
	}

	return &http.Transport{
		Dial: dialer.Dial,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if ctxDialer, ok := dialer.(proxy.ContextDialer); ok {
				return ctxDialer.DialContext(ctx, network, addr)
			}

			type result struct {
				conn net.Conn
				err  error
			}

			resultCh := make(chan result, 1)
			go func() {
				if ctxErr := ctx.Err(); ctxErr != nil {
					resultCh <- result{conn: nil, err: ctxErr}
					return
				}
				conn, dialErr := dialer.Dial(network, addr)
				if ctx.Err() != nil && conn != nil {
					_ = conn.Close()
					dialErr = ctx.Err()
					conn = nil
				}
				resultCh <- result{conn: conn, err: dialErr}
			}()

			select {
			case res := <-resultCh:
				return res.conn, res.err
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		ForceAttemptHTTP2:     false, // SOCKS5 通常不支持 HTTP/2
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
	}, nil
}

