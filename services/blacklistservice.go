package services

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/daodao97/xgo/xdb"
)

// BlacklistService 管理供应商黑名单
type BlacklistService struct {
	settingsService     *SettingsService
	notificationService *NotificationService
	// mu 串行化"读计数→判断→写回"的整段读改写:
	// 计数的 SELECT 与 UPDATE 之间无事务,并发失败会互相吞掉计数,
	// 导致达到拉黑阈值的时机被推迟甚至错过
	mu sync.Mutex
}

// BlacklistStatus 黑名单状态（用于前端展示）
type BlacklistStatus struct {
	Platform         string     `json:"platform"`
	ProviderName     string     `json:"providerName"`
	FailureCount     int        `json:"failureCount"`
	BlacklistedAt    *time.Time `json:"blacklistedAt"`
	BlacklistedUntil *time.Time `json:"blacklistedUntil"`
	LastFailureAt    *time.Time `json:"lastFailureAt"`
	IsBlacklisted    bool       `json:"isBlacklisted"`
	RemainingSeconds int        `json:"remainingSeconds"` // 剩余拉黑时间（秒）

	// v0.4.0 新增：等级拉黑相关字段
	BlacklistLevel       int        `json:"blacklistLevel"`       // 当前黑名单等级 (0-5)
	LastRecoveredAt      *time.Time `json:"lastRecoveredAt"`      // 最后恢复时间
	ForgivenessRemaining int        `json:"forgivenessRemaining"` // 距离宽恕还剩多少秒（3小时倒计时）
}

func NewBlacklistService(settingsService *SettingsService, notificationService *NotificationService) *BlacklistService {
	return &BlacklistService{
		settingsService:     settingsService,
		notificationService: notificationService,
	}
}

// RecordSuccess 记录 provider 成功，清零连续失败计数，执行降级和宽恕逻辑
func (bs *BlacklistService) RecordSuccess(platform string, providerName string) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	providerName = ResolveProviderAlias(platform, providerName)
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}

	// 获取等级拉黑配置
	levelConfig, err := bs.settingsService.GetBlacklistLevelConfig()
	if err != nil {
		log.Printf("⚠️  获取等级拉黑配置失败: %v", err)
		levelConfig = DefaultBlacklistLevelConfig()
	}

	// 查询现有记录
	var id int
	var blacklistLevel int
	var lastRecoveredAt sql.NullTime
	var lastDegradeHour int
	var blacklistedUntil sql.NullTime

	err = db.QueryRow(`
		SELECT id, blacklist_level, last_recovered_at, last_degrade_hour, blacklisted_until
		FROM provider_blacklist
		WHERE platform = ? AND provider_name = ?
	`, platform, providerName).Scan(&id, &blacklistLevel, &lastRecoveredAt, &lastDegradeHour, &blacklistedUntil)

	if err == sql.ErrNoRows {
		// 没有失败记录，无需操作
		return nil
	} else if err != nil {
		return fmt.Errorf("查询黑名单记录失败: %w", err)
	}

	now := time.Now()

	// 检查是否刚从拉黑中恢复（blacklisted_until 刚过期且 last_recovered_at 未设置）
	justRecovered := false
	if blacklistedUntil.Valid && blacklistedUntil.Time.Before(now) && !lastRecoveredAt.Valid {
		justRecovered = true
		lastRecoveredAt = sql.NullTime{Time: now, Valid: true}
		log.Printf("🔓 Provider %s/%s 从黑名单恢复（L%d），开始降级计时", platform, providerName, blacklistLevel)
	}

	// 如果功能关闭，只清零失败计数
	if !levelConfig.EnableLevelBlacklist {
		err = GlobalDBQueue.Exec(`
			UPDATE provider_blacklist
			SET failure_count = 0
			WHERE id = ?
		`, id)

		if err != nil {
			return fmt.Errorf("清零失败计数失败: %w", err)
		}

		log.Printf("✅ Provider %s/%s 成功，连续失败计数已清零（固定模式）", platform, providerName)
		return nil
	}

	// 执行降级和宽恕逻辑（仅在等级拉黑模式开启时）
	newLevel := blacklistLevel
	newLastDegradeHour := lastDegradeHour

	if lastRecoveredAt.Valid && blacklistLevel > 0 {
		timeSinceRecovery := now.Sub(lastRecoveredAt.Time)

		// 降级步长取自配置的正常降级间隔（防止手改 JSON 为 0 导致除零，兜底 1 小时）
		degradeInterval := time.Duration(levelConfig.NormalDegradeIntervalHours * float64(time.Hour))
		if degradeInterval <= 0 {
			degradeInterval = time.Hour
		}
		intervalsSinceRecovery := int(timeSinceRecovery / degradeInterval)

		// 宽恕机制：稳定 3 小时且等级 >= 3，直接清零到 L0
		if timeSinceRecovery >= time.Duration(levelConfig.ForgivenessHours*float64(time.Hour)) && blacklistLevel >= 3 {
			newLevel = 0
			newLastDegradeHour = 0
			log.Printf("🎉 Provider %s/%s 触发宽恕机制（稳定 %.1f 小时），等级清零（L%d → L0）",
				platform, providerName, timeSinceRecovery.Hours(), blacklistLevel)
		} else if intervalsSinceRecovery > lastDegradeHour {
			// 正常降级：每个降级间隔 -1 等级（last_degrade_hour 记录已消费的间隔数，防止同一间隔内重复降级）
			degradeCount := intervalsSinceRecovery - lastDegradeHour

			newLevel = blacklistLevel - degradeCount
			if newLevel < 0 {
				newLevel = 0
			}

			newLastDegradeHour = intervalsSinceRecovery

			if degradeCount > 0 {
				log.Printf("📉 Provider %s/%s 降级（L%d → L%d，经过 %d 个降级间隔）",
					platform, providerName, blacklistLevel, newLevel, degradeCount)
			}
		}
	}

	// 更新数据库
	updateSQL := `
		UPDATE provider_blacklist
		SET failure_count = 0,
			blacklist_level = ?,
			last_recovered_at = ?,
			last_degrade_hour = ?
		WHERE id = ?
	`

	var lastRecoveredTime interface{}
	if lastRecoveredAt.Valid {
		lastRecoveredTime = lastRecoveredAt.Time
	} else {
		lastRecoveredTime = nil
	}

	err = GlobalDBQueue.Exec(updateSQL, newLevel, lastRecoveredTime, newLastDegradeHour, id)

	if err != nil {
		return fmt.Errorf("更新成功记录失败: %w", err)
	}

	if justRecovered {
		log.Printf("✅ Provider %s/%s 成功（刚恢复），失败计数已清零，当前等级: L%d", platform, providerName, newLevel)
	} else if newLevel != blacklistLevel {
		log.Printf("✅ Provider %s/%s 成功，失败计数已清零，等级: L%d → L%d", platform, providerName, blacklistLevel, newLevel)
	} else {
		log.Printf("✅ Provider %s/%s 成功，失败计数已清零，当前等级: L%d", platform, providerName, newLevel)
	}

	return nil
}

// RecordFailure 记录 provider 失败，连续失败次数达到阈值时自动拉黑（支持等级拉黑）
func (bs *BlacklistService) RecordFailure(platform string, providerName string) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	providerName = ResolveProviderAlias(platform, providerName)
	// 检查拉黑功能是否启用
	if !bs.settingsService.IsBlacklistEnabled() {
		log.Printf("🚫 拉黑功能已关闭，跳过 provider %s/%s 的失败记录", platform, providerName)
		return nil
	}

	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}

	// 获取等级拉黑配置
	levelConfig, err := bs.settingsService.GetBlacklistLevelConfig()
	if err != nil {
		log.Printf("⚠️  获取等级拉黑配置失败: %v", err)
		levelConfig = DefaultBlacklistLevelConfig()
	}

	// 如果功能关闭，使用旧的固定拉黑模式
	if !levelConfig.EnableLevelBlacklist {
		// 从数据库读取配置（优先使用数据库配置而非默认值）
		threshold, duration, err := bs.settingsService.GetBlacklistSettings()
		if err != nil {
			log.Printf("⚠️  获取数据库拉黑配置失败: %v，使用默认值", err)
			threshold = levelConfig.FailureThreshold
			duration = levelConfig.FallbackDurationMinutes
		}
		return bs.recordFailureFixedMode(platform, providerName, levelConfig.FallbackMode, duration, threshold)
	}

	now := time.Now()

	// 查询现有记录
	var id int
	var failureCount int
	var blacklistedUntil sql.NullTime
	var blacklistLevel int
	var lastRecoveredAt sql.NullTime
	var lastFailureWindowStart sql.NullTime

	err = db.QueryRow(`
		SELECT id, failure_count, blacklisted_until, blacklist_level, last_recovered_at, last_failure_window_start
		FROM provider_blacklist
		WHERE platform = ? AND provider_name = ?
	`, platform, providerName).Scan(&id, &failureCount, &blacklistedUntil, &blacklistLevel, &lastRecoveredAt, &lastFailureWindowStart)

	if err == sql.ErrNoRows {
		// 阈值 <= 1 时首次失败即达标，直接插入拉黑记录（否则实际表现为阈值 2）
		if levelConfig.FailureThreshold <= 1 {
			newLevel := 1 // 无历史记录，首次拉黑为 L1
			duration := bs.getLevelDuration(newLevel, levelConfig)
			blacklistedUntil := now.Add(time.Duration(duration) * time.Minute)

			err = GlobalDBQueue.Exec(`
				INSERT INTO provider_blacklist
					(platform, provider_name, failure_count, last_failure_at, blacklisted_at, blacklisted_until, blacklist_level, last_failure_window_start, auto_recovered)
				VALUES (?, ?, 0, ?, ?, ?, ?, ?, 0)
			`, platform, providerName, now, now, blacklistedUntil, newLevel, now)

			if err != nil {
				return fmt.Errorf("插入拉黑记录失败: %w", err)
			}

			log.Printf("⛔ Provider %s/%s 已拉黑（L0 → L%d，%d 分钟），过期时间: %s",
				platform, providerName, newLevel, duration, blacklistedUntil.Format("15:04:05"))

			// 发送拉黑通知
			if bs.notificationService != nil {
				bs.notificationService.NotifyProviderBlacklisted(platform, providerName, newLevel, duration)
			}
			return nil
		}

		// 首次失败，插入新记录
		err = GlobalDBQueue.Exec(`
			INSERT INTO provider_blacklist
				(platform, provider_name, failure_count, last_failure_at, last_failure_window_start, blacklist_level)
			VALUES (?, ?, 1, ?, ?, 0)
		`, platform, providerName, now, now)

		if err != nil {
			return fmt.Errorf("插入失败记录失败: %w", err)
		}

		log.Printf("📊 Provider %s/%s 失败计数: 1/%d（等级拉黑模式）", platform, providerName, levelConfig.FailureThreshold)
		return nil
	} else if err != nil {
		return fmt.Errorf("查询黑名单记录失败: %w", err)
	}

	// 如果已经拉黑且未过期，不重复计数
	if blacklistedUntil.Valid && blacklistedUntil.Time.After(now) {
		log.Printf("⛔ Provider %s/%s 已在黑名单中（L%d），过期时间: %s",
			platform, providerName, blacklistLevel, blacklistedUntil.Time.Format("15:04:05"))
		return nil
	}

	// 30秒去重窗口检测（防止客户端重试误判）
	if lastFailureWindowStart.Valid {
		timeSinceLastFailure := now.Sub(lastFailureWindowStart.Time)
		if timeSinceLastFailure < time.Duration(levelConfig.DedupeWindowSeconds)*time.Second {
			log.Printf("🔄 Provider %s/%s 在30秒去重窗口内，忽略此次失败", platform, providerName)
			return nil
		}
	}

	// 失败计数 +1，更新去重窗口起始时间
	failureCount++

	// 检查是否达到拉黑阈值
	if failureCount >= levelConfig.FailureThreshold {
		// 计算等级升级策略
		newLevel := blacklistLevel
		var levelIncrease int

		if lastRecoveredAt.Valid {
			timeSinceRecovery := now.Sub(lastRecoveredAt.Time)
			jumpPenaltyWindow := time.Duration(levelConfig.JumpPenaltyWindowHours * float64(time.Hour))

			if timeSinceRecovery <= jumpPenaltyWindow {
				// 跳级惩罚：恢复后短时间内再次失败
				levelIncrease = 2
				log.Printf("⚡ Provider %s/%s 触发跳级惩罚（恢复后 %.1f 小时内再次失败）",
					platform, providerName, timeSinceRecovery.Hours())
			} else {
				// 正常升级
				levelIncrease = 1
				log.Printf("📈 Provider %s/%s 正常升级（恢复后 %.1f 小时再次失败）",
					platform, providerName, timeSinceRecovery.Hours())
			}
		} else {
			// 首次拉黑，默认 L1
			levelIncrease = 1
		}

		newLevel = blacklistLevel + levelIncrease
		if newLevel > 5 {
			newLevel = 5 // 最高 L5
		}

		// 根据等级获取拉黑时长
		duration := bs.getLevelDuration(newLevel, levelConfig)
		blacklistedAt := now
		blacklistedUntil := now.Add(time.Duration(duration) * time.Minute)

		// 清空 last_recovered_at / last_degrade_hour：降级与宽恕计时必须以本次拉黑之后的恢复时间为基准，
		// 否则到期后首个成功会用上一轮的陈旧恢复时间直接触发宽恕，L4/L5 等级瞬间清零
		err = GlobalDBQueue.Exec(`
			UPDATE provider_blacklist
			SET failure_count = 0,
				last_failure_at = ?,
				blacklisted_at = ?,
				blacklisted_until = ?,
				blacklist_level = ?,
				auto_recovered = 0,
				last_failure_window_start = ?,
				last_recovered_at = NULL,
				last_degrade_hour = 0
			WHERE id = ?
		`, now, blacklistedAt, blacklistedUntil, newLevel, now, id)

		if err != nil {
			return fmt.Errorf("更新拉黑状态失败: %w", err)
		}

		log.Printf("⛔ Provider %s/%s 已拉黑（L%d → L%d，%d 分钟），过期时间: %s",
			platform, providerName, blacklistLevel, newLevel, duration, blacklistedUntil.Format("15:04:05"))

		// 发送拉黑通知
		if bs.notificationService != nil {
			bs.notificationService.NotifyProviderBlacklisted(platform, providerName, newLevel, duration)
		}

	} else {
		// 未达到阈值，仅更新失败计数和窗口起始时间
		err = GlobalDBQueue.Exec(`
			UPDATE provider_blacklist
			SET failure_count = ?, last_failure_at = ?, last_failure_window_start = ?
			WHERE id = ?
		`, failureCount, now, now, id)

		if err != nil {
			return fmt.Errorf("更新失败计数失败: %w", err)
		}

		log.Printf("📊 Provider %s/%s 失败计数: %d/%d（当前等级: L%d）",
			platform, providerName, failureCount, levelConfig.FailureThreshold, blacklistLevel)
	}

	return nil
}

// recordFailureFixedMode 固定拉黑模式（向后兼容）
func (bs *BlacklistService) recordFailureFixedMode(platform string, providerName string, fallbackMode string, fallbackDuration int, failureThreshold int) error {
	if fallbackMode == "none" {
		log.Printf("🚫 Provider %s/%s 失败，但等级拉黑已关闭且 fallbackMode=none，不拉黑", platform, providerName)
		return nil
	}

	// 使用旧的固定拉黑逻辑
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}

	now := time.Now()

	// 查询现有记录
	var id int
	var failureCount int
	var blacklistedUntil sql.NullTime

	err = db.QueryRow(`
		SELECT id, failure_count, blacklisted_until
		FROM provider_blacklist
		WHERE platform = ? AND provider_name = ?
	`, platform, providerName).Scan(&id, &failureCount, &blacklistedUntil)

	if err == sql.ErrNoRows {
		// 阈值 <= 1 时首次失败即达标，直接插入拉黑记录（否则实际表现为阈值 2）
		if failureThreshold <= 1 {
			blacklistedUntil := now.Add(time.Duration(fallbackDuration) * time.Minute)

			err = GlobalDBQueue.Exec(`
				INSERT INTO provider_blacklist
					(platform, provider_name, failure_count, last_failure_at, blacklisted_at, blacklisted_until, auto_recovered)
				VALUES (?, ?, 0, ?, ?, ?, 0)
			`, platform, providerName, now, now, blacklistedUntil)

			if err != nil {
				return fmt.Errorf("插入拉黑记录失败: %w", err)
			}

			log.Printf("⛔ Provider %s/%s 已拉黑 %d 分钟（固定模式，失败 1 次），过期时间: %s",
				platform, providerName, fallbackDuration, blacklistedUntil.Format("15:04:05"))

			// 发送拉黑通知（固定模式无等级，level 传 0）
			if bs.notificationService != nil {
				bs.notificationService.NotifyProviderBlacklisted(platform, providerName, 0, fallbackDuration)
			}
			return nil
		}

		// 首次失败，插入新记录
		err = GlobalDBQueue.Exec(`
			INSERT INTO provider_blacklist
				(platform, provider_name, failure_count, last_failure_at)
			VALUES (?, ?, 1, ?)
		`, platform, providerName, now)

		if err != nil {
			return fmt.Errorf("插入失败记录失败: %w", err)
		}

		log.Printf("📊 Provider %s/%s 失败计数: 1/%d（固定拉黑模式）", platform, providerName, failureThreshold)
		return nil
	} else if err != nil {
		return fmt.Errorf("查询黑名单记录失败: %w", err)
	}

	// 如果已经拉黑且未过期，不重复计数
	if blacklistedUntil.Valid && blacklistedUntil.Time.After(now) {
		log.Printf("⛔ Provider %s/%s 已在黑名单中（固定模式），过期时间: %s", platform, providerName, blacklistedUntil.Time.Format("15:04:05"))
		return nil
	}

	// 失败计数 +1
	failureCount++

	// 检查是否达到拉黑阈值
	if failureCount >= failureThreshold {
		blacklistedAt := now
		blacklistedUntil := now.Add(time.Duration(fallbackDuration) * time.Minute)

		// 与等级模式一致写 failure_count = 0：若保留阈值计数，期满后至自动恢复扫描前的窗口内
		// 单次偶发失败会立即按全时长重新拉黑，形成期满边界振荡
		err = GlobalDBQueue.Exec(`
			UPDATE provider_blacklist
			SET failure_count = 0,
				last_failure_at = ?,
				blacklisted_at = ?,
				blacklisted_until = ?,
				auto_recovered = 0
			WHERE id = ?
		`, now, blacklistedAt, blacklistedUntil, id)

		if err != nil {
			return fmt.Errorf("更新拉黑状态失败: %w", err)
		}

		log.Printf("⛔ Provider %s/%s 已拉黑 %d 分钟（固定模式，失败 %d 次），过期时间: %s",
			platform, providerName, fallbackDuration, failureCount, blacklistedUntil.Format("15:04:05"))

		// 发送拉黑通知（固定模式无等级，level 传 0）
		if bs.notificationService != nil {
			bs.notificationService.NotifyProviderBlacklisted(platform, providerName, 0, fallbackDuration)
		}

	} else {
		// 更新失败计数
		err = GlobalDBQueue.Exec(`
			UPDATE provider_blacklist
			SET failure_count = ?, last_failure_at = ?
			WHERE id = ?
		`, failureCount, now, id)

		if err != nil {
			return fmt.Errorf("更新失败计数失败: %w", err)
		}

		log.Printf("📊 Provider %s/%s 失败计数: %d/%d（固定模式）", platform, providerName, failureCount, failureThreshold)
	}

	return nil
}

// getLevelDuration 根据等级获取拉黑时长（分钟）
func (bs *BlacklistService) getLevelDuration(level int, config *BlacklistLevelConfig) int {
	switch level {
	case 1:
		return config.L1DurationMinutes
	case 2:
		return config.L2DurationMinutes
	case 3:
		return config.L3DurationMinutes
	case 4:
		return config.L4DurationMinutes
	case 5:
		return config.L5DurationMinutes
	default:
		return config.L1DurationMinutes // 默认 L1
	}
}

// IsBlacklisted 检查 provider 是否在黑名单中
func (bs *BlacklistService) IsBlacklisted(platform string, providerName string) (bool, *time.Time) {
	providerName = ResolveProviderAlias(platform, providerName)
	// 如果拉黑功能已关闭，始终返回未拉黑
	if !bs.settingsService.IsBlacklistEnabled() {
		return false, nil
	}

	db, err := xdb.DB("default")
	if err != nil {
		log.Printf("⚠️  获取数据库连接失败: %v", err)
		return false, nil
	}

	var blacklistedUntil sql.NullTime

	// 移除 SQL 时间比较，改为 Go 代码判断（修复时区 bug）
	err = db.QueryRow(`
		SELECT blacklisted_until
		FROM provider_blacklist
		WHERE platform = ? AND provider_name = ? AND blacklisted_until IS NOT NULL
	`, platform, providerName).Scan(&blacklistedUntil)

	if err == sql.ErrNoRows {
		return false, nil
	} else if err != nil {
		log.Printf("⚠️  查询黑名单状态失败: %v", err)
		return false, nil
	}

	if blacklistedUntil.Valid {
		// 使用 Go 代码比较时间（正确处理时区）
		if blacklistedUntil.Time.After(time.Now()) {
			return true, &blacklistedUntil.Time
		}
	}

	return false, nil
}

// ManualUnblockAndReset 手动解除拉黑（保留等级，如需清零请调用 ManualResetLevel）
func (bs *BlacklistService) ManualUnblockAndReset(platform string, providerName string) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	providerName = ResolveProviderAlias(platform, providerName)
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}

	now := time.Now()

	// 先检查记录是否存在
	var exists int
	err = db.QueryRow(`
		SELECT 1 FROM provider_blacklist
		WHERE platform = ? AND provider_name = ?
	`, platform, providerName).Scan(&exists)

	if err == sql.ErrNoRows {
		return fmt.Errorf("provider %s/%s 不在黑名单中", platform, providerName)
	} else if err != nil {
		return fmt.Errorf("查询黑名单记录失败: %w", err)
	}

	// 【重要】保留 blacklist_level，让降级/宽恕机制逐渐降低等级
	err = GlobalDBQueue.Exec(`
		UPDATE provider_blacklist
		SET blacklisted_at = NULL,
			blacklisted_until = NULL,
			failure_count = 0,
			last_recovered_at = ?,
			last_degrade_hour = 0,
			auto_recovered = 0
		WHERE platform = ? AND provider_name = ?
	`, now, platform, providerName)

	if err != nil {
		return fmt.Errorf("手动解除拉黑失败: %w", err)
	}

	log.Printf("✅ 手动解除拉黑: %s/%s（等级保留，重新开始降级计时）", platform, providerName)
	return nil
}

// ManualUnblock 手动解除拉黑（向后兼容，调用 ManualUnblockAndReset）
func (bs *BlacklistService) ManualUnblock(platform string, providerName string) error {
	return bs.ManualUnblockAndReset(platform, providerName)
}

// ManualResetLevel 手动清零等级（不解除拉黑，仅重置等级）
func (bs *BlacklistService) ManualResetLevel(platform string, providerName string) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	providerName = ResolveProviderAlias(platform, providerName)
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}

	// 先检查记录是否存在
	var exists int
	err = db.QueryRow(`
		SELECT 1 FROM provider_blacklist
		WHERE platform = ? AND provider_name = ?
	`, platform, providerName).Scan(&exists)

	if err == sql.ErrNoRows {
		return fmt.Errorf("provider %s/%s 不存在", platform, providerName)
	} else if err != nil {
		return fmt.Errorf("查询黑名单记录失败: %w", err)
	}

	err = GlobalDBQueue.Exec(`
		UPDATE provider_blacklist
		SET blacklist_level = 0,
			last_degrade_hour = 0
		WHERE platform = ? AND provider_name = ?
	`, platform, providerName)

	if err != nil {
		return fmt.Errorf("手动清零等级失败: %w", err)
	}

	log.Printf("✅ 手动清零等级: %s/%s（等级 → L0，拉黑状态保留）", platform, providerName)
	return nil
}

// AutoRecoverExpired 自动恢复过期的黑名单（由定时器调用）
// 使用事务批量处理，避免多次单独写入导致的并发锁冲突
func (bs *BlacklistService) AutoRecoverExpired() error {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}

	// 查询需要恢复的 provider（移除 SQL 时间比较，改为 Go 代码判断）
	rows, err := db.Query(`
		SELECT platform, provider_name, blacklisted_until
		FROM provider_blacklist
		WHERE blacklisted_until IS NOT NULL
			AND auto_recovered = 0
	`)

	if err != nil {
		return fmt.Errorf("查询过期黑名单失败: %w", err)
	}
	defer rows.Close()

	now := time.Now()
	type RecoverItem struct {
		Platform     string
		ProviderName string
	}
	var toRecover []RecoverItem

	// 收集所有需要恢复的 provider
	for rows.Next() {
		var platform, providerName string
		var blacklistedUntil sql.NullTime

		if err := rows.Scan(&platform, &providerName, &blacklistedUntil); err != nil {
			log.Printf("⚠️  读取恢复记录失败: %v", err)
			continue
		}

		// 使用 Go 代码判断是否过期（正确处理时区）
		if !blacklistedUntil.Valid || blacklistedUntil.Time.After(now) {
			continue // 未过期，跳过
		}

		toRecover = append(toRecover, RecoverItem{
			Platform:     platform,
			ProviderName: providerName,
		})
	}

	// 如果没有需要恢复的，直接返回
	if len(toRecover) == 0 {
		return nil
	}

	var recovered []string
	var failed []string

	// 批量更新所有过期的 provider（使用队列）
	// 【重要】保留 blacklist_level，让 RecordSuccess 中的降级/宽恕机制逐渐降低等级
	for _, item := range toRecover {
		err := GlobalDBQueue.Exec(`
			UPDATE provider_blacklist
			SET auto_recovered = 1,
				failure_count = 0,
				last_recovered_at = ?,
				last_degrade_hour = 0
			WHERE platform = ? AND provider_name = ?
		`, now, item.Platform, item.ProviderName)

		if err != nil {
			failed = append(failed, fmt.Sprintf("%s/%s", item.Platform, item.ProviderName))
			log.Printf("⚠️  标记恢复状态失败: %s/%s - %v", item.Platform, item.ProviderName, err)
		} else {
			recovered = append(recovered, fmt.Sprintf("%s/%s", item.Platform, item.ProviderName))
		}
	}

	if len(recovered) > 0 {
		log.Printf("✅ 自动恢复 %d 个过期拉黑（等级保留，等待降级）: %v", len(recovered), recovered)
	}

	if len(failed) > 0 {
		log.Printf("⚠️  恢复失败 %d 个: %v", len(failed), failed)
	}

	return nil
}

// GetBlacklistStatus 获取所有黑名单状态（用于前端展示，支持等级拉黑）
func (bs *BlacklistService) GetBlacklistStatus(platform string) ([]BlacklistStatus, error) {
	db, err := xdb.DB("default")
	if err != nil {
		return nil, fmt.Errorf("获取数据库连接失败: %w", err)
	}

	// 获取等级拉黑配置（用于计算宽恕倒计时）
	levelConfig, err := bs.settingsService.GetBlacklistLevelConfig()
	if err != nil {
		levelConfig = DefaultBlacklistLevelConfig()
	}

	rows, err := db.Query(`
		SELECT
			platform,
			provider_name,
			failure_count,
			blacklisted_at,
			blacklisted_until,
			last_failure_at,
			blacklist_level,
			last_recovered_at
		FROM provider_blacklist
		WHERE platform = ?
		ORDER BY last_failure_at DESC
	`, platform)

	if err != nil {
		return nil, fmt.Errorf("查询黑名单状态失败: %w", err)
	}
	defer rows.Close()

	var statuses []BlacklistStatus
	now := time.Now()

	for rows.Next() {
		var s BlacklistStatus
		var blacklistedAt, blacklistedUntil, lastFailureAt, lastRecoveredAt sql.NullTime

		err := rows.Scan(
			&s.Platform,
			&s.ProviderName,
			&s.FailureCount,
			&blacklistedAt,
			&blacklistedUntil,
			&lastFailureAt,
			&s.BlacklistLevel,
			&lastRecoveredAt,
		)

		if err != nil {
			log.Printf("⚠️  读取黑名单状态失败: %v", err)
			continue
		}

		// 基础时间字段
		if blacklistedAt.Valid {
			s.BlacklistedAt = &blacklistedAt.Time
		}
		if blacklistedUntil.Valid {
			s.BlacklistedUntil = &blacklistedUntil.Time
			s.IsBlacklisted = blacklistedUntil.Time.After(now)
			if s.IsBlacklisted {
				s.RemainingSeconds = int(blacklistedUntil.Time.Sub(now).Seconds())
			}
		}
		if lastFailureAt.Valid {
			s.LastFailureAt = &lastFailureAt.Time
		}
		if lastRecoveredAt.Valid {
			s.LastRecoveredAt = &lastRecoveredAt.Time
		}

		// 计算宽恕倒计时（如果正在降级计时中）
		if levelConfig.EnableLevelBlacklist && lastRecoveredAt.Valid && s.BlacklistLevel >= 3 {
			timeSinceRecovery := now.Sub(lastRecoveredAt.Time)
			forgivenessThreshold := time.Duration(levelConfig.ForgivenessHours * float64(time.Hour))

			if timeSinceRecovery < forgivenessThreshold {
				s.ForgivenessRemaining = int((forgivenessThreshold - timeSinceRecovery).Seconds())
			} else {
				s.ForgivenessRemaining = 0 // 已触发宽恕
			}
		}

		statuses = append(statuses, s)
	}

	return statuses, nil
}

// ShouldUseFixedMode 返回是否应该使用固定拉黑模式（禁用自动降级）
// 满足以下所有条件时返回 true：
// 1. 黑名单总开关已启用
// 2. 且满足以下任一：
//   - 等级拉黑开启
//   - 等级拉黑关闭但 fallbackMode="fixed"
func (bs *BlacklistService) ShouldUseFixedMode() bool {
	// 首先检查全局开关
	if !bs.settingsService.IsBlacklistEnabled() {
		return false // 全局拉黑关闭 → 始终降级
	}

	config, err := bs.settingsService.GetBlacklistLevelConfig()
	if err != nil {
		// 读取失败：使用默认配置
		log.Printf("[BlacklistService] 读取配置失败，使用默认值: %v", err)
		defaultConfig := DefaultBlacklistLevelConfig()
		return defaultConfig.FallbackMode == "fixed"
	}

	// 等级拉黑开启 → 固定模式
	if config.EnableLevelBlacklist {
		return true
	}

	// 等级拉黑关闭 → 根据 fallbackMode 决定
	switch config.FallbackMode {
	case "fixed":
		return true
	case "none":
		return false
	default:
		// 未知值：记录警告并视为 none（保持降级）
		log.Printf("[BlacklistService] 未知的 fallbackMode: %s，视为 none", config.FallbackMode)
		return false
	}
}

// IsBlacklistEnabled 返回拉黑总开关状态（用于固定拉黑模式判断）
func (bs *BlacklistService) IsBlacklistEnabled() bool {
	return bs.settingsService.IsBlacklistEnabled()
}

// IsLevelBlacklistEnabled 返回等级拉黑功能是否开启
// 用于 proxyHandler 判断是否启用自动降级
func (bs *BlacklistService) IsLevelBlacklistEnabled() bool {
	config, err := bs.settingsService.GetBlacklistLevelConfig()
	if err != nil {
		return false // 出错时默认关闭（保持降级行为）
	}
	return config.EnableLevelBlacklist
}

// RetryConfig 重试配置（供 proxyHandler 使用）
type RetryConfig struct {
	FailureThreshold    int // 失败阈值（达到后触发拉黑）
	RetryWaitSeconds    int // 重试等待时间（秒）
	DedupeWindowSeconds int // 去重窗口（秒）
}

// GetRetryConfig 获取重试相关配置
// 用于 proxyHandler 实现同 Provider 重试机制
func (bs *BlacklistService) GetRetryConfig() *RetryConfig {
	config, err := bs.settingsService.GetBlacklistLevelConfig()
	if err != nil {
		// 【修复】读取配置失败时，也尝试从数据库读取阈值
		// 确保内层重试次数与实际拉黑阈值一致
		defaultConfig := DefaultBlacklistLevelConfig()
		result := &RetryConfig{
			FailureThreshold:    defaultConfig.FailureThreshold,
			RetryWaitSeconds:    defaultConfig.RetryWaitSeconds,
			DedupeWindowSeconds: defaultConfig.DedupeWindowSeconds,
		}
		// 尝试从数据库读取阈值
		if dbThreshold, _, dbErr := bs.settingsService.GetBlacklistSettings(); dbErr == nil && dbThreshold > 0 {
			result.FailureThreshold = dbThreshold
		}
		return result
	}
	return &RetryConfig{
		FailureThreshold:    config.FailureThreshold,
		RetryWaitSeconds:    config.RetryWaitSeconds,
		DedupeWindowSeconds: config.DedupeWindowSeconds,
	}
}
