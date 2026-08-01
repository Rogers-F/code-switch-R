package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// ==================== endpoint 模型段解析/重写 ====================

func TestExtractGeminiModelFromEndpointBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		expected string
	}{
		{"标准生成", "/v1beta/models/gemini-2.5-pro:generateContent", "gemini-2.5-pro"},
		{"流式+查询参数", "/v1beta/models/gemini-2.5-pro:streamGenerateContent?alt=sse", "gemini-2.5-pro"},
		{"无版本前缀", "models/gemini-2.5-flash:generateContent", "gemini-2.5-flash"},
		{"带斜杠的映射目标完整提取", "/v1beta/models/vendor/gemini-x:generateContent", "vendor/gemini-x"},
		{"notmodels 不得误匹配", "/v1beta/notmodels/foo:bar", ""},
		{"查询串里的 models 不算路径", "/v1beta/other?next=/models/x", ""},
		{"空模型段", "/v1beta/models/:generateContent", ""},
		{"无 models 段", "/v1beta/cachedContents", ""},
		{"空串", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractGeminiModelFromEndpoint(tt.endpoint); got != tt.expected {
				t.Errorf("extractGeminiModelFromEndpoint(%q) = %q, 期望 %q", tt.endpoint, got, tt.expected)
			}
		})
	}
}

func TestRewriteGeminiModelInEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		from     string
		to       string
		expected string
	}{
		{
			"重写保留动作与查询参数",
			"/v1beta/models/gemini-2.5-pro:streamGenerateContent?alt=sse",
			"gemini-2.5-pro", "vendor/gemini-x",
			"/v1beta/models/vendor/gemini-x:streamGenerateContent?alt=sse",
		},
		{
			"from 与路径段不一致时原样返回",
			"/v1beta/models/gemini-2.5-flash:generateContent",
			"gemini-2.5-pro", "vendor-x",
			"/v1beta/models/gemini-2.5-flash:generateContent",
		},
		{
			"from 为空原样返回",
			"/v1beta/models/gemini-2.5-pro:generateContent",
			"", "vendor-x",
			"/v1beta/models/gemini-2.5-pro:generateContent",
		},
		{
			"to 为空原样返回",
			"/v1beta/models/gemini-2.5-pro:generateContent",
			"gemini-2.5-pro", "",
			"/v1beta/models/gemini-2.5-pro:generateContent",
		},
		{
			"无 models 段原样返回",
			"/v1beta/cachedContents",
			"gemini-2.5-pro", "vendor-x",
			"/v1beta/cachedContents",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rewriteGeminiModelInEndpoint(tt.endpoint, tt.from, tt.to); got != tt.expected {
				t.Errorf("rewriteGeminiModelInEndpoint(%q, %q, %q) = %q, 期望 %q",
					tt.endpoint, tt.from, tt.to, got, tt.expected)
			}
		})
	}
}

// ==================== Gemini handler 级测试 ====================

// newGeminiTestRelay 构造带 GeminiService 的代理服务与路由
func newGeminiTestRelay(t *testing.T, providers []GeminiProvider) (*gin.Engine, *GeminiService) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	gs := NewGeminiService("127.0.0.1:18100", nil)
	for _, p := range providers {
		if err := gs.AddProvider(p); err != nil {
			t.Fatalf("添加 gemini provider 失败: %v", err)
		}
	}

	appSettings := NewAppSettingsService(NewAutoStartService())
	notificationService := NewNotificationService(appSettings)
	blacklistService := NewBlacklistService(NewSettingsService(), notificationService)
	prs := NewProviderRelayService(NewProviderService(), gs, blacklistService, notificationService, appSettings, "")

	router := gin.New()
	router.POST("/gemini/v1beta/*any", prs.geminiProxyHandler("/v1beta"))
	return router, gs
}

// 映射按 provider 隔离：A 的映射改写自己的上游路径，
// A 失败降级后 B 必须拿到未被污染的原始 endpoint
func TestGeminiHandlerMappingIsolatedPerProvider(t *testing.T) {
	setupGeminiTestHome(t)

	var gotPathA, gotPathB string
	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPathA = r.URL.Path
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer upstreamA.Close()
	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPathB = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"candidates":[]}`))
	}))
	defer upstreamB.Close()

	router, _ := newGeminiTestRelay(t, []GeminiProvider{
		{
			Name: "A", BaseURL: upstreamA.URL, APIKey: "key-a", Enabled: true, Level: 1,
			ModelMapping: map[string]string{"gemini-2.5-pro": "vendor-pro"},
		},
		{
			Name: "B", BaseURL: upstreamB.URL, APIKey: "key-b", Enabled: true, Level: 2,
		},
	})

	req := httptest.NewRequest("POST", "/gemini/v1beta/models/gemini-2.5-pro:generateContent",
		strings.NewReader(`{"contents":[]}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200（B 兜底成功），得到 %d: %s", w.Code, w.Body.String())
	}
	if gotPathA != "/v1beta/models/vendor-pro:generateContent" {
		t.Errorf("A 应收到映射后的路径，实际: %s", gotPathA)
	}
	if gotPathB != "/v1beta/models/gemini-2.5-pro:generateContent" {
		t.Errorf("B 应收到未映射的原始路径（不得被 A 污染），实际: %s", gotPathB)
	}
}

// 白名单不匹配的 provider 全部被跳过时按 claude 路径同款措辞回 404
func TestGeminiHandlerWhitelistFiltersAll(t *testing.T) {
	setupGeminiTestHome(t)

	router, _ := newGeminiTestRelay(t, []GeminiProvider{
		{
			Name: "OnlyFlash", BaseURL: "http://127.0.0.1:1", APIKey: "k", Enabled: true,
			SupportedModels: map[string]bool{"gemini-2.5-flash": true},
		},
	})

	req := httptest.NewRequest("POST", "/gemini/v1beta/models/gemini-2.5-pro:generateContent",
		strings.NewReader(`{"contents":[]}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("期望 404，得到 %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是 JSON: %v", err)
	}
	msg, _ := body["error"].(string)
	if !strings.Contains(msg, "gemini-2.5-pro") || !strings.Contains(msg, "白名单") {
		t.Errorf("404 文案应包含模型名与白名单不匹配的原因说明，实际: %s", msg)
	}
}

// 通配符映射展开结果不在白名单内时该 provider 必须被跳过
// （静态校验对通配符无能为力，只查请求模型会绕过明确声明的白名单）
func TestGeminiHandlerEffectiveModelRecheckedAgainstWhitelist(t *testing.T) {
	setupGeminiTestHome(t)

	router, _ := newGeminiTestRelay(t, []GeminiProvider{
		{
			Name: "StrictWhitelist", BaseURL: "http://127.0.0.1:1", APIKey: "k", Enabled: true,
			SupportedModels: map[string]bool{"vendor/gemini-flash": true, "gemini-*": true},
			ModelMapping:    map[string]string{"gemini-*": "vendor/gemini-*"},
		},
	})

	// gemini-pro 命中映射 gemini-* -> vendor/gemini-pro，但 vendor/gemini-pro 不在白名单
	req := httptest.NewRequest("POST", "/gemini/v1beta/models/gemini-pro:generateContent",
		strings.NewReader(`{"contents":[]}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("映射结果不在白名单时应 404，得到 %d: %s", w.Code, w.Body.String())
	}
}

// 恒等通配符映射（gemini-* -> gemini-*）不得绕过白名单：
// 映射 key 会让请求通过初筛，展开结果与请求相同也必须再过白名单
func TestGeminiHandlerIdentityWildcardMappingStillWhitelisted(t *testing.T) {
	setupGeminiTestHome(t)

	router, _ := newGeminiTestRelay(t, []GeminiProvider{
		{
			Name: "IdentityMap", BaseURL: "http://127.0.0.1:1", APIKey: "k", Enabled: true,
			SupportedModels: map[string]bool{"gemini-flash": true},
			ModelMapping:    map[string]string{"gemini-*": "gemini-*"},
		},
	})

	req := httptest.NewRequest("POST", "/gemini/v1beta/models/gemini-pro:generateContent",
		strings.NewReader(`{"contents":[]}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("恒等映射不得绕过白名单，期望 404，得到 %d: %s", w.Code, w.Body.String())
	}

	// 白名单内的模型经恒等映射仍应放行（进入调度，此处上游不可达返回 502 而非 404）
	req2 := httptest.NewRequest("POST", "/gemini/v1beta/models/gemini-flash:generateContent",
		strings.NewReader(`{"contents":[]}`))
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code == http.StatusNotFound {
		t.Fatalf("白名单内模型不应被过滤，得到 404: %s", w2.Body.String())
	}
}
