package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daodao97/xgo/xrequest"
	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"
)

// TestSanitizeUpstreamHeadersDropsClientCredentials 客户端自带的认证头必须在转发前清掉,
// 否则用户本机的真实 API Key 会随请求发给链路上每一个第三方供应商。
// cloneHeaders 保留的是 Go 规范化后的键名(X-Api-Key),小写字面量 delete 删不掉。
func TestSanitizeUpstreamHeadersDropsClientCredentials(t *testing.T) {
	headers := map[string]string{
		"X-Api-Key":       "client-real-key",
		"Authorization":   "Bearer client-token",
		"Api-Key":         "another",
		"X-Goog-Api-Key":  "gemini-key",
		"Accept-Encoding": "gzip, deflate",
		"Connection":      "keep-alive",
		"Content-Type":    "application/json",
		"Anthropic-Beta":  "prompt-caching-2024-07-31",
	}

	sanitizeUpstreamHeaders(headers)

	for _, name := range []string{"x-api-key", "authorization", "api-key", "x-goog-api-key", "accept-encoding", "connection"} {
		if got := getHeaderFold(headers, name); got != "" {
			t.Errorf("头 %s 应被清理,实际仍为 %q", name, got)
		}
	}
	// 业务头必须保留:Anthropic-Beta 承载 prompt caching 等特性开关
	if headers["Content-Type"] != "application/json" {
		t.Errorf("Content-Type 被误删")
	}
	if headers["Anthropic-Beta"] == "" {
		t.Errorf("Anthropic-Beta 被误删,会丢失上游特性开关")
	}
}

// TestSetHeaderCanonicalReplacesOtherCasing 注入供应商凭据时必须覆盖客户端同名头的其它大小写形式。
// xrequest 是 req.Header[k] = []string{v} 直接赋值不做规范化,
// 若残留 X-Api-Key 与新写的 x-api-key,两个条目会同时发到上游。
func TestSetHeaderCanonicalReplacesOtherCasing(t *testing.T) {
	headers := map[string]string{"X-Api-Key": "client-key"}

	setHeaderCanonical(headers, "x-api-key", "provider-key")

	if len(headers) != 1 {
		t.Fatalf("期望只剩 1 个 x-api-key 条目,实际 %d 个: %v", len(headers), headers)
	}
	if headers["X-Api-Key"] != "provider-key" {
		t.Errorf("期望规范化键 X-Api-Key=provider-key,实际 %v", headers)
	}
}

// TestForwardRequestSendsOnlyProviderCredentials 端到端确认转发到上游的请求里
// 只有本代理注入的供应商凭据,且没有把客户端 Accept-Encoding 透传出去。
func TestForwardRequestSendsOnlyProviderCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tmpHome := setupRenameTestEnv(t)
	_ = tmpHome

	type captured struct {
		apiKeyValues   []string
		authorization  []string
		acceptEncoding string
	}
	var got captured

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.apiKeyValues = r.Header.Values("X-Api-Key")
		got.authorization = r.Header.Values("Authorization")
		got.acceptEncoding = r.Header.Get("Accept-Encoding")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	prs := newTestRelayService(NewProviderService())

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req, err := http.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	// 客户端带上自己的凭据与压缩协商,模拟 Claude Code 的真实请求头
	req.Header.Set("X-Api-Key", "client-real-key")
	req.Header.Set("Authorization", "Bearer client-token")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	c.Request = req

	provider := Provider{
		Name:                 "p1",
		APIURL:               upstream.URL,
		APIKey:               "provider-secret",
		Enabled:              true,
		ConnectivityAuthType: "x-api-key",
	}
	ok, ferr := prs.forwardRequest(c, "claude", provider, "/v1/messages",
		map[string]string{}, cloneHeaders(req.Header), []byte(`{"model":"m"}`), false, "m", 0)
	if !ok {
		t.Fatalf("转发应成功,实际失败: %v", ferr)
	}

	if len(got.apiKeyValues) != 1 {
		t.Errorf("上游应只收到 1 个 x-api-key,实际 %d 个: %v", len(got.apiKeyValues), got.apiKeyValues)
	}
	if len(got.apiKeyValues) > 0 && got.apiKeyValues[0] != "provider-secret" {
		t.Errorf("x-api-key 应为供应商密钥,实际 %q", got.apiKeyValues[0])
	}
	for _, v := range got.apiKeyValues {
		if v == "client-real-key" {
			t.Errorf("客户端真实密钥泄漏到上游")
		}
	}
	if len(got.authorization) != 0 {
		t.Errorf("x-api-key 认证模式下不应残留客户端 Authorization,实际 %v", got.authorization)
	}
	// Go 会自行加 Accept-Encoding: gzip 并自动解压;透传客户端的值会让响应体保持压缩,
	// SSE 与 usage 解析随之失效
	if strings.Contains(got.acceptEncoding, "deflate") {
		t.Errorf("客户端 Accept-Encoding 被透传到上游: %q", got.acceptEncoding)
	}
}

// TestIsClientSideUpstreamStatus 上游 4xx 的分类:请求内容问题不该计入供应商失败,
// 而密钥失效/路径配错/限流仍属供应商侧。
func TestIsClientSideUpstreamStatus(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{http.StatusBadRequest, true},
		{http.StatusRequestEntityTooLarge, true},
		{http.StatusUnprocessableEntity, true},
		{http.StatusUnsupportedMediaType, true},
		{http.StatusUnauthorized, false},
		{http.StatusForbidden, false},
		{http.StatusNotFound, false},
		{http.StatusTooManyRequests, false},
		{http.StatusInternalServerError, false},
		{http.StatusBadGateway, false},
		{http.StatusOK, false},
	}
	for _, tc := range cases {
		if got := isClientSideUpstreamStatus(tc.status); got != tc.want {
			t.Errorf("isClientSideUpstreamStatus(%d) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

// TestMaskSensitiveQuery Gemini 端点常带 ?key=<API Key>,日志必须脱敏。
func TestMaskSensitiveQuery(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/v1beta/models/gemini-2.5-pro:generateContent", "/v1beta/models/gemini-2.5-pro:generateContent"},
		{"/v1beta/x?alt=sse&key=AIzaSecret", "/v1beta/x?alt=sse&key=***"},
		{"/v1beta/x?KEY=AIzaSecret", "/v1beta/x?KEY=***"},
		{"/v1beta/x?access_token=tok&alt=sse", "/v1beta/x?access_token=***&alt=sse"},
	}
	for _, tc := range cases {
		if got := maskSensitiveQuery(tc.in); got != tc.want {
			t.Errorf("maskSensitiveQuery(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestCodexUsageFromNonStreamingResponse 非流式 /responses 直接返回 Response 对象,
// usage 在根级而非 response.usage,漏解析会让 token 与成本全部记 0。
func TestCodexUsageFromNonStreamingResponse(t *testing.T) {
	body := `{"id":"resp_1","object":"response","service_tier":"flex","usage":{
		"input_tokens":1000,"output_tokens":300,
		"input_tokens_details":{"cached_tokens":400},
		"output_tokens_details":{"reasoning_tokens":120}}}`

	usage := &ReqeustLog{}
	CodexParseTokenUsageFromResponse(body, usage)

	if usage.InputTokens != 600 {
		t.Errorf("InputTokens = %d, want 600(1000-400 缓存读取)", usage.InputTokens)
	}
	if usage.CacheReadTokens != 400 {
		t.Errorf("CacheReadTokens = %d, want 400", usage.CacheReadTokens)
	}
	if usage.ReasoningTokens != 120 {
		t.Errorf("ReasoningTokens = %d, want 120", usage.ReasoningTokens)
	}
	// output_tokens 已含 reasoning_tokens,计费引擎是 OutputCost+ReasoningCost 相加,
	// 不拆开会把推理 token 计两次
	if usage.OutputTokens != 180 {
		t.Errorf("OutputTokens = %d, want 180(300-120 推理),否则推理 token 被重复计费", usage.OutputTokens)
	}
	if usage.ServiceTier == "" {
		t.Errorf("根级 service_tier 未被解析")
	}
}

// TestCodexUsageStreamingStillParsed 流式 response.completed 事件的口径不能被上面的修复破坏。
func TestCodexUsageStreamingStillParsed(t *testing.T) {
	body := `{"type":"response.completed","response":{"service_tier":"default","usage":{
		"input_tokens":50,"output_tokens":20,
		"output_tokens_details":{"reasoning_tokens":8}}}}`

	usage := &ReqeustLog{}
	CodexParseTokenUsageFromResponse(body, usage)

	if usage.InputTokens != 50 || usage.OutputTokens != 12 || usage.ReasoningTokens != 8 {
		t.Errorf("流式解析结果不符: input=%d output=%d reasoning=%d, want 50/12/8",
			usage.InputTokens, usage.OutputTokens, usage.ReasoningTokens)
	}
}

// TestGeminiUsageTotalFallbackExcludesThoughts totalTokenCount 含 thoughtsTokenCount,
// 直接 total-prompt 会把思考 token 也算进输出,与单独入库的 ReasoningTokens 重复计费。
func TestGeminiUsageTotalFallbackExcludesThoughts(t *testing.T) {
	data := `{"usageMetadata":{"promptTokenCount":100,"thoughtsTokenCount":40,"totalTokenCount":200}}`

	usage := &ReqeustLog{}
	GeminiParseTokenUsageFromResponse(data, usage)

	if usage.ReasoningTokens != 40 {
		t.Errorf("ReasoningTokens = %d, want 40", usage.ReasoningTokens)
	}
	if usage.OutputTokens != 60 {
		t.Errorf("OutputTokens = %d, want 60(200-100-40),否则思考 token 被重复计费", usage.OutputTokens)
	}
}

// TestGeminiUsageKeepsExplicitCandidates 上游给出 candidatesTokenCount 时不走估算分支。
func TestGeminiUsageKeepsExplicitCandidates(t *testing.T) {
	data := `{"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":70,"thoughtsTokenCount":40,"totalTokenCount":210}}`

	usage := &ReqeustLog{}
	GeminiParseTokenUsageFromResponse(data, usage)

	if usage.OutputTokens != 70 {
		t.Errorf("OutputTokens = %d, want 70(上游显式值)", usage.OutputTokens)
	}
}

// TestAnthropicSSEConverterEmitsEventLines Anthropic 流式规范要求每个事件带 "event: <类型>" 行,
// 官方 anthropic SDK(Claude Code 使用)按事件名分发,只有 data: 行的流会被整体丢弃。
func TestAnthropicSSEConverterEmitsEventLines(t *testing.T) {
	conv := NewOpenAIToAnthropicSSEConverter("test-model")

	out := conv.ProcessLine(`data: {"choices":[{"delta":{"content":"hi"}}]}`)
	for _, want := range []string{"event: message_start", "event: content_block_start", "event: content_block_delta"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少 %q\n实际输出:\n%s", want, out)
		}
	}
	// data: 行必须仍然存在,代理自身的 usage 解析只看 data:
	if !strings.Contains(out, "data: {") {
		t.Errorf("输出缺少 data: 行:\n%s", out)
	}

	done := conv.ProcessLine("data: [DONE]")
	for _, want := range []string{"event: content_block_stop", "event: message_delta", "event: message_stop"} {
		if !strings.Contains(done, want) {
			t.Errorf("终止事件缺少 %q\n实际输出:\n%s", want, done)
		}
	}

	// 每个事件块的 event: 行必须紧跟一个 data: 行
	for _, block := range strings.Split(strings.TrimSpace(out+done), "\n\n") {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		if len(lines) != 2 || !strings.HasPrefix(lines[0], "event: ") || !strings.HasPrefix(lines[1], "data: ") {
			t.Errorf("事件块格式不符 SSE 规范: %q", block)
		}
	}
}

// TestAnthropicSSEConverterFinalizeOnAbruptEnd 上游未发 [DONE] 就断流时必须补齐终止事件,
// 否则客户端一直等 message_stop,且 message_delta 里的 usage 也随之丢失。
func TestAnthropicSSEConverterFinalizeOnAbruptEnd(t *testing.T) {
	conv := NewOpenAIToAnthropicSSEConverter("test-model")
	conv.ProcessLine(`data: {"usage":{"prompt_tokens":30,"completion_tokens":7},"choices":[{"delta":{"content":"hi"}}]}`)

	tail := conv.FinalizeIfUnterminated()
	if !strings.Contains(tail, "event: message_stop") {
		t.Fatalf("断流后应补齐 message_stop,实际:\n%s", tail)
	}

	// 补齐的 message_delta 必须带上已捕获的 usage,供计费落库
	usage := &ReqeustLog{}
	parseEventPayload(tail, ClaudeCodeParseTokenUsageFromResponse, usage)
	if usage.InputTokens != 30 || usage.OutputTokens != 7 {
		t.Errorf("补齐事件未带上 usage: input=%d output=%d, want 30/7", usage.InputTokens, usage.OutputTokens)
	}

	// 已终止后重复调用不应再输出
	if again := conv.FinalizeIfUnterminated(); again != "" {
		t.Errorf("重复 finalize 应返回空串,实际:\n%s", again)
	}
}

// TestAnthropicSSEConverterFinalizeNoopAfterDone 正常收到 [DONE] 后不应重复补事件。
func TestAnthropicSSEConverterFinalizeNoopAfterDone(t *testing.T) {
	conv := NewOpenAIToAnthropicSSEConverter("test-model")
	conv.ProcessLine(`data: {"choices":[{"delta":{"content":"hi"}}]}`)
	conv.ProcessLine("data: [DONE]")

	if tail := conv.FinalizeIfUnterminated(); tail != "" {
		t.Errorf("已正常终止,不应再补事件,实际:\n%s", tail)
	}
}

// TestEnsureRequestLogCreatedAtOnLegacyTable SQLite 不允许 ALTER TABLE ADD COLUMN 带
// CURRENT_TIMESTAMP 这类非常量默认值。建表时没有 created_at 的旧库若走通用迁移会直接失败,
// 让 InitDatabase 报错、应用无法启动;迁移后新插入的行还必须能拿到时间戳。
func TestEnsureRequestLogCreatedAtOnLegacyTable(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	// 旧库:没有 created_at 列,且已有数据(空表不会触发 SQLite 的非常量默认值限制)
	if _, err := db.Exec(`CREATE TABLE request_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT, platform TEXT, model TEXT, provider TEXT)`); err != nil {
		t.Fatalf("建旧表失败: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO request_log (platform) VALUES ('claude')`); err != nil {
		t.Fatalf("写历史数据失败: %v", err)
	}

	if err := ensureRequestLogCreatedAt(db); err != nil {
		t.Fatalf("created_at 迁移失败(旧库将无法启动应用): %v", err)
	}

	// 历史行必须被回填
	var historical sql.NullString
	if err := db.QueryRow(`SELECT created_at FROM request_log WHERE id = 1`).Scan(&historical); err != nil {
		t.Fatalf("查询历史行失败: %v", err)
	}
	if !historical.Valid || historical.String == "" {
		t.Errorf("历史行 created_at 未回填")
	}

	// 迁移出来的列没有默认值,新插入行要靠触发器补时间戳,
	// 否则按时间统计的用量与成本全部失效
	if _, err := db.Exec(`INSERT INTO request_log (platform) VALUES ('codex')`); err != nil {
		t.Fatalf("插入新行失败: %v", err)
	}
	var fresh sql.NullString
	if err := db.QueryRow(`SELECT created_at FROM request_log WHERE platform = 'codex'`).Scan(&fresh); err != nil {
		t.Fatalf("查询新行失败: %v", err)
	}
	if !fresh.Valid || fresh.String == "" {
		t.Errorf("迁移后新插入行的 created_at 为 NULL,时间维度统计会失效")
	}

	// 幂等:重复迁移不应报错
	if err := ensureRequestLogCreatedAt(db); err != nil {
		t.Errorf("重复迁移应幂等,实际报错: %v", err)
	}
}

// TestEnsureRequestLogTableOnFreshDB 新建库走完整建表 + 迁移路径应无错,
// 且 created_at 默认值仍然生效。
func TestEnsureRequestLogTableOnFreshDB(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	if err := ensureRequestLogTableWithDB(db); err != nil {
		t.Fatalf("新库建表失败: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO request_log (platform) VALUES ('claude')`); err != nil {
		t.Fatalf("插入失败: %v", err)
	}
	var createdAt sql.NullString
	if err := db.QueryRow(`SELECT created_at FROM request_log`).Scan(&createdAt); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if !createdAt.Valid || createdAt.String == "" {
		t.Errorf("新库 created_at 应由默认值填充")
	}

	// service_tier 等后续追加列也应就位
	for _, col := range []string{"ephemeral_5m_tokens", "ephemeral_1h_tokens", "service_tier"} {
		exists, err := requestLogColumnExists(db, col)
		if err != nil {
			t.Fatalf("查询列 %s 失败: %v", col, err)
		}
		if !exists {
			t.Errorf("列 %s 缺失", col)
		}
	}
}

// TestRelayHTTPClientReusesTransport 转发必须共用连接池。
// xrequest 的默认路径每次调用都新建 http.Client 与 http.Transport,
// 连接零复用、空闲连接与读写协程长期滞留。
func TestRelayHTTPClientReusesTransport(t *testing.T) {
	if relayHTTPClient == nil || relayHTTPClient.Transport == nil {
		t.Fatal("relayHTTPClient 未配置共享 Transport")
	}
	transport, ok := relayHTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport 类型异常: %T", relayHTTPClient.Transport)
	}
	if transport.MaxIdleConnsPerHost <= 1 {
		t.Errorf("MaxIdleConnsPerHost = %d,连接无法有效复用", transport.MaxIdleConnsPerHost)
	}
	if transport.IdleConnTimeout == 0 {
		t.Errorf("IdleConnTimeout 未设置,空闲连接不会被回收")
	}
	if relayHTTPClient.Timeout == 0 {
		t.Errorf("客户端未设置兜底超时")
	}
}

// TestProxyHandlerRejectsInvalidJSONBody 空 body / 非法 JSON 会被每个上游各拒一次,
// 还会白耗一轮降级,应在入口直接挡掉。
func TestProxyHandlerRejectsInvalidJSONBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupRenameTestEnv(t)

	prs := newTestRelayService(NewProviderService())
	router := gin.New()
	router.POST("/v1/messages", prs.proxyHandler("claude", "/v1/messages"))

	for _, body := range []string{"", "not-json", "{\"model\":"} {
		recorder := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/v1/messages", strings.NewReader(body))
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("body=%q 期望 400,实际 %d", body, recorder.Code)
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Errorf("body=%q 响应不是 JSON: %v", body, err)
		}
	}
}

// TestStripCredentialQueryParams Gemini REST 支持 ?key=<API Key>,原样转发会把用户本机的
// 真实 Key 发给降级链上每一个第三方供应商;非凭据参数(alt=sse 等)必须原样保留。
func TestStripCredentialQueryParams(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"空查询串", "", ""},
		{"剔除 key", "alt=sse&key=AIzaSecret", "alt=sse"},
		{"大小写不敏感", "KEY=AIzaSecret&alt=sse", "alt=sse"},
		{"剔除多种凭据", "key=a&access_token=b&api_key=c&alt=sse&token=d", "alt=sse"},
		{"只有凭据时清空", "key=AIzaSecret", ""},
		{"非凭据参数原样保留", "alt=sse&pageSize=10", "alt=sse&pageSize=10"},
		{"值中的等号与编码不被改写", "alt=sse&filter=a%3Db%3Dc", "alt=sse&filter=a%3Db%3Dc"},
		{"无值参数保留", "prettyPrint&alt=sse", "prettyPrint&alt=sse"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripCredentialQueryParams(tc.in); got != tc.want {
				t.Errorf("stripCredentialQueryParams(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestRespondAllProvidersFailedStatus 全部供应商都以"请求内容有问题"拒绝时必须回 4xx。
// 回 502 会让 SDK 按服务端故障自动重试,一个永远不可能成功的坏请求被反复重发,
// 每次都完整扫一遍全部供应商、白耗上游配额。
func TestRespondAllProvidersFailedStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name            string
		lastError       error
		allClientErrors bool
		wantStatus      int
	}{
		{"供应商故障回 502", errors.New("upstream status 503"), true, http.StatusBadGateway},
		{"全部都是请求内容被拒回 400", fmt.Errorf("%w: upstream status 400", errUpstreamClientError), true, http.StatusBadRequest},
		// 混合失败：降级链末尾那个挑剔的备用供应商回 400，不能掩盖前面"临时过载、稍后可用"的供应商。
		// 回 400 会让 SDK 放弃重试，用户拿到"请求格式有问题"，而请求对前一个供应商完全合法
		{"混合失败维持 502", fmt.Errorf("%w: upstream status 400", errUpstreamClientError), false, http.StatusBadGateway},
		{"客户端中断按供应商故障口径", fmt.Errorf("%w: canceled", errClientAbort), true, http.StatusBadGateway},
		{"无错误信息回 502", nil, true, http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request, _ = http.NewRequest("POST", "/v1/messages", nil)

			respondAllProvidersFailed(c, tc.lastError, tc.allClientErrors, gin.H{"error": "all failed"})

			if recorder.Code != tc.wantStatus {
				t.Errorf("状态码 = %d, want %d", recorder.Code, tc.wantStatus)
			}
		})
	}
}

// TestForwardRequestDegradesWhenNothingWritten 上游返回 2xx 但响应体在写出任何字节之前就读失败时,
// 仍应按普通失败上报以便降级到下一个供应商;判成"已部分写出"会白白放弃可用的供应商,
// 客户端只拿到一个空的 200。
func TestForwardRequestDegradesWhenNothingWritten(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupRenameTestEnv(t)

	// 上游声明 SSE 且给出 Content-Length,但只写一半就断开,让读取阶段失败
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("da"))
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
	}))
	defer upstream.Close()

	prs := newTestRelayService(NewProviderService())
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request, _ = http.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))

	provider := Provider{Name: "p1", APIURL: upstream.URL, APIKey: "k", Enabled: true}
	ok, err := prs.forwardRequest(c, "claude", provider, "/v1/messages",
		map[string]string{}, map[string]string{}, []byte(`{"model":"m"}`), true, "m", 0)

	if ok {
		t.Fatalf("上游中途断开不应判为成功")
	}
	// 关键：没有写出任何字节时不能标成 errUpstreamStreamAborted，否则调用方会放弃降级
	if c.Writer.Written() {
		t.Skip("本次上游在断开前已写出字节，不构成待测场景")
	}
	if errors.Is(err, errUpstreamStreamAborted) {
		t.Errorf("未写出任何字节却被判为已部分写出，调用方会放弃降级: %v", err)
	}
}

// TestRelayConnectAddressNormalizesBindAddress 绑定地址不能直接当连接地址写进 CLI 配置：
// lan 模式绑的是 0.0.0.0，把它写成 base_url 客户端根本连不上
// （Windows 上 connect 0.0.0.0 直接返回 WSAEADDRNOTAVAIL）。
func TestRelayConnectAddressNormalizesBindAddress(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"回环原样保留", "127.0.0.1:18100", "127.0.0.1:18100"},
		{"全网卡归一为回环", "0.0.0.0:18100", "127.0.0.1:18100"},
		{"IPv6 全网卡归一为回环", "[::]:18100", "127.0.0.1:18100"},
		{"省略 host 归一为回环", ":18100", "127.0.0.1:18100"},
		{"具体网卡地址保留", "172.20.16.1:18100", "172.20.16.1:18100"},
		{"非法地址回落", "not-an-address", "127.0.0.1:18100"},
		{"空串回落", "", "127.0.0.1:18100"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RelayConnectAddress(tc.in); got != tc.want {
				t.Errorf("RelayConnectAddress(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestResolveRelayListenAddressFallsBackToLoopback 网络设置缺失或损坏时必须回落到仅回环，
// 绝不能意外绑到全网卡把带供应商密钥的代理暴露出去。
func TestResolveRelayListenAddressFallsBackToLoopback(t *testing.T) {
	const loopback = "127.0.0.1:18100"

	t.Run("设置文件不存在", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		t.Setenv("USERPROFILE", tmp)
		assertHomeIsolated(t, tmp)

		if got := ResolveRelayListenAddress(); got != loopback {
			t.Errorf("无设置文件时应回落到 %s，实际 %s", loopback, got)
		}
	})

	t.Run("设置文件损坏", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		t.Setenv("USERPROFILE", tmp)
		assertHomeIsolated(t, tmp)

		dir := filepath.Join(tmp, appSettingsDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, networkSettingsFile), []byte("{not json"), 0o644); err != nil {
			t.Fatalf("写损坏设置失败: %v", err)
		}

		if got := ResolveRelayListenAddress(); got != loopback {
			t.Errorf("设置损坏时应回落到 %s，实际 %s", loopback, got)
		}
	})

	t.Run("显式 lan 模式绑全网卡", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		t.Setenv("USERPROFILE", tmp)
		assertHomeIsolated(t, tmp)

		dir := filepath.Join(tmp, appSettingsDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, networkSettingsFile),
			[]byte(`{"listenMode":"lan"}`), 0o644); err != nil {
			t.Fatalf("写设置失败: %v", err)
		}

		got := ResolveRelayListenAddress()
		if got != "0.0.0.0:18100" {
			t.Errorf("lan 模式应绑 0.0.0.0:18100，实际 %s", got)
		}
		// 但写进 CLI 配置的连接地址必须归一化，否则客户端连不上
		if conn := RelayConnectAddress(got); conn != loopback {
			t.Errorf("lan 模式的连接地址应为 %s，实际 %s", loopback, conn)
		}
	})
}

// TestGeminiClientErrorNotCountedAsProviderFailure Gemini 路径也必须区分
// "上游拒绝请求内容"与"供应商故障"，否则一个坏请求会把全部 Gemini 供应商拉黑。
func TestGeminiClientErrorNotCountedAsProviderFailure(t *testing.T) {
	cases := []struct {
		name   string
		errMsg string
		want   bool
	}{
		{"请求内容被拒", geminiClientErrorPrefix + "HTTP 400: bad request", true},
		{"普通供应商故障", "HTTP 503: upstream down", false},
		{"客户端中断", geminiClientAbortMsg, false},
		{"空错误", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isGeminiClientError(tc.errMsg); got != tc.want {
				t.Errorf("isGeminiClientError(%q) = %v, want %v", tc.errMsg, got, tc.want)
			}
		})
	}
}

// TestCheckNonStreamTruncated 非流式响应被上游截断时必须能识别出来。
// xrequest 内部 `body, _ := io.ReadAll(...)` 丢弃了读错误，截断的响应会被当成完整响应，
// 半死的供应商在非流式请求上永远被记成功、永远不会被拉黑。
func TestCheckNonStreamTruncated(t *testing.T) {
	newResp := func(contentLength int64, encoding string) *xrequest.Response {
		header := http.Header{}
		if encoding != "" {
			header.Set("Content-Encoding", encoding)
		}
		return &xrequest.Response{RawResponse: &http.Response{
			ContentLength: contentLength,
			Header:        header,
		}}
	}

	cases := []struct {
		name      string
		resp      *xrequest.Response
		written   int64
		wantError bool
	}{
		{"完整响应", newResp(100, ""), 100, false},
		{"被截断", newResp(100, ""), 40, true},
		{"写出多于声明(不判截断)", newResp(100, ""), 120, false},
		{"分块传输无法校验", newResp(-1, ""), 40, false},
		{"空响应体", newResp(0, ""), 0, false},
		{"压缩响应不可比", newResp(100, "gzip"), 40, false},
		{"resp 为空", nil, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkNonStreamTruncated(tc.resp, tc.written)
			if (err != nil) != tc.wantError {
				t.Errorf("checkNonStreamTruncated(written=%d) 错误 = %v, 期望有错误 = %v",
					tc.written, err, tc.wantError)
			}
		})
	}
}

// TestResolveRelayListenAddressesWSLAutoKeepsLoopback wsl_auto 必须保留回环为主地址：
// 只绑 WSL 网卡会让本机 CLI 全部连不上，绑 0.0.0.0 又会把无鉴权、
// 自动注入供应商密钥的代理暴露给整个物理局域网。
func TestResolveRelayListenAddressesWSLAutoKeepsLoopback(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	assertHomeIsolated(t, tmp)

	dir := filepath.Join(tmp, appSettingsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, networkSettingsFile),
		[]byte(`{"listenMode":"wsl_auto"}`), 0o644); err != nil {
		t.Fatalf("写设置失败: %v", err)
	}

	addrs := ResolveRelayListenAddresses()
	if len(addrs) == 0 {
		t.Fatal("至少应返回一个监听地址")
	}
	if addrs[0] != "127.0.0.1:18100" {
		t.Errorf("主监听地址应为回环，实际 %s", addrs[0])
	}
	for _, a := range addrs {
		if strings.HasPrefix(a, "0.0.0.0:") {
			t.Errorf("wsl_auto 不应绑定 0.0.0.0（会暴露到物理局域网），实际地址列表 %v", addrs)
		}
	}
	// 连接地址始终由主地址派生，必须可连
	if conn := RelayConnectAddress(addrs[0]); conn != "127.0.0.1:18100" {
		t.Errorf("连接地址 = %s, want 127.0.0.1:18100", conn)
	}
}

// TestRelayStartBindsExtraAddresses 额外监听地址应真正被绑定，
// 且其中一个失败不能影响主地址继续服务。
func TestRelayStartBindsExtraAddresses(t *testing.T) {
	setupRenameTestEnv(t)

	// 先占住一个端口，用它当"必然绑定失败"的额外地址
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("占位监听失败: %v", err)
	}
	defer occupied.Close()

	// 主地址取一个空闲端口
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("探测空闲端口失败: %v", err)
	}
	primary := probe.Addr().String()
	_ = probe.Close()

	prs := newTestRelayService(NewProviderService())
	prs.addr = primary
	prs.extraAddrs = []string{occupied.Addr().String()}

	if err := prs.Start(); err != nil {
		t.Fatalf("额外地址绑定失败不应导致启动失败: %v", err)
	}
	defer prs.Stop()

	// 主地址必须真的在监听
	conn, err := net.Dial("tcp", primary)
	if err != nil {
		t.Fatalf("主地址未在监听: %v", err)
	}
	_ = conn.Close()
}

// TestIsLoopbackHostPort WSL2 里的 127.0.0.1 是 WSL 自己的回环，到不了宿主机代理，
// 因此判断"WSL 能否访问"时必须把回环地址排除掉。
func TestIsLoopbackHostPort(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"127.0.0.1:18100", true},
		{"127.0.0.5:18100", true},
		{"localhost:18100", true},
		{"[::1]:18100", true},
		{"0.0.0.0:18100", false},
		{"172.20.144.1:18100", false},
		{"不是地址", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isLoopbackHostPort(tc.in); got != tc.want {
			t.Errorf("isLoopbackHostPort(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestBoundAddressesReflectsActualListeners 监听地址在启动时冻结，
// UI 展示与"WSL 能不能连"的判断都要以实际绑定为准。
func TestBoundAddressesReflectsActualListeners(t *testing.T) {
	setupRenameTestEnv(t)

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("探测空闲端口失败: %v", err)
	}
	primary := probe.Addr().String()
	_ = probe.Close()

	prs := newTestRelayService(NewProviderService())
	prs.addr = primary

	if got := prs.BoundAddresses(); len(got) != 0 {
		t.Errorf("未启动时不应有绑定地址，实际 %v", got)
	}

	if err := prs.Start(); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	defer prs.Stop()

	bound := prs.BoundAddresses()
	if len(bound) != 1 || bound[0] != primary {
		t.Errorf("BoundAddresses() = %v, want [%s]", bound, primary)
	}

	// 返回的必须是副本，外部改动不能影响内部状态
	bound[0] = "tampered"
	if again := prs.BoundAddresses(); again[0] != primary {
		t.Errorf("BoundAddresses 返回了内部切片，被外部改坏: %v", again)
	}
}

// TestBoundAddressesClearedAfterStop 停掉之后再对外报告"正在监听 xxx"
// 会误导 UI 与 WSL 可达性判断。
func TestBoundAddressesClearedAfterStop(t *testing.T) {
	setupRenameTestEnv(t)

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("探测空闲端口失败: %v", err)
	}
	primary := probe.Addr().String()
	_ = probe.Close()

	prs := newTestRelayService(NewProviderService())
	prs.addr = primary

	if err := prs.Start(); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	if len(prs.BoundAddresses()) != 1 {
		t.Fatalf("启动后应有 1 个绑定地址，实际 %v", prs.BoundAddresses())
	}

	if err := prs.Stop(); err != nil {
		t.Fatalf("停止失败: %v", err)
	}
	if got := prs.BoundAddresses(); len(got) != 0 {
		t.Errorf("停止后不应再报告绑定地址，实际 %v", got)
	}
}

// TestResolveWSLReachableAddress WSL 可达性判定必须覆盖三种网络形态，
// 既不能把合法场景误杀（镜像模式、绑具体网卡），也不能对真正不可达的情况报成功。
func TestResolveWSLReachableAddress(t *testing.T) {
	ns := &NetworkService{}

	cases := []struct {
		name     string
		wslHost  string
		bound    []string
		want     string
		wantFail bool
	}{
		{
			name:    "绑全部网卡且探测到 WSL 网段",
			wslHost: "172.20.144.1",
			bound:   []string{"0.0.0.0:18100"},
			want:    "172.20.144.1:18100",
		},
		{
			name:    "回环+WSL 网段双监听",
			wslHost: "172.20.144.1",
			bound:   []string{"127.0.0.1:18100", "172.20.144.1:18100"},
			want:    "172.20.144.1:18100",
		},
		{
			// custom 模式绑具体局域网 IP：NAT 下的 WSL 能路由到宿主机局域网地址，
			// 不该一律拒绝
			name:    "绑具体局域网网卡",
			wslHost: "172.20.144.1",
			bound:   []string{"192.168.1.10:18100"},
			want:    "192.168.1.10:18100",
		},
		{
			// networkingMode=mirrored 下没有 vEthernet(WSL) 网卡，
			// 且 WSL 内的 127.0.0.1 就是宿主机回环
			name:    "镜像模式：探测不到网卡且只绑回环",
			wslHost: "",
			bound:   []string{"127.0.0.1:18100"},
			want:    "127.0.0.1:18100",
		},
		{
			name:    "自定义端口的镜像模式",
			wslHost: "",
			bound:   []string{"127.0.0.1:9000"},
			want:    "127.0.0.1:9000",
		},
		{
			// NAT 模式（探测到网卡）却只绑回环：真的连不上，必须失败
			name:     "NAT 模式只绑回环",
			wslHost:  "172.20.144.1",
			bound:    []string{"127.0.0.1:18100"},
			wantFail: true,
		},
		{
			name:     "代理未启动",
			wslHost:  "172.20.144.1",
			bound:    nil,
			wantFail: true,
		},
		{
			// 绑全部网卡必然覆盖回环，严格比"只绑回环"更可达；
			// 探测不到网卡按镜像模式处理，不能反而判失败
			name:    "镜像模式下绑全部网卡",
			wslHost: "",
			bound:   []string{"0.0.0.0:18100"},
			want:    "127.0.0.1:18100",
		},
		{
			// 解析不了的地址不能影响判定，更不能被当成"只绑回环"而误报可达
			name:     "监听地址全部无法解析",
			wslHost:  "",
			bound:    []string{"这不是地址", "也不是"},
			wantFail: true,
		},
		{
			// 混合输入：无法解析的项被跳过，回环项决定走镜像模式分支并沿用其端口
			name:    "无法解析项混在回环里",
			wslHost: "",
			bound:   []string{"垃圾", "127.0.0.1:9100"},
			want:    "127.0.0.1:9100",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, failure := ns.resolveWSLReachableAddress(tc.wslHost, tc.bound)
			if tc.wantFail {
				if got != "" {
					t.Errorf("应判定为不可达，实际返回 %q", got)
				}
				if failure == "" {
					t.Errorf("不可达时必须给出失败原因")
				}
				return
			}
			if got != tc.want {
				t.Errorf("可达地址 = %q, want %q（失败原因: %s）", got, tc.want, failure)
			}
		})
	}
}

// respondNoEligibleProviders 按跳过原因分支输出可操作的排查文案（issue #29）
func TestRespondNoEligibleProvidersBranches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	run := func(model string, m, b, i int) string {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		respondNoEligibleProviders(c, model, m, b, i)
		if w.Code != http.StatusNotFound {
			t.Fatalf("应为 404, 实际 %d", w.Code)
		}
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		msg, _ := body["error"].(string)
		return msg
	}
	if msg := run("claude-x", 2, 1, 0); !strings.Contains(msg, "claude-x") ||
		!strings.Contains(msg, "白名单") || !strings.Contains(msg, "拉黑") {
		t.Errorf("白名单分支文案缺要素: %s", msg)
	}
	if msg := run("", 0, 3, 0); !strings.Contains(msg, "拉黑") || !strings.Contains(msg, "黑名单页") {
		t.Errorf("拉黑分支文案缺要素: %s", msg)
	}
	// 混合原因必须全部列出，不得只挑一个当代表（否则"都被拉黑"会掩盖校验失败）
	if msg := run("", 0, 2, 1); !strings.Contains(msg, "拉黑") || !strings.Contains(msg, "配置校验") {
		t.Errorf("拉黑+校验失败组合应同时列出两种原因: %s", msg)
	}
	if msg := run("", 0, 0, 2); !strings.Contains(msg, "配置校验") {
		t.Errorf("校验失败分支文案缺要素: %s", msg)
	}
	if msg := run("", 0, 0, 0); !strings.Contains(msg, "没有已启用的供应商") {
		t.Errorf("空供应商分支文案缺要素: %s", msg)
	}
}
