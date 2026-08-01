package services

import (
	"strings"
	"testing"
)

// 回归：白名单里值为 false 的条目不算支持（旧实现的通配符循环忽略 value，
// {"m": false} 仍会精确命中）
func TestModelSupportedBySkipsFalseEntries(t *testing.T) {
	supported := map[string]bool{
		"claude-sonnet-4": false,
		"claude-*":        false,
	}
	if modelSupportedBy(supported, nil, "claude-sonnet-4") {
		t.Error("值为 false 的精确条目不应判定为支持")
	}
	if modelSupportedBy(supported, nil, "claude-opus-4") {
		t.Error("值为 false 的通配符条目不应判定为支持")
	}

	mixed := map[string]bool{
		"claude-*": false,
		"gpt-*":    true,
	}
	if modelSupportedBy(mixed, nil, "claude-sonnet-4") {
		t.Error("false 通配符不应放行")
	}
	if !modelSupportedBy(mixed, nil, "gpt-4") {
		t.Error("true 通配符应放行")
	}
}

// 回归：多条通配符映射同时命中时结果必须确定
// （旧实现直接遍历 map，取哪条取决于随机迭代序）
func TestEffectiveModelForDeterministicOverlap(t *testing.T) {
	// 字面量长度不同：gemini-（7）> -pro（4），应选 gemini-*
	mapping := map[string]string{
		"gemini-*": "vendor-a/gemini-*",
		"*-pro":    "vendor-b/*",
	}
	for i := 0; i < 50; i++ {
		got := effectiveModelFor(mapping, "gemini-pro")
		if got != "vendor-a/gemini-pro" {
			t.Fatalf("第 %d 次: 期望 vendor-a/gemini-pro（字面量最长优先），得到 %q", i, got)
		}
	}

	// 字面量等长：按字典序取小者（"*-x" < "z-*"）
	tie := map[string]string{
		"*-x": "left-*",
		"z-*": "right-*",
	}
	for i := 0; i < 50; i++ {
		got := effectiveModelFor(tie, "z-x")
		if got != "left-z" {
			t.Fatalf("第 %d 次: 期望 left-z（等长按字典序），得到 %q", i, got)
		}
	}

	// 精确映射仍优先于任何通配符
	exact := map[string]string{
		"gemini-pro": "exact-target",
		"gemini-*":   "wild-*",
	}
	if got := effectiveModelFor(exact, "gemini-pro"); got != "exact-target" {
		t.Fatalf("精确映射应优先，得到 %q", got)
	}
}

// 映射目标为空串必须报配置错误（会把请求模型改写成空，上游必拒）
func TestValidateModelConfigEmptyMappingTarget(t *testing.T) {
	errs := validateModelConfig(nil, map[string]string{"gemini-pro": "  "})
	if len(errs) != 1 || !strings.Contains(errs[0], "目标模型为空") {
		t.Fatalf("期望空目标报错，得到 %v", errs)
	}

	// 目标命中白名单里值为 false 的条目也算不在白名单
	errs = validateModelConfig(
		map[string]bool{"real-model": true, "fake-model": false},
		map[string]string{"x": "fake-model"},
	)
	if len(errs) != 1 || !strings.Contains(errs[0], "不在 supportedModels") {
		t.Fatalf("期望 false 条目视为不在白名单，得到 %v", errs)
	}

	// 合法配置不报错
	errs = validateModelConfig(
		map[string]bool{"real-model": true},
		map[string]string{"x": "real-model"},
	)
	if len(errs) != 0 {
		t.Fatalf("合法配置不应报错，得到 %v", errs)
	}
}

// 回归：前后缀在 text 中重叠的单星号规则不得匹配，
// 否则 applyWildcardMapping 会切片越界 panic（如 "gemini-*-pro" 撞上 "gemini-pro"）
func TestMatchWildcardOverlapNoPanic(t *testing.T) {
	if matchWildcard("gemini-*-pro", "gemini-pro") {
		t.Error("前后缀重叠时不应匹配（text 长度不足以容纳前缀+后缀）")
	}
	// 长度恰好等于前后缀之和：* 匹配空串，应匹配
	if !matchWildcard("gemini-*-pro", "gemini--pro") {
		t.Error("* 匹配空串的边界情形应匹配")
	}
	// 即使直接调用展开函数也不得 panic，未真正匹配时原样返回 replacement
	if got := applyWildcardMapping("gemini-*-pro", "x-*-y", "gemini-pro"); got != "x-*-y" {
		t.Errorf("不匹配的输入应返回原始 replacement，得到 %q", got)
	}
	// 经确定性选择路径整体走一遍，确认无 panic
	mapping := map[string]string{"gemini-*-pro": "vendor-*"}
	if got := effectiveModelFor(mapping, "gemini-pro"); got != "gemini-pro" {
		t.Errorf("重叠规则不匹配时应原样返回请求模型，得到 %q", got)
	}
}

// GeminiProvider 与 Provider 走同一套模型匹配逻辑
func TestGeminiProviderModelMethods(t *testing.T) {
	p := &GeminiProvider{
		SupportedModels: map[string]bool{"gemini-2.5-*": true, "vendor/gemini-2.5-pro": true},
		ModelMapping:    map[string]string{"gemini-2.5-pro": "vendor/gemini-2.5-pro"},
	}
	if !p.IsModelSupported("gemini-2.5-flash") {
		t.Error("通配符白名单应放行 gemini-2.5-flash")
	}
	if p.IsModelSupported("gemini-1.5-pro") {
		t.Error("白名单外模型不应放行")
	}
	if got := p.GetEffectiveModel("gemini-2.5-pro"); got != "vendor/gemini-2.5-pro" {
		t.Errorf("映射未生效: %q", got)
	}
	if errs := p.ValidateConfiguration(); len(errs) != 0 {
		t.Errorf("合法配置不应报错: %v", errs)
	}

	// 未配置时全放行（向后兼容）
	empty := &GeminiProvider{}
	if !empty.IsModelSupported("anything") {
		t.Error("未配置白名单/映射时应支持所有模型")
	}
}
