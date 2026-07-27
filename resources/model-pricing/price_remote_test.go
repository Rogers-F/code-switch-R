package modelpricing

import (
	"sync"
	"testing"
)

func f64(v float64) *float64 { return &v }

// findEmbeddedKey 在嵌入快照中找一个满足条件的 key。
func findEmbeddedKey(t *testing.T, pred func(*PricingEntry) bool) string {
	t.Helper()
	svc, err := NewService()
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	snap := svc.currentSnapshot()
	for _, key := range sortedKeys(snap.pricingMap) {
		if pred(snap.pricingMap[key]) {
			return key
		}
	}
	t.Fatal("嵌入表中找不到满足条件的条目")
	return ""
}

// TestRebuildAddsNewRemoteModel 远程新模型入表,presence 语义:未提供的缓存价保持 0,不做通用推导。
func TestRebuildAddsNewRemoteModel(t *testing.T) {
	svc, err := NewService()
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	stats, err := svc.Rebuild(map[string]RemoteEntry{
		"test-brand-new-model": {Input: f64(2e-6), Output: f64(1e-5)},
	})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if stats.RemoteAdded != 1 {
		t.Errorf("RemoteAdded = %d, want 1", stats.RemoteAdded)
	}
	entry, ok := svc.currentSnapshot().getPricing("test-brand-new-model")
	if !ok {
		t.Fatal("新模型应能查到价格")
	}
	if entry.InputCostPerToken != 2e-6 || entry.OutputCostPerToken != 1e-5 {
		t.Errorf("基础价不符: in=%v out=%v", entry.InputCostPerToken, entry.OutputCostPerToken)
	}
	if entry.CacheCreationInputTokenCost != 0 || entry.CacheReadInputTokenCost != 0 {
		t.Errorf("远程新模型缺失缓存价时不应伪造推导: create=%v read=%v",
			entry.CacheCreationInputTokenCost, entry.CacheReadInputTokenCost)
	}
}

// TestRebuildUpdatesSimpleEntryAndKeepsExplicitZero 简单条目被远程基础价覆盖,显式 0 保留。
func TestRebuildUpdatesSimpleEntryAndKeepsExplicitZero(t *testing.T) {
	key := findEmbeddedKey(t, func(e *PricingEntry) bool {
		return !isRichEntry(e) && e.InputCostPerToken > 0
	})
	svc, err := NewService()
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	stats, err := svc.Rebuild(map[string]RemoteEntry{
		key: {Input: f64(7e-6), CacheRead: f64(0)},
	})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if stats.RemoteUpdated != 1 {
		t.Errorf("RemoteUpdated = %d, want 1 (key=%s)", stats.RemoteUpdated, key)
	}
	entry, _ := svc.currentSnapshot().getPricing(key)
	if entry.InputCostPerToken != 7e-6 {
		t.Errorf("input 应被远程覆盖为 7e-6,实际 %v", entry.InputCostPerToken)
	}
	if entry.CacheReadInputTokenCost != 0 {
		t.Errorf("远程显式 0 的 cache_read 应保留为 0,实际 %v", entry.CacheReadInputTokenCost)
	}
	if entry.OutputCostPerToken <= 0 {
		t.Errorf("远程未提供 output 时应保留嵌入值,实际 %v", entry.OutputCostPerToken)
	}
}

// TestRebuildKeepsRichEntryIntact 含扩展档位的条目整条保留本地,远程被忽略。
func TestRebuildKeepsRichEntryIntact(t *testing.T) {
	key := findEmbeddedKey(t, func(e *PricingEntry) bool { return isRichEntry(e) })
	svc, err := NewService()
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	before, _ := svc.currentSnapshot().getPricing(key)
	original := *before

	stats, err := svc.Rebuild(map[string]RemoteEntry{
		key: {Input: f64(9e-3), Output: f64(9e-3)},
	})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if stats.KeptComplex != 1 {
		t.Errorf("KeptComplex = %d, want 1 (key=%s)", stats.KeptComplex, key)
	}
	after, _ := svc.currentSnapshot().getPricing(key)
	if after.InputCostPerToken != original.InputCostPerToken ||
		after.OutputCostPerToken != original.OutputCostPerToken {
		t.Errorf("富条目基础价不应被远程覆盖: before=%v/%v after=%v/%v",
			original.InputCostPerToken, original.OutputCostPerToken,
			after.InputCostPerToken, after.OutputCostPerToken)
	}
}

// TestRebuildRejectsNormalizedConflict 远程新 key 与既有 canonical 归一化冲突时拒收。
func TestRebuildRejectsNormalizedConflict(t *testing.T) {
	key := findEmbeddedKey(t, func(e *PricingEntry) bool { return e.InputCostPerToken > 0 })
	conflicting := "ZZZ-" + key
	// 构造归一化后与 key 相同的新 key:大写化即可(normalizeName 会 lower)
	upper := ""
	for _, r := range key {
		if r >= 'a' && r <= 'z' {
			upper += string(r - 32)
		} else {
			upper += string(r)
		}
	}
	if upper == key {
		t.Skip("找不到含小写字母的 key")
	}
	_ = conflicting

	svc, err := NewService()
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	stats, err := svc.Rebuild(map[string]RemoteEntry{
		upper: {Input: f64(1e-6), Output: f64(1e-6)},
	})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if stats.DroppedConflict != 1 {
		t.Errorf("DroppedConflict = %d, want 1 (key=%s upper=%s)", stats.DroppedConflict, key, upper)
	}
	if _, exists := svc.currentSnapshot().pricingMap[upper]; exists {
		t.Errorf("冲突 key %s 不应入表", upper)
	}
}

// TestGlmFamilyFallback 同步進来的 zai/glm-* 可以被裸名 glm-* 命中(family 兜底)。
func TestGlmFamilyFallback(t *testing.T) {
	svc, err := NewService()
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if _, err := svc.Rebuild(map[string]RemoteEntry{
		"zai/glm-99-test": {Input: f64(1e-6), Output: f64(2e-6)},
	}); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	entry, ok := svc.currentSnapshot().getPricing("glm-99-test")
	if !ok || entry.InputCostPerToken != 1e-6 {
		t.Errorf("裸名 glm-99-test 应经 family 规则命中 zai/glm-99-test, ok=%v", ok)
	}
}

// TestResetToEmbedded 恢复后远程条目消失。
func TestResetToEmbedded(t *testing.T) {
	svc, err := NewService()
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if _, err := svc.Rebuild(map[string]RemoteEntry{
		"test-reset-model": {Input: f64(1e-6), Output: f64(1e-6)},
	}); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if !svc.HasPositivePricing("test-reset-model") {
		t.Fatal("重建后应能查到远程模型")
	}
	if _, err := svc.ResetToEmbedded(); err != nil {
		t.Fatalf("ResetToEmbedded: %v", err)
	}
	if svc.HasPositivePricing("test-reset-model") {
		t.Error("恢复内置后远程模型不应存在")
	}
}

// TestStableFacadeSeesRebuild 长期持有 *Service 的消费方(如 LogService)在 Rebuild 后可见新价。
func TestStableFacadeSeesRebuild(t *testing.T) {
	svc, err := NewService()
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	held := svc // 模拟构造期缓存的指针
	cost := held.CalculateCost("facade-test-model", UsageSnapshot{InputTokens: 1000})
	if cost.HasPricing {
		t.Fatal("重建前不应有价")
	}
	if _, err := svc.Rebuild(map[string]RemoteEntry{
		"facade-test-model": {Input: f64(5e-6), Output: f64(5e-6)},
	}); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	cost = held.CalculateCost("facade-test-model", UsageSnapshot{InputTokens: 1000})
	if !cost.HasPricing || cost.InputCost <= 0 {
		t.Errorf("重建后旧指针应看到新价: HasPricing=%v InputCost=%v", cost.HasPricing, cost.InputCost)
	}
}

// TestConcurrentCalculateAndRebuild 计算与重建并发安全(go test -race 下验证)。
func TestConcurrentCalculateAndRebuild(t *testing.T) {
	svc, err := NewService()
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = svc.CalculateCost("claude-opus-4-5", UsageSnapshot{InputTokens: 100, CacheReadTokens: 50})
				}
			}
		}()
	}
	for i := 0; i < 20; i++ {
		if _, err := svc.Rebuild(map[string]RemoteEntry{
			"race-test-model": {Input: f64(float64(i+1) * 1e-6), Output: f64(1e-6)},
		}); err != nil {
			t.Fatalf("Rebuild #%d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()
}

// TestConvertCatalogs 前缀规则、单位换算与无价条目过滤。
func TestConvertCatalogs(t *testing.T) {
	catalogs := map[string]*RemoteCatalog{
		"anthropic": {ID: "anthropic", Models: map[string]RemoteModel{
			"claude-test": {ID: "claude-test", Cost: &RemoteCost{Input: f64(10), Output: f64(50), CacheRead: f64(1), CacheWrite: f64(12.5)}},
		}},
		"alibaba": {ID: "alibaba", Models: map[string]RemoteModel{
			"qwen-test": {ID: "qwen-test", Cost: &RemoteCost{Input: f64(1), Output: f64(2)}},
		}},
		"google": {ID: "google", Models: map[string]RemoteModel{
			"free-preview": {ID: "free-preview", Cost: &RemoteCost{Input: f64(0), Output: f64(0)}},
			"no-cost":      {ID: "no-cost"},
		}},
	}
	entries := ConvertCatalogs(catalogs)
	claude, ok := entries["claude-test"]
	if !ok || *claude.Input != 10e-6 || *claude.CacheWrite != 12.5e-6 {
		t.Errorf("anthropic 裸名转换错误: %+v", claude)
	}
	if _, ok := entries["dashscope/qwen-test"]; !ok {
		t.Error("alibaba 应加 dashscope/ 前缀")
	}
	if _, ok := entries["free-preview"]; ok {
		t.Error("input/output 均非正的条目不应产出价格")
	}
	if _, ok := entries["no-cost"]; ok {
		t.Error("无 cost 的条目不应产出价格")
	}
}

// TestParseRemoteCatalogValidation 回显校验与非法条目清洗。
func TestParseRemoteCatalogValidation(t *testing.T) {
	if _, err := ParseRemoteCatalog("openai", []byte(`{"id":"google","models":{"m":{}}}`)); err == nil {
		t.Error("id 回显不符应报错")
	}
	catalog, err := ParseRemoteCatalog("openai", []byte(`{
		"id":"openai",
		"models":{
			"good-model":{"id":"good-model","cost":{"input":1,"output":2}},
			"bad price":{"id":"bad price","cost":{"input":1,"output":2}},
			"too-expensive":{"id":"too-expensive","cost":{"input":99999,"output":1}}
		}
	}`))
	if err != nil {
		t.Fatalf("ParseRemoteCatalog: %v", err)
	}
	if _, ok := catalog.Models["good-model"]; !ok {
		t.Error("合法条目应保留")
	}
	if _, ok := catalog.Models["bad price"]; ok {
		t.Error("含空格的非法 id 应被清洗")
	}
	if _, ok := catalog.Models["too-expensive"]; ok {
		t.Error("超价上限条目应被清洗")
	}
	if catalog.DroppedModels != 2 {
		t.Errorf("DroppedModels = %d, want 2", catalog.DroppedModels)
	}
}

// TestEmbeddedSeedCatalogs 内置种子完整可用,且离线首启即可为静态兜底模型供价。
func TestEmbeddedSeedCatalogs(t *testing.T) {
	seed := EmbeddedSeedCatalogs()
	for _, id := range RemoteProviderIDs {
		if seed[id] == nil || len(seed[id].Models) == 0 {
			t.Errorf("内置种子缺失厂商 %s", id)
		}
	}
	svc, err := NewService()
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if _, err := svc.Rebuild(ConvertCatalogs(seed)); err != nil {
		t.Fatalf("Rebuild(seed): %v", err)
	}
	for _, model := range []string{"gpt-5.6", "gemini-3.5-flash", "claude-haiku-4-5-20251001", "claude-opus-5", "gemini-3.1-pro-preview", "claude-fable-5"} {
		if !svc.HasPositivePricing(model) {
			t.Errorf("种子重建后静态兜底/最新模型 %s 应有价", model)
		}
	}
}
