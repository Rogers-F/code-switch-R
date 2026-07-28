package modelpricing

import "testing"

// TestOneMillionSuffixUsesBaseEntry 回归:带 "[1m]" 后缀的 1M 长上下文模型
// 应回退基础条目计价,超 200k 用量自动走 above_200k 档,不再整笔计 0。
func TestOneMillionSuffixUsesBaseEntry(t *testing.T) {
	svc := newTestService(t)
	base := "claude-sonnet-4-5-20250929"
	entry, ok := svc.currentSnapshot().pricingMap[base]
	if !ok {
		t.Skip(base + " 不在 JSON 中")
	}
	if entry.InputCostPerTokenAbove200k == 0 {
		t.Skip(base + " 无 above_200k 字段,样本需更新")
	}

	usage := UsageSnapshot{InputTokens: 300000, OutputTokens: 2000}
	withSuffix := svc.CalculateCost(base+"[1m]", usage)
	plain := svc.CalculateCost(base, usage)

	if !withSuffix.HasPricing {
		t.Fatal("[1m] 模型应回退基础条目并有定价")
	}
	if !withSuffix.IsLongContext {
		t.Error("300k prompt 应命中长上下文档")
	}
	if withSuffix.TotalCost != plain.TotalCost {
		t.Errorf("[1m] 与基础名同用量费用应一致: %g != %g", withSuffix.TotalCost, plain.TotalCost)
	}
}

// TestStrip1MSuffix 覆盖 "[1m]" 剥离的边界:大小写、无后缀原样返回。
func TestStrip1MSuffix(t *testing.T) {
	cases := map[string]string{
		"claude-sonnet-4-5-20250929[1m]": "claude-sonnet-4-5-20250929",
		"claude-sonnet-4-5[1M]":          "claude-sonnet-4-5",
		"gpt-5":                          "gpt-5",
	}
	for input, want := range cases {
		if got := strip1MSuffix(input); got != want {
			t.Errorf("strip1MSuffix(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestReasoningFallbackToOutputRate 回归:条目缺 reasoning 单价时,推理 token 一律按
// output 单价回退计费。ReasoningTokens 与 OutputTokens 在入库前已被拆成互不重叠的两桶
// (Gemini 的 thoughtsTokenCount 本就独立上报;OpenAI Responses 的 reasoning_tokens 在
// CodexParseTokenUsageFromResponse 里已从 output_tokens 扣除),所以回退不会重复计费;
// 不回退才会让这部分 token 全部 0 计费——目前只有 gemini 与 qwen 系条目带 reasoning 单价。
func TestReasoningFallbackToOutputRate(t *testing.T) {
	svc := newTestService(t)
	entry, ok := svc.currentSnapshot().pricingMap["gemini-2.5-pro"]
	if !ok {
		t.Skip("gemini-2.5-pro 不在 JSON 中")
	}
	if entry.OutputCostPerReasoningToken > 0 {
		t.Skip("gemini-2.5-pro 已带 reasoning 单价,样本需更新")
	}

	res := svc.CalculateCost("gemini-2.5-pro", UsageSnapshot{
		InputTokens:     1000,
		OutputTokens:    500,
		ReasoningTokens: 2000,
	})
	assertApprox(t, res.ReasoningCost, 2000*entry.OutputCostPerToken)

	// 超 200k 时回退价应跟随长上下文档的 output 单价
	long := svc.CalculateCost("gemini-2.5-pro", UsageSnapshot{
		InputTokens:     250000,
		OutputTokens:    500,
		ReasoningTokens: 2000,
	})
	if long.IsLongContext && entry.OutputCostPerTokenAbove200k > 0 {
		assertApprox(t, long.ReasoningCost, 2000*entry.OutputCostPerTokenAbove200k)
	}

	// OpenAI 系条目同样没有 reasoning 单价,推理 token 必须按 output 单价计费,
	// 否则 codex 的推理消耗全部记 0
	gptEntry, ok := svc.currentSnapshot().pricingMap["gpt-5"]
	if !ok || gptEntry.OutputCostPerReasoningToken > 0 {
		t.Skip("gpt-5 样本不满足前提")
	}
	gpt := svc.CalculateCost("gpt-5", UsageSnapshot{
		InputTokens:     1000,
		OutputTokens:    500,
		ReasoningTokens: 400,
	})
	assertApprox(t, gpt.ReasoningCost, 400*gptEntry.OutputCostPerToken)

	// 带 reasoning 单价的条目仍按专用单价计费,不被回退覆盖
	for _, name := range []string{"qwen-turbo", "dashscope/qwen-turbo"} {
		rich, ok := svc.currentSnapshot().pricingMap[name]
		if !ok || rich.OutputCostPerReasoningToken <= 0 {
			continue
		}
		got := svc.CalculateCost(name, UsageSnapshot{OutputTokens: 100, ReasoningTokens: 100})
		assertApprox(t, got.ReasoningCost, 100*rich.OutputCostPerReasoningToken)
		break
	}
}
