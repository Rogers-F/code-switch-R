package services

import (
	"database/sql"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newQueueFixTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", "file:queue-fix-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`CREATE TABLE events (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

// TestDBWriteQueueShutdownRaceNoLostTask 并发入队与 Shutdown 竞态下的不变量:
// closed 检查与入队在读锁内原子完成,成功入队的任务必然会被 worker 排空执行。
// 旧实现中检查通过后 worker 可能已排空退出,任务落入死队列——
// 表现为关闭后队列残留任务、等待结果的调用方长时间不返回。
func TestDBWriteQueueShutdownRaceNoLostTask(t *testing.T) {
	db := newQueueFixTestDB(t)
	queue := NewDBWriteQueue(db, 100, false)

	var successCount atomic.Int64
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				err := queue.Exec(`INSERT INTO events (name) VALUES (?)`, "x")
				if err == nil {
					successCount.Add(1)
					continue
				}
				if !strings.Contains(err.Error(), "写入队列已关闭") {
					t.Errorf("意外错误: %v", err)
				}
				return
			}
		}()
	}

	time.Sleep(50 * time.Millisecond)
	if err := queue.Shutdown(5 * time.Second); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	close(stop)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("调用方在关闭后未及时返回,疑似任务落入无消费者的死队列")
	}

	// 关闭完成后队列必须已被排空(旧实现的竞态会让任务滞留在队列里)
	if n := len(queue.queue); n != 0 {
		t.Fatalf("关闭后队列仍残留 %d 个任务", n)
	}

	// 返回成功的写入必须全部落库,返回失败的写入不应落库
	var rows int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != successCount.Load() {
		t.Fatalf("落库行数 %d 与成功返回数 %d 不一致", rows, successCount.Load())
	}
}

// TestDBWriteQueueBatchRejectsAfterShutdown 关闭后批量入口也必须立即拒绝
func TestDBWriteQueueBatchRejectsAfterShutdown(t *testing.T) {
	db := newQueueFixTestDB(t)
	queue := NewDBWriteQueue(db, 10, true)

	if err := queue.Shutdown(time.Second); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	err := queue.ExecBatch(`INSERT INTO events (name) VALUES (?)`, "after")
	if err == nil {
		t.Fatal("expected closed queue error")
	}
	if !strings.Contains(err.Error(), "写入队列已关闭") {
		t.Fatalf("unexpected error: %v", err)
	}
}
