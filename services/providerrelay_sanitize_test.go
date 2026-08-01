package services

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

// slp 构造 *[]string 测试字面量；slp() 表示"显式空数组 = 什么都不删"。
func slp(items ...string) *[]string {
	v := append([]string{}, items...)
	return &v
}

// —— sanitizeRequestBody ——

func TestSanitizeRequestBodyDefaults(t *testing.T) {
	body := []byte(`{"model":"m1","prompt_caching":{"enabled":true},"messages":[{"role":"user","content":"hi"}]}`)
	cleaned, removed := sanitizeRequestBody(body, nil)

	if !reflect.DeepEqual(removed, []string{"prompt_caching"}) {
		t.Fatalf("expected to remove prompt_caching, got %v", removed)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(cleaned, &m); err != nil {
		t.Fatalf("cleaned body is not valid JSON: %v", err)
	}
	if _, ok := m["prompt_caching"]; ok {
		t.Fatal("prompt_caching should be removed")
	}
	// 非目标字段必须逐字节保留
	if string(m["messages"]) != `[{"role":"user","content":"hi"}]` {
		t.Fatalf("messages value changed: %s", m["messages"])
	}
	if string(m["model"]) != `"m1"` {
		t.Fatalf("model value changed: %s", m["model"])
	}
}

func TestSanitizeRequestBodyCustomList(t *testing.T) {
	cfg := &SanitizeConfig{BlockedBodyFields: slp("foo", "bar")}
	body := []byte(`{"foo":1,"bar":2,"baz":3,"prompt_caching":true}`)
	cleaned, removed := sanitizeRequestBody(body, cfg)

	if !reflect.DeepEqual(removed, []string{"bar", "foo"}) {
		t.Fatalf("expected [bar foo] removed (sorted), got %v", removed)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(cleaned, &m); err != nil {
		t.Fatal(err)
	}
	// 自定义列表生效时不再叠加内置默认：prompt_caching 保留
	if _, ok := m["prompt_caching"]; !ok {
		t.Fatal("custom list should fully replace defaults; prompt_caching must stay")
	}
	if _, ok := m["baz"]; !ok {
		t.Fatal("baz should stay")
	}
}

// 三态语义：nil = 用内置默认；空数组 = 什么都不删。
func TestSanitizeRequestBodyTriState(t *testing.T) {
	body := []byte(`{"prompt_caching":true,"x":1}`)

	// nil 列表 → 默认黑名单生效
	cleanedNil, removedNil := sanitizeRequestBody(body, &SanitizeConfig{})
	if len(removedNil) != 1 || removedNil[0] != "prompt_caching" {
		t.Fatalf("nil list should fall back to defaults, removed=%v", removedNil)
	}
	_ = cleanedNil

	// 显式空数组 → 不删任何字段
	cleanedEmpty, removedEmpty := sanitizeRequestBody(body, &SanitizeConfig{BlockedBodyFields: slp()})
	if len(removedEmpty) != 0 {
		t.Fatalf("empty list means remove nothing, removed=%v", removedEmpty)
	}
	if string(cleanedEmpty) != string(body) {
		t.Fatal("body must be untouched when list is explicitly empty")
	}
}

// 含点号的顶层键按字面量删除，不能当作嵌套路径误删。
func TestSanitizeRequestBodyDottedKey(t *testing.T) {
	cfg := &SanitizeConfig{BlockedBodyFields: slp("a.b")}
	body := []byte(`{"a.b":1,"a":{"b":2}}`)
	cleaned, removed := sanitizeRequestBody(body, cfg)

	if !reflect.DeepEqual(removed, []string{"a.b"}) {
		t.Fatalf("expected literal key a.b removed, got %v", removed)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(cleaned, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["a.b"]; ok {
		t.Fatal("literal a.b should be removed")
	}
	if string(m["a"]) != `{"b":2}` {
		t.Fatalf("nested a.b must not be touched, got %s", m["a"])
	}
}

// 顶层重复键的畸形 body 原样放行，不做任何改写。
func TestSanitizeRequestBodyDuplicateKeysPassthrough(t *testing.T) {
	body := []byte(`{"prompt_caching":1,"prompt_caching":2,"x":3}`)
	cleaned, removed := sanitizeRequestBody(body, nil)
	if len(removed) != 0 {
		t.Fatalf("duplicate-key body must pass through, removed=%v", removed)
	}
	if string(cleaned) != string(body) {
		t.Fatal("duplicate-key body must be byte-identical")
	}
}

func TestSanitizeRequestBodyNonObjectAndInvalid(t *testing.T) {
	for _, body := range []string{`[1,2,3]`, `"str"`, `not-json`} {
		cleaned, removed := sanitizeRequestBody([]byte(body), nil)
		if len(removed) != 0 || string(cleaned) != body {
			t.Fatalf("non-object body %q must pass through untouched", body)
		}
	}
}

func TestSanitizeRequestBodyNoMatchFastPath(t *testing.T) {
	body := []byte(`{"model":"m","messages":[]}`)
	cleaned, removed := sanitizeRequestBody(body, nil)
	if len(removed) != 0 {
		t.Fatalf("nothing should be removed, got %v", removed)
	}
	if &cleaned[0] != &body[0] {
		t.Fatal("fast path should return the original slice without rebuild")
	}
}

// —— sanitizeHeaders / cleanAnthropicBeta ——

func TestSanitizeHeadersBlockedAndBeta(t *testing.T) {
	cfg := &SanitizeConfig{BlockedHeaders: slp("x-custom-junk")}
	headers := map[string]string{
		"X-Custom-Junk":  "v", // 大小写不敏感命中
		"Content-Type":   "application/json",
		"Anthropic-Beta": "prompt-caching-scope-2026-01-05, context-1m-2025-08-07",
	}
	cleaned := sanitizeHeaders(headers, cfg)

	if _, ok := cleaned["X-Custom-Junk"]; ok {
		t.Fatal("blocked header should be removed case-insensitively")
	}
	if cleaned["Content-Type"] != "application/json" {
		t.Fatal("unrelated header must stay")
	}
	// beta 值走默认黑名单（cfg.BlockedBetaValues 为 nil）
	if cleaned["Anthropic-Beta"] != "context-1m-2025-08-07" {
		t.Fatalf("beta value not cleaned correctly: %q", cleaned["Anthropic-Beta"])
	}
}

func TestSanitizeHeadersBetaBecomesEmpty(t *testing.T) {
	headers := map[string]string{
		"anthropic-beta": "prompt-caching-scope-2026-01-05, redact-thinking-2026-02-12",
	}
	cleaned := sanitizeHeaders(headers, nil)
	if _, ok := cleaned["anthropic-beta"]; ok {
		t.Fatal("beta header must be dropped entirely when all values are blocked")
	}
}

func TestSanitizeHeadersEmptyListMeansKeepAll(t *testing.T) {
	cfg := &SanitizeConfig{BlockedBetaValues: slp()}
	headers := map[string]string{"anthropic-beta": "prompt-caching-scope-2026-01-05"}
	cleaned := sanitizeHeaders(headers, cfg)
	if cleaned["anthropic-beta"] != "prompt-caching-scope-2026-01-05" {
		t.Fatal("explicit empty beta blocklist means keep everything")
	}
}

func TestCleanAnthropicBetaSpacing(t *testing.T) {
	blocked := map[string]bool{"b": true}
	if got := cleanAnthropicBeta(" a , b ,, c ", blocked); got != "a, c" {
		t.Fatalf("expected %q, got %q", "a, c", got)
	}
	if got := cleanAnthropicBeta("b", blocked); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestSanitizeHTTPHeaders(t *testing.T) {
	cfg := &SanitizeConfig{BlockedHeaders: slp("x-stainless-lang")}
	h := http.Header{}
	h.Set("X-Stainless-Lang", "go")
	h.Set("Anthropic-Beta", "redact-thinking-2026-02-12, keep-me")
	h.Set("Accept", "application/json")

	sanitizeHTTPHeaders(h, cfg)

	if h.Get("X-Stainless-Lang") != "" {
		t.Fatal("blocked header should be deleted")
	}
	if h.Get("Anthropic-Beta") != "keep-me" {
		t.Fatalf("beta not cleaned: %q", h.Get("Anthropic-Beta"))
	}
	if h.Get("Accept") != "application/json" {
		t.Fatal("unrelated header must stay")
	}
}

// 同名 anthropic-beta 头有多个值时逐个清理，不能只处理第一个、丢弃其余合法值。
func TestSanitizeHTTPHeadersMultiValueBeta(t *testing.T) {
	h := http.Header{}
	h.Add("Anthropic-Beta", "redact-thinking-2026-02-12")
	h.Add("Anthropic-Beta", "keep-me-1")
	h.Add("Anthropic-Beta", "prompt-caching-scope-2026-01-05, keep-me-2")

	sanitizeHTTPHeaders(h, nil)

	vals := h.Values("Anthropic-Beta")
	if len(vals) != 2 || vals[0] != "keep-me-1" || vals[1] != "keep-me-2" {
		t.Fatalf("multi-value beta not cleaned per value: %v", vals)
	}

	// 全部被黑名单吃掉时整个头删除
	h2 := http.Header{}
	h2.Add("Anthropic-Beta", "redact-thinking-2026-02-12")
	h2.Add("Anthropic-Beta", "prompt-caching-scope-2026-01-05")
	sanitizeHTTPHeaders(h2, nil)
	if len(h2.Values("Anthropic-Beta")) != 0 {
		t.Fatalf("fully-blocked multi-value beta should be dropped, got %v", h2.Values("Anthropic-Beta"))
	}
}

// —— 端到端出站捕获：经 forwardRequest 实际转发，验证清理作用于真实出站请求 ——

func TestForwardRequestSanitizesOutbound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = setupRenameTestEnv(t)

	type captured struct {
		body    []byte
		junk    []string
		beta    []string
		apiKey  []string
	}
	var got captured

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.body, _ = io.ReadAll(r.Body)
		got.junk = r.Header.Values("X-Junk")
		got.beta = r.Header.Values("Anthropic-Beta")
		got.apiKey = r.Header.Values("X-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	prs := newTestRelayService(NewProviderService())

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"m","prompt_caching":true,"messages":[]}`)
	req, err := http.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.Header.Set("X-Junk", "should-be-removed")
	req.Header.Set("Anthropic-Beta", "prompt-caching-scope-2026-01-05, real-beta")
	c.Request = req

	provider := Provider{
		Name:                   "sanitize-me",
		APIURL:                 upstream.URL,
		APIKey:                 "provider-secret",
		Enabled:                true,
		ConnectivityAuthType:   "x-api-key",
		RequestSanitizeEnabled: true,
		SanitizeConfig:         &SanitizeConfig{BlockedHeaders: slp("x-junk")},
	}
	ok, ferr := prs.forwardRequest(c, "claude", provider, "/v1/messages",
		map[string]string{}, cloneHeaders(req.Header), body, false, "m")
	if !ok {
		t.Fatalf("转发应成功,实际失败: %v", ferr)
	}

	// 请求体：默认黑名单删掉 prompt_caching，其余保留
	var outBody map[string]json.RawMessage
	if err := json.Unmarshal(got.body, &outBody); err != nil {
		t.Fatalf("出站 body 不是合法 JSON: %v", err)
	}
	if _, exists := outBody["prompt_caching"]; exists {
		t.Error("出站 body 应已删除 prompt_caching")
	}
	if string(outBody["model"]) != `"m"` {
		t.Errorf("出站 body 的 model 被改动: %s", outBody["model"])
	}

	// 请求头：自定义黑名单删 x-junk；beta 值走默认黑名单只留合法值；注入的凭据不受清理影响
	if len(got.junk) != 0 {
		t.Errorf("出站不应携带 X-Junk,实际 %v", got.junk)
	}
	if len(got.beta) != 1 || got.beta[0] != "real-beta" {
		t.Errorf("anthropic-beta 应只剩 real-beta,实际 %v", got.beta)
	}
	if len(got.apiKey) != 1 || got.apiKey[0] != "provider-secret" {
		t.Errorf("供应商凭据应完好注入,实际 %v", got.apiKey)
	}
}

// 复制供应商必须带上 TLS 跳验与请求清理配置，且 SanitizeConfig 为深拷贝。
func TestDuplicateProviderCopiesTLSAndSanitize(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()

	source := Provider{
		ID:                     1,
		Name:                   "src",
		APIURL:                 "https://api.example.com",
		APIKey:                 "sk",
		Enabled:                true,
		InsecureSkipVerify:     true,
		RequestSanitizeEnabled: true,
		SanitizeConfig: &SanitizeConfig{
			BlockedBodyFields: slp("foo"),
			BlockedHeaders:    slp(),
		},
	}
	if err := ps.SaveProviders("claude", []Provider{source}); err != nil {
		t.Fatalf("保存夹具失败: %v", err)
	}

	cloned, err := ps.DuplicateProvider("claude", source.ID)
	if err != nil {
		t.Fatalf("DuplicateProvider 失败: %v", err)
	}

	if !cloned.InsecureSkipVerify {
		t.Error("副本应保留 InsecureSkipVerify")
	}
	if !cloned.RequestSanitizeEnabled {
		t.Error("副本应保留 RequestSanitizeEnabled")
	}
	if cloned.SanitizeConfig == nil {
		t.Fatal("副本应保留 SanitizeConfig")
	}
	if got := cfgBodyFields(cloned.SanitizeConfig); len(got) != 1 || got[0] != "foo" {
		t.Errorf("BlockedBodyFields 复制不完整: %v", got)
	}
	// 三态：显式空数组保持为空数组指针，不能退化成 nil（= 用默认）
	if cloned.SanitizeConfig.BlockedHeaders == nil || len(*cloned.SanitizeConfig.BlockedHeaders) != 0 {
		t.Errorf("显式空 BlockedHeaders 应保持空数组指针: %v", cloned.SanitizeConfig.BlockedHeaders)
	}
	// 深拷贝：改副本不影响源
	(*cloned.SanitizeConfig.BlockedBodyFields)[0] = "mutated"
	if (*source.SanitizeConfig.BlockedBodyFields)[0] != "foo" {
		t.Error("SanitizeConfig 应为深拷贝，副本修改不能影响源")
	}
}
