package services

import (
	"os"
	"sync"
	"testing"
	"time"
)

// TestConsoleServicePauseLoggingConcurrentAccess 并发调用 addLog 与
// GetLogs/GetRecentLogs/ClearLogs，验证 pauseLogging 标志在并发读写下不产生数据竞争。
// 旧实现是裸 bool 且无锁读写，需要配合 `go test -race` 才能稳定复现。
func TestConsoleServicePauseLoggingConcurrentAccess(t *testing.T) {
	cs := &ConsoleService{
		logs:    make([]ConsoleLog, 0, 16),
		maxLogs: 16,
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				cs.addLog("INFO", "并发写入测试日志")
			}
		}
	}()

	for i := 0; i < 200; i++ {
		cs.GetLogs()
		cs.GetRecentLogs(5)
	}
	cs.ClearLogs()

	close(stop)
	wg.Wait()
}

// TestConsoleServiceCleanupPrintDoesNotBlockOtherCallers 验证清理提示的打印已经移出
// cs.mutex 的临界区：即便该打印写入的目标（这里用 os.Pipe 模拟被劫持的 stdout）已经
// 缓冲写满而永久阻塞，其他需要同一把锁的调用方（如 GetLogs）也不应被拖累。
//
// 旧实现在持锁期间调用 fmt.Printf 写向被自身劫持的 stdout 管道：一旦管道缓冲写满，
// 而唯一能读走数据的 readPipe 又需要抢同一把锁才能继续消费，两边就会互相等待、死锁，
// 挂死整个应用的日志输出。
func TestConsoleServiceCleanupPrintDoesNotBlockOtherCallers(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建管道失败: %v", err)
	}
	defer pr.Close()
	defer pw.Close()

	// 没有任何 goroutine 读取 pr，后台起一次远超常见管道缓冲区大小的写入，
	// 让它写满缓冲后一直阻塞在这里，模拟"stdout 管道缓冲已被占满"的场景。
	go func() {
		_, _ = pw.Write(make([]byte, 16*1024*1024))
	}()
	time.Sleep(300 * time.Millisecond)

	cs := &ConsoleService{
		maxLogs:   100,
		oldStdout: pw,
		logs: []ConsoleLog{
			{Timestamp: time.Now().Add(-96 * time.Hour), Level: "INFO", Message: "old"},
		},
	}

	// 触发一次清理：cleanOldLogs 会清掉上面这条过期日志并尝试打印提示，
	// 该打印写向已经写满的管道，必然阻塞在这次调用内部。
	go cs.addLog("INFO", "trigger cleanup")
	time.Sleep(100 * time.Millisecond)

	result := make(chan []ConsoleLog, 1)
	go func() {
		result <- cs.GetLogs()
	}()

	select {
	case <-result:
		// 符合预期：清理打印已移出临界区，GetLogs 不受阻塞的管道写入拖累
	case <-time.After(3 * time.Second):
		t.Fatal("GetLogs 被阻塞超过 3 秒：addLog 很可能仍在持锁期间做阻塞的 stdout 写入")
	}
}
