package services

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestApplyOutcomesContextCanceledNotFailure 回归:关停取消在途批次不应计入
// 失败退避账本(FailStreak/LastError/NextDue 均不动),真实失败仍照常退避。
func TestApplyOutcomesContextCanceledNotFailure(t *testing.T) {
	s := newTestSyncService(t, nil)
	prevDue := time.Now().Add(30 * time.Minute)
	s.state.Providers["openai"] = &providerSyncState{NextDue: prevDue}

	s.applyOutcomes([]providerOutcome{
		{id: "openai", err: context.Canceled},
		{id: "google", err: fmt.Errorf("fetch: %w", context.Canceled)}, // 包装形式同样豁免
	})

	s.mu.Lock()
	st := s.state.Providers["openai"]
	gst := s.state.Providers["google"]
	s.mu.Unlock()
	if st.FailStreak != 0 || st.LastError != "" {
		t.Fatalf("取消不应记失败: %+v", st)
	}
	if !st.NextDue.Equal(prevDue) {
		t.Errorf("取消不应改动 NextDue: %v -> %v", prevDue, st.NextDue)
	}
	if st.CheckedAt.IsZero() {
		t.Error("取消仍应更新 CheckedAt")
	}
	if gst == nil || gst.FailStreak != 0 || gst.LastError != "" || !gst.NextDue.IsZero() {
		t.Errorf("包装的取消错误同样不应记失败: %+v", gst)
	}

	// 真实失败仍照常记退避
	s.applyOutcomes([]providerOutcome{{id: "openai", err: fmt.Errorf("HTTP 500")}})
	s.mu.Lock()
	st = s.state.Providers["openai"]
	s.mu.Unlock()
	if st.FailStreak != 1 || st.LastError == "" {
		t.Errorf("真实失败应计退避: %+v", st)
	}
}

// TestSyncNowReportsNotRunningAfterCompletion 回归:同步批次已结束时,
// runSync 的返回状态不应仍报 Running=true。
func TestSyncNowReportsNotRunningAfterCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	s := newTestSyncService(t, []string{server.URL})
	status := s.runSync([]string{"openai"})
	if status.Running {
		t.Error("批次结束后返回状态不应为 Running=true")
	}
}

// TestRestoreBuiltinPricingRejectedAfterStop 回归:Stop() 之后恢复内置应被拒绝,
// 不得再删缓存/写盘,避免与关停并发留下混合持久化态。
func TestRestoreBuiltinPricingRejectedAfterStop(t *testing.T) {
	s := newTestSyncService(t, nil)
	cache := filepath.Join(s.dir, "openai.json")
	if err := os.WriteFile(cache, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	s.Stop()
	if _, err := s.RestoreBuiltinPricing(); err == nil {
		t.Fatal("停止后恢复内置应返回错误")
	}
	if _, err := os.Stat(cache); err != nil {
		t.Errorf("停止后缓存文件不应被删除: %v", err)
	}
}
