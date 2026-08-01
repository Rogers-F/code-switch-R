package services

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ========== 供应商多地址池（issue #27）==========
//
// 设计约定（与方案评审收敛结果一致）：
// - APIURL 保持唯一主地址，FallbackAPIURLs 为备用地址（存储上限 4）；
// - 一个入站请求内每个不同地址最多尝试一次，地址循环在 forwardRequest 内部，
//   一次池遍历对应一条请求日志、至多一次供应商失败记录；
// - 池耗尽即切下一供应商（仅多地址供应商；单地址完全保留现有语义）；
// - 可切换错误：传输层失败、408、421、429、5xx；不可切换：客户端取消、
//   响应已提交、400/401/403/413/415/422 等请求/凭据类错误；
// - 冷却为进程内状态：失败地址短时间排到队尾，429 按 Retry-After 冷却，
//   全部冷却时只放最早到期的地址做一次 half-open 探测。

// errEndpointPoolExhausted 表示多地址供应商的全部地址在本次请求内都已失败。
// 调用方应记一次供应商失败后立即切换到下一供应商（拉黑模式也不再重试）。
var errEndpointPoolExhausted = errors.New("all endpoints of provider exhausted")

// fallbackEndpointLimit 备用地址存储上限（v1）
const fallbackEndpointLimit = 4

// defaultEndpointCooldown 地址失败后的默认冷却时长（429 无 Retry-After 时同用）
const defaultEndpointCooldown = 60 * time.Second

// EndpointPool 返回按声明序、规范化去重后的完整地址池（主地址在首位）。
// 长度 >1 即"多地址供应商"，转发走地址兜底路径。
func (p *Provider) EndpointPool() []string {
	pool := make([]string, 0, 1+len(p.FallbackAPIURLs))
	seen := make(map[string]bool, 1+len(p.FallbackAPIURLs))
	appendAddr := func(raw string) {
		u := strings.TrimSpace(raw)
		if u == "" {
			return
		}
		key := normalizeURL(u)
		if seen[key] {
			return
		}
		seen[key] = true
		pool = append(pool, u)
	}
	appendAddr(p.APIURL)
	for _, raw := range p.FallbackAPIURLs {
		appendAddr(raw)
	}
	return pool
}

// upstreamStatusError 带状态码的上游失败：地址兜底需要按状态分类决定
// 是否切换下一地址。Error() 文本与旧实现保持一致，调用方打印无感知。
type upstreamStatusError struct {
	status     int
	detail     string
	retryAfter time.Duration // 仅 429 场景可能非零
}

func (e *upstreamStatusError) Error() string { return e.detail }

// addressSwitchableError 判断失败是否值得换下一个地址重试。
// 传输层错误（拿不到状态码）可切；带状态码的按白名单：408/421/429/5xx 可切。
// 客户端取消、响应已提交、请求内容或凭据类 4xx 不切——这些换地址也一样失败。
func addressSwitchableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errClientAbort) ||
		errors.Is(err, errUpstreamStreamAborted) ||
		errors.Is(err, errUpstreamClientError) {
		return false
	}
	var statusErr *upstreamStatusError
	if errors.As(err, &statusErr) {
		switch {
		case statusErr.status == http.StatusRequestTimeout, // 408
			statusErr.status == http.StatusMisdirectedRequest, // 421
			statusErr.status == http.StatusTooManyRequests:    // 429
			return true
		case statusErr.status >= 500:
			return true
		default:
			return false
		}
	}
	// 无状态码：传输层失败（dial/TLS/超时/连接重置），可切
	return true
}

// retryAfterOf 提取失败中的建议冷却时长；无建议时返回默认值
func retryAfterOf(err error) time.Duration {
	var statusErr *upstreamStatusError
	if errors.As(err, &statusErr) && statusErr.retryAfter > 0 {
		return statusErr.retryAfter
	}
	return defaultEndpointCooldown
}

// parseRetryAfter 解析 Retry-After 头（秒数或 HTTP 日期），失败返回 0
func parseRetryAfter(header string, now time.Time) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	if secs, err := strconv.Atoi(header); err == nil {
		if secs <= 0 {
			return 0
		}
		// 上游给出离谱值时封顶 10 分钟，避免地址被一次 429 冷死
		d := time.Duration(secs) * time.Second
		if d > 10*time.Minute {
			d = 10 * time.Minute
		}
		return d
	}
	if t, err := http.ParseTime(header); err == nil {
		d := t.Sub(now)
		if d <= 0 {
			return 0
		}
		if d > 10*time.Minute {
			d = 10 * time.Minute
		}
		return d
	}
	return 0
}

// endpointCooldownStore 地址冷却的进程内存储。
// 键为 (platform, providerID, normalizedURL)：供应商可改名，不能用 name。
// 应用重启即清零——这是合理的 half-open 重置，不持久化过期网络状态。
type endpointCooldownStore struct {
	mu      sync.Mutex
	expires map[string]time.Time
	nowFn   func() time.Time
}

func newEndpointCooldownStore() *endpointCooldownStore {
	return &endpointCooldownStore{
		expires: make(map[string]time.Time),
		nowFn:   time.Now,
	}
}

func (s *endpointCooldownStore) key(platform string, providerID int64, addr string) string {
	return platform + "\x00" + strconv.FormatInt(providerID, 10) + "\x00" + normalizeURL(addr)
}

// MarkFailure 记录地址失败并进入冷却
func (s *endpointCooldownStore) MarkFailure(platform string, providerID int64, addr string, d time.Duration) {
	if d <= 0 {
		d = defaultEndpointCooldown
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// 机会式全量清扫：地址被改配/删除后其键不会再被 Order 访问，
	// 不清扫会随配置变更缓慢累积
	if len(s.expires) > 64 {
		now := s.nowFn()
		for k, until := range s.expires {
			if !until.After(now) {
				delete(s.expires, k)
			}
		}
	}
	s.expires[s.key(platform, providerID, addr)] = s.nowFn().Add(d)
}

// MarkSuccess 地址成功即清除冷却
func (s *endpointCooldownStore) MarkSuccess(platform string, providerID int64, addr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.expires, s.key(platform, providerID, addr))
}

// Order 按冷却状态排序地址池：未冷却的保持声明序在前，冷却中的按到期
// 时间升序排队尾；全部冷却时只返回最早到期的一个（half-open 探测），
// 避免每个请求把整池死地址重撞一遍。顺带惰性清理已过期条目。
func (s *endpointCooldownStore) Order(platform string, providerID int64, pool []string) []string {
	if len(pool) <= 1 {
		return pool
	}

	now := s.nowFn()
	s.mu.Lock()
	type coolingAddr struct {
		addr  string
		until time.Time
	}
	active := make([]string, 0, len(pool))
	cooling := make([]coolingAddr, 0, len(pool))
	for _, addr := range pool {
		k := s.key(platform, providerID, addr)
		until, ok := s.expires[k]
		if !ok || !until.After(now) {
			if ok {
				delete(s.expires, k) // 已过期，惰性清理
			}
			active = append(active, addr)
			continue
		}
		cooling = append(cooling, coolingAddr{addr: addr, until: until})
	}
	s.mu.Unlock()

	sort.SliceStable(cooling, func(i, j int) bool { return cooling[i].until.Before(cooling[j].until) })

	if len(active) == 0 {
		// 全冷却：只放最早到期者做 half-open 探测
		return []string{cooling[0].addr}
	}
	ordered := make([]string, 0, len(pool))
	ordered = append(ordered, active...)
	for _, c := range cooling {
		ordered = append(ordered, c.addr)
	}
	return ordered
}

// validateFallbackURLs 校验备用地址（供 ValidateConfiguration 复用）：
// 数量上限与绝对 http/https 形式（必须能解析出 scheme+host，
// 只查字符串前缀会放过 "https://"、"http://%zz" 这类运行时必炸的值）
func validateFallbackURLs(urls []string) []string {
	errs := make([]string, 0)
	if len(urls) > fallbackEndpointLimit {
		errs = append(errs, fmt.Sprintf("备用地址最多 %d 个（当前 %d 个）", fallbackEndpointLimit, len(urls)))
	}
	for _, raw := range urls {
		u := strings.TrimSpace(raw)
		if u == "" {
			continue
		}
		parsed, err := url.Parse(u)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			errs = append(errs, fmt.Sprintf("备用地址无效（必须是 http/https 绝对地址）: %s", u))
		}
	}
	return errs
}
