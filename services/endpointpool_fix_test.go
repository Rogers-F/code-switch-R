package services

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// ==================== 地址池与校验 ====================

func TestEndpointPool(t *testing.T) {
	p := &Provider{
		APIURL: "https://a.example.com",
		FallbackAPIURLs: []string{
			"https://b.example.com",
			"  https://A.example.com/  ", // 与主地址归一化后重复
			"",
			"https://c.example.com",
		},
	}
	pool := p.EndpointPool()
	if len(pool) != 3 || pool[0] != "https://a.example.com" ||
		pool[1] != "https://b.example.com" || pool[2] != "https://c.example.com" {
		t.Fatalf("地址池归一化/去重错误: %v", pool)
	}

	single := &Provider{APIURL: "https://only.example.com"}
	if got := single.EndpointPool(); len(got) != 1 {
		t.Fatalf("单地址供应商池应为 1: %v", got)
	}
}

func TestValidateFallbackURLs(t *testing.T) {
	if errs := validateFallbackURLs([]string{"https://a.com", "http://b.com"}); len(errs) != 0 {
		t.Errorf("合法备用地址不应报错: %v", errs)
	}
	if errs := validateFallbackURLs([]string{"a", "b", "c", "d", "e"}); len(errs) == 0 {
		t.Error("超过 4 个备用地址应报错")
	}
	if errs := validateFallbackURLs([]string{"ftp://x.com"}); len(errs) != 1 || !strings.Contains(errs[0], "http") {
		t.Errorf("非 http/https 地址应报错: %v", errs)
	}
}

// ==================== 冷却存储 ====================

func TestEndpointCooldownStore(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	store := newEndpointCooldownStore()
	store.nowFn = func() time.Time { return now }

	pool := []string{"https://a.com", "https://b.com", "https://c.com"}

	// 初始：原序
	if got := store.Order("claude", 1, pool); got[0] != "https://a.com" || len(got) != 3 {
		t.Fatalf("无冷却时应保持原序: %v", got)
	}

	// a 失败：排队尾
	store.MarkFailure("claude", 1, "https://a.com", time.Minute)
	got := store.Order("claude", 1, pool)
	if got[0] != "https://b.com" || got[2] != "https://a.com" {
		t.Fatalf("冷却中的地址应排队尾: %v", got)
	}

	// 不同供应商/平台互不影响
	if got := store.Order("claude", 2, pool); got[0] != "https://a.com" {
		t.Fatalf("冷却不应跨供应商生效: %v", got)
	}
	if got := store.Order("codex", 1, pool); got[0] != "https://a.com" {
		t.Fatalf("冷却不应跨平台生效: %v", got)
	}

	// 全冷却：只放最早到期者 half-open
	store.MarkFailure("claude", 1, "https://b.com", 2*time.Minute)
	store.MarkFailure("claude", 1, "https://c.com", 3*time.Minute)
	got = store.Order("claude", 1, pool)
	if len(got) != 1 || got[0] != "https://a.com" {
		t.Fatalf("全冷却应只放最早到期地址: %v", got)
	}

	// 成功清除冷却
	store.MarkSuccess("claude", 1, "https://a.com")
	got = store.Order("claude", 1, pool)
	if got[0] != "https://a.com" || len(got) != 3 {
		t.Fatalf("成功后应立即恢复参战: %v", got)
	}

	// 过期惰性清理
	now = now.Add(10 * time.Minute)
	got = store.Order("claude", 1, pool)
	if len(got) != 3 || got[0] != "https://a.com" {
		t.Fatalf("过期冷却应自动失效: %v", got)
	}
	store.mu.Lock()
	remaining := len(store.expires)
	store.mu.Unlock()
	if remaining != 0 {
		t.Errorf("过期条目应被惰性清理, 剩余 %d", remaining)
	}
}

// ==================== 错误分类 ====================

func TestAddressSwitchableError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"客户端取消", errClientAbort, false},
		{"响应已提交断流", errUpstreamStreamAborted, false},
		{"请求内容被拒", errUpstreamClientError, false},
		{"401 凭据类", &upstreamStatusError{status: 401, detail: "x"}, false},
		{"404", &upstreamStatusError{status: 404, detail: "x"}, false},
		{"408 超时", &upstreamStatusError{status: 408, detail: "x"}, true},
		{"421", &upstreamStatusError{status: 421, detail: "x"}, true},
		{"429 限流", &upstreamStatusError{status: 429, detail: "x"}, true},
		{"500", &upstreamStatusError{status: 500, detail: "x"}, true},
		{"503", &upstreamStatusError{status: 503, detail: "x"}, true},
		{"传输层错误", errors.New("dial tcp: connection refused"), true},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := addressSwitchableError(tc.err); got != tc.want {
				t.Errorf("addressSwitchableError(%v) = %v, 期望 %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestRetryAfterParsing(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	if d := parseRetryAfter("30", now); d != 30*time.Second {
		t.Errorf("秒数解析错误: %v", d)
	}
	if d := parseRetryAfter("99999", now); d != 10*time.Minute {
		t.Errorf("离谱值应封顶 10 分钟: %v", d)
	}
	if d := parseRetryAfter("garbage", now); d != 0 {
		t.Errorf("垃圾值应返回 0: %v", d)
	}
	if d := parseRetryAfter(now.Add(90*time.Second).Format(http.TimeFormat), now); d != 90*time.Second {
		t.Errorf("HTTP 日期解析错误: %v", d)
	}

	// retryAfterOf：无建议给默认
	if d := retryAfterOf(errors.New("x")); d != defaultEndpointCooldown {
		t.Errorf("无建议应给默认冷却: %v", d)
	}
	if d := retryAfterOf(&upstreamStatusError{status: 429, retryAfter: 5 * time.Second}); d != 5*time.Second {
		t.Errorf("应取错误内建议值: %v", d)
	}
}

// ==================== 转发级：同一请求内地址兜底 ====================

// 主地址 5xx → 同一请求内改试备用地址成功；两个地址各被打一次
func TestForwardRequestFallsBackToSecondAddress(t *testing.T) {
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)

	var hitsA, hitsB atomic.Int32
	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsA.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer upstreamA.Close()
	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsB.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","content":[]}`))
	}))
	defer upstreamB.Close()

	ps := NewProviderService()
	if err := ps.SaveProviders("claude", []Provider{{
		ID: 1, Name: "MultiAddr", APIURL: upstreamA.URL, APIKey: "sk-x", Enabled: true,
		FallbackAPIURLs: []string{upstreamB.URL},
	}}); err != nil {
		t.Fatalf("预置供应商失败: %v", err)
	}
	prs := newTestRelayService(ps)

	router := gin.New()
	router.POST("/v1/messages", prs.proxyHandler("claude", "/v1/messages"))

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m","stream":false}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望备用地址接管返回 200, got %d: %s", w.Code, w.Body.String())
	}
	if hitsA.Load() != 1 || hitsB.Load() != 1 {
		t.Errorf("多地址路径应关闭隐藏重试：A=%d B=%d, 期望各 1 次", hitsA.Load(), hitsB.Load())
	}
}

// 凭据类失败（401）不切地址：备用地址一次都不该被打
func TestForwardRequestDoesNotSwitchOnAuthError(t *testing.T) {
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)

	var hitsA, hitsB atomic.Int32
	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsA.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer upstreamA.Close()
	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsB.Add(1)
	}))
	defer upstreamB.Close()

	ps := NewProviderService()
	if err := ps.SaveProviders("claude", []Provider{{
		ID: 1, Name: "AuthFail", APIURL: upstreamA.URL, APIKey: "sk-x", Enabled: true,
		FallbackAPIURLs: []string{upstreamB.URL},
	}}); err != nil {
		t.Fatalf("预置供应商失败: %v", err)
	}
	prs := newTestRelayService(ps)

	router := gin.New()
	router.POST("/v1/messages", prs.proxyHandler("claude", "/v1/messages"))

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("401 不应成功, got %d", w.Code)
	}
	if hitsB.Load() != 0 {
		t.Errorf("凭据类失败不应改试备用地址, B 被打了 %d 次", hitsB.Load())
	}
	if hitsA.Load() != 1 {
		t.Errorf("多地址路径应关闭隐藏重试, A=%d", hitsA.Load())
	}
}

// ==================== 健康探测：主地址失败备用接管 ====================

func TestHealthProbeFallsBackToSecondAddress(t *testing.T) {
	setupRenameTestEnv(t)

	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstreamA.Close()
	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_ok"}`))
	}))
	defer upstreamB.Close()

	hcs := NewHealthCheckService(NewProviderService(), nil, NewSettingsService(), nil)
	provider := Provider{
		ID: 7, Name: "HealthMulti", APIURL: upstreamA.URL, APIKey: "sk-h", Enabled: true,
		FallbackAPIURLs: []string{upstreamB.URL},
	}

	result := hcs.checkProvider(context.Background(), provider, "claude")
	if result.Status == HealthStatusFailed {
		t.Fatalf("备用地址可用时不应判失败: %+v", result)
	}
	if !strings.Contains(result.ErrorMessage, "备用地址") {
		t.Errorf("应标注备用地址接管: %q", result.ErrorMessage)
	}

	// 凭据类失败不再探备用
	upstreamAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstreamAuth.Close()
	var fallbackHit atomic.Int32
	upstreamNever := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHit.Add(1)
	}))
	defer upstreamNever.Close()

	provider2 := Provider{
		ID: 8, Name: "HealthAuthFail", APIURL: upstreamAuth.URL, APIKey: "sk-h", Enabled: true,
		FallbackAPIURLs: []string{upstreamNever.URL},
	}
	result2 := hcs.checkProvider(context.Background(), provider2, "claude")
	if result2.Status != HealthStatusFailed {
		t.Fatalf("认证失败应判失败: %+v", result2)
	}
	if fallbackHit.Load() != 0 {
		t.Errorf("认证失败不应继续探备用地址, 被打了 %d 次", fallbackHit.Load())
	}
}
