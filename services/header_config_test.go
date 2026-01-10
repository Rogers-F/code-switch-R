package services

import (
	"encoding/json"
	"net/http"
	"testing"
)

// ==================== buildForwardHeaders 测试 ====================

func TestBuildForwardHeaders_NilProvider(t *testing.T) {
	original := http.Header{
		"Content-Type": {"application/json"},
		"Accept":       {"text/plain"},
	}

	result := buildForwardHeaders(original, nil)

	// nil provider 应该直接返回原始 headers 的副本
	if result.Get("Content-Type") != "application/json" {
		t.Errorf("期望 Content-Type = application/json, 实际 = %s", result.Get("Content-Type"))
	}
	if result.Get("Accept") != "text/plain" {
		t.Errorf("期望 Accept = text/plain, 实际 = %s", result.Get("Accept"))
	}
}

func TestBuildForwardHeaders_EmptyProvider(t *testing.T) {
	original := http.Header{
		"Content-Type": {"application/json"},
		"X-Custom":     {"original-value"},
	}

	provider := &Provider{Name: "test"}
	result := buildForwardHeaders(original, provider)

	// 空配置的 provider 应该保留所有原始 headers
	if result.Get("Content-Type") != "application/json" {
		t.Errorf("期望 Content-Type = application/json, 实际 = %s", result.Get("Content-Type"))
	}
	if result.Get("X-Custom") != "original-value" {
		t.Errorf("期望 X-Custom = original-value, 实际 = %s", result.Get("X-Custom"))
	}
}

func TestBuildForwardHeaders_StripHeaders(t *testing.T) {
	tests := []struct {
		name         string
		original     http.Header
		stripHeaders []string
		expectGone   []string
		expectKept   []string
	}{
		{
			name: "移除单个 Header",
			original: http.Header{
				"Content-Type":    {"application/json"},
				"X-Forwarded-For": {"192.168.1.1"},
				"Accept":          {"*/*"},
			},
			stripHeaders: []string{"X-Forwarded-For"},
			expectGone:   []string{"X-Forwarded-For"},
			expectKept:   []string{"Content-Type", "Accept"},
		},
		{
			name: "移除多个 Headers",
			original: http.Header{
				"Content-Type":    {"application/json"},
				"X-Forwarded-For": {"192.168.1.1"},
				"X-Real-Ip":       {"10.0.0.1"},
				"Accept":          {"*/*"},
			},
			stripHeaders: []string{"X-Forwarded-For", "X-Real-Ip"},
			expectGone:   []string{"X-Forwarded-For", "X-Real-Ip"},
			expectKept:   []string{"Content-Type", "Accept"},
		},
		{
			name: "移除不存在的 Header（无影响）",
			original: http.Header{
				"Content-Type": {"application/json"},
			},
			stripHeaders: []string{"X-Non-Existent"},
			expectGone:   []string{"X-Non-Existent"},
			expectKept:   []string{"Content-Type"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &Provider{
				Name:         "test",
				StripHeaders: tt.stripHeaders,
			}

			result := buildForwardHeaders(tt.original, provider)

			// 验证应被移除的 headers
			for _, h := range tt.expectGone {
				if result.Get(h) != "" {
					t.Errorf("期望 %s 被移除，但实际存在: %s", h, result.Get(h))
				}
			}

			// 验证应保留的 headers
			for _, h := range tt.expectKept {
				if result.Get(h) == "" {
					t.Errorf("期望 %s 被保留，但实际不存在", h)
				}
			}
		})
	}
}

func TestBuildForwardHeaders_OverrideHeaders(t *testing.T) {
	tests := []struct {
		name            string
		original        http.Header
		overrideHeaders map[string]string
		expected        map[string]string
	}{
		{
			name: "覆盖已存在的 Header",
			original: http.Header{
				"Content-Type": {"text/plain"},
			},
			overrideHeaders: map[string]string{
				"Content-Type": "application/json",
			},
			expected: map[string]string{
				"Content-Type": "application/json",
			},
		},
		{
			name: "添加不存在的 Header",
			original: http.Header{
				"Accept": {"*/*"},
			},
			overrideHeaders: map[string]string{
				"X-Custom-Header": "custom-value",
			},
			expected: map[string]string{
				"Accept":          "*/*",
				"X-Custom-Header": "custom-value",
			},
		},
		{
			name: "覆盖多个 Headers",
			original: http.Header{
				"Content-Type": {"text/plain"},
				"Accept":       {"text/html"},
			},
			overrideHeaders: map[string]string{
				"Content-Type": "application/json",
				"Accept":       "application/json",
			},
			expected: map[string]string{
				"Content-Type": "application/json",
				"Accept":       "application/json",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &Provider{
				Name:            "test",
				OverrideHeaders: tt.overrideHeaders,
			}

			result := buildForwardHeaders(tt.original, provider)

			for key, expectedValue := range tt.expected {
				actualValue := result.Get(key)
				if actualValue != expectedValue {
					t.Errorf("Header %s: 期望 %q, 实际 %q", key, expectedValue, actualValue)
				}
			}
		})
	}
}

func TestBuildForwardHeaders_ExtraHeaders(t *testing.T) {
	tests := []struct {
		name         string
		original     http.Header
		extraHeaders map[string]string
		expected     map[string]string
	}{
		{
			name: "添加不存在的 Header",
			original: http.Header{
				"Accept": {"*/*"},
			},
			extraHeaders: map[string]string{
				"X-Custom-Client": "code-switch",
			},
			expected: map[string]string{
				"Accept":          "*/*",
				"X-Custom-Client": "code-switch",
			},
		},
		{
			name: "不覆盖已存在的 Header",
			original: http.Header{
				"Content-Type": {"text/plain"},
			},
			extraHeaders: map[string]string{
				"Content-Type": "application/json",
			},
			expected: map[string]string{
				"Content-Type": "text/plain", // 保持原值
			},
		},
		{
			name:     "原请求为空时添加 Headers",
			original: http.Header{},
			extraHeaders: map[string]string{
				"X-Custom-A": "value-a",
				"X-Custom-B": "value-b",
			},
			expected: map[string]string{
				"X-Custom-A": "value-a",
				"X-Custom-B": "value-b",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &Provider{
				Name:         "test",
				ExtraHeaders: tt.extraHeaders,
			}

			result := buildForwardHeaders(tt.original, provider)

			for key, expectedValue := range tt.expected {
				actualValue := result.Get(key)
				if actualValue != expectedValue {
					t.Errorf("Header %s: 期望 %q, 实际 %q", key, expectedValue, actualValue)
				}
			}
		})
	}
}

func TestBuildForwardHeaders_Priority(t *testing.T) {
	// 测试优先级：Strip → Override → Extra
	// 场景：同一 Header 被 Strip 和 Override 同时配置

	original := http.Header{
		"X-Target":  {"original"},
		"X-Another": {"another-original"},
		"X-Keep":    {"keep-original"},
	}

	provider := &Provider{
		Name:         "test",
		StripHeaders: []string{"X-Target"},
		OverrideHeaders: map[string]string{
			"X-Target":  "override-value", // Strip 后再 Override
			"X-Another": "override-another",
		},
		ExtraHeaders: map[string]string{
			"X-Target":  "extra-value", // Override 后不再应用
			"X-Another": "extra-another",
			"X-New":     "new-value",
		},
	}

	result := buildForwardHeaders(original, provider)

	// X-Target: Strip 先移除，Override 再设置
	if result.Get("X-Target") != "override-value" {
		t.Errorf("X-Target: 期望 override-value, 实际 %q", result.Get("X-Target"))
	}

	// X-Another: Override 覆盖原值
	if result.Get("X-Another") != "override-another" {
		t.Errorf("X-Another: 期望 override-another, 实际 %q", result.Get("X-Another"))
	}

	// X-Keep: 未被任何规则影响
	if result.Get("X-Keep") != "keep-original" {
		t.Errorf("X-Keep: 期望 keep-original, 实际 %q", result.Get("X-Keep"))
	}

	// X-New: ExtraHeaders 添加新 Header
	if result.Get("X-New") != "new-value" {
		t.Errorf("X-New: 期望 new-value, 实际 %q", result.Get("X-New"))
	}
}

func TestBuildForwardHeaders_DeepCopy(t *testing.T) {
	// 验证返回的 headers 是深拷贝，修改不影响原始
	original := http.Header{
		"Content-Type": {"application/json"},
		"X-Multi":      {"value1", "value2"},
	}

	provider := &Provider{Name: "test"}
	result := buildForwardHeaders(original, provider)

	// 修改结果
	result.Set("Content-Type", "text/plain")
	result.Add("X-Multi", "value3")

	// 验证原始未被影响
	if original.Get("Content-Type") != "application/json" {
		t.Errorf("原始 Content-Type 被意外修改")
	}
	if len(original["X-Multi"]) != 2 {
		t.Errorf("原始 X-Multi 值数量被意外修改: %d", len(original["X-Multi"]))
	}
}

// ==================== cloneHeaders 测试 ====================

func TestCloneHeaders_AuthHeadersFiltered(t *testing.T) {
	tests := []struct {
		name           string
		originalHeader string
		originalValue  string
		shouldExist    bool
	}{
		{"Authorization 被过滤", "Authorization", "Bearer sk-xxx", false},
		{"X-Api-Key 被过滤", "X-Api-Key", "sk-ant-xxx", false},
		{"X-Goog-Api-Key 被过滤", "X-Goog-Api-Key", "AIza-xxx", false},
		{"Content-Type 被保留", "Content-Type", "application/json", true},
		{"Accept 被保留", "Accept", "*/*", true},
		{"X-Custom 被保留", "X-Custom", "custom-value", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := http.Header{
				tt.originalHeader: {tt.originalValue},
			}

			result := cloneHeaders(original)

			exists := result.Get(tt.originalHeader) != ""
			if exists != tt.shouldExist {
				if tt.shouldExist {
					t.Errorf("期望 %s 被保留，但被过滤了", tt.originalHeader)
				} else {
					t.Errorf("期望 %s 被过滤，但被保留了: %s", tt.originalHeader, result.Get(tt.originalHeader))
				}
			}
		})
	}
}

func TestCloneHeaders_HopByHopFiltered(t *testing.T) {
	// Hop-by-hop headers 应被过滤
	hopHeaders := []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	}

	for _, h := range hopHeaders {
		t.Run(h+"被过滤", func(t *testing.T) {
			original := http.Header{
				h: {"some-value"},
			}

			result := cloneHeaders(original)

			if result.Get(h) != "" {
				t.Errorf("期望 hop-by-hop header %s 被过滤，但实际存在: %s", h, result.Get(h))
			}
		})
	}
}

func TestCloneHeaders_MultipleValues(t *testing.T) {
	// 验证多值 Header 被正确复制
	original := http.Header{
		"Accept-Encoding": {"gzip", "deflate", "br"},
		"Cache-Control":   {"no-cache", "no-store"},
	}

	result := cloneHeaders(original)

	// 验证值数量
	if len(result["Accept-Encoding"]) != 3 {
		t.Errorf("Accept-Encoding 值数量不正确: 期望 3, 实际 %d", len(result["Accept-Encoding"]))
	}

	if len(result["Cache-Control"]) != 2 {
		t.Errorf("Cache-Control 值数量不正确: 期望 2, 实际 %d", len(result["Cache-Control"]))
	}

	// 验证深拷贝
	result["Accept-Encoding"][0] = "modified"
	if original["Accept-Encoding"][0] == "modified" {
		t.Errorf("原始 Header 被意外修改")
	}
}

// ==================== Provider Header Config JSON 序列化测试 ====================

func TestProviderHeaderConfigSerialization(t *testing.T) {
	tests := []struct {
		name          string
		provider      Provider
		expectFields  []string // 期望存在的 JSON 字段
		expectMissing []string // 期望不存在的 JSON 字段（omitempty）
	}{
		{
			name: "完整 Header 配置",
			provider: Provider{
				ID:   1,
				Name: "test",
				ExtraHeaders: map[string]string{
					"X-Custom": "value",
				},
				OverrideHeaders: map[string]string{
					"Content-Type": "application/json",
				},
				StripHeaders: []string{"X-Forwarded-For"},
			},
			expectFields:  []string{"extraHeaders", "overrideHeaders", "stripHeaders"},
			expectMissing: []string{},
		},
		{
			name: "只有 ExtraHeaders",
			provider: Provider{
				ID:   2,
				Name: "test2",
				ExtraHeaders: map[string]string{
					"X-Custom": "value",
				},
			},
			expectFields:  []string{"extraHeaders"},
			expectMissing: []string{"overrideHeaders", "stripHeaders"},
		},
		{
			name: "空 Header 配置（omitempty）",
			provider: Provider{
				ID:   3,
				Name: "test3",
			},
			expectFields:  []string{},
			expectMissing: []string{"extraHeaders", "overrideHeaders", "stripHeaders"},
		},
		{
			name: "空 map 和 slice（omitempty 行为）",
			provider: Provider{
				ID:              4,
				Name:            "test4",
				ExtraHeaders:    map[string]string{},
				OverrideHeaders: map[string]string{},
				StripHeaders:    []string{},
			},
			// 空 map/slice 在 omitempty 下会被忽略
			expectMissing: []string{"extraHeaders", "overrideHeaders", "stripHeaders"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.provider)
			if err != nil {
				t.Fatalf("JSON 序列化失败: %v", err)
			}

			jsonStr := string(data)

			// 验证期望存在的字段
			for _, field := range tt.expectFields {
				if !containsString(jsonStr, `"`+field+`"`) {
					t.Errorf("期望 JSON 包含字段 %q，但实际 JSON: %s", field, jsonStr)
				}
			}

			// 验证期望不存在的字段
			for _, field := range tt.expectMissing {
				if containsString(jsonStr, `"`+field+`"`) {
					t.Errorf("期望 JSON 不包含字段 %q（omitempty），但实际 JSON: %s", field, jsonStr)
				}
			}
		})
	}
}

func TestProviderHeaderConfigDeserialization(t *testing.T) {
	jsonData := `{
		"id": 1,
		"name": "test-provider",
		"apiUrl": "https://api.example.com",
		"apiKey": "sk-xxx",
		"enabled": true,
		"extraHeaders": {
			"X-Custom-A": "value-a",
			"X-Custom-B": "value-b"
		},
		"overrideHeaders": {
			"Content-Type": "application/json"
		},
		"stripHeaders": ["X-Forwarded-For", "X-Real-Ip"]
	}`

	var provider Provider
	err := json.Unmarshal([]byte(jsonData), &provider)
	if err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}

	// 验证 ExtraHeaders
	if len(provider.ExtraHeaders) != 2 {
		t.Errorf("ExtraHeaders 数量不正确: 期望 2, 实际 %d", len(provider.ExtraHeaders))
	}
	if provider.ExtraHeaders["X-Custom-A"] != "value-a" {
		t.Errorf("ExtraHeaders[X-Custom-A] 期望 value-a, 实际 %s", provider.ExtraHeaders["X-Custom-A"])
	}

	// 验证 OverrideHeaders
	if len(provider.OverrideHeaders) != 1 {
		t.Errorf("OverrideHeaders 数量不正确: 期望 1, 实际 %d", len(provider.OverrideHeaders))
	}
	if provider.OverrideHeaders["Content-Type"] != "application/json" {
		t.Errorf("OverrideHeaders[Content-Type] 期望 application/json, 实际 %s", provider.OverrideHeaders["Content-Type"])
	}

	// 验证 StripHeaders
	if len(provider.StripHeaders) != 2 {
		t.Errorf("StripHeaders 数量不正确: 期望 2, 实际 %d", len(provider.StripHeaders))
	}
	if provider.StripHeaders[0] != "X-Forwarded-For" {
		t.Errorf("StripHeaders[0] 期望 X-Forwarded-For, 实际 %s", provider.StripHeaders[0])
	}
}

func TestProviderHeaderConfigRoundTrip(t *testing.T) {
	// 测试序列化 → 反序列化完整性
	original := Provider{
		ID:      12345,
		Name:    "roundtrip-test",
		APIURL:  "https://api.example.com",
		APIKey:  "sk-test-key",
		Enabled: true,
		Level:   2,
		ExtraHeaders: map[string]string{
			"X-Header-A": "value-a",
			"X-Header-B": "value-b",
		},
		OverrideHeaders: map[string]string{
			"Content-Type": "application/json",
			"Accept":       "application/json",
		},
		StripHeaders: []string{"X-Forwarded-For", "X-Real-Ip", "X-Forwarded-Host"},
	}

	// 序列化
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	// 反序列化
	var restored Provider
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	// 验证完整性
	if restored.ID != original.ID {
		t.Errorf("ID 不匹配")
	}
	if restored.Name != original.Name {
		t.Errorf("Name 不匹配")
	}
	if len(restored.ExtraHeaders) != len(original.ExtraHeaders) {
		t.Errorf("ExtraHeaders 长度不匹配")
	}
	for k, v := range original.ExtraHeaders {
		if restored.ExtraHeaders[k] != v {
			t.Errorf("ExtraHeaders[%s] 不匹配: 期望 %s, 实际 %s", k, v, restored.ExtraHeaders[k])
		}
	}
	if len(restored.OverrideHeaders) != len(original.OverrideHeaders) {
		t.Errorf("OverrideHeaders 长度不匹配")
	}
	for k, v := range original.OverrideHeaders {
		if restored.OverrideHeaders[k] != v {
			t.Errorf("OverrideHeaders[%s] 不匹配: 期望 %s, 实际 %s", k, v, restored.OverrideHeaders[k])
		}
	}
	if len(restored.StripHeaders) != len(original.StripHeaders) {
		t.Errorf("StripHeaders 长度不匹配")
	}
	for i, v := range original.StripHeaders {
		if restored.StripHeaders[i] != v {
			t.Errorf("StripHeaders[%d] 不匹配: 期望 %s, 实际 %s", i, v, restored.StripHeaders[i])
		}
	}
}

// ==================== 集成场景测试 ====================

func TestBuildForwardHeaders_RealWorldScenarios(t *testing.T) {
	scenarios := []struct {
		name     string
		original http.Header
		provider *Provider
		validate func(t *testing.T, result http.Header)
	}{
		{
			name: "场景1：OpenRouter 需要 HTTP-Referer",
			original: http.Header{
				"Content-Type": {"application/json"},
				"Accept":       {"*/*"},
			},
			provider: &Provider{
				Name: "OpenRouter",
				ExtraHeaders: map[string]string{
					"HTTP-Referer": "https://my-app.example.com",
					"X-Title":      "My AI App",
				},
			},
			validate: func(t *testing.T, result http.Header) {
				if result.Get("HTTP-Referer") != "https://my-app.example.com" {
					t.Error("HTTP-Referer 未正确添加")
				}
				if result.Get("X-Title") != "My AI App" {
					t.Error("X-Title 未正确添加")
				}
			},
		},
		{
			name: "场景2：非标准 API 需要强制 Content-Type",
			original: http.Header{
				"Content-Type": {"text/plain"},
			},
			provider: &Provider{
				Name: "LegacyAPI",
				OverrideHeaders: map[string]string{
					"Content-Type": "application/json",
				},
			},
			validate: func(t *testing.T, result http.Header) {
				if result.Get("Content-Type") != "application/json" {
					t.Error("Content-Type 未被正确覆盖")
				}
			},
		},
		{
			name: "场景3：移除可能导致问题的 Headers",
			original: http.Header{
				"Content-Type":    {"application/json"},
				"X-Forwarded-For": {"client-ip"},
				"X-Real-Ip":       {"real-ip"},
				"X-Request-Id":    {"req-123"},
			},
			provider: &Provider{
				Name:         "StrictAPI",
				StripHeaders: []string{"X-Forwarded-For", "X-Real-Ip"},
			},
			validate: func(t *testing.T, result http.Header) {
				if result.Get("X-Forwarded-For") != "" {
					t.Error("X-Forwarded-For 应被移除")
				}
				if result.Get("X-Real-Ip") != "" {
					t.Error("X-Real-Ip 应被移除")
				}
				if result.Get("X-Request-Id") != "req-123" {
					t.Error("X-Request-Id 应被保留")
				}
			},
		},
		{
			name: "场景4：复合配置",
			original: http.Header{
				"Content-Type":    {"text/plain"},
				"Accept":          {"text/html"},
				"X-Forwarded-For": {"192.168.1.1"},
				"X-Client-Ip":     {"10.0.0.1"},
			},
			provider: &Provider{
				Name:         "ComplexAPI",
				StripHeaders: []string{"X-Forwarded-For", "X-Client-Ip"},
				OverrideHeaders: map[string]string{
					"Content-Type": "application/json",
					"Accept":       "application/json",
				},
				ExtraHeaders: map[string]string{
					"X-Custom-Header": "custom-value",
					"Accept":          "should-not-apply", // 已被 Override 设置
				},
			},
			validate: func(t *testing.T, result http.Header) {
				// 验证 Strip
				if result.Get("X-Forwarded-For") != "" {
					t.Error("X-Forwarded-For 应被移除")
				}
				if result.Get("X-Client-Ip") != "" {
					t.Error("X-Client-Ip 应被移除")
				}
				// 验证 Override
				if result.Get("Content-Type") != "application/json" {
					t.Error("Content-Type 应被覆盖为 application/json")
				}
				if result.Get("Accept") != "application/json" {
					t.Error("Accept 应被覆盖为 application/json")
				}
				// 验证 Extra
				if result.Get("X-Custom-Header") != "custom-value" {
					t.Error("X-Custom-Header 应被添加")
				}
			},
		},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			result := buildForwardHeaders(sc.original, sc.provider)
			sc.validate(t, result)
		})
	}
}

// ==================== 性能测试 ====================

func BenchmarkBuildForwardHeaders_Simple(b *testing.B) {
	original := http.Header{
		"Content-Type": {"application/json"},
		"Accept":       {"*/*"},
	}
	provider := &Provider{Name: "test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buildForwardHeaders(original, provider)
	}
}

func BenchmarkBuildForwardHeaders_WithConfig(b *testing.B) {
	original := http.Header{
		"Content-Type":    {"application/json"},
		"Accept":          {"*/*"},
		"X-Forwarded-For": {"192.168.1.1"},
		"X-Real-Ip":       {"10.0.0.1"},
		"X-Request-Id":    {"req-123"},
	}
	provider := &Provider{
		Name:         "test",
		StripHeaders: []string{"X-Forwarded-For", "X-Real-Ip"},
		OverrideHeaders: map[string]string{
			"Content-Type": "application/json",
		},
		ExtraHeaders: map[string]string{
			"X-Custom": "value",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buildForwardHeaders(original, provider)
	}
}

func BenchmarkCloneHeaders(b *testing.B) {
	original := http.Header{
		"Content-Type":    {"application/json"},
		"Accept":          {"*/*"},
		"Accept-Encoding": {"gzip", "deflate", "br"},
		"Authorization":   {"Bearer sk-xxx"},
		"X-Api-Key":       {"sk-ant-xxx"},
		"X-Custom-A":      {"value-a"},
		"X-Custom-B":      {"value-b"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cloneHeaders(original)
	}
}
