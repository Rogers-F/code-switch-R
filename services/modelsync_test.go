package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	modelpricing "codeswitch/resources/model-pricing"
)

// newTestSyncService 构造隔离的同步服务(独立目录/独立定价实例/无种子)。
func newTestSyncService(t *testing.T, baseURLs []string) *ModelSyncService {
	t.Helper()
	pricing, err := modelpricing.NewService()
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &ModelSyncService{
		pricing:    pricing,
		baseURLs:   baseURLs,
		httpClient: &http.Client{},
		dir:        t.TempDir(),
		state:      newModelSyncState(),
		catalogs:   make(map[string]*modelpricing.RemoteCatalog),
		sources:    make(map[string]string),
		seed:       make(map[string]*modelpricing.RemoteCatalog),
		rootCtx:    ctx,
		cancel:     cancel,
	}
}

// testCatalogJSON 生成一份最小合法目录。
func testCatalogJSON(provider string, models map[string]map[string]any) []byte {
	payload := map[string]any{"id": provider, "models": models}
	data, _ := json.Marshal(payload)
	return data
}

func testModelSpec(input, output float64) map[string]any {
	return map[string]any{
		"cost": map[string]any{"input": input, "output": output},
	}
}

func TestValidateBatchCanary(t *testing.T) {
	s := newTestSyncService(t, nil)
	body := testCatalogJSON("openai", map[string]map[string]any{
		"foo-model": testModelSpec(1, 2),
	})
	if _, err := s.validateBatch("openai", body, nil, nil); err == nil {
		t.Error("缺少 gpt-* canary 应拒收")
	}
	body = testCatalogJSON("openai", map[string]map[string]any{
		"gpt-5.6": testModelSpec(5, 30),
	})
	if _, err := s.validateBatch("openai", body, nil, nil); err != nil {
		t.Errorf("含 canary 的批次应通过: %v", err)
	}
}

func TestValidateBatchHighWaterShrink(t *testing.T) {
	s := newTestSyncService(t, nil)
	prev := &providerSyncState{HighWater: 10, HighWaterAt: time.Now()}
	models := map[string]map[string]any{}
	for i := 0; i < 4; i++ { // 4 < ceil(10*0.5)=5
		models[fmt.Sprintf("gpt-5.%d", i)] = testModelSpec(1, 2)
	}
	body := testCatalogJSON("openai", models)
	if _, err := s.validateBatch("openai", body, prev, nil); err == nil {
		t.Error("模型数骤降应隔离批次")
	}
	// 高水位过期(>30 天)后不再约束
	prevOld := &providerSyncState{HighWater: 10, HighWaterAt: time.Now().Add(-31 * 24 * time.Hour)}
	if _, err := s.validateBatch("openai", body, prevOld, nil); err != nil {
		t.Errorf("过期高水位不应拦截: %v", err)
	}
}

func TestValidateBatchPriceJump(t *testing.T) {
	s := newTestSyncService(t, nil)
	prev := &providerSyncState{LastPrices: map[string][2]float64{"gpt-5.6": {5, 30}}}
	body := testCatalogJSON("openai", map[string]map[string]any{
		"gpt-5.6": testModelSpec(60, 30), // 60 > 5*10
	})
	if _, err := s.validateBatch("openai", body, prev, nil); err == nil {
		t.Error("已知模型价格跳变超 10 倍应隔离")
	}
	// 旧值为 0 时不做倍率检查(首次提供价格是正常情形)
	prevZero := &providerSyncState{LastPrices: map[string][2]float64{"gpt-5.6": {0, 0}}}
	if _, err := s.validateBatch("openai", body, prevZero, nil); err != nil {
		t.Errorf("旧值为 0 不应触发倍率隔离: %v", err)
	}
}

func TestValidateBatchFamilyRegression(t *testing.T) {
	s := newTestSyncService(t, nil)
	prevCatalog, err := modelpricing.ParseRemoteCatalog("google", testCatalogJSON("google", map[string]map[string]any{
		"gemini-3.1-pro-preview": testModelSpec(2, 12),
		"gemini-3.5-flash":       testModelSpec(1.5, 9),
	}))
	if err != nil {
		t.Fatalf("ParseRemoteCatalog: %v", err)
	}
	body := testCatalogJSON("google", map[string]map[string]any{
		"gemini-3.5-flash": testModelSpec(1.5, 9), // pro 家族整族消失
	})
	if _, err := s.validateBatch("google", body, nil, prevCatalog); err == nil {
		t.Error("既有 pro 家族整族消失应隔离")
	}
}

// TestRunSyncEndToEnd 主源全量同步:落盘、状态、目录来源、定价热更新与条件请求。
func TestRunSyncEndToEnd(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.Header.Get("If-None-Match") == `W/"gen-1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		var body []byte
		switch r.URL.Path {
		case "/api/providers/anthropic.json":
			body = testCatalogJSON("anthropic", map[string]map[string]any{
				"claude-sync-test": testModelSpec(10, 50),
			})
		case "/api/providers/openai.json":
			body = testCatalogJSON("openai", map[string]map[string]any{
				"gpt-5.6": testModelSpec(5, 30),
			})
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", `W/"gen-1"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	s := newTestSyncService(t, []string{server.URL})
	s.runSync([]string{"anthropic", "openai"})

	s.mu.Lock()
	anthropicState := s.state.Providers["anthropic"]
	openaiState := s.state.Providers["openai"]
	source := s.sources["anthropic"]
	s.mu.Unlock()

	if anthropicState == nil || anthropicState.ModelCount != 1 || anthropicState.LastError != "" {
		t.Fatalf("anthropic 状态异常: %+v", anthropicState)
	}
	if openaiState == nil || openaiState.ETagByOrigin[server.URL] != `W/"gen-1"` {
		t.Fatalf("openai ETag 未记录: %+v", openaiState)
	}
	if source != "remote" {
		t.Errorf("来源应为 remote,实际 %s", source)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "anthropic.json")); err != nil {
		t.Errorf("缓存文件未落盘: %v", err)
	}
	if !s.pricing.HasPositivePricing("claude-sync-test") {
		t.Error("同步后新模型应有价")
	}

	// 第二轮:条件请求命中 304,数据不重拉(FetchedAt 不变),但周期刷新(NextDue 距今约 24h)
	firstFetched := openaiState.FetchedAt
	secondRunAt := time.Now()
	s.runSync([]string{"openai"})
	s.mu.Lock()
	openaiState2 := s.state.Providers["openai"]
	s.mu.Unlock()
	if openaiState2.LastError != "" {
		t.Errorf("304 不应记错误: %+v", openaiState2)
	}
	if !openaiState2.FetchedAt.Equal(firstFetched) {
		t.Errorf("304 不应更新 FetchedAt: %v -> %v", firstFetched, openaiState2.FetchedAt)
	}
	if openaiState2.NextDue.Before(secondRunAt.Add(23 * time.Hour)) {
		t.Errorf("304 后 NextDue 应约为 24h 后,实际 %v", openaiState2.NextDue.Sub(secondRunAt))
	}
}

// TestRunSyncMirrorFallback 主源 5xx 时改走镜像。
func TestRunSyncMirrorFallback(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer primary.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/providers/openai.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(testCatalogJSON("openai", map[string]map[string]any{
			"gpt-5.6": testModelSpec(5, 30),
		}))
	}))
	defer mirror.Close()

	s := newTestSyncService(t, []string{primary.URL, mirror.URL})
	s.runSync([]string{"openai"})

	s.mu.Lock()
	st := s.state.Providers["openai"]
	src := s.sources["openai"]
	s.mu.Unlock()
	if st == nil || st.LastError != "" || src != "remote" {
		t.Fatalf("镜像回退失败: state=%+v source=%s", st, src)
	}
}

// TestRunSyncFailureBackoff 全部源失败时记录错误并退避。
func TestRunSyncFailureBackoff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	s := newTestSyncService(t, []string{server.URL})
	before := time.Now()
	s.runSync([]string{"openai"})

	s.mu.Lock()
	st := s.state.Providers["openai"]
	s.mu.Unlock()
	if st == nil || st.LastError == "" || st.FailStreak != 1 {
		t.Fatalf("失败应记录错误与退避: %+v", st)
	}
	if st.NextDue.Before(before.Add(50*time.Minute)) || st.NextDue.After(before.Add(70*time.Minute)) {
		t.Errorf("首次失败退避应约 1h,实际 %v", st.NextDue.Sub(before))
	}
}

// TestLoadLocalCatalogsHashMismatch 缓存被篡改时废弃凭据并回退种子。
func TestLoadLocalCatalogsHashMismatch(t *testing.T) {
	s := newTestSyncService(t, nil)
	seedCatalog, err := modelpricing.ParseRemoteCatalog("openai", testCatalogJSON("openai", map[string]map[string]any{
		"gpt-seed": testModelSpec(1, 2),
	}))
	if err != nil {
		t.Fatalf("ParseRemoteCatalog: %v", err)
	}
	s.seed = map[string]*modelpricing.RemoteCatalog{"openai": seedCatalog}
	s.state.Providers["openai"] = &providerSyncState{
		SHA256:       "deadbeef",
		ETagByOrigin: map[string]string{"https://x": `W/"1"`},
		NextDue:      time.Now().Add(time.Hour),
	}
	if err := os.WriteFile(filepath.Join(s.dir, "openai.json"), testCatalogJSON("openai", map[string]map[string]any{
		"gpt-tampered": testModelSpec(1, 2),
	}), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	s.loadLocalCatalogs()

	s.mu.Lock()
	st := s.state.Providers["openai"]
	src := s.sources["openai"]
	catalog := s.catalogs["openai"]
	s.mu.Unlock()
	if st.ETagByOrigin != nil || st.SHA256 != "" || !st.NextDue.IsZero() {
		t.Errorf("哈希失配应清空凭据并立即到期: %+v", st)
	}
	if src != "seed" || catalog == nil {
		t.Errorf("应回退种子: source=%s", src)
	}
	if _, ok := catalog.Models["gpt-seed"]; !ok {
		t.Error("目录应来自种子而非被篡改文件")
	}
	if len(st.LastPrices) == 0 {
		t.Error("应以生效目录初始化 LastPrices 基线(10x 跳变校验首轮生效)")
	}
}

// TestRestoreBuiltinPricing 恢复内置:关自动同步、清缓存、种子即时生效。
func TestRestoreBuiltinPricing(t *testing.T) {
	s := newTestSyncService(t, nil)
	s.appSettings = &AppSettingsService{path: filepath.Join(t.TempDir(), "app.json")}

	// 先放一份"远程"数据
	s.state.Providers["openai"] = &providerSyncState{SHA256: "abc"}
	if err := os.WriteFile(filepath.Join(s.dir, "openai.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := s.pricing.Rebuild(map[string]modelpricing.RemoteEntry{
		"restore-test-model": {Input: fptr(1e-6), Output: fptr(1e-6)},
	}); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	if _, err := s.RestoreBuiltinPricing(); err != nil {
		t.Fatalf("RestoreBuiltinPricing: %v", err)
	}

	settings, err := s.appSettings.GetAppSettings()
	if err != nil {
		t.Fatalf("GetAppSettings: %v", err)
	}
	if settings.AutoSyncModels {
		t.Error("恢复内置应关闭自动同步")
	}
	if _, err := os.Stat(filepath.Join(s.dir, "openai.json")); !os.IsNotExist(err) {
		t.Error("缓存文件应被删除")
	}
	if s.pricing.HasPositivePricing("restore-test-model") {
		t.Error("恢复后远程价格应消失")
	}
}

// TestGetSyncStatusWithPolicyNoDeadlock 状态查询会经 policy 回调 Catalogs(),
// 必须不与 s.mu 重入死锁(回归:曾因持锁调用 policy 导致永久阻塞)。
func TestGetSyncStatusWithPolicyNoDeadlock(t *testing.T) {
	s := newTestSyncService(t, nil)
	policy := NewDefaultModelPolicy()
	policy.SetSource(s)
	s.policy = policy

	done := make(chan struct{})
	go func() {
		_ = s.GetSyncStatus()
		_ = s.runSync(nil) // 单飞被拒路径同样要走锁外装饰
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("GetSyncStatus/runSync 与 Catalogs 重入死锁")
	}
}

// TestLoadLocalCatalogsMissingFileClearsETag 缓存文件被删后必须废弃条件请求凭据,
// 否则服务端持续 304 会让应用永远卡在种子数据上。
func TestLoadLocalCatalogsMissingFileClearsETag(t *testing.T) {
	s := newTestSyncService(t, nil)
	s.state.Providers["openai"] = &providerSyncState{
		SHA256:       "deadbeef",
		ETagByOrigin: map[string]string{"https://x": `W/"1"`},
		NextDue:      time.Now().Add(time.Hour),
	}
	s.loadLocalCatalogs() // 目录下没有 openai.json

	st := s.state.Providers["openai"]
	if st.ETagByOrigin != nil || st.SHA256 != "" || !st.NextDue.IsZero() {
		t.Fatalf("缺失文件应清空缓存凭据并立即到期: %+v", st)
	}
}

// TestSyncSingleFlight 并发触发只有一个批次真正执行。
func TestSyncSingleFlight(t *testing.T) {
	var hits int32
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		<-block
		_, _ = w.Write(testCatalogJSON("openai", map[string]map[string]any{
			"gpt-5.6": testModelSpec(5, 30),
		}))
	}))
	defer server.Close()

	s := newTestSyncService(t, []string{server.URL})
	done := make(chan struct{})
	go func() {
		s.runSync([]string{"openai"})
		close(done)
	}()
	// 等第一个批次进入请求
	for atomic.LoadInt32(&hits) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	// 第二次触发应直接返回(单飞),不产生新请求
	s.runSync([]string{"openai"})
	if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("单飞期间不应发起第二个请求,hits=%d", hits)
	}
	close(block)
	<-done
}
