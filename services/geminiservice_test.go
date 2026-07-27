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
			name:    "Empty file",
			content: "",
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
		{"KEY-NAME", false},  // hyphen not allowed
		{"KEY.NAME", false},  // dot not allowed
		{"KEY NAME", false},  // space not allowed
		{"", true},           // empty is technically valid (no invalid chars)
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

func TestGeminiProvider_DeepCopyMaps(t *testing.T) {
	// Test that provider EnvConfig is properly deep copied when needed
	original := GeminiProvider{
		Name: "Test",
		EnvConfig: map[string]string{
			"KEY1": "value1",
		},
	}

	// Create a copy manually (simulating what should happen in duplication)
	copied := GeminiProvider{
		Name:      original.Name,
		EnvConfig: make(map[string]string),
	}
	for k, v := range original.EnvConfig {
		copied.EnvConfig[k] = v
	}

	// Modify copied
	copied.EnvConfig["KEY2"] = "value2"

	// Original should not be affected
	if _, exists := original.EnvConfig["KEY2"]; exists {
		t.Error("Original EnvConfig was modified when copy was changed")
	}

	if len(original.EnvConfig) != 1 {
		t.Errorf("Original EnvConfig length changed: got %d, expected 1", len(original.EnvConfig))
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
