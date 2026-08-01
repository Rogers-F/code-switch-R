package services

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ========== 按供应商并发限制（issue #21）==========
//
// 语义约定（与方案评审收敛结果一致）：
// - MaxConcurrency 只约束"代理转发的推理请求"（/v1/messages、/responses、
//   gemini 生成、custom 转发）；/v1/models、健康检查、模型同步等内部请求不占配额；
// - 这是单进程内的限制：同一供应商账号配置在多个平台条目或多个应用进程时不合并；
// - 满载即"忙"：不计供应商失败、不进黑名单、不耗重试预算、不写请求日志；
// - 配置容量热更新以保存路径递增的配置代数为准，在途请求携带的旧副本不得回写旧容量。

// concurrencyWaiterLimit 等待阶段的全局等待者上限：超过即直接按忙处理，
// 避免只限等待时长却放任无限等待者堆积
const concurrencyWaiterLimit = 256

// concurrencyWaitBudget 等待阶段的总时长预算（实际 deadline 还会被客户端
// context 截短）。做成 limiter 字段便于测试注入，不暴露为设置项。
const concurrencyWaitBudget = 30 * time.Second

type concurrencyEntry struct {
	limit    int
	gen      int64
	inFlight int
}

// concurrencyLimiter 供应商并发配额的进程内登记表。
// 换代 channel 承担"任一释放"广播：Release 在锁内 close 当前代并换新代，
// 等待者先取当前代引用、再做整遍重扫，扫完无果才阻塞在旧代上——
// 引用先于扫描获取，释放发生在两者之间也不会丢失唤醒。
type concurrencyLimiter struct {
	mu         sync.Mutex
	entries    map[string]*concurrencyEntry
	releaseGen chan struct{}
	waiters    int
	waitBudget time.Duration
}

func newConcurrencyLimiter() *concurrencyLimiter {
	return &concurrencyLimiter{
		entries:    make(map[string]*concurrencyEntry),
		releaseGen: make(chan struct{}),
		waitBudget: concurrencyWaitBudget,
	}
}

// errProviderBusy 表示供应商并发配额已满：不计失败、不进黑名单、
// 不耗重试预算、不写请求日志，调度器跳过该供应商并可能进入等待阶段
var errProviderBusy = errors.New("provider at max concurrency")

func concurrencyKey(platform string, providerKey string) string {
	return platform + "\x00" + providerKey
}

// TryAcquire 尝试占用一个配额。limit<=0 表示不限（仍跟踪 inFlight，
// 以便之后从不限改为有限时知道当前在途量）。gen 为调用方装载配置时的
// 配置代数：更高代数才允许更新容量，防止在途旧副本把新容量改回去。
func (l *concurrencyLimiter) TryAcquire(platform string, providerKey string, limit int, gen int64) bool {
	key := concurrencyKey(platform, providerKey)
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.entries[key]
	if !ok {
		entry = &concurrencyEntry{limit: limit, gen: gen}
		l.entries[key] = entry
	} else if gen >= entry.gen {
		entry.limit = limit
		entry.gen = gen
	}

	if entry.limit > 0 && entry.inFlight >= entry.limit {
		return false
	}
	entry.inFlight++
	return true
}

// Release 归还配额并广播"有释放发生"（换代唤醒全部等待者重扫）
func (l *concurrencyLimiter) Release(platform string, providerKey string) {
	key := concurrencyKey(platform, providerKey)
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.entries[key]
	if !ok || entry.inFlight <= 0 {
		// 重复 release 属编程错误，静默容错但不让计数变负
		return
	}
	entry.inFlight--
	// 空闲条目也不删除：entry 里的最高配置代数是"旧副本不得回写容量"的
	// 依据，删掉后携带旧代配置的在途请求会用旧容量重建条目。
	// 条目数量以供应商数为界，常驻内存可忽略。

	close(l.releaseGen)
	l.releaseGen = make(chan struct{})
}

// releaseSignal 返回当前代的释放信号 channel。
// 必须在整遍扫描之前获取：扫描期间发生的释放会 close 这一代，
// select 立即返回，不会丢失唤醒。
func (l *concurrencyLimiter) releaseSignal() <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.releaseGen
}

// enterWaitPhase 注册一个等待者；超过全局上限返回 false（调用方按忙终态处理）
func (l *concurrencyLimiter) enterWaitPhase() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.waiters >= concurrencyWaiterLimit {
		return false
	}
	l.waiters++
	return true
}

func (l *concurrencyLimiter) leaveWaitPhase() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.waiters > 0 {
		l.waiters--
	}
}

// waitForRelease 阻塞到任一配额释放/客户端取消/预算耗尽。
// 返回 true 表示收到释放信号（值得整遍重扫），false 表示该放弃了。
// signal 必须是调用方在上一遍扫描之前通过 releaseSignal() 取的引用。
func (l *concurrencyLimiter) waitForRelease(ctx context.Context, deadline time.Time, signal <-chan struct{}) bool {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return false
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-signal:
		return true
	case <-ctx.Done():
		return false
	case <-timer.C:
		return false
	}
}

// concurrencyBusyRef 等待阶段登记的忙候选：键、装载时的容量与配置代数
type concurrencyBusyRef struct {
	Key   string
	Limit int
	Gen   int64
}

// anyCapacity 只读检查：忙候选中是否有任一供应商已腾出空位。
// 等待阶段必须用它做唤醒门控——全局释放信号会被本轮实际尝试供应商的
// 正常释放触发，不加门控直接重扫会形成自唤醒重试风暴。
func (l *concurrencyLimiter) anyCapacity(platform string, pending map[string]concurrencyBusyRef) bool {
	if len(pending) == 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, ref := range pending {
		entry, ok := l.entries[concurrencyKey(platform, ref.Key)]
		if !ok {
			return true
		}
		limit := entry.limit
		// 与 TryAcquire 的同代更新规则一致（>=）
		if ref.Gen >= entry.gen {
			limit = ref.Limit
		}
		if limit <= 0 || entry.inFlight < limit {
			return true
		}
	}
	return false
}

// snapshotInFlight 测试与诊断用：读取当前在途数
func (l *concurrencyLimiter) snapshotInFlight(platform string, providerKey string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry, ok := l.entries[concurrencyKey(platform, providerKey)]; ok {
		return entry.inFlight
	}
	return 0
}
