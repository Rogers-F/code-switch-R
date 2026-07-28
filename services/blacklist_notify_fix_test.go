package services

import (
	"database/sql"
	"testing"
	"time"

	"github.com/daodao97/xgo/xdb"
	_ "modernc.org/sqlite"
)

// setupBlacklistFixEnv 复用 rename 测试的隔离环境（HOME/USERPROFILE 重定向 + 独立 app.db），
// 并额外初始化 GlobalDBQueue 与 app_settings 表，供拉黑读改写路径使用。
func setupBlacklistFixEnv(t *testing.T) {
	t.Helper()
	setupRenameTestEnv(t)

	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("获取数据库失败: %v", err)
	}

	// RecordFailure/RecordSuccess 走 GlobalDBQueue 写入，指向测试库并在结束时恢复
	oldQueue := GlobalDBQueue
	GlobalDBQueue = NewDBWriteQueue(db, 100, false)
	t.Cleanup(func() {
		_ = GlobalDBQueue.Shutdown(2 * time.Second)
		GlobalDBQueue = oldQueue
	})

	// 建 app_settings 表并写入默认配置（enable_blacklist=false 等）
	NewSettingsService()
}

// setAppSetting 以 UPSERT 方式写入 app_settings 键值。
func setAppSetting(t *testing.T, key, value string) {
	t.Helper()
	db, _ := xdb.DB("default")
	if _, err := db.Exec(`
		INSERT INTO app_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value); err != nil {
		t.Fatalf("写入设置 %s 失败: %v", key, err)
	}
}

// TestRecordFailure_BlacklistClearsStaleRecoveryTime 等级模式拉黑时必须清空 last_recovered_at，
// 否则拉黑到期后（自动恢复扫描前）的首个成功会用上一轮的陈旧恢复时间触发宽恕，L4 直接清零。
func TestRecordFailure_BlacklistClearsStaleRecoveryTime(t *testing.T) {
	setupBlacklistFixEnv(t)
	setAppSetting(t, "enable_blacklist", "true")
	setAppSetting(t, "blacklist_level_enabled", "true")

	db, _ := xdb.DB("default")
	now := time.Now()
	// L3 供应商，上一轮恢复时间在 7 小时前（超过默认宽恕阈值 3h），失败计数 2/3
	if _, err := db.Exec(`
		INSERT INTO provider_blacklist
			(platform, provider_name, failure_count, last_failure_at, blacklist_level,
			 last_recovered_at, last_degrade_hour, last_failure_window_start, auto_recovered)
		VALUES ('claude', 'p46', 2, ?, 3, ?, 2, ?, 1)
	`, now.Add(-10*time.Minute), now.Add(-7*time.Hour), now.Add(-10*time.Minute)); err != nil {
		t.Fatalf("seed 失败: %v", err)
	}

	bs := NewBlacklistService(NewSettingsService(), nil)
	if err := bs.RecordFailure("claude", "p46"); err != nil {
		t.Fatalf("RecordFailure 失败: %v", err)
	}

	// 拉黑后：等级升到 L4，恢复计时字段必须被清空
	var level, degradeHour int
	var lastRecoveredValid int
	if err := db.QueryRow(`
		SELECT blacklist_level, last_degrade_hour, last_recovered_at IS NOT NULL
		FROM provider_blacklist WHERE platform='claude' AND provider_name='p46'
	`).Scan(&level, &degradeHour, &lastRecoveredValid); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if level != 4 {
		t.Errorf("等级应升到 L4,实际 L%d", level)
	}
	if lastRecoveredValid != 0 {
		t.Errorf("拉黑时 last_recovered_at 应清空,实际仍保留")
	}
	if degradeHour != 0 {
		t.Errorf("拉黑时 last_degrade_hour 应清零,实际 %d", degradeHour)
	}

	// 模拟拉黑到期但自动恢复尚未扫描：首个成功不应触发宽恕清零
	if _, err := db.Exec(`
		UPDATE provider_blacklist SET blacklisted_until = ?
		WHERE platform='claude' AND provider_name='p46'
	`, now.Add(-1*time.Minute)); err != nil {
		t.Fatalf("模拟到期失败: %v", err)
	}

	if err := bs.RecordSuccess("claude", "p46"); err != nil {
		t.Fatalf("RecordSuccess 失败: %v", err)
	}

	if err := db.QueryRow(`
		SELECT blacklist_level FROM provider_blacklist
		WHERE platform='claude' AND provider_name='p46'
	`).Scan(&level); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if level != 4 {
		t.Errorf("到期后首个成功不应触发宽恕,等级应保持 L4,实际 L%d", level)
	}
}

// TestRecordSuccess_UsesConfiguredDegradeInterval 降级步长应使用配置的 NormalDegradeIntervalHours，
// 而不是硬编码的 1 小时。
func TestRecordSuccess_UsesConfiguredDegradeInterval(t *testing.T) {
	cases := []struct {
		name         string
		intervalHour float64
		recoveredAgo time.Duration
		startLevel   int
		wantLevel    int
	}{
		{"半小时间隔_恢复61分钟应降2级", 0.5, 61 * time.Minute, 2, 0},
		{"两小时间隔_恢复61分钟不应降级", 2.0, 61 * time.Minute, 2, 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupBlacklistFixEnv(t)
			setAppSetting(t, "blacklist_level_enabled", "true")

			ss := NewSettingsService()
			cfg := DefaultBlacklistLevelConfig()
			cfg.NormalDegradeIntervalHours = tc.intervalHour
			if err := ss.SaveBlacklistLevelConfig(cfg); err != nil {
				t.Fatalf("保存配置失败: %v", err)
			}

			db, _ := xdb.DB("default")
			now := time.Now()
			if _, err := db.Exec(`
				INSERT INTO provider_blacklist
					(platform, provider_name, failure_count, last_failure_at, blacklist_level,
					 last_recovered_at, last_degrade_hour, auto_recovered)
				VALUES ('claude', 'p88', 0, ?, ?, ?, 0, 1)
			`, now.Add(-2*time.Hour), tc.startLevel, now.Add(-tc.recoveredAgo)); err != nil {
				t.Fatalf("seed 失败: %v", err)
			}

			bs := NewBlacklistService(ss, nil)
			if err := bs.RecordSuccess("claude", "p88"); err != nil {
				t.Fatalf("RecordSuccess 失败: %v", err)
			}

			var level int
			if err := db.QueryRow(`
				SELECT blacklist_level FROM provider_blacklist
				WHERE platform='claude' AND provider_name='p88'
			`).Scan(&level); err != nil {
				t.Fatalf("查询失败: %v", err)
			}
			if level != tc.wantLevel {
				t.Errorf("降级间隔 %.1fh、恢复 %v 后,等级应为 L%d,实际 L%d",
					tc.intervalHour, tc.recoveredAgo, tc.wantLevel, level)
			}
		})
	}
}

// TestRecordFailureFixedMode_ResetsFailureCountOnBlacklist 固定模式拉黑时 failure_count 应清零，
// 否则期满后至自动恢复扫描前的窗口内单次失败即按全时长重新拉黑。
func TestRecordFailureFixedMode_ResetsFailureCountOnBlacklist(t *testing.T) {
	setupBlacklistFixEnv(t)
	setAppSetting(t, "enable_blacklist", "true")
	// 等级拉黑保持默认关闭 → 走固定模式（fallbackMode 默认 fixed，阈值 3）

	db, _ := xdb.DB("default")
	now := time.Now()
	if _, err := db.Exec(`
		INSERT INTO provider_blacklist
			(platform, provider_name, failure_count, last_failure_at)
		VALUES ('claude', 'p91', 2, ?)
	`, now.Add(-10*time.Minute)); err != nil {
		t.Fatalf("seed 失败: %v", err)
	}

	bs := NewBlacklistService(NewSettingsService(), nil)
	if err := bs.RecordFailure("claude", "p91"); err != nil {
		t.Fatalf("RecordFailure 失败: %v", err)
	}

	var failureCount int
	var untilValid int
	if err := db.QueryRow(`
		SELECT failure_count, blacklisted_until IS NOT NULL
		FROM provider_blacklist WHERE platform='claude' AND provider_name='p91'
	`).Scan(&failureCount, &untilValid); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if untilValid != 1 {
		t.Fatal("达到阈值后应被拉黑")
	}
	if failureCount != 0 {
		t.Errorf("固定模式拉黑时 failure_count 应清零,实际 %d", failureCount)
	}
}

// TestRecordFailure_ThresholdOneBlacklistsOnFirstFailure 阈值为 1 时，
// 无历史记录的供应商首次失败即应拉黑（等级模式与固定模式）。
func TestRecordFailure_ThresholdOneBlacklistsOnFirstFailure(t *testing.T) {
	cases := []struct {
		name      string
		levelMode bool
	}{
		{"等级模式", true},
		{"固定模式", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupBlacklistFixEnv(t)
			setAppSetting(t, "enable_blacklist", "true")
			setAppSetting(t, "blacklist_failure_threshold", "1")
			if tc.levelMode {
				setAppSetting(t, "blacklist_level_enabled", "true")
			}

			bs := NewBlacklistService(NewSettingsService(), nil)
			if err := bs.RecordFailure("claude", "p92"); err != nil {
				t.Fatalf("RecordFailure 失败: %v", err)
			}

			db, _ := xdb.DB("default")
			var level, failureCount int
			var until sql.NullTime
			if err := db.QueryRow(`
				SELECT blacklist_level, failure_count, blacklisted_until
				FROM provider_blacklist WHERE platform='claude' AND provider_name='p92'
			`).Scan(&level, &failureCount, &until); err != nil {
				t.Fatalf("首次失败应已插入拉黑记录: %v", err)
			}
			if !until.Valid || !until.Time.After(time.Now()) {
				t.Error("阈值 1 时首次失败应立即拉黑,blacklisted_until 应在未来")
			}
			if failureCount != 0 {
				t.Errorf("拉黑时 failure_count 应清零,实际 %d", failureCount)
			}
			if tc.levelMode && level != 1 {
				t.Errorf("等级模式首次拉黑应为 L1,实际 L%d", level)
			}
		})
	}
}

// TestValidateBlacklistLevelConfig_RetryWaitSeconds 校验必须覆盖 RetryWaitSeconds，
// 并强制其大于去重窗口（否则同 Provider 重试不会计入失败次数）。
func TestValidateBlacklistLevelConfig_RetryWaitSeconds(t *testing.T) {
	cases := []struct {
		name      string
		retryWait int
		dedupe    int
		wantErr   bool
	}{
		{"默认值合法", 3, 2, false},
		{"零值拒绝", 0, 2, true},
		{"负值拒绝", -1, 2, true},
		{"超上限拒绝", 301, 2, true},
		{"等于去重窗口拒绝", 30, 30, true},
		{"小于去重窗口拒绝", 3, 30, true},
		{"大于去重窗口通过", 31, 30, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultBlacklistLevelConfig()
			cfg.RetryWaitSeconds = tc.retryWait
			cfg.DedupeWindowSeconds = tc.dedupe

			err := validateBlacklistLevelConfig(cfg)
			if tc.wantErr && err == nil {
				t.Errorf("retryWait=%d dedupe=%d 应被拒绝", tc.retryWait, tc.dedupe)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("retryWait=%d dedupe=%d 应通过,实际报错: %v", tc.retryWait, tc.dedupe, err)
			}
		})
	}
}

// TestNotifyProviderSwitch_ThrottleUpdatesTimestampSynchronously 节流时间戳必须在检查通过时
// 持锁写入（而非异步 goroutine 中），否则突发降级的连续调用会全部绕过节流形成通知风暴。
func TestNotifyProviderSwitch_ThrottleUpdatesTimestampSynchronously(t *testing.T) {
	ns := &NotificationService{minInterval: 3 * time.Second}
	// appSettings 为 nil → 通知视为开启；上次通知在很久之前，本次调用应通过节流
	ns.lastNotifyTime = time.Now().Add(-time.Hour)

	ns.NotifyProviderSwitch(SwitchNotification{
		FromProvider: "a",
		ToProvider:   "b",
		Platform:     "claude",
	})

	// 返回后立即读取：时间戳应已同步更新，使突发的后续调用立刻被节流
	ns.mu.RLock()
	updated := ns.lastNotifyTime
	ns.mu.RUnlock()
	if time.Since(updated) > time.Minute {
		t.Fatal("lastNotifyTime 应在 NotifyProviderSwitch 返回前同步更新,否则突发调用会绕过节流")
	}

	// 紧随其后的第二次调用必须被节流：时间戳不应被再次推进
	before := updated
	ns.NotifyProviderSwitch(SwitchNotification{
		FromProvider: "b",
		ToProvider:   "c",
		Platform:     "claude",
	})
	ns.mu.RLock()
	after := ns.lastNotifyTime
	ns.mu.RUnlock()
	if !after.Equal(before) {
		t.Fatal("最小间隔内的第二次调用应被节流,不应更新 lastNotifyTime")
	}
}
