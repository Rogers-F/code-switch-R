package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// ==================== 限流器单元 ====================

func TestConcurrencyLimiterBasic(t *testing.T) {
	l := newConcurrencyLimiter()

	if !l.TryAcquire("claude", "1", 2, 1) || !l.TryAcquire("claude", "1", 2, 1) {
		t.Fatal("容量 2 应允许两次占用")
	}
	if l.TryAcquire("claude", "1", 2, 1) {
		t.Fatal("第三次占用应被拒绝")
	}
	// 平台隔离：不同平台同 key 互不影响
	if !l.TryAcquire("codex", "1", 1, 1) {
		t.Fatal("不同平台应独立计数")
	}
	l.Release("claude", "1")
	if !l.TryAcquire("claude", "1", 2, 1) {
		t.Fatal("释放后应可再次占用")
	}
	// limit=0 不限但跟踪 inFlight
	if !l.TryAcquire("claude", "free", 0, 1) || !l.TryAcquire("claude", "free", 0, 1) {
		t.Fatal("不限容量应始终放行")
	}
	if got := l.snapshotInFlight("claude", "free"); got != 2 {
		t.Fatalf("不限容量也要跟踪 inFlight, got %d", got)
	}
}

func TestConcurrencyLimiterGenerationGuard(t *testing.T) {
	l := newConcurrencyLimiter()

	// gen=1 容量 2，占满
	l.TryAcquire("claude", "1", 2, 1)
	l.TryAcquire("claude", "1", 2, 1)

	// 配置改为 1（gen=2）：在途 2 个保留，但新请求按新容量拒绝
	if l.TryAcquire("claude", "1", 1, 2) {
		t.Fatal("降容后 inFlight>=newLimit 应拒绝新请求")
	}
	// 旧代请求不得把容量改回 2
	if l.TryAcquire("claude", "1", 2, 1) {
		t.Fatal("旧配置代数不得回写容量")
	}
	l.Release("claude", "1")
	l.Release("claude", "1")
	// 降容生效后只放 1 个
	if !l.TryAcquire("claude", "1", 1, 2) {
		t.Fatal("清空后新容量应放行 1 个")
	}
	if l.TryAcquire("claude", "1", 1, 2) {
		t.Fatal("新容量 1 不应放第 2 个")
	}
	// 升容（gen=3）立即生效
	if !l.TryAcquire("claude", "1", 3, 3) {
		t.Fatal("升容后应放行")
	}
	// 0→N：从不限改有限时已跟踪的 inFlight 生效
	l2 := newConcurrencyLimiter()
	l2.TryAcquire("claude", "x", 0, 1)
	l2.TryAcquire("claude", "x", 0, 1)
	if l2.TryAcquire("claude", "x", 2, 2) {
		t.Fatal("不限改为 2 时在途已有 2 个，应拒绝")
	}
}

func TestConcurrencyLimiterWaitSignal(t *testing.T) {
	l := newConcurrencyLimiter()
	l.TryAcquire("claude", "1", 1, 1)

	signal := l.releaseSignal()
	go func() {
		time.Sleep(30 * time.Millisecond)
		l.Release("claude", "1")
	}()
	if !l.waitForRelease(context.Background(), time.Now().Add(2*time.Second), signal) {
		t.Fatal("释放后应收到信号")
	}

	// 信号先于等待发生（先取引用→再扫描→期间释放）也不丢失
	l.TryAcquire("claude", "1", 1, 2)
	signal2 := l.releaseSignal()
	l.Release("claude", "1") // 在 select 之前释放
	if !l.waitForRelease(context.Background(), time.Now().Add(2*time.Second), signal2) {
		t.Fatal("扫描间隙的释放不得丢失唤醒")
	}

	// ctx 取消
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if l.waitForRelease(ctx, time.Now().Add(time.Second), l.releaseSignal()) {
		t.Fatal("ctx 取消应返回 false")
	}
	// deadline 耗尽
	if l.waitForRelease(context.Background(), time.Now().Add(-time.Second), l.releaseSignal()) {
		t.Fatal("过期 deadline 应返回 false")
	}
}

func TestConcurrencyLimiterWaiterCap(t *testing.T) {
	l := newConcurrencyLimiter()
	for i := 0; i < concurrencyWaiterLimit; i++ {
		if !l.enterWaitPhase() {
			t.Fatalf("第 %d 个等待者不应被拒", i+1)
		}
	}
	if l.enterWaitPhase() {
		t.Fatal("超过等待者上限应被拒")
	}
	l.leaveWaitPhase()
	if !l.enterWaitPhase() {
		t.Fatal("释放名额后应可再进入")
	}
}

func TestConcurrencyLimiterNoLeak(t *testing.T) {
	l := newConcurrencyLimiter()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.TryAcquire("claude", "1", 8, 1) {
				time.Sleep(time.Millisecond)
				l.Release("claude", "1")
			}
		}()
	}
	wg.Wait()
	if got := l.snapshotInFlight("claude", "1"); got != 0 {
		t.Fatalf("全部释放后 inFlight 应为 0, got %d", got)
	}
}

// ==================== handler 级 ====================

// maxConcurrency=1 的供应商被占用时，第二个并发请求应立即改走下一供应商
func TestConcurrencyBusySkipsToNextProvider(t *testing.T) {
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)

	releaseA := make(chan struct{})
	var hitsA, hitsB atomic.Int32
	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsA.Add(1)
		<-releaseA
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"a"}`))
	}))
	defer upstreamA.Close()
	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsB.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b"}`))
	}))
	defer upstreamB.Close()

	ps := NewProviderService()
	if err := ps.SaveProviders("claude", []Provider{
		{ID: 1, Name: "Limited", APIURL: upstreamA.URL, APIKey: "k1", Enabled: true, Level: 1, MaxConcurrency: 1},
		{ID: 2, Name: "Backup", APIURL: upstreamB.URL, APIKey: "k2", Enabled: true, Level: 2},
	}); err != nil {
		t.Fatalf("预置供应商失败: %v", err)
	}
	prs := newTestRelayService(ps)
	router := gin.New()
	router.POST("/v1/messages", prs.proxyHandler("claude", "/v1/messages"))

	// 第一个请求占住 Limited
	firstDone := make(chan int, 1)
	go func() {
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		firstDone <- w.Code
	}()

	// 等到 Limited 确实被占用
	deadline := time.Now().Add(2 * time.Second)
	for hitsA.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if hitsA.Load() == 0 {
		t.Fatal("首请求未打到 Limited")
	}

	// 第二个请求：Limited 满载，应改走 Backup
	req2 := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK || hitsB.Load() != 1 {
		t.Fatalf("第二请求应由 Backup 接住: code=%d hitsB=%d", w2.Code, hitsB.Load())
	}
	if hitsA.Load() != 1 {
		t.Fatalf("Limited 满载期间不应再被打: hitsA=%d", hitsA.Load())
	}

	close(releaseA)
	if code := <-firstDone; code != http.StatusOK {
		t.Fatalf("首请求应成功: %d", code)
	}
}

// 唯一供应商满载：第二请求进入等待，配额释放后接管成功
func TestConcurrencyWaitPhaseTakesOverAfterRelease(t *testing.T) {
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)

	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if n == 1 {
			time.Sleep(150 * time.Millisecond)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	defer upstream.Close()

	ps := NewProviderService()
	if err := ps.SaveProviders("claude", []Provider{
		{ID: 1, Name: "Only", APIURL: upstream.URL, APIKey: "k", Enabled: true, MaxConcurrency: 1},
	}); err != nil {
		t.Fatalf("预置供应商失败: %v", err)
	}
	prs := newTestRelayService(ps)
	router := gin.New()
	router.POST("/v1/messages", prs.proxyHandler("claude", "/v1/messages"))

	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			codes[idx] = w.Code
		}(i)
		time.Sleep(20 * time.Millisecond) // 保证第一个先占住配额
	}
	wg.Wait()

	if codes[0] != http.StatusOK || codes[1] != http.StatusOK {
		t.Fatalf("两个请求都应成功（第二个等待接管）: %v", codes)
	}
	if hits.Load() != 2 {
		t.Fatalf("上游应被打两次: %d", hits.Load())
	}
}

// 等待预算耗尽：纯忙终态 503 + Retry-After + 机器码
func TestConcurrencyAllBusyReturns503(t *testing.T) {
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)

	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()
	// LIFO：必须先放行被卡住的 handler，再让 Close 等待连接结束，
	// 反过来会死锁（Close 等 handler，handler 等 release）
	defer close(release)

	ps := NewProviderService()
	if err := ps.SaveProviders("claude", []Provider{
		{ID: 1, Name: "Only", APIURL: upstream.URL, APIKey: "k", Enabled: true, MaxConcurrency: 1},
	}); err != nil {
		t.Fatalf("预置供应商失败: %v", err)
	}
	prs := newTestRelayService(ps)
	prs.concurrency.waitBudget = 60 * time.Millisecond // 注入短预算
	router := gin.New()
	router.POST("/v1/messages", prs.proxyHandler("claude", "/v1/messages"))

	go func() {
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
		router.ServeHTTP(httptest.NewRecorder(), req)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for prs.concurrency.snapshotInFlight("claude", "1") == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	req2 := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusServiceUnavailable {
		t.Fatalf("纯忙终态应 503, got %d: %s", w2.Code, w2.Body.String())
	}
	if w2.Header().Get("Retry-After") == "" {
		t.Error("应带 Retry-After 头")
	}
	var body map[string]any
	_ = json.Unmarshal(w2.Body.Bytes(), &body)
	if !strings.Contains(w2.Body.String(), "provider_concurrency_exhausted") {
		t.Errorf("应带稳定机器码: %s", w2.Body.String())
	}
}

// gemini 平台：满载供应商被跳过，下一供应商接住
func TestConcurrencyGeminiBusySkips(t *testing.T) {
	setupGeminiTestHome(t)
	gin.SetMode(gin.TestMode)

	releaseA := make(chan struct{})
	var hitsB atomic.Int32
	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-releaseA
		_, _ = w.Write([]byte(`{"candidates":[]}`))
	}))
	defer upstreamA.Close()
	// LIFO：先放行 handler 再关服务器，避免 Close 与 release 互等死锁
	defer close(releaseA)
	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsB.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[]}`))
	}))
	defer upstreamB.Close()

	router, _ := newGeminiTestRelay(t, []GeminiProvider{
		{Name: "GLimited", ID: "g1", BaseURL: upstreamA.URL, APIKey: "k1", Enabled: true, Level: 1, MaxConcurrency: 1},
		{Name: "GBackup", ID: "g2", BaseURL: upstreamB.URL, APIKey: "k2", Enabled: true, Level: 2},
	})

	go func() {
		req := httptest.NewRequest("POST", "/gemini/v1beta/models/gemini-pro:generateContent", strings.NewReader(`{}`))
		router.ServeHTTP(httptest.NewRecorder(), req)
	}()
	time.Sleep(80 * time.Millisecond) // 等首请求占住 GLimited

	req2 := httptest.NewRequest("POST", "/gemini/v1beta/models/gemini-pro:generateContent", strings.NewReader(`{}`))
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK || hitsB.Load() != 1 {
		t.Fatalf("gemini 满载应跳到下一供应商: code=%d hitsB=%d", w2.Code, hitsB.Load())
	}
}

// ==================== 终审回归 ====================

// 混合场景：A 满载 + B 快速真实失败。B 的正常释放不得形成自唤醒
// 重试风暴，也不得在等待重扫中被再次尝试（已实际失败）
func TestConcurrencyMixedBusyAndFailureNoStorm(t *testing.T) {
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)

	holdA := make(chan struct{})
	var hitsA, hitsB atomic.Int32
	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsA.Add(1)
		<-holdA
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstreamA.Close()
	defer close(holdA)
	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsB.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer upstreamB.Close()

	ps := NewProviderService()
	if err := ps.SaveProviders("claude", []Provider{
		{ID: 1, Name: "BusyOne", APIURL: upstreamA.URL, APIKey: "k1", Enabled: true, Level: 1, MaxConcurrency: 1},
		{ID: 2, Name: "FastFail", APIURL: upstreamB.URL, APIKey: "k2", Enabled: true, Level: 2},
	}); err != nil {
		t.Fatalf("预置供应商失败: %v", err)
	}
	prs := newTestRelayService(ps)
	prs.concurrency.waitBudget = 120 * time.Millisecond
	router := gin.New()
	router.POST("/v1/messages", prs.proxyHandler("claude", "/v1/messages"))

	// 占住 BusyOne
	go func() {
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
		router.ServeHTTP(httptest.NewRecorder(), req)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for hitsA.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	// 第二请求：BusyOne 忙、FastFail 真实失败一次 → 等待预算耗尽 → 503
	req2 := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusServiceUnavailable {
		t.Fatalf("混合忙+失败且等待耗尽应 503, got %d: %s", w2.Code, w2.Body.String())
	}
	if hitsB.Load() != 1 {
		t.Fatalf("FastFail 只该被真实尝试一次（无自唤醒风暴、重扫不碰已尝试者）, hitsB=%d", hitsB.Load())
	}
}

// 墓碑保留：空闲不限容量条目不删除，旧代配置不得借重建回写容量
func TestConcurrencyLimiterTombstoneKeepsGeneration(t *testing.T) {
	l := newConcurrencyLimiter()

	// gen=5 声明不限并归零（曾经的删除逻辑会把 entry 抹掉）
	l.TryAcquire("claude", "1", 0, 5)
	l.Release("claude", "1")

	// 携带旧代（gen=3）的在途副本尝试写入旧容量 8
	if !l.TryAcquire("claude", "1", 8, 3) {
		t.Fatal("不限容量条目应放行")
	}
	// 新代（gen=6）降容为 1：在途 1 个，新请求应被拒
	if l.TryAcquire("claude", "1", 1, 6) {
		t.Fatal("降容后应拒绝新请求")
	}
	l.Release("claude", "1")
	// 旧代 8 不得覆盖新代 1
	if !l.TryAcquire("claude", "1", 8, 3) {
		t.Fatal("清空后按当前容量 1 应放行第一个")
	}
	if l.TryAcquire("claude", "1", 8, 3) {
		t.Fatal("旧代容量 8 不得回写，第二个应被拒")
	}
}
