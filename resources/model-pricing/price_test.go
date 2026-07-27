package modelpricing

import (
	"math"
	"sync"
	"testing"
)

// assertApprox 容忍 0.1% 相对误差(或 1e-15 绝对下限),用于比例外推等浮点链式断言。
// 精确相等场景应直接用 ==,不要用这个 helper。
func assertApprox(t *testing.T, got, want float64) {
	t.Helper()
	const rel = 1e-3
	const abs = 1e-15
	diff := math.Abs(got - want)
	limit := math.Abs(want) * rel
	if limit < abs {
		limit = abs
	}
	if diff > limit {
		t.Fatalf("got=%g want=%g diff=%g limit=%g", got, want, diff, limit)
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	svc, err := NewService()
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	return svc
}

// TestSampleSpecSkipped 确保 JSON 里的 sample_spec 文档条目不污染 pricingMap。
func TestSampleSpecSkipped(t *testing.T) {
	svc := newTestService(t)
	if _, ok := svc.currentSnapshot().pricingMap["sample_spec"]; ok {
		t.Fatal("sample_spec 应该被跳过,当前仍在 pricingMap 中")
	}
}

func TestClosedModelKeysFiltered(t *testing.T) {
	svc := newTestService(t)
	cases := []string{
		"chatgpt-4o-latest",
		"codex-mini-latest",
		"gpt-4-0125-preview",
		"gpt-4o-realtime-preview-2025-06-03",
		"text-moderation-007",
		"gemini-3-pro-preview",
		"gemini/gemini-3.1-flash-lite-preview",
		"vertex_ai/imagen-3.0-generate-002",
		"claude-sonnet-4-20250514",
		"us.anthropic.claude-sonnet-4-20250514-v1:0",
		"anthropic.claude-3-5-haiku-20241022-v1:0",
	}
	for _, model := range cases {
		if _, ok := svc.currentSnapshot().pricingMap[model]; ok {
			t.Errorf("%q 不应进入 pricingMap", model)
		}
		if entry, ok := svc.currentSnapshot().getPricing(model); ok && entry != nil {
			t.Errorf("%q 不应通过候选匹配拿到定价", model)
		}
	}
}

// TestOverlayAliases 检验 overlay 映射的裸名能查到价格。
func TestOverlayAliases(t *testing.T) {
	svc := newTestService(t)
	cases := []string{
		"qwen-max", "qwen-plus", "qwen-turbo", "qwen-coder", "qwen-flash",
		"qwen3-coder-flash", "qwen3-coder-plus",
		"kimi-latest", "kimi-k2-0711-preview",
		"glm-4.5", "glm-4.5-air", "glm-4.6",
	}
	for _, m := range cases {
		entry, ok := svc.currentSnapshot().getPricing(m)
		if !ok || entry == nil {
			t.Errorf("overlay 别名 %q 应该有定价", m)
		}
	}
}

// TestFamilyFallback 检验 family fallback 规则按前缀命中 vendor 版。
func TestFamilyFallback(t *testing.T) {
	svc := newTestService(t)
	cases := map[string]string{
		// qwen- 前缀没有 dashscope/ 对应项时,family fallback 也能尝试拼接命中
		"qwen-plus-latest":      "dashscope/qwen-plus-latest",
		"qwen3-max-preview":     "dashscope/qwen3-max-preview",
		"kimi-thinking-preview": "moonshot/kimi-thinking-preview",
	}
	for input, expectedKey := range cases {
		entry, ok := svc.currentSnapshot().getPricing(input)
		if !ok || entry == nil {
			t.Errorf("family fallback 应该为 %q 命中 %q", input, expectedKey)
		}
		// 验证 expectedKey 在 pricingMap 中存在(前提校验)
		if _, exists := svc.currentSnapshot().pricingMap[expectedKey]; !exists {
			t.Errorf("前提失败:%q 不在 pricingMap,测试用例需更新", expectedKey)
		}
	}
}

// TestSubstringFallbackRemoved 确保删除了无序 substring fallback 后,
// 明显不合法的模型名不会被意外命中。
func TestSubstringFallbackRemoved(t *testing.T) {
	svc := newTestService(t)
	// "grok" 裸名不应该命中任何 xai/grok-*(之前的 substring 会随机命中一个)
	if entry, ok := svc.currentSnapshot().getPricing("grok"); ok && entry != nil {
		t.Errorf("裸名 'grok' 不应该命中任何条目(意味着 substring fallback 未删除)")
	}
	if entry, ok := svc.currentSnapshot().getPricing("totally-nonexistent-model-xyz"); ok && entry != nil {
		t.Errorf("不存在的模型不应该命中,得到:%v", entry)
	}
}

// TestExactAndAliasCandidates 检验基础的 gpt-5-codex→gpt-5 与 region 去前缀仍生效。
func TestExactAndAliasCandidates(t *testing.T) {
	svc := newTestService(t)

	// gpt-5 本身必须存在
	if _, ok := svc.currentSnapshot().getPricing("gpt-5"); !ok {
		t.Fatal("前提失败:gpt-5 应存在于 pricingMap")
	}
	// gpt-5-codex 应该通过 alias 候选命中 gpt-5(如果 pricingMap 里没有直接定义)
	if _, ok := svc.currentSnapshot().getPricing("gpt-5-codex"); !ok {
		t.Error("gpt-5-codex 应该通过 alias 回退到 gpt-5")
	}

	// anthropic 前缀去除
	if _, ok := svc.currentSnapshot().pricingMap["anthropic.claude-sonnet-4-5-20250929-v1:0"]; ok {
		// 已经带前缀,测试去掉 us. 前缀应命中
		if _, ok := svc.currentSnapshot().getPricing("us.anthropic.claude-sonnet-4-5-20250929-v1:0"); !ok {
			t.Error("region 前缀去除应命中 anthropic.claude-sonnet-4-5-20250929-v1:0")
		}
	}
}

// TestTieredPricing 检验 tiered_pricing 分段价生效。
// 用 dashscope/qwen-flash 做样本(它有 2 段:[0,256k] / [256k,1M])。
func TestTieredPricing(t *testing.T) {
	svc := newTestService(t)

	entry, ok := svc.currentSnapshot().pricingMap["dashscope/qwen-flash"]
	if !ok {
		t.Skip("dashscope/qwen-flash 不在当前 JSON 中,跳过")
	}
	if len(entry.TieredPricing) < 2 {
		t.Fatalf("期望 dashscope/qwen-flash 有 >=2 个 tier,实际 %d", len(entry.TieredPricing))
	}

	// 低档位:10k prompt tokens 应落在 [0, 256k] band
	low := svc.CalculateCost("dashscope/qwen-flash", UsageSnapshot{
		InputTokens:  10000,
		OutputTokens: 1000,
	})
	if !low.IsTiered {
		t.Error("低档应标记 IsTiered=true")
	}
	if !low.HasPricing {
		t.Error("低档应有定价")
	}

	// 高档位:500k prompt tokens 应落在 [256k, 1M] band
	high := svc.CalculateCost("dashscope/qwen-flash", UsageSnapshot{
		InputTokens:  500000,
		OutputTokens: 1000,
	})
	if !high.IsTiered {
		t.Error("高档应标记 IsTiered=true")
	}

	// 高档输入单价应严格大于低档(qwen-flash 256k+ 贵 5 倍)
	lowRate := low.InputCost / 10000
	highRate := high.InputCost / 500000
	if highRate <= lowRate {
		t.Errorf("tiered_pricing 高档单价 %.9f 应该 > 低档 %.9f", highRate, lowRate)
	}
}

// TestAbove200kPricing 验证长上下文 above_200k 字段在超阈值时被消费。
func TestAbove200kPricing(t *testing.T) {
	svc := newTestService(t)

	// 找一个带 above_200k 的 anthropic 模型
	var target string
	for k, v := range svc.currentSnapshot().pricingMap {
		if v.InputCostPerTokenAbove200k > 0 && v.InputCostPerToken > 0 &&
			v.InputCostPerTokenAbove200k > v.InputCostPerToken {
			target = k
			break
		}
	}
	if target == "" {
		t.Skip("没有找到带 above_200k 字段的模型,跳过")
	}

	short := svc.CalculateCost(target, UsageSnapshot{InputTokens: 50000, OutputTokens: 1000})
	long := svc.CalculateCost(target, UsageSnapshot{InputTokens: 250000, OutputTokens: 1000})

	shortRate := short.InputCost / 50000
	longRate := long.InputCost / 250000

	if longRate <= shortRate {
		t.Errorf("超 200k 单价 %.9f 应该 > 短上下文 %.9f (model=%s)", longRate, shortRate, target)
	}
	if !long.IsLongContext {
		t.Errorf("超 200k 应标记 IsLongContext=true (model=%s)", target)
	}
}

// TestCalculateCostBasic 基础 token 计费(无 tiered/无 above 阈值)。
func TestCalculateCostBasic(t *testing.T) {
	svc := newTestService(t)
	cost := svc.CalculateCost("gpt-5", UsageSnapshot{
		InputTokens:  1000,
		OutputTokens: 500,
	})
	if !cost.HasPricing {
		t.Error("gpt-5 应有定价")
	}
	if cost.TotalCost <= 0 {
		t.Errorf("gpt-5 TotalCost 应 >0,实际 %f", cost.TotalCost)
	}
}

// TestUnknownModelNoPricing 未知模型不应强行给价。
func TestUnknownModelNoPricing(t *testing.T) {
	svc := newTestService(t)
	cost := svc.CalculateCost("this-model-definitely-does-not-exist-xyz-123", UsageSnapshot{
		InputTokens:  1000,
		OutputTokens: 500,
	})
	if cost.HasPricing {
		t.Error("未知模型不应有定价")
	}
	if cost.TotalCost != 0 {
		t.Errorf("未知模型 TotalCost 应为 0,实际 %f", cost.TotalCost)
	}
}

// TestFamilyFallbackOrder 确保 qwen3- 优先于 qwen- 匹配(顺序依赖)。
func TestFamilyFallbackOrder(t *testing.T) {
	cands := familyFallbackCandidates("qwen3-coder-new-version")
	if len(cands) == 0 {
		t.Fatal("qwen3- 应命中 family 规则")
	}
	if cands[0] != "dashscope/qwen3-coder-new-version" {
		t.Errorf("首选应是 dashscope/qwen3-coder-new-version,实际 %v", cands)
	}
}

// TestCandidatesDeduplication 确保候选列表去重。
func TestCandidatesDeduplication(t *testing.T) {
	// 没有 region/anthropic 前缀的模型应只产生 1 个候选
	c := buildCandidates("gpt-4")
	if len(c) != 1 {
		t.Errorf("gpt-4 期望 1 个候选,实际 %d: %v", len(c), c)
	}
}

// TestGpt5CodexAliasCandidate 直接验证 gpt-5-codex 的候选列表含 gpt-5,
// 即便 pricingMap 里 gpt-5-codex 本身存在,别名链条依然生效。
func TestGpt5CodexAliasCandidate(t *testing.T) {
	cands := buildCandidates("gpt-5-codex")
	found := false
	for _, c := range cands {
		if c == "gpt-5" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("gpt-5-codex 候选应包含 gpt-5,实际 %v", cands)
	}
}

// TestTierBoundaryExact 锁定 [lo, hi) 语义:range 上界值本身归入下一档。
func TestTierBoundaryExact(t *testing.T) {
	bands := []TieredPricingBand{
		{Range: [2]float64{0, 256000}, InputCostPerToken: 1e-7, OutputCostPerToken: 2e-7},
		{Range: [2]float64{256000, 1000000}, InputCostPerToken: 5e-7, OutputCostPerToken: 1e-6},
	}
	// 255999 应命中低档
	if pickTier(bands, 255999).InputCostPerToken != 1e-7 {
		t.Error("255999 应落在低档 [0, 256000)")
	}
	// 256000 恰好等于上界,应归入高档
	if pickTier(bands, 256000).InputCostPerToken != 5e-7 {
		t.Error("256000 应归入高档 [256000, 1000000)")
	}
	// 超过最大 band:返回最后一段
	if pickTier(bands, 2000000).InputCostPerToken != 5e-7 {
		t.Error("超过最大 band 应返回最后一段")
	}
}

// TestAbove272kPricing 验证 272k 档位(GPT-5.x 系列)。
func TestAbove272kPricing(t *testing.T) {
	svc := newTestService(t)

	var target string
	for k, v := range svc.currentSnapshot().pricingMap {
		if v.InputCostPerTokenAbove272k > 0 && v.InputCostPerToken > 0 &&
			v.InputCostPerTokenAbove272k > v.InputCostPerToken {
			target = k
			break
		}
	}
	if target == "" {
		t.Skip("当前 JSON 没有 above_272k 模型")
	}

	short := svc.CalculateCost(target, UsageSnapshot{InputTokens: 100000, OutputTokens: 1000})
	long := svc.CalculateCost(target, UsageSnapshot{InputTokens: 300000, OutputTokens: 1000})

	if !long.IsLongContext {
		t.Errorf("300k prompt 应标记 IsLongContext (model=%s)", target)
	}
	if long.InputCost/300000 <= short.InputCost/100000 {
		t.Errorf("272k+ 单价应 > 基础价 (model=%s)", target)
	}
}

// TestAbove200kCacheTokenRates 验证超 200k 时 cache_read/cache_create 切到对应档位单价。
// 样本:anthropic.claude-3-5-sonnet-20240620-v1:0
//
//	cache_read_input_token_cost = 3e-07
//	cache_read_input_token_cost_above_200k_tokens = 6e-07
func TestAbove200kCacheTokenRates(t *testing.T) {
	svc := newTestService(t)
	target := "anthropic.claude-3-5-sonnet-20240620-v1:0"
	entry, ok := svc.currentSnapshot().pricingMap[target]
	if !ok {
		t.Skip(target + " 不在 JSON 中")
	}
	if entry.CacheReadInputTokenCostAbove200k == 0 {
		t.Skip(target + " 未携带 cache_read_above_200k 字段")
	}

	// 纯 input 超 200k + 一些 cache_read
	res := svc.CalculateCost(target, UsageSnapshot{
		InputTokens:     250000,
		OutputTokens:    1000,
		CacheReadTokens: 10000,
	})
	if !res.IsLongContext {
		t.Fatal("期望 IsLongContext=true")
	}
	expected := 10000 * entry.CacheReadInputTokenCostAbove200k
	if res.CacheReadCost < expected*0.999 || res.CacheReadCost > expected*1.001 {
		t.Errorf("CacheReadCost 期望 ~%f(使用 above_200k 单价),实际 %f",
			expected, res.CacheReadCost)
	}
}

// TestCache1hFromJSONFirst 验证 1h cache_create 价优先吃 JSON 的
// cache_creation_input_token_cost_above_1hr 字段,而不是硬编码的 opus/sonnet/haiku 映射。
// 样本:claude-3-haiku-20240307 JSON 里 above_1hr = 6e-06,
// 硬编码给 haiku 1.6e-06(相差 ~4 倍)。
func TestCache1hFromJSONFirst(t *testing.T) {
	svc := newTestService(t)
	target := "claude-3-haiku-20240307"
	entry, ok := svc.currentSnapshot().pricingMap[target]
	if !ok {
		t.Skip(target + " 不在 JSON 中")
	}
	if entry.CacheCreationInputTokenCostAbove1Hr == 0 {
		t.Skip(target + " 无 above_1hr 字段")
	}

	res := svc.CalculateCost(target, UsageSnapshot{
		InputTokens:  1000,
		OutputTokens: 100,
		CacheCreation: &CacheCreationDetail{
			Ephemeral1hTokens: 10000,
		},
		CacheCreateTokens: 10000,
	})
	// 期望:1h 价应该用 JSON 的 6e-06,不是硬编码的 1.6e-06
	jsonRate := entry.CacheCreationInputTokenCostAbove1Hr
	expected := 10000 * jsonRate
	if res.Ephemeral1hCost < expected*0.999 || res.Ephemeral1hCost > expected*1.001 {
		t.Errorf("Ephemeral1hCost 期望 ~%f(使用 JSON above_1hr %f),实际 %f",
			expected, jsonRate, res.Ephemeral1hCost)
	}
}

// TestOverlayMissingTargetFailFast 验证 overlay 里 target 不存在时启动失败。
// 用替换 overlayFile 的方式模拟错误配置(恢复原值避免影响其他测试)。
func TestOverlayMissingTargetFailFast(t *testing.T) {
	original := overlayFile
	defer func() { overlayFile = original }()

	overlayFile = []byte(`{"aliases": {"fake-model": "this-target-does-not-exist-xyz"}}`)
	// 需要绕过 sync.Once,直接调用 NewService
	if _, err := NewService(); err == nil {
		t.Error("overlay 映射到不存在的 target,NewService 应返回 error")
	}
}

// TestCacheHitFallback 验证 DeepSeek 等使用 cache_hit 字段的模型,
// ensureCachePricing 会把它当作 cache_read 价,不再掉到 0.1x 兜底。
func TestCacheHitFallback(t *testing.T) {
	svc := newTestService(t)
	entry, ok := svc.currentSnapshot().pricingMap["deepseek/deepseek-r1"]
	if !ok {
		t.Skip("deepseek/deepseek-r1 不在 JSON 中")
	}
	if entry.InputCostPerTokenCacheHit == 0 {
		t.Skip("deepseek/deepseek-r1 无 cache_hit 字段")
	}
	if entry.CacheReadInputTokenCost != entry.InputCostPerTokenCacheHit {
		t.Errorf("期望 CacheReadInputTokenCost=%g(来自 cache_hit),实际 %g",
			entry.InputCostPerTokenCacheHit, entry.CacheReadInputTokenCost)
	}
}

// TestPriorityServiceTier 验证 UsageSnapshot.ServiceTier=priority 时使用 *_priority 字段。
func TestPriorityServiceTier(t *testing.T) {
	svc := newTestService(t)

	var target string
	for k, v := range svc.currentSnapshot().pricingMap {
		if v.InputCostPerTokenPriority > 0 && v.InputCostPerTokenPriority > v.InputCostPerToken {
			target = k
			break
		}
	}
	if target == "" {
		t.Skip("当前 JSON 没有 priority 字段模型")
	}

	defaultCost := svc.CalculateCost(target, UsageSnapshot{InputTokens: 1000, OutputTokens: 100})
	priorityCost := svc.CalculateCost(target, UsageSnapshot{
		InputTokens:  1000,
		OutputTokens: 100,
		ServiceTier:  ServiceTierPriority,
	})

	if priorityCost.InputCost <= defaultCost.InputCost {
		t.Errorf("priority tier 单价应 > default (model=%s): priority=%g default=%g",
			target, priorityCost.InputCost, defaultCost.InputCost)
	}
}

// TestPriorityLongContextDoesNotInventTierPrice 验证:模型有 priority 基础字段但缺对应
// above_Xk_priority 时,长上下文不应把短上下文 priority 基础价当作组合价。
func TestPriorityLongContextDoesNotInventTierPrice(t *testing.T) {
	// 构造一个合成 entry:有 output base/priority,有 above_272k default,无 above_272k priority
	synthetic := &PricingEntry{
		InputCostPerToken:           2.5e-6,
		InputCostPerTokenPriority:   5e-6,
		OutputCostPerToken:          1.5e-5,
		OutputCostPerTokenPriority:  3e-5,
		InputCostPerTokenAbove272k:  5e-6,
		OutputCostPerTokenAbove272k: 2.25e-5, // default 长上下文 < priority 基础
	}
	band := synthetic.resolveLongContextBand(300000, ServiceTierPriority)
	if !band.active {
		t.Fatal("应该命中 >272k band")
	}
	if band.inputPerTok != synthetic.InputCostPerTokenAbove272k {
		t.Errorf("缺少 priority 长上下文字段时,input 应用 default 长上下文价 %g,实际 %g",
			synthetic.InputCostPerTokenAbove272k, band.inputPerTok)
	}
	if band.outputPerTok != synthetic.OutputCostPerTokenAbove272k {
		t.Errorf("缺少 priority 长上下文字段时,output 应用 default 长上下文价 %g,实际 %g",
			synthetic.OutputCostPerTokenAbove272k, band.outputPerTok)
	}

	// 对比 default 请求仍吃 above_272k default
	bandDef := synthetic.resolveLongContextBand(300000, ServiceTierDefault)
	if bandDef.outputPerTok != synthetic.OutputCostPerTokenAbove272k {
		t.Errorf("default+>272k 输出价应 = above_272k default,实际 %g", bandDef.outputPerTok)
	}
}

// TestLongContextTierStrictMatch 验证精确匹配 only,不再无序 fallback 到任意 tier。
func TestLongContextTierStrictMatch(t *testing.T) {
	svc := newTestService(t)
	cost := svc.CalculateCost("unknown-model-xyz[1m]", UsageSnapshot{InputTokens: 250000})
	if cost.IsLongContext {
		t.Error("未注册的 [1m] 模型不应命中 longContextTier")
	}
}

// TestCacheCreationSplit 验证 Ephemeral5m/1h 拆分时 1h 价生效。
func TestCacheCreationSplit(t *testing.T) {
	svc := newTestService(t)
	target := "claude-3-haiku-20240307"
	entry, ok := svc.currentSnapshot().pricingMap[target]
	if !ok {
		t.Skip(target + " 不在 JSON 中")
	}
	if entry.CacheCreationInputTokenCostAbove1Hr == 0 {
		t.Skip(target + " 无 above_1hr 字段")
	}

	// 总 cache create 15000 token = 5000 5m + 10000 1h
	res := svc.CalculateCost(target, UsageSnapshot{
		InputTokens:       1000,
		OutputTokens:      100,
		CacheCreateTokens: 15000,
		CacheCreation: &CacheCreationDetail{
			Ephemeral5mTokens: 5000,
			Ephemeral1hTokens: 10000,
		},
	})
	expected5m := 5000 * entry.CacheCreationInputTokenCost
	expected1h := 10000 * entry.CacheCreationInputTokenCostAbove1Hr
	if res.Ephemeral5mCost < expected5m*0.999 || res.Ephemeral5mCost > expected5m*1.001 {
		t.Errorf("Ephemeral5mCost 期望 ~%f,实际 %f", expected5m, res.Ephemeral5mCost)
	}
	if res.Ephemeral1hCost < expected1h*0.999 || res.Ephemeral1hCost > expected1h*1.001 {
		t.Errorf("Ephemeral1hCost 期望 ~%f,实际 %f", expected1h, res.Ephemeral1hCost)
	}
}

// TestFlexServiceTier 验证 ServiceTierFlex 时基础档 input/output/cache_read 切到 *_flex 字段。
// 动态扫 pricingMap 找首个同时具备 3 个 *_flex 字段的模型,不硬编码模型名。
func TestFlexServiceTier(t *testing.T) {
	svc := newTestService(t)
	var model string
	var entry *PricingEntry
	for k, v := range svc.currentSnapshot().pricingMap {
		if v.InputCostPerTokenFlex > 0 && v.OutputCostPerTokenFlex > 0 && v.CacheReadInputTokenCostFlex > 0 {
			model = k
			entry = v
			break
		}
	}
	if model == "" {
		t.Skip("JSON 中无同时带 3 个 *_flex 字段的模型")
	}

	usage := UsageSnapshot{
		InputTokens:     1000,
		OutputTokens:    100,
		CacheReadTokens: 200,
	}
	flexUsage := usage
	flexUsage.ServiceTier = ServiceTierFlex

	flexCost := svc.CalculateCost(model, flexUsage)
	defaultCost := svc.CalculateCost(model, usage)

	assertApprox(t, flexCost.InputCost, float64(usage.InputTokens)*entry.InputCostPerTokenFlex)
	assertApprox(t, flexCost.OutputCost, float64(usage.OutputTokens)*entry.OutputCostPerTokenFlex)
	assertApprox(t, flexCost.CacheReadCost, float64(usage.CacheReadTokens)*entry.CacheReadInputTokenCostFlex)

	if flexCost.TotalCost >= defaultCost.TotalCost {
		t.Fatalf("flex total=%g 应小于 default total=%g", flexCost.TotalCost, defaultCost.TotalCost)
	}
}

// TestStandardServiceTierAliasesDefault 验证 standard 和 observed-default 都 alias 到 default,
// 与空值 ServiceTierDefault 计费完全相等(== 精确比,不用 approx)。
// 样本挑一个带 *_flex 字段的模型,确认 alias 路径不会误走 flex 分支。
func TestStandardServiceTierAliasesDefault(t *testing.T) {
	svc := newTestService(t)
	var model string
	for k, v := range svc.currentSnapshot().pricingMap {
		if v.InputCostPerTokenFlex > 0 && v.InputCostPerToken > 0 {
			model = k
			break
		}
	}
	if model == "" {
		t.Skip("JSON 中无带 *_flex 字段的模型可做 alias 区分测试")
	}
	baseUsage := UsageSnapshot{
		InputTokens:     1000,
		OutputTokens:    100,
		CacheReadTokens: 200,
	}
	defaultCost := svc.CalculateCost(model, baseUsage)

	for _, tier := range []ServiceTier{ServiceTierStandard, ServiceTierObservedDefault} {
		u := baseUsage
		u.ServiceTier = tier
		got := svc.CalculateCost(model, u)
		if got.InputCost != defaultCost.InputCost ||
			got.OutputCost != defaultCost.OutputCost ||
			got.CacheReadCost != defaultCost.CacheReadCost ||
			got.TotalCost != defaultCost.TotalCost {
			t.Fatalf("tier=%q 应与 default 完全相等: got=%+v default=%+v", tier, got, defaultCost)
		}
	}
}

// TestFlexLongContextScalesFromDefaultLongBand 验证 300k prompt + flex tier 时,
// 长窗口 band 按短窗口 flex/default 比例外推(JSON 无 *_flex_above_* 字段,走 scaleLongRate 路径)。
// 使用合成 PricingEntry,不依赖 JSON 样本,公式独立可验证。
func TestFlexLongContextScalesFromDefaultLongBand(t *testing.T) {
	entry := &PricingEntry{
		InputCostPerToken:                2.5e-06,
		InputCostPerTokenFlex:            1.25e-06,
		InputCostPerTokenAbove272k:       5e-06,
		OutputCostPerToken:               1.5e-05,
		OutputCostPerTokenFlex:           7.5e-06,
		OutputCostPerTokenAbove272k:      2.25e-05,
		CacheReadInputTokenCost:          2.5e-07,
		CacheReadInputTokenCostFlex:      1.3e-07,
		CacheReadInputTokenCostAbove272k: 5e-07,
	}

	band := entry.resolveLongContextBand(300000, ServiceTierFlex)
	if !band.active {
		t.Fatal("300k prompt 应命中 >272k band")
	}

	// 预期:长窗默认价 × (flex基础 / default基础)
	expectedInput := 5e-06 * (1.25e-06 / 2.5e-06)    // = 2.5e-06
	expectedOutput := 2.25e-05 * (7.5e-06 / 1.5e-05) // = 1.125e-05
	expectedCacheRead := 5e-07 * (1.3e-07 / 2.5e-07) // = 2.6e-07

	assertApprox(t, band.inputPerTok, expectedInput)
	assertApprox(t, band.outputPerTok, expectedOutput)
	assertApprox(t, band.cacheRead, expectedCacheRead)

	// 比例外推后的 flex 长窗 input 应严格低于 default 长窗
	if band.inputPerTok >= entry.InputCostPerTokenAbove272k {
		t.Errorf("flex 长窗 input %g 应低于 default above_272k %g",
			band.inputPerTok, entry.InputCostPerTokenAbove272k)
	}

	// default tier 长窗不受 flex 影响
	bandDef := entry.resolveLongContextBand(300000, ServiceTierDefault)
	if bandDef.inputPerTok != entry.InputCostPerTokenAbove272k {
		t.Errorf("default 长窗 input 应 = above_272k default %g,实际 %g",
			entry.InputCostPerTokenAbove272k, bandDef.inputPerTok)
	}
}

func TestGpt54FlexLongContextUsesOfficialCacheReadRate(t *testing.T) {
	svc := newTestService(t)
	entry, ok := svc.currentSnapshot().pricingMap["gpt-5.4"]
	if !ok {
		t.Fatal("gpt-5.4 应存在于 pricingMap")
	}

	res := svc.CalculateCost("gpt-5.4", UsageSnapshot{
		InputTokens:     300000,
		OutputTokens:    1000,
		CacheReadTokens: 10000,
		ServiceTier:     ServiceTierFlex,
	})
	if !res.IsLongContext {
		t.Fatal("gpt-5.4 300k prompt 应命中长上下文价")
	}
	assertApprox(t, res.CacheReadCost, float64(10000)*2.5e-7)
	assertApprox(t, res.InputCost, float64(300000)*2.5e-6)
	assertApprox(t, res.OutputCost, float64(1000)*1.125e-5)

	if entry.CacheReadInputTokenCostFlex == 0 {
		t.Fatal("前提失败:gpt-5.4 应保留短上下文 flex cache_read 价")
	}
}

func TestGpt55ProPricingExists(t *testing.T) {
	svc := newTestService(t)
	cost := svc.CalculateCost("gpt-5.5-pro", UsageSnapshot{
		InputTokens:  300000,
		OutputTokens: 1000,
	})
	if !cost.HasPricing {
		t.Fatal("gpt-5.5-pro 应有定价")
	}
	if !cost.IsLongContext {
		t.Fatal("gpt-5.5-pro 300k prompt 应命中长上下文价")
	}
	assertApprox(t, cost.InputCost, float64(300000)*6e-5)
	assertApprox(t, cost.OutputCost, float64(1000)*2.7e-4)
}

func TestGpt55ProFlexDoesNotInventLongContextPrice(t *testing.T) {
	svc := newTestService(t)
	short := svc.CalculateCost("gpt-5.5-pro", UsageSnapshot{
		InputTokens:  1000,
		OutputTokens: 100,
		ServiceTier:  ServiceTierFlex,
	})
	if !short.HasPricing {
		t.Fatal("gpt-5.5-pro flex 短上下文应有定价")
	}
	assertApprox(t, short.InputCost, float64(1000)*1.5e-5)
	assertApprox(t, short.OutputCost, float64(100)*9e-5)

	long := svc.CalculateCost("gpt-5.5-pro", UsageSnapshot{
		InputTokens:  300000,
		OutputTokens: 1000,
		ServiceTier:  ServiceTierFlex,
	})
	if !long.IsLongContext {
		t.Fatal("gpt-5.5-pro 300k prompt 应命中长上下文价")
	}
	assertApprox(t, long.InputCost, float64(300000)*6e-5)
	assertApprox(t, long.OutputCost, float64(1000)*2.7e-4)
}

func TestProModelsDoNotSynthesizeCacheReadPricing(t *testing.T) {
	svc := newTestService(t)
	for _, model := range []string{
		"gpt-5-pro",
		"gpt-5-pro-2025-10-06",
		"gpt-5.2-pro",
		"gpt-5.2-pro-2025-12-11",
		"gpt-5.4-pro",
		"gpt-5.4-pro-2026-03-05",
	} {
		cost := svc.CalculateCost(model, UsageSnapshot{
			InputTokens:     1000,
			OutputTokens:    100,
			CacheReadTokens: 1000,
		})
		if !cost.HasPricing {
			t.Fatalf("%s 应有基础定价", model)
		}
		if cost.CacheReadCost != 0 {
			t.Fatalf("%s 不应合成 cache_read 价格,实际 %g", model, cost.CacheReadCost)
		}
	}
}

// TestUnknownTierWarningOnce 验证 NormalizeObservedServiceTier:
//   - 同一未知值多次调用只触发 onUnknown 一次
//   - 不同未知值分别触发一次
//   - 已知值(priority/flex/standard/default/空)不触发
//   - 未知值原样返回 lower 后的字符串(保留 raw 审计)
func TestUnknownTierWarningOnce(t *testing.T) {
	var seen sync.Map
	var warned []string
	onUnknown := func(tier string) {
		if _, loaded := seen.LoadOrStore(tier, struct{}{}); loaded {
			return
		}
		warned = append(warned, tier)
	}

	cases := []struct {
		raw  string
		want ServiceTier
	}{
		{" economy ", ServiceTier("economy")},
		{"ECONOMY", ServiceTier("economy")},
		{"turbo", ServiceTier("turbo")},
		{"priority", ServiceTierPriority},
		{"flex", ServiceTierFlex},
		{"standard", ServiceTierStandard},
		{"default", ServiceTierObservedDefault},
		{"", ServiceTierDefault},
	}
	for _, c := range cases {
		if got := NormalizeObservedServiceTier(c.raw, onUnknown); got != c.want {
			t.Errorf("NormalizeObservedServiceTier(%q) = %q, want %q", c.raw, got, c.want)
		}
	}

	if len(warned) != 2 {
		t.Fatalf("len(warned)=%d, want 2; warned=%v", len(warned), warned)
	}
	if warned[0] != "economy" {
		t.Errorf("warned[0]=%q, want \"economy\"", warned[0])
	}
	if warned[1] != "turbo" {
		t.Errorf("warned[1]=%q, want \"turbo\"", warned[1])
	}
}
