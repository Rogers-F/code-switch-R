// services/dbqueue.go
// SQLite 并发写入队列 - 消除 SQLITE_BUSY 错误
// Author: Half open flowers

package services

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daodao97/xgo/xdb"
)

// GlobalDBQueue 全局单次写入队列（用于异构写入：blacklist、settings 等）
var GlobalDBQueue *DBWriteQueue

// GlobalDBQueueLogs 全局批量写入队列（仅用于 request_log 同构写入）
var GlobalDBQueueLogs *DBWriteQueue

// InitGlobalDBQueue 初始化全局队列（双队列架构）
func InitGlobalDBQueue() error {
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}

	// 队列 1：单次写入队列（禁用批量，用于异构写入）
	// 用途：blacklist、app_settings 等不同表、不同操作的写入
	GlobalDBQueue = NewDBWriteQueue(db, 5000, false)

	// 队列 2：批量写入队列（启用批量，仅用于 request_log）
	// 用途：高频 request_log INSERT（同表同操作，严格同构）
	// 批量配置：50 条/批，最长等待 3 分钟提交（低频时减少写盘）
	GlobalDBQueueLogs = NewDBWriteQueue(db, 5000, true)

	return nil
}

// ShutdownGlobalDBQueue 关闭全局队列（双队列）
func ShutdownGlobalDBQueue(timeout time.Duration) error {
	var err1, err2 error

	// 关闭单次队列
	if GlobalDBQueue != nil {
		err1 = GlobalDBQueue.Shutdown(timeout)
	}

	// 关闭批量队列
	if GlobalDBQueueLogs != nil {
		err2 = GlobalDBQueueLogs.Shutdown(timeout)
	}

	// 如果有任何一个队列关闭失败，返回错误
	if err1 != nil {
		return fmt.Errorf("单次队列关闭失败: %w", err1)
	}
	if err2 != nil {
		return fmt.Errorf("批量队列关闭失败: %w", err2)
	}

	return nil
}

// GetGlobalDBQueueStats 获取单次队列统计
func GetGlobalDBQueueStats() QueueStats {
	if GlobalDBQueue != nil {
		return GlobalDBQueue.GetStats()
	}
	return QueueStats{}
}

// GetGlobalDBQueueLogsStats 获取批量队列统计
func GetGlobalDBQueueLogsStats() QueueStats {
	if GlobalDBQueueLogs != nil {
		return GlobalDBQueueLogs.GetStats()
	}
	return QueueStats{}
}

// WriteTask 写入任务
type WriteTask struct {
	SQL    string        // SQL语句
	Args   []interface{} // 参数
	Result chan error    // 结果通道（同步等待）
}

// DBWriteQueue 数据库写入队列
type DBWriteQueue struct {
	db           *sql.DB
	queue        chan *WriteTask
	batchQueue   chan *WriteTask // 批量提交队列
	shutdownChan chan struct{}
	wg           sync.WaitGroup

	// 关闭状态标志（防止 Shutdown 后仍可入队）
	closed atomic.Bool

	// 性能监控
	stats   *QueueStats
	statsMu sync.RWMutex

	// P99 延迟计算（环形缓冲区存储最近1000个样本）
	latencySamples []float64 // 延迟样本（毫秒）
	sampleIndex    int       // 当前写入位置
	sampleCount    int64     // 已记录样本数
}

// QueueStats 队列统计
type QueueStats struct {
	QueueLength      int     // 当前单次队列长度
	BatchQueueLength int     // 当前批量队列长度（如果启用）
	TotalWrites      int64   // 总写入数
	SuccessWrites    int64   // 成功写入数
	FailedWrites     int64   // 失败写入数
	AvgLatencyMs     float64 // 平均延迟（毫秒）
	P99LatencyMs     float64 // P99延迟
	BatchCommits     int64   // 批量提交次数
}

// NewDBWriteQueue 创建写入队列
// queueSize: 队列缓冲大小（推荐 1000-5000）
// enableBatch: 是否启用批量提交
//
// ⚠️ **批量模式使用约束**（critical）：
// - **仅用于同构写入**：批量通道（ExecBatch）只应用于相同表、相同操作的 SQL
//   - ✅ 正确用法：所有 request_log 的 INSERT（同一表、同一操作、参数结构相同）
//   - ❌ 错误用法：混入不同表的写入（request_log + provider_blacklist）
//   - ❌ 错误用法：混入不同操作（INSERT + UPDATE + DELETE）
// - **为什么必须同构**：
//   - 统计模型假设批次延迟在所有任务间均匀分布（perTaskLatencyMs = batchLatencyMs / count）
//   - 如果批次内有慢 SQL（触发器、复杂索引），会稀释快 SQL 的延迟统计
//   - P99 延迟会被低估，无法真实反映单请求 SLA
// - **代码审查检查点**：
//   - 搜索所有 ExecBatch/ExecBatchCtx 调用
//   - 确认每个调用点只写入同一个表的同一种操作
//   - 异构写入必须使用 Exec/ExecCtx（单次提交，统计准确）
func NewDBWriteQueue(db *sql.DB, queueSize int, enableBatch bool) *DBWriteQueue {
	q := &DBWriteQueue{
		db:             db,
		queue:          make(chan *WriteTask, queueSize),
		shutdownChan:   make(chan struct{}),
		stats:          &QueueStats{},
		latencySamples: make([]float64, 1000), // 环形缓冲区容量1000
		sampleIndex:    0,
		sampleCount:    0,
	}

	if enableBatch {
		q.batchQueue = make(chan *WriteTask, queueSize)
		q.wg.Add(1)
		go q.batchWorker() // 批量提交 worker
	}

	q.wg.Add(1)
	go q.worker() // 主 worker

	return q
}

// worker 单线程顺序处理所有写入
func (q *DBWriteQueue) worker() {
	defer q.wg.Done()

	var currentTask *WriteTask // 命名变量，用于在 panic 时返回错误

	// panic 保护：确保 worker 不会因未捕获的 panic 而崩溃
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("🚨 数据库写入队列 worker panic: %v\n", r)

				// 关键修复：如果 panic 时正在处理任务，必须返回错误，否则调用方永久阻塞
				if currentTask != nil && currentTask.Result != nil {
					currentTask.Result <- fmt.Errorf("数据库写入 panic: %v", r)
					close(currentTask.Result)
				}

			// 等待1秒后重启，避免快速循环（如果是系统性问题）
			time.Sleep(1 * time.Second)

			// 自动重启 worker
			q.wg.Add(1)
			go q.worker()
		}
	}()

	for {
		select {
			case task := <-q.queue:
				currentTask = task // 记录当前任务，用于 panic 时返回错误

				start := time.Now()
				_, err := q.db.Exec(task.SQL, task.Args...)

				// 更新统计（单次写入，count=1）
				q.updateStats(1, time.Since(start), err)

				// 返回结果（可选：支持异步入队）
				if task.Result != nil {
					task.Result <- err
					close(task.Result)
				}

				currentTask = nil // 清空当前任务（防止下一次 panic 误用）

			case <-q.shutdownChan:
			// 排空 queue 中的所有剩余任务
			for {
				select {
					case task := <-q.queue:
						currentTask = task // shutdown 排空时也需要跟踪，防止 panic

						start := time.Now()
						_, err := q.db.Exec(task.SQL, task.Args...)
						q.updateStats(1, time.Since(start), err)
						if task.Result != nil {
							task.Result <- err
							close(task.Result)
						}

						currentTask = nil
					default:
						// queue 已空，安全退出
						return
				}
			}
		}
	}
}

// batchWorker 批量提交 worker（可选）
func (q *DBWriteQueue) batchWorker() {
	defer q.wg.Done()

	var currentBatch []*WriteTask // 命名变量，用于在 panic 时返回错误

	// panic 保护：确保 batchWorker 不会因未捕获的 panic 而崩溃
	defer func() {
			if r := recover(); r != nil {
				fmt.Printf("🚨 数据库批量写入队列 worker panic: %v\n", r)

				// 关键修复：如果 panic 时正在处理批次，必须给所有任务返回错误
				if len(currentBatch) > 0 {
					panicErr := fmt.Errorf("批量写入 panic: %v", r)
					for _, task := range currentBatch {
						if task.Result != nil {
							task.Result <- panicErr
							close(task.Result)
						}
					}
				}

			// 等待1秒后重启，避免快速循环（如果是系统性问题）
			time.Sleep(1 * time.Second)

			// 自动重启 batchWorker
			q.wg.Add(1)
			go q.batchWorker()
		}
	}()

		const (
			batchFlushInterval = 3 * time.Minute
			batchMaxSize       = 50
		)

	var batch []*WriteTask
	var flushTimer *time.Timer
	var flushC <-chan time.Time

	stopFlushTimer := func() {
		if flushTimer == nil {
			return
		}
		if !flushTimer.Stop() {
			// Timer 可能已触发但还没被消费，尝试 drain
			select {
			case <-flushTimer.C:
			default:
			}
		}
		flushTimer = nil
		flushC = nil
	}

	startFlushTimer := func() {
		// 仅在 batch 从空变为非空时启动，避免无任务时常驻唤醒
		if flushTimer != nil {
			return
		}
		flushTimer = time.NewTimer(batchFlushInterval)
		flushC = flushTimer.C
	}

	commitAndReset := func() {
		if len(batch) == 0 {
			stopFlushTimer()
			return
		}
		// 提交前先停止定时器，避免重复触发
		stopFlushTimer()
		currentBatch = batch // 记录当前批次，用于 panic 时返回错误
		q.commitBatch(batch)
		batch = nil
		currentBatch = nil
	}

	for {
		select {
		case task := <-q.batchQueue:
			batch = append(batch, task)

			// 首次入队才启动 flush timer（按需唤醒）
			if len(batch) == 1 {
				startFlushTimer()
			}

			// 批次达到上限（50条），立即提交
			if len(batch) >= batchMaxSize {
				commitAndReset()
			}

		case <-flushC:
			commitAndReset()

		case <-q.shutdownChan:
			// 1. 先停止定时器并提交当前批次
			commitAndReset()

			// 2. 排空 batchQueue 中的所有剩余任务
			for {
				select {
				case task := <-q.batchQueue:
					batch = append(batch, task)
					// 每收集50个或队列空了就提交一次
					if len(batch) >= batchMaxSize {
						currentBatch = batch
						q.commitBatch(batch)
						batch = nil
						currentBatch = nil
					}
				default:
					// batchQueue 已空，提交最后一批
					if len(batch) > 0 {
						currentBatch = batch
						q.commitBatch(batch)
						currentBatch = nil
					}
					return
				}
			}
		}
	}
}

// commitBatch 批量提交（使用事务）
func (q *DBWriteQueue) commitBatch(tasks []*WriteTask) {
	start := time.Now()

	// 辅助函数：给所有任务返回结果（成功或失败）
	sendResultToAll := func(err error) {
		for _, task := range tasks {
			if task.Result != nil {
				task.Result <- err
				close(task.Result)
			}
		}
		// 更新统计（批量提交，count=任务数）
		q.updateStats(len(tasks), time.Since(start), err)
		if err == nil {
			q.statsMu.Lock()
			q.stats.BatchCommits++
			q.statsMu.Unlock()
		}
	}

	tx, err := q.db.Begin()
	if err != nil {
		// 事务开启失败，所有任务都失败
		sendResultToAll(err)
		return
	}
	defer tx.Rollback()

	// 执行所有任务，记录第一个错误
	var firstErr error
	for _, task := range tasks {
		_, err := tx.Exec(task.SQL, task.Args...)
		if err != nil && firstErr == nil {
			firstErr = err // 记录第一个错误，但继续执行以清理资源
		}
	}

	// 如果有任何错误，回滚并通知所有任务
	if firstErr != nil {
		sendResultToAll(fmt.Errorf("批量提交失败: %w", firstErr))
		return
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		sendResultToAll(fmt.Errorf("事务提交失败: %w", err))
		return
	}

	// 全部成功
	sendResultToAll(nil)
}

// Exec 同步执行写入（阻塞直到完成，默认 30 秒超时）
// 防御性设计：即使在高频路径误用，也有 30 秒兜底超时，避免永久阻塞
func (q *DBWriteQueue) Exec(sql string, args ...interface{}) error {
	// 先检查关闭状态
	if q.closed.Load() {
		return fmt.Errorf("写入队列已关闭")
	}

	task := &WriteTask{
		SQL:    sql,
		Args:   args,
		Result: make(chan error, 1),
	}

	// 默认 30 秒超时（防止误用导致永久阻塞）
	timeout := time.After(30 * time.Second)

	select {
	case q.queue <- task:
		// 成功入队，等待结果（支持超时）
		select {
		case err := <-task.Result:
			return err
		case <-timeout:
			// 超时，但任务已入队，无法撤销，需等待结果以避免 goroutine 泄漏
			go func() { <-task.Result }()
			return fmt.Errorf("写入超时（30秒），队列可能积压严重")
		}

	case <-timeout:
		// 入队失败（队列满），直接返回
		return fmt.Errorf("入队超时（30秒），队列已满")

	case <-q.shutdownChan:
		return fmt.Errorf("写入队列已关闭")
	}
}

// ExecBatch 批量执行（异步，高吞吐量场景，默认 30 秒超时）
// 防御性设计：即使误用，也有 30 秒兜底超时
func (q *DBWriteQueue) ExecBatch(sql string, args ...interface{}) error {
	// 先检查关闭状态
	if q.closed.Load() {
		return fmt.Errorf("写入队列已关闭")
	}

	if q.batchQueue == nil {
		return fmt.Errorf("批量模式未启用")
	}

	task := &WriteTask{
		SQL:    sql,
		Args:   args,
		Result: make(chan error, 1),
	}

	// 默认 30 秒超时（防止误用导致永久阻塞）
	timeout := time.After(30 * time.Second)

	select {
	case q.batchQueue <- task:
		// 成功入队，等待结果（支持超时）
		select {
		case err := <-task.Result:
			return err
		case <-timeout:
			// 超时，但任务已入队，无法撤销
			go func() { <-task.Result }()
			return fmt.Errorf("批量写入超时（30秒），批量队列可能积压严重")
		}

	case <-timeout:
		// 入队失败（队列满），直接返回
		return fmt.Errorf("批量入队超时（30秒），队列已满")

	case <-q.shutdownChan:
		return fmt.Errorf("写入队列已关闭")
	}
}

// ExecCtx 支持 context 的写入（带超时控制）
func (q *DBWriteQueue) ExecCtx(ctx context.Context, sql string, args ...interface{}) error {
	// 先检查关闭状态
	if q.closed.Load() {
		return fmt.Errorf("写入队列已关闭")
	}

	task := &WriteTask{
		SQL:    sql,
		Args:   args,
		Result: make(chan error, 1),
	}

	select {
	case q.queue <- task:
		// 成功入队，等待结果（支持超时）
		select {
		case err := <-task.Result:
			return err
		case <-ctx.Done():
			// 超时或取消，但任务已入队，无法撤销
			// 仍需等待结果以避免 goroutine 泄漏
			go func() { <-task.Result }()
			return fmt.Errorf("写入超时或已取消: %w", ctx.Err())
		}

	case <-ctx.Done():
		// 入队失败（队列满），直接返回
		return fmt.Errorf("入队超时或已取消（队列满）: %w", ctx.Err())

	case <-q.shutdownChan:
		return fmt.Errorf("写入队列已关闭")
	}
}

// ExecBatchCtx 支持 context 的批量写入（带超时控制）
func (q *DBWriteQueue) ExecBatchCtx(ctx context.Context, sql string, args ...interface{}) error {
	// 先检查关闭状态
	if q.closed.Load() {
		return fmt.Errorf("写入队列已关闭")
	}

	if q.batchQueue == nil {
		return fmt.Errorf("批量模式未启用")
	}

	task := &WriteTask{
		SQL:    sql,
		Args:   args,
		Result: make(chan error, 1),
	}

	select {
	case q.batchQueue <- task:
		// 成功入队，等待结果（支持超时）
		select {
		case err := <-task.Result:
			return err
		case <-ctx.Done():
			// 超时或取消，但任务已入队，无法撤销
			go func() { <-task.Result }()
			return fmt.Errorf("批量写入超时或已取消: %w", ctx.Err())
		}

	case <-ctx.Done():
		// 入队失败（队列满），直接返回
		return fmt.Errorf("批量入队超时或已取消（队列满）: %w", ctx.Err())

	case <-q.shutdownChan:
		return fmt.Errorf("写入队列已关闭")
	}
}

// EnqueueBatch best-effort 批量入队（不等待落盘）
// 用途：request_log 这类“丢了也不影响核心功能”的高频日志写入，避免阻塞请求路径。
func (q *DBWriteQueue) EnqueueBatch(sql string, args ...interface{}) error {
	// 先检查关闭状态
	if q.closed.Load() {
		return fmt.Errorf("写入队列已关闭")
	}

	if q.batchQueue == nil {
		return fmt.Errorf("批量模式未启用")
	}

	task := &WriteTask{
		SQL:  sql,
		Args: args,
		// Result=nil 表示异步入队：worker/commitBatch 不会尝试回写结果
		Result: nil,
	}

	// 不阻塞主流程：队列满则直接丢弃
	select {
	case q.batchQueue <- task:
		return nil
	default:
		return fmt.Errorf("批量队列已满，已丢弃")
	}
}

// Shutdown 优雅关闭
func (q *DBWriteQueue) Shutdown(timeout time.Duration) error {
	// 关键修复：先设置关闭标志，拒绝新请求入队
	q.closed.Store(true)

	// 然后关闭 shutdownChan，通知 worker 排空队列
	close(q.shutdownChan)

	done := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("关闭超时，队列中仍有 %d 个任务", len(q.queue))
	}
}

// GetStats 获取统计信息
func (q *DBWriteQueue) GetStats() QueueStats {
	q.statsMu.RLock()
	defer q.statsMu.RUnlock()

	stats := *q.stats
	stats.QueueLength = len(q.queue)

	// 如果启用了批量队列，也返回其长度
	if q.batchQueue != nil {
		stats.BatchQueueLength = len(q.batchQueue)
	}

	return stats
}

// updateStats 更新统计信息
// count: 本次操作涵盖的任务数（单次=1，批量=len(tasks)）
// latency: 操作耗时
// err: 错误（nil表示成功）
//
// 📌 统计假设与局限性说明：
//
// 1. **平均延迟计算假设**：
//    - 批量提交时，假设批次延迟在所有任务间均匀分布
//    - 计算公式：AvgLatencyMs = (旧总延迟 + 批次延迟) / 新总任务数
//    - 局限性：如果批次内不同 SQL 耗时差异巨大（如含触发器、复杂索引），统计会失真
//
// 2. **P99 延迟计算假设**：
//    - 批量提交时，将批次延迟平均分摊到每个任务（perTaskLatencyMs = latencyMs / count）
//    - 每个任务记录相同的延迟样本，用于 P99 计算
//    - 局限性：真实情况下，批次内首个任务可能耗时更长（事务开启开销），最后一个任务可能更快
//
// 3. **适用场景**：
//    - ✅ 批次内所有 SQL 耗时相近（如 request_log INSERT，相同表结构、无触发器）
//    - ✅ 关注整体系统性能趋势，而非单条 SQL 精确耗时
//    - ❌ 批次内混合不同类型操作（INSERT + UPDATE + DELETE）
//    - ❌ 需要精确追踪每条 SQL 的实际耗时
//
// 4. **改进方向**（如需精确统计）：
//    - 在 WriteTask 中添加 startTime 字段，worker 执行时逐个记录真实耗时
//    - 成本：每个任务额外 8 字节（time.Time）+ 逐个更新统计的锁竞争
func (q *DBWriteQueue) updateStats(count int, latency time.Duration, err error) {
	q.statsMu.Lock()
	defer q.statsMu.Unlock()

	// 按任务数累加（而非按批次数）
	q.stats.TotalWrites += int64(count)
	if err == nil {
		q.stats.SuccessWrites += int64(count)
	} else {
		q.stats.FailedWrites += int64(count)
	}

	latencyMs := float64(latency.Milliseconds())

	// 更新平均延迟（使用加权平均，批量提交时延迟按任务数权重分摊）
	oldTotal := q.stats.TotalWrites - int64(count)
	q.stats.AvgLatencyMs = (q.stats.AvgLatencyMs*float64(oldTotal) + latencyMs*float64(count)) / float64(q.stats.TotalWrites)

	// P99 样本按单任务记录（批量提交时将批次延迟均分）
	perTaskLatencyMs := latencyMs / float64(count)
	for i := 0; i < count; i++ {
		q.latencySamples[q.sampleIndex] = perTaskLatencyMs
		q.sampleIndex = (q.sampleIndex + 1) % len(q.latencySamples)
		q.sampleCount++
	}

	// 计算 P99 延迟（每100次更新一次，避免频繁排序）
	if q.sampleCount%100 == 0 || q.sampleCount < 100 {
		q.stats.P99LatencyMs = q.calculateP99()
	}
}

// calculateP99 计算 P99 延迟（需持有锁）
func (q *DBWriteQueue) calculateP99() float64 {
	// 确定有效样本数量
	validSamples := int(q.sampleCount)
	if validSamples > len(q.latencySamples) {
		validSamples = len(q.latencySamples)
	}

	if validSamples == 0 {
		return 0
	}

	// 复制样本并排序（使用标准库快速排序）
	samples := make([]float64, validSamples)
	copy(samples, q.latencySamples[:validSamples])
	sort.Float64s(samples)

	// 计算 P99 位置
	p99Index := int(float64(validSamples) * 0.99)
	if p99Index >= validSamples {
		p99Index = validSamples - 1
	}

	return samples[p99Index]
}
