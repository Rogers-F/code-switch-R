package services

import (
	"testing"
	"time"

	modelpricing "codeswitch/resources/model-pricing"
)

type fakeCatalogSource map[string]*modelpricing.RemoteCatalog

func (f fakeCatalogSource) Catalogs() map[string]*modelpricing.RemoteCatalog { return f }

func fptr(v float64) *float64 { return &v }
func bptr(v bool) *bool       { return &v }

func catalogModel(id, release string, opts ...func(*modelpricing.RemoteModel)) modelpricing.RemoteModel {
	m := modelpricing.RemoteModel{
		ID:          id,
		ReleaseDate: release,
		Cost:        &modelpricing.RemoteCost{Input: fptr(1), Output: fptr(2)},
	}
	for _, opt := range opts {
		opt(&m)
	}
	return m
}

func newTestPolicy(t *testing.T, now string, catalogs fakeCatalogSource) *DefaultModelPolicy {
	t.Helper()
	pricing, err := modelpricing.NewService()
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if _, err := pricing.Rebuild(modelpricing.ConvertCatalogs(catalogs)); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	fixed, err := time.ParseInLocation("2006-01-02", now, time.UTC)
	if err != nil {
		t.Fatalf("parse now: %v", err)
	}
	return &DefaultModelPolicy{
		pricing: pricing,
		source:  catalogs,
		now:     func() time.Time { return fixed },
	}
}

func TestCompareVersionSegments(t *testing.T) {
	cases := []struct {
		a, b []int
		want int
	}{
		{[]int{5, 10}, []int{5, 9}, 1},
		{[]int{5}, []int{5, 0}, 0},
		{[]int{4, 5}, []int{5}, -1},
		{[]int{3, 1}, []int{3}, 1},
	}
	for _, c := range cases {
		if got := compareVersionSegments(c.a, c.b); got != c.want {
			t.Errorf("compare(%v,%v) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestCodexDefaultModel 主线与 codex 专线的取舍:专线版本 >= 主线才选专线。
func TestCodexDefaultModel(t *testing.T) {
	policy := newTestPolicy(t, "2026-07-28", fakeCatalogSource{
		"openai": {ID: "openai", Models: map[string]modelpricing.RemoteModel{
			"gpt-5.6":       catalogModel("gpt-5.6", "2026-07-09"),
			"gpt-5.5":       catalogModel("gpt-5.5", "2026-04-23"),
			"gpt-5.3-codex": catalogModel("gpt-5.3-codex", "2026-02-05"),
			"gpt-5.6-luna":  catalogModel("gpt-5.6-luna", "2026-07-09"), // 变体不参与主线竞争
		}},
	})
	if got := policy.CodexDefaultModel(); got != "gpt-5.6" {
		t.Errorf("codex 默认 = %s, want gpt-5.6 (专线 5.3 < 主线 5.6)", got)
	}

	policy2 := newTestPolicy(t, "2026-07-28", fakeCatalogSource{
		"openai": {ID: "openai", Models: map[string]modelpricing.RemoteModel{
			"gpt-5.6":       catalogModel("gpt-5.6", "2026-07-09"),
			"gpt-5.6-codex": catalogModel("gpt-5.6-codex", "2026-07-10"),
		}},
	})
	if got := policy2.CodexDefaultModel(); got != "gpt-5.6-codex" {
		t.Errorf("codex 默认 = %s, want gpt-5.6-codex (专线版本持平应选专线)", got)
	}
}

// TestCodexDefaultRequiresToolCall tool_call 显式 false 的模型不作产品默认;缺失不排除。
func TestCodexDefaultRequiresToolCall(t *testing.T) {
	policy := newTestPolicy(t, "2026-07-28", fakeCatalogSource{
		"openai": {ID: "openai", Models: map[string]modelpricing.RemoteModel{
			"gpt-7":   catalogModel("gpt-7", "2026-06-01", func(m *modelpricing.RemoteModel) { m.ToolCall = bptr(false) }),
			"gpt-5.6": catalogModel("gpt-5.6", "2026-07-09", func(m *modelpricing.RemoteModel) { m.ToolCall = bptr(true) }),
			"gpt-5.5": catalogModel("gpt-5.5", "2026-04-23"), // tool_call 缺失
		}},
	})
	if got := policy.CodexDefaultModel(); got != "gpt-5.6" {
		t.Errorf("codex 默认 = %s, want gpt-5.6 (gpt-7 tool_call=false 应排除)", got)
	}
}

// TestGeminiDefaultVersionBeatsChannel 版本优先于频道:3.1-pro-preview 胜 2.5-pro(stable)。
func TestGeminiDefaultVersionBeatsChannel(t *testing.T) {
	policy := newTestPolicy(t, "2026-07-28", fakeCatalogSource{
		"google": {ID: "google", Models: map[string]modelpricing.RemoteModel{
			"gemini-2.5-pro":         catalogModel("gemini-2.5-pro", "2025-06-17"),
			"gemini-3.1-pro-preview": catalogModel("gemini-3.1-pro-preview", "2026-02-19"),
			"gemini-3.1-pro-image":   catalogModel("gemini-3.1-pro-image", "2026-05-28"), // 非目标家族
		}},
	})
	if got := policy.GeminiDefaultModel(); got != "gemini-3.1-pro-preview" {
		t.Errorf("gemini 默认 = %s, want gemini-3.1-pro-preview", got)
	}
}

// TestGeminiSameVersionStableBeatsPreview 同版本 stable 优先于 preview。
func TestGeminiSameVersionStableBeatsPreview(t *testing.T) {
	policy := newTestPolicy(t, "2026-07-28", fakeCatalogSource{
		"google": {ID: "google", Models: map[string]modelpricing.RemoteModel{
			"gemini-3.1-pro":         catalogModel("gemini-3.1-pro", "2026-05-19"),
			"gemini-3.1-pro-preview": catalogModel("gemini-3.1-pro-preview", "2026-02-19"),
		}},
	})
	if got := policy.GeminiDefaultModel(); got != "gemini-3.1-pro" {
		t.Errorf("gemini 默认 = %s, want gemini-3.1-pro (同版本 stable 优先)", got)
	}
}

// TestProbeStabilityWindow 探测模型要求发布满 30 天:gemini-3.6-flash(7 天)让位 3.5-flash。
func TestProbeStabilityWindow(t *testing.T) {
	policy := newTestPolicy(t, "2026-07-28", fakeCatalogSource{
		"google": {ID: "google", Models: map[string]modelpricing.RemoteModel{
			"gemini-3.6-flash": catalogModel("gemini-3.6-flash", "2026-07-21"),
			"gemini-3.5-flash": catalogModel("gemini-3.5-flash", "2026-05-19"),
		}},
	})
	if got := policy.ProbeModel("gemini"); got != "gemini-3.5-flash" {
		t.Errorf("gemini 探测 = %s, want gemini-3.5-flash (3.6 未满稳定窗)", got)
	}
}

// TestClaudeProbeDatedTieBreakWithinVersion 日期变体只在同版本内决胜,不高于版本比较。
func TestClaudeProbeDatedTieBreakWithinVersion(t *testing.T) {
	policy := newTestPolicy(t, "2026-07-28", fakeCatalogSource{
		"anthropic": {ID: "anthropic", Models: map[string]modelpricing.RemoteModel{
			"claude-haiku-4-5":          catalogModel("claude-haiku-4-5", "2025-10-15"),
			"claude-haiku-4-5-20251001": catalogModel("claude-haiku-4-5-20251001", "2025-10-15"),
			"claude-3-5-haiku-20241022": catalogModel("claude-3-5-haiku-20241022", "2024-10-22"),
		}},
	})
	if got := policy.ProbeModel("claude"); got != "claude-haiku-4-5-20251001" {
		t.Errorf("claude 探测 = %s, want claude-haiku-4-5-20251001 (同版本带日期优先)", got)
	}

	// 出现更高版本裸名时,版本优先于"带日期"
	policy2 := newTestPolicy(t, "2026-07-28", fakeCatalogSource{
		"anthropic": {ID: "anthropic", Models: map[string]modelpricing.RemoteModel{
			"claude-haiku-5":            catalogModel("claude-haiku-5", "2026-05-01"),
			"claude-haiku-4-5-20251001": catalogModel("claude-haiku-4-5-20251001", "2025-10-15"),
		}},
	})
	if got := policy2.ProbeModel("claude"); got != "claude-haiku-5" {
		t.Errorf("claude 探测 = %s, want claude-haiku-5 (版本高于日期偏好)", got)
	}
}

// TestResolverExcludesFutureAndMissingRelease 未来日期与缺失 release_date 不入选。
func TestResolverExcludesFutureAndMissingRelease(t *testing.T) {
	policy := newTestPolicy(t, "2026-07-28", fakeCatalogSource{
		"openai": {ID: "openai", Models: map[string]modelpricing.RemoteModel{
			"gpt-9":   catalogModel("gpt-9", "2026-09-01"), // 未来
			"gpt-8":   catalogModel("gpt-8", ""),           // 缺失
			"gpt-5.6": catalogModel("gpt-5.6", "2026-07-09"),
		}},
	})
	if got := policy.CodexDefaultModel(); got != "gpt-5.6" {
		t.Errorf("codex 默认 = %s, want gpt-5.6 (未来/缺失日期应排除)", got)
	}
}

// TestPolicyFallbacksWithoutSource 无目录源时回退静态兜底。
func TestPolicyFallbacksWithoutSource(t *testing.T) {
	policy := NewDefaultModelPolicy()
	if got := policy.CodexDefaultModel(); got != FallbackCodexDefaultModel {
		t.Errorf("无源 codex 默认 = %s, want %s", got, FallbackCodexDefaultModel)
	}
	if got := policy.ProbeModel("claude"); got != FallbackClaudeProbeModel {
		t.Errorf("无源 claude 探测 = %s, want %s", got, FallbackClaudeProbeModel)
	}
	if got := policy.ProbeModel("gemini"); got != FallbackGeminiProbeModel {
		t.Errorf("无源 gemini 探测 = %s, want %s", got, FallbackGeminiProbeModel)
	}
}

// TestResolverRequiresPositivePricing 目录有条目但价格表无价的候选被跳过。
func TestResolverRequiresPositivePricing(t *testing.T) {
	catalogs := fakeCatalogSource{
		"openai": {ID: "openai", Models: map[string]modelpricing.RemoteModel{
			"gpt-7":   catalogModel("gpt-7", "2026-06-01", func(m *modelpricing.RemoteModel) { m.Cost = nil }), // 无价
			"gpt-5.6": catalogModel("gpt-5.6", "2026-07-09"),
		}},
	}
	policy := newTestPolicy(t, "2026-07-28", catalogs)
	if got := policy.CodexDefaultModel(); got != "gpt-5.6" {
		t.Errorf("codex 默认 = %s, want gpt-5.6 (无价条目应跳过)", got)
	}
}
