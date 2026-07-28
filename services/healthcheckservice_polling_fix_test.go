package services

import (
	"testing"
	"time"
)

// TestHealthCheckPolling_StopStartLeavesSinglePoller 验证 Stop→Start 快速切换后只剩一个巡检协程：
// 旧实现协程每轮无锁重读 hcs.stopChan，旧协程处于启动 jitter（0-10s）期间错过 close，
// 回到 select 读到新 channel 后永不退出，与新协程双跑（失败计数翻倍、历史重复写入）。
func TestHealthCheckPolling_StopStartLeavesSinglePoller(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	// Windows 的 os.UserHomeDir() 读的是 USERPROFILE,只设 HOME 会让测试写到真实用户配置目录
	t.Setenv("USERPROFILE", tmpHome)
	assertHomeIsolated(t, tmpHome)

	hcs := NewHealthCheckService(NewProviderService(), nil, nil, nil)
	t.Cleanup(func() {
		hcs.StopBackgroundPolling()
		// 等待巡检协程退出，避免污染其他测试
		time.Sleep(200 * time.Millisecond)
	})

	// 首次启动：协程 G1 处于 0-10s 的启动 jitter 睡眠中
	hcs.StartBackgroundPolling()
	// 立即 Stop→Start：关闭旧 channel 并换新，触发缺陷窗口
	hcs.StopBackgroundPolling()
	hcs.StartBackgroundPolling()

	// jitter 最长 10s；等两个协程都完成初始检测（无供应商配置，检测本身极快）后，
	// 巡检协程数应稳定收敛为 1（旧协程读到已关闭的捕获 channel 后退出）
	if got := waitForStableGoroutineCount(t, "StartBackgroundPolling.func", 1, 25*time.Second); got != 1 {
		t.Errorf("巡检开启中应恰有 1 个巡检协程，实际 %d（旧协程未退出说明 Stop→Start 竞态仍在）", got)
	}
}
