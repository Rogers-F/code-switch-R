package services

import (
	"testing"
)

func TestGeminiService_GetPresets(t *testing.T) {
	svc := NewGeminiService("127.0.0.1:18100", nil)
	presets := svc.GetPresets()

	if len(presets) == 0 {
		t.Fatal("GetPresets should return at least one preset")
	}

	// Check Google Official preset
	var googlePreset *GeminiPreset
	for _, p := range presets {
		if p.Name == "Google Official" {
			googlePreset = &p
			break
		}
	}

	if googlePreset == nil {
		t.Fatal("Google Official preset should exist")
	}

	if googlePreset.Category != "official" {
		t.Errorf("Google Official category should be 'official', got '%s'", googlePreset.Category)
	}

	// Check PackyCode preset
	var packyPreset *GeminiPreset
	for _, p := range presets {
		if p.Name == "PackyCode" {
			packyPreset = &p
			break
		}
	}

	if packyPreset == nil {
		t.Fatal("PackyCode preset should exist")
	}

	if packyPreset.Category != "third_party" {
		t.Errorf("PackyCode category should be 'third_party', got '%s'", packyPreset.Category)
	}

	if packyPreset.BaseURL == "" {
		t.Error("PackyCode should have a BaseURL")
	}
}

func TestDetectGeminiAuthType(t *testing.T) {
	tests := []struct {
		name     string
		provider GeminiProvider
		expected GeminiAuthType
	}{
		{
			name: "Google Official OAuth (empty base and key)",
			provider: GeminiProvider{
				Name:    "Google Official",
				BaseURL: "",
				APIKey:  "",
			},
			expected: GeminiAuthOAuth,
		},
		{
			name: "PackyCode API Key",
			provider: GeminiProvider{
				Name:                "PackyCode",
				BaseURL:             "https://www.packyapi.com",
				APIKey:              "pk-xxx",
				PartnerPromotionKey: "packycode",
			},
			expected: GeminiAuthPackycode,
		},
		{
			name: "Generic API Key",
			provider: GeminiProvider{
				Name:    "Custom",
				BaseURL: "https://custom.api.com",
				APIKey:  "sk-xxx",
			},
			expected: GeminiAuthGeneric,
		},
		{
			name: "Generic provider with no base URL",
			provider: GeminiProvider{
				Name:    "Native Gemini",
				BaseURL: "",
				APIKey:  "AIza-xxx",
			},
			expected: GeminiAuthGeneric, // 无 partner_promotion_key 且名称不匹配 google/packy，默认为 Generic
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectGeminiAuthType(&tt.provider)
			if result != tt.expected {
				t.Errorf("detectGeminiAuthType() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestParseEnvFile(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected map[string]string
	}{
		{
			name:     "Empty file",
			content:  "",
			expected: map[string]string{},
		},
		{
			name:    "Single variable",
			content: "GEMINI_API_KEY=test-key",
			expected: map[string]string{
				"GEMINI_API_KEY": "test-key",
			},
		},
		{
			name: "Multiple variables",
			content: `GEMINI_API_KEY=test-key
GOOGLE_GEMINI_BASE_URL=https://api.test.com
GEMINI_MODEL=gemini-pro`,
			expected: map[string]string{
				"GEMINI_API_KEY":         "test-key",
				"GOOGLE_GEMINI_BASE_URL": "https://api.test.com",
				"GEMINI_MODEL":           "gemini-pro",
			},
		},
		{
			name: "With comments and empty lines",
			content: `# This is a comment
GEMINI_API_KEY=test-key

# Another comment
GOOGLE_GEMINI_BASE_URL=https://api.test.com
`,
			expected: map[string]string{
				"GEMINI_API_KEY":         "test-key",
				"GOOGLE_GEMINI_BASE_URL": "https://api.test.com",
			},
		},
		{
			name:    "Value with equals sign",
			content: "SOME_KEY=value=with=equals",
			expected: map[string]string{
				"SOME_KEY": "value=with=equals",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseEnvFile(tt.content)
			if len(result) != len(tt.expected) {
				t.Errorf("parseEnvFile() returned %d items, expected %d", len(result), len(tt.expected))
			}
			for key, expectedValue := range tt.expected {
				if result[key] != expectedValue {
					t.Errorf("parseEnvFile()[%s] = %q, expected %q", key, result[key], expectedValue)
				}
			}
		})
	}
}

func TestIsValidEnvKey(t *testing.T) {
	tests := []struct {
		key      string
		expected bool
	}{
		{"GEMINI_API_KEY", true},
		{"gemini_api_key", true},
		{"GOOGLE_GEMINI_BASE_URL", true},
		{"KEY123", true},
		{"_KEY", true},
		{"KEY-NAME", false}, // hyphen not allowed
		{"KEY.NAME", false}, // dot not allowed
		{"KEY NAME", false}, // space not allowed
		{"", true},          // empty is technically valid (no invalid chars)
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			result := isValidEnvKey(tt.key)
			if result != tt.expected {
				t.Errorf("isValidEnvKey(%q) = %v, expected %v", tt.key, result, tt.expected)
			}
		})
	}
}

// TestGeminiProvider_DeepCopyMaps 调用真实的 GeminiService.DuplicateProvider,
// 验证副本命名/禁用态/Level/EnvConfig 与 SettingsConfig 深拷贝隔离(修改副本不影响内存中的源供应商)。
func TestGeminiProvider_DeepCopyMaps(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	// Windows 的 os.UserHomeDir() 读的是 USERPROFILE,只设 HOME 会写到真实用户配置目录
	t.Setenv("USERPROFILE", tmpHome)
	assertHomeIsolated(t, tmpHome)

	svc := NewGeminiService("127.0.0.1:18100", nil)
	svc.providers = []GeminiProvider{
		{
			ID:      "prov-1",
			Name:    "Test",
			Enabled: true,
			Level:   5,
			EnvConfig: map[string]string{
				"KEY1": "value1",
			},
			SettingsConfig: map[string]any{
				"setting1": "a",
			},
		},
	}

	cloned, err := svc.DuplicateProvider("prov-1")
	if err != nil {
		t.Fatalf("DuplicateProvider failed: %v", err)
	}

	if cloned.Name != "Test (副本)" {
		t.Errorf("cloned name = %q, expected %q", cloned.Name, "Test (副本)")
	}
	if cloned.Enabled {
		t.Error("cloned provider should be disabled by default")
	}
	if cloned.ID == "prov-1" {
		t.Error("cloned provider should have a new ID")
	}
	if cloned.Level != 5 {
		t.Errorf("cloned Level = %d, expected %d（复制时漏拷 Level 会导致副本优先级被静默重置为最高）", cloned.Level, 5)
	}
	if cloned.EnvConfig["KEY1"] != "value1" {
		t.Errorf("cloned EnvConfig[KEY1] = %q, expected %q", cloned.EnvConfig["KEY1"], "value1")
	}
	if cloned.SettingsConfig["setting1"] != "a" {
		t.Errorf("cloned SettingsConfig[setting1] = %v, expected %q", cloned.SettingsConfig["setting1"], "a")
	}

	// 深拷贝隔离:修改副本的 map 不应影响内存中的源供应商
	cloned.EnvConfig["KEY2"] = "value2"
	if _, exists := svc.providers[0].EnvConfig["KEY2"]; exists {
		t.Error("source EnvConfig was modified when clone was changed")
	}
	if len(svc.providers[0].EnvConfig) != 1 {
		t.Errorf("source EnvConfig length changed: got %d, expected 1", len(svc.providers[0].EnvConfig))
	}
	cloned.SettingsConfig["setting2"] = "b"
	if _, exists := svc.providers[0].SettingsConfig["setting2"]; exists {
		t.Error("source SettingsConfig was modified when clone was changed")
	}

	// 副本应已追加到列表并落盘,源供应商保持不变
	if len(svc.providers) != 2 {
		t.Fatalf("providers length = %d, expected 2", len(svc.providers))
	}
	if svc.providers[0].Name != "Test" || !svc.providers[0].Enabled {
		t.Errorf("source provider should be unchanged, got %+v", svc.providers[0])
	}
}

// TestGeminiDuplicateProvider_NotFound 源 ID 不存在时应报错。
func TestGeminiDuplicateProvider_NotFound(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	assertHomeIsolated(t, tmpHome)

	svc := NewGeminiService("127.0.0.1:18100", nil)
	if _, err := svc.DuplicateProvider("missing-id"); err == nil {
		t.Fatal("duplicating a missing provider should fail")
	}
}

// TestGeminiDuplicateProvider_UniqueNameOnRepeat 连续复制同一供应商多次时，
// 生成的副本名不能互相重复：前端 Gemini 分支以 name 作为增删改的匹配键，
// 重名会导致删除静默失效、编辑写到错误的供应商上。
func TestGeminiDuplicateProvider_UniqueNameOnRepeat(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	assertHomeIsolated(t, tmpHome)

	svc := NewGeminiService("127.0.0.1:18100", nil)
	svc.providers = []GeminiProvider{
		{ID: "prov-1", Name: "A"},
	}

	first, err := svc.DuplicateProvider("prov-1")
	if err != nil {
		t.Fatalf("第一次 DuplicateProvider 失败: %v", err)
	}
	if first.Name != "A (副本)" {
		t.Errorf("第一次副本名 = %q, 期望 %q", first.Name, "A (副本)")
	}

	second, err := svc.DuplicateProvider("prov-1")
	if err != nil {
		t.Fatalf("第二次 DuplicateProvider 失败: %v", err)
	}
	if second.Name == first.Name {
		t.Errorf("连续复制生成了重名供应商: %q", second.Name)
	}
	if second.Name != "A (副本) 2" {
		t.Errorf("第二次副本名 = %q, 期望 %q", second.Name, "A (副本) 2")
	}

	third, err := svc.DuplicateProvider("prov-1")
	if err != nil {
		t.Fatalf("第三次 DuplicateProvider 失败: %v", err)
	}
	if third.Name != "A (副本) 3" {
		t.Errorf("第三次副本名 = %q, 期望 %q", third.Name, "A (副本) 3")
	}

	seen := make(map[string]int, len(svc.providers))
	for _, p := range svc.providers {
		seen[p.Name]++
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("供应商名 %q 重复了 %d 次", name, count)
		}
	}
}

func TestGeminiPreset_Fields(t *testing.T) {
	svc := NewGeminiService("127.0.0.1:18100", nil)
	presets := svc.GetPresets()

	for _, p := range presets {
		// All presets should have Name
		if p.Name == "" {
			t.Error("Preset has empty name")
		}

		// All presets except custom should have WebsiteURL
		if p.WebsiteURL == "" && p.Category != "custom" {
			t.Errorf("Preset %q has empty WebsiteURL", p.Name)
		}

		// All presets should have Category
		if p.Category == "" {
			t.Errorf("Preset %q has empty Category", p.Name)
		}

		// Category should be valid
		validCategories := map[string]bool{
			"official":    true,
			"third_party": true,
			"custom":      true,
		}
		if !validCategories[p.Category] {
			t.Errorf("Preset %q has invalid Category: %q", p.Name, p.Category)
		}
	}
}
