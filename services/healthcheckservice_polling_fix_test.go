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
	// 巡检协程数应收敛为 1（旧协程读到已关闭的捕获 channel 后退出）
	deadline := time.Now().Add(15 * time.Second)
	for {
		if countGoroutinesContaining(t, "StartBackgroundPolling.func") <= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Stop→Start 切换后仍有 %d 个巡检协程存活（应为 1）",
				countGoroutinesContaining(t, "StartBackgroundPolling.func"))
		}
		time.Sleep(200 * time.Millisecond)
	}
	if got := countGoroutinesContaining(t, "StartBackgroundPolling.func"); got != 1 {
		t.Errorf("巡检开启中应恰有 1 个巡检协程，实际 %d", got)
	}
}
