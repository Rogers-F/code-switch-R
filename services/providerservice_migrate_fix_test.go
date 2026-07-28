package services

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestLoadProviders_MigrationDoesNotClobberConcurrentSave 验证迁移回写不会用锁外读到的
// 旧快照覆盖并发保存的新配置：旧实现在锁外 ReadFile 后拿锁直接回写该快照，
// 期间其他协程写入的新供应商会被静默抹掉。
func TestLoadProviders_MigrationDoesNotClobberConcurrentSave(t *testing.T) {
	setupRenameTestEnv(t)

	ps := NewProviderService()

	// 初始文件带旧字段 connectivityCheck，触发 LoadProviders 的迁移回写路径
	saveProviderFixture(t, ps, []Provider{
		{ID: 1, Name: "A", APIURL: "https://a.com", APIKey: "k", ConnectivityCheck: true},
	})

	// 测试先持有 ps.mu，让 LoadProviders 在锁外读完旧快照后阻塞在拿锁处
	ps.mu.Lock()

	type loadResult struct {
		providers []Provider
		err       error
	}
	done := make(chan loadResult, 1)
	go func() {
		providers, err := ps.LoadProviders("claude")
		done <- loadResult{providers, err}
	}()

	// 等 LoadProviders 完成锁外读取并阻塞在 ps.mu 上
	time.Sleep(300 * time.Millisecond)

	// 模拟并发保存：旧字段已清除、新增供应商 B（绕过服务直接写文件，避免与持有的锁死锁）
	path, err := providerFilePath("claude")
	if err != nil {
		t.Fatalf("获取路径失败: %v", err)
	}
	newData, _ := serializeProviders([]Provider{
		{ID: 1, Name: "A", APIURL: "https://a.com", APIKey: "k", AvailabilityMonitorEnabled: true},
		{ID: 2, Name: "B", APIURL: "https://b.com", APIKey: "k2"},
	})
	if err := os.WriteFile(path, newData, 0o644); err != nil {
		t.Fatalf("写入新配置失败: %v", err)
	}

	ps.mu.Unlock()
	res := <-done
	if res.err != nil {
		t.Fatalf("LoadProviders 失败: %v", res.err)
	}

	// 返回值应包含并发写入的供应商 B
	if len(res.providers) != 2 {
		t.Fatalf("返回值应含 2 个供应商（并发新增的 B 不应丢失），实际 %d 个: %+v",
			len(res.providers), res.providers)
	}

	// 磁盘文件也不应被旧快照覆盖
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回配置失败: %v", err)
	}
	var envelope providerEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("解析配置失败: %v", err)
	}
	foundB := false
	for _, p := range envelope.Providers {
		if p.ID == 2 && p.Name == "B" {
			foundB = true
		}
	}
	if !foundB {
		t.Errorf("迁移回写覆盖了并发保存：磁盘文件丢失供应商 B，实际内容: %s", string(data))
	}
}
