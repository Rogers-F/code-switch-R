package services

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// CacheAffinity 缓存亲和性记录
// 用于 5 分钟同源缓存优化：当某个 user+platform+model 组合成功使用某个 provider 后，
// 后续请求优先使用同一个 provider，避免频繁切换导致的不必要降级开销
type CacheAffinity struct {
	ProviderName string    `json:"provider_name"` // 缓存的 provider 名称
	ExpireAt     time.Time `json:"expire_at"`     // 过期时间
	RequestCount int64     `json:"request_count"` // 命中次数（统计用，使用 atomic 操作）
}

// CacheAffinityManager 缓存亲和性管理器
// 线程安全的内存缓存，支持 TTL 过期和后台清理
type CacheAffinityManager struct {
	store       map[string]*CacheAffinity
	mu          sync.RWMutex
	defaultTTL  time.Duration
	stopCh      chan struct{}
	startOnce   sync.Once // 防止多次启动清理任务
	stopOnce    sync.Once // 防止多次关闭 channel 导致 panic
	debugLog    bool      // 是否启用调试日志
}

// NewCacheAffinityManager 创建缓存亲和性管理器
// defaultTTL: 默认缓存时间（推荐 5 分钟 = 300 秒）
func NewCacheAffinityManager(defaultTTL time.Duration) *CacheAffinityManager {
	cam := &CacheAffinityManager{
		store:      make(map[string]*CacheAffinity),
		defaultTTL: defaultTTL,
		stopCh:     make(chan struct{}),
		debugLog:   false, // 默认关闭调试日志，生产环境更安静
	}
	return cam
}

// SetDebugLog 设置是否启用调试日志
func (cam *CacheAffinityManager) SetDebugLog(enabled bool) {
	cam.debugLog = enabled
}

// logDebug 输出调试日志（仅在 debugLog 启用时输出）
func (cam *CacheAffinityManager) logDebug(format string, args ...interface{}) {
	if cam.debugLog {
		fmt.Printf(format, args...)
	}
}

// GenerateAffinityKey 生成缓存亲和性键
// 格式: {user_id}:{platform}:{model}
// 其中 user_id 是 API Key 的 hash（保护隐私）
// 空模型名使用 "_default" 作为哨兵值，避免缓存冲突
func GenerateAffinityKey(userID, platform, model string) string {
	if model == "" {
		model = "_default"
	}
	return fmt.Sprintf("%s:%s:%s", userID, platform, model)
}

// HashAPIKey 对 API Key 进行 hash 处理，用于生成 user_id
// 只取前 16 个字符（8 字节 hex），足够区分不同用户但不暴露原始 key
func HashAPIKey(apiKey string) string {
	if apiKey == "" {
		return "anonymous"
	}
	hash := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(hash[:8]) // 只取前 8 字节
}

// Get 获取缓存的 provider 名称
// 如果缓存存在且未过期，返回 provider 名称；否则返回空字符串
// 使用 atomic 操作更新命中次数，避免写锁竞争
func (cam *CacheAffinityManager) Get(key string) string {
	// 快速路径：读锁检查
	cam.mu.RLock()
	affinity, exists := cam.store[key]
	if !exists {
		cam.mu.RUnlock()
		return ""
	}

	now := time.Now()
	if now.After(affinity.ExpireAt) {
		// 已过期，需要删除
		cam.mu.RUnlock()
		cam.mu.Lock()
		// 双重检查
		if aff, ok := cam.store[key]; ok && now.After(aff.ExpireAt) {
			delete(cam.store, key)
			cam.logDebug("[CacheAffinity] 缓存已过期: %s\n", key)
		}
		cam.mu.Unlock()
		return ""
	}

	// 有效缓存 - 使用 atomic 更新命中次数，避免获取写锁
	providerName := affinity.ProviderName
	count := atomic.AddInt64(&affinity.RequestCount, 1)
	cam.mu.RUnlock()

	cam.logDebug("[CacheAffinity] 缓存命中: %s → %s (count: %d)\n",
		key, providerName, count)

	return providerName
}

// Set 设置缓存亲和性
// 当请求成功时调用，记录 user+platform+model 对应的 provider
func (cam *CacheAffinityManager) Set(key, providerName string) {
	cam.mu.Lock()
	defer cam.mu.Unlock()

	cam.store[key] = &CacheAffinity{
		ProviderName: providerName,
		ExpireAt:     time.Now().Add(cam.defaultTTL),
		RequestCount: 1,
	}

	cam.logDebug("[CacheAffinity] 缓存设置: %s → %s (TTL: %v)\n",
		key, providerName, cam.defaultTTL)
}

// Invalidate 使缓存失效
// 当缓存的 provider 失败时调用，清除缓存以便尝试其他 provider
func (cam *CacheAffinityManager) Invalidate(key string) {
	cam.mu.Lock()
	defer cam.mu.Unlock()

	if _, exists := cam.store[key]; exists {
		delete(cam.store, key)
		cam.logDebug("[CacheAffinity] 缓存失效: %s\n", key)
	}
}

// StartCleanupTask 启动后台清理任务
// 每分钟清理一次过期的缓存条目，避免内存泄漏
// 使用 sync.Once 保证只启动一次，防止多次调用产生多个 goroutine
func (cam *CacheAffinityManager) StartCleanupTask() {
	cam.startOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(60 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					cam.cleanup()
				case <-cam.stopCh:
					return
				}
			}
		}()
		cam.logDebug("[CacheAffinity] 后台清理任务已启动（间隔: 60s）\n")
	})
}

// StopCleanupTask 停止后台清理任务
// 使用 sync.Once 保证只关闭一次，防止多次调用导致 panic
func (cam *CacheAffinityManager) StopCleanupTask() {
	cam.stopOnce.Do(func() {
		close(cam.stopCh)
	})
}

// cleanup 清理过期的缓存条目
func (cam *CacheAffinityManager) cleanup() {
	cam.mu.Lock()
	defer cam.mu.Unlock()

	now := time.Now()
	before := len(cam.store)
	for key, affinity := range cam.store {
		if now.After(affinity.ExpireAt) {
			delete(cam.store, key)
		}
	}
	removed := before - len(cam.store)

	if removed > 0 {
		cam.logDebug("[CacheAffinity] 清理了 %d 个过期缓存（剩余 %d 个）\n",
			removed, len(cam.store))
	}
}

// Stats 获取缓存统计信息
func (cam *CacheAffinityManager) Stats() (total int, expired int) {
	cam.mu.RLock()
	defer cam.mu.RUnlock()

	now := time.Now()
	total = len(cam.store)
	for _, affinity := range cam.store {
		if now.After(affinity.ExpireAt) {
			expired++
		}
	}
	return
}
