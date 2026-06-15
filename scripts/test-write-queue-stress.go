//go:build ignore

// scripts/test-write-queue-stress.go
// SQLite 并发写入队列 - 压力测试脚本
// Author: Half open flowers

package main

import (
	"codeswitch/services"
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/daodao97/xgo/xdb"
)

func main() {
	fmt.Println("========== SQLite 写入队列压力测试 ==========")
	fmt.Println("目标：500 req/s 持续 60 秒，验证 100% 写入成功")
	fmt.Println()

	// 1. 初始化数据库连接
	err := xdb.Inits([]xdb.Config{
		{
			Name:   "default",
			Driver: "sqlite",
			DSN:    "app.db?cache=shared&mode=rwc&_journal_mode=WAL",
		},
	})
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}

	db, err := xdb.DB("default")
	if err != nil {
		log.Fatalf("获取数据库连接失败: %v", err)
	}

	// 2. 创建测试表（如果不存在）
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS request_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			platform TEXT NOT NULL,
			model TEXT,
			provider TEXT,
			http_code INTEGER,
			input_tokens INTEGER DEFAULT 0,
			output_tokens INTEGER DEFAULT 0,
			cache_create_tokens INTEGER DEFAULT 0,
			cache_read_tokens INTEGER DEFAULT 0,
			reasoning_tokens INTEGER DEFAULT 0,
			is_stream INTEGER DEFAULT 0,
			duration_sec REAL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		log.Fatalf("创建测试表失败: %v", err)
	}

	// 3. 创建批量队列（对齐生产环境：request_log 使用批量模式）
	// ✅ request_log 是同构写入（同表同操作），适合批量提交
	queue := services.NewDBWriteQueue(db, 5000, true)
	defer func() {
		fmt.Println("\n正在关闭队列...")
		if err := queue.Shutdown(30 * time.Second); err != nil {
			fmt.Printf("⚠️  队列关闭超时: %v\n", err)
		} else {
			fmt.Println("✅ 队列已安全关闭")
		}
	}()

	// 4. 压力测试参数
	const (
		targetRPS = 500              // 目标请求速率（每秒）
		duration  = 60 * time.Second // 测试持续时间
	)

	fmt.Printf("测试配置：\n")
	fmt.Printf("  - 目标速率: %d req/s\n", targetRPS)
	fmt.Printf("  - 持续时间: %v\n", duration)
	fmt.Printf("  - 队列大小: 5000\n")
	fmt.Printf("  - 批量模式: 启用（50条/批，100ms超时提交）\n")
	fmt.Printf("  - 超时控制: 5 秒（对齐高频写入路径）\n")
	fmt.Println()

	// 5. 启动统计输出 goroutine
	stopStats := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				stats := queue.GetStats()
				fmt.Printf("[统计] 队列长度=%d | 总写入=%d | 成功=%d | 失败=%d | 平均延迟=%.2fms | P99延迟=%.2fms\n",
					stats.QueueLength, stats.TotalWrites, stats.SuccessWrites, stats.FailedWrites,
					stats.AvgLatencyMs, stats.P99LatencyMs)
			case <-stopStats:
				return
			}
		}
	}()

	// 6. 执行压力测试
	var totalWrites int64
	var errors int64

	start := time.Now()
	ticker := time.NewTicker(time.Second / time.Duration(targetRPS)) // 每个请求的间隔
	defer ticker.Stop()

	timeout := time.After(duration)

	fmt.Println("========== 开始压力测试 ==========\n")

	for {
		select {
		case <-ticker.C:
			go func() {
				// 使用 ExecBatchCtx(5s) 对齐生产环境批量写入用法
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				// 模拟真实 request_log 写入（批量提交）
				err := queue.ExecBatchCtx(ctx, `
					INSERT INTO request_log (
						platform, model, provider, http_code,
						input_tokens, output_tokens, cache_create_tokens, cache_read_tokens,
						reasoning_tokens, is_stream, duration_sec
					) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				`,
					"claude",                     // platform
					"claude-sonnet-4-5-20250929", // model
					"Anthropic Official",         // provider
					200,                          // http_code
					1000+int(atomic.LoadInt64(&totalWrites))%5000, // input_tokens (变化)
					500+int(atomic.LoadInt64(&totalWrites))%2000,  // output_tokens (变化)
					0, // cache_create_tokens
					0, // cache_read_tokens
					0, // reasoning_tokens
					1, // is_stream
					2.5+float64(atomic.LoadInt64(&totalWrites)%10)/10, // duration_sec (变化)
				)

				atomic.AddInt64(&totalWrites, 1)
				if err != nil {
					atomic.AddInt64(&errors, 1)
					// 只输出前 10 个错误，避免刷屏
					if atomic.LoadInt64(&errors) <= 10 {
						fmt.Printf("写入失败: %v\n", err)
					}
				}
			}()

		case <-timeout:
			// 测试结束，等待所有 goroutine 完成
			fmt.Println("\n测试时间到，等待所有任务完成...")
			time.Sleep(10 * time.Second) // 等待剩余任务处理完成

			// 停止统计输出
			close(stopStats)

			// 7. 输出最终结果
			elapsed := time.Since(start)
			stats := queue.GetStats()

			fmt.Println("\n========== 压力测试结果 ==========")
			fmt.Printf("持续时间: %v\n", elapsed)
			fmt.Printf("发起写入数: %d\n", totalWrites)
			fmt.Printf("成功写入数: %d\n", stats.SuccessWrites)
			fmt.Printf("失败写入数: %d\n", errors)
			fmt.Printf("成功率: %.2f%%\n", float64(stats.SuccessWrites)/float64(totalWrites)*100)
			fmt.Printf("实际吞吐量: %.2f req/s\n", float64(stats.TotalWrites)/elapsed.Seconds())
			fmt.Println()
			fmt.Printf("队列统计：\n")
			fmt.Printf("  - 当前队列长度: %d\n", stats.QueueLength)
			fmt.Printf("  - 平均延迟: %.2fms\n", stats.AvgLatencyMs)
			fmt.Printf("  - P99 延迟: %.2fms\n", stats.P99LatencyMs)
			fmt.Printf("  - 批量提交次数: %d\n", stats.BatchCommits)
			fmt.Println()

			// 8. 验收标准检查
			fmt.Println("========== 验收标准检查 ==========")
			passed := true

			// 检查成功率
			successRate := float64(stats.SuccessWrites) / float64(totalWrites) * 100
			if successRate >= 100.0 {
				fmt.Println("✅ 成功率: 100% (符合要求)")
			} else {
				fmt.Printf("❌ 成功率: %.2f%% (要求 100%%)\n", successRate)
				passed = false
			}

			// 检查平均延迟
			if stats.AvgLatencyMs < 50 {
				fmt.Printf("✅ 平均延迟: %.2fms (符合要求 <50ms)\n", stats.AvgLatencyMs)
			} else {
				fmt.Printf("❌ 平均延迟: %.2fms (要求 <50ms)\n", stats.AvgLatencyMs)
				passed = false
			}

			// 检查 P99 延迟
			if stats.P99LatencyMs < 200 {
				fmt.Printf("✅ P99 延迟: %.2fms (符合要求 <200ms)\n", stats.P99LatencyMs)
			} else {
				fmt.Printf("❌ P99 延迟: %.2fms (要求 <200ms)\n", stats.P99LatencyMs)
				passed = false
			}

			// 检查队列长度
			peakQueueLength := stats.QueueLength // 注意：这只是最终快照，实际峰值可能更高
			if peakQueueLength < 1000 {
				fmt.Printf("✅ 队列长度: %d (符合要求 <1000)\n", peakQueueLength)
			} else {
				fmt.Printf("⚠️  队列长度: %d (建议 <1000，但不影响验收)\n", peakQueueLength)
			}

			fmt.Println("====================================")
			if passed {
				fmt.Println("🎉 压力测试通过！所有验收标准均满足。")
			} else {
				fmt.Println("⚠️  压力测试未通过，请检查上述失败项。")
			}

			return
		}
	}
}
