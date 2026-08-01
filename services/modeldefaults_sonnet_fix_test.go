package services

import (
	"testing"

	modelpricing "codeswitch/resources/model-pricing"
)

// ClaudeDefaultModel:稳定 Sonnet 家族、新旧命名并存取版本更高者、
// tool_call 显式 false 排除、目录不可用回退兜底常量
func TestClaudeDefaultModelSelection(t *testing.T) {
	policy := newTestPolicy(t, "2026-07-28", fakeCatalogSource{
		"anthropic": {ID: "anthropic", Models: map[string]modelpricing.RemoteModel{
			"claude-sonnet-4-5":          catalogModel("claude-sonnet-4-5", "2025-09-29"),
			"claude-sonnet-5":            catalogModel("claude-sonnet-5", "2026-05-01"),
			"claude-3-7-sonnet-20250219": catalogModel("claude-3-7-sonnet-20250219", "2025-02-19"), // legacy 命名
			"claude-haiku-4-5":           catalogModel("claude-haiku-4-5", "2025-10-01"),           // 别的家族不参赛
			"claude-opus-4-6":            catalogModel("claude-opus-4-6", "2026-02-05"),
		}},
	})
	if got := policy.ClaudeDefaultModel(); got != "claude-sonnet-5" {
		t.Errorf("claude 默认 = %s, want claude-sonnet-5", got)
	}
}

// legacy 命名（claude-3-7-sonnet / 带日期变体）在 pattern 层可正确解析。
// 注：选择器级验证用新式 id——测试夹具的定价快照对 legacy 条目的命中
// 依赖生产环境完整别名表，属定价层行为，不在本用例范围
func TestClaudeSonnetPatternLegacyNaming(t *testing.T) {
	cases := []struct {
		id      string
		version []int
		dated   string
		ok      bool
	}{
		{"claude-3-7-sonnet", []int{3, 7}, "", true},
		{"claude-3-7-sonnet-20250219", []int{3, 7}, "20250219", true},
		{"claude-sonnet-4-5", []int{4, 5}, "", true},
		{"claude-sonnet-4-5-20250929", []int{4, 5}, "20250929", true},
		{"claude-sonnet-5", []int{5}, "", true},
		{"claude-haiku-4-5", nil, "", false},
		{"claude-3-opus", nil, "", false},
		{"claude-sonnet", nil, "", false},
	}
	for _, c := range cases {
		v, dated, _, ok := claudeSonnetPattern(c.id)
		if ok != c.ok || dated != c.dated || (ok && !equalIntSlice(v, c.version)) {
			t.Errorf("%s: got (v=%v dated=%q ok=%v), want (v=%v dated=%q ok=%v)",
				c.id, v, dated, ok, c.version, c.dated, c.ok)
		}
	}
}

func equalIntSlice(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestClaudeDefaultModelRequiresToolCall(t *testing.T) {
	policy := newTestPolicy(t, "2026-07-28", fakeCatalogSource{
		"anthropic": {ID: "anthropic", Models: map[string]modelpricing.RemoteModel{
			"claude-sonnet-6": catalogModel("claude-sonnet-6", "2026-06-01", func(m *modelpricing.RemoteModel) { m.ToolCall = bptr(false) }),
			"claude-sonnet-5": catalogModel("claude-sonnet-5", "2026-05-01", func(m *modelpricing.RemoteModel) { m.ToolCall = bptr(true) }),
		}},
	})
	if got := policy.ClaudeDefaultModel(); got != "claude-sonnet-5" {
		t.Errorf("tool_call=false 应被排除, 实际 %s", got)
	}
}

func TestClaudeDefaultModelFallbackWithoutCatalog(t *testing.T) {
	policy := NewDefaultModelPolicy() // 无目录源
	if got := policy.ClaudeDefaultModel(); got != FallbackClaudeDefaultModel {
		t.Errorf("无目录应回退 %s, 实际 %s", FallbackClaudeDefaultModel, got)
	}
}
