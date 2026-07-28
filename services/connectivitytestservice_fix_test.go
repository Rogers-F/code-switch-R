package services

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// setupConnectivityTestHome 把家目录隔离到临时目录并写入一份 claude 供应商配置。
func setupConnectivityTestHome(t *testing.T, providers []Provider) {
	t.Helper()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	// Windows 的 os.UserHomeDir() 读的是 USERPROFILE,只设 HOME 会让测试写到真实用户配置目录
	t.Setenv("USERPROFILE", tmpHome)
	assertHomeIsolated(t, tmpHome)

	configDir := filepath.Join(tmpHome, ".code-switch")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("创建配置目录失败: %v", err)
	}

	data, err := serializeProviders(providers)
	if err != nil {
		t.Fatalf("序列化 fixture 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "claude-code.json"), data, 0o644); err != nil {
		t.Fatalf("写 fixture 失败: %v", err)
	}
}

// TestConnectivityTestAll_FiltersByAvailabilityMonitor 验证 TestAll 按 AvailabilityMonitorEnabled 过滤：
// 旧字段 ConnectivityCheck 在保存时会被清零，若仍按它过滤则所有供应商都被跳过、自动测试恒空转。
func TestConnectivityTestAll_FiltersByAvailabilityMonitor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"hi"}]}`))
	}))
	defer server.Close()

	setupConnectivityTestHome(t, []Provider{
		{ID: 1, Name: "Monitored", APIURL: server.URL, APIKey: "k", AvailabilityMonitorEnabled: true},
		{ID: 2, Name: "NotMonitored", APIURL: server.URL, APIKey: "k"},
	})

	cts := NewConnectivityTestService(NewProviderService(), nil, nil, nil)
	results := cts.TestAll("claude")

	if len(results) != 1 {
		t.Fatalf("应只测试启用可用性监控的 1 个供应商，实际测试了 %d 个", len(results))
	}
	if results[0].ProviderID != 1 {
		t.Errorf("被测试的应是 ID=1 的 Monitored，实际 ID=%d", results[0].ProviderID)
	}
}

// countGoroutinesContaining 统计当前所有 goroutine 栈中包含指定函数名片段的数量。
func countGoroutinesContaining(t *testing.T, fragment string) int {
	t.Helper()
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return strings.Count(string(buf[:n]), fragment)
}

// TestConnectivityAutoTest_StopStartLeavesSinglePoller 验证快速关-开自动测试后只剩一个轮询协程：
// 旧实现协程每轮无锁重读 cts.stopChan，旧协程忙于 TestAll 时错过 close，
// 回到 select 读到新 channel 后与新协程并存双跑。
func TestConnectivityAutoTest_StopStartLeavesSinglePoller(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		// 拖慢单次测试，制造旧协程在 close 时正忙于 TestAll 的窗口
		time.Sleep(800 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"hi"}]}`))
	}))
	defer server.Close()

	setupConnectivityTestHome(t, []Provider{
		{ID: 1, Name: "Slow", APIURL: server.URL, APIKey: "k", AvailabilityMonitorEnabled: true},
	})

	cts := NewConnectivityTestService(NewProviderService(), nil, nil, nil)
	t.Cleanup(func() {
		_ = cts.SetAutoTestEnabled(false)
		// 等待轮询协程退出，避免污染其他测试
		time.Sleep(200 * time.Millisecond)
	})

	if err := cts.SetAutoTestEnabled(true); err != nil {
		t.Fatalf("开启自动测试失败: %v", err)
	}
	// 让旧协程进入慢速 TestAll
	time.Sleep(100 * time.Millisecond)
	if err := cts.SetAutoTestEnabled(false); err != nil {
		t.Fatalf("关闭自动测试失败: %v", err)
	}
	if err := cts.SetAutoTestEnabled(true); err != nil {
		t.Fatalf("重新开启自动测试失败: %v", err)
	}

	// 等两轮初始 TestAll 都结束后，轮询协程数应收敛为 1
	deadline := time.Now().Add(10 * time.Second)
	for {
		if countGoroutinesContaining(t, "startAutoTest.func") <= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("关-开切换后仍有 %d 个轮询协程存活（应为 1）",
				countGoroutinesContaining(t, "startAutoTest.func"))
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got := countGoroutinesContaining(t, "startAutoTest.func"); got != 1 {
		t.Errorf("自动测试开启中应恰有 1 个轮询协程，实际 %d", got)
	}
}
