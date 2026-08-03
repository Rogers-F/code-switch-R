package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
)

// captureBuffer 的每字段上限、总预算与截断/跳过标记
func TestCaptureBufferLimitsAndBudget(t *testing.T) {
	var budget atomic.Int64
	// 单缓冲：写入超过 captureFieldLimit 应截断
	cb := newCaptureBuffer(&budget)
	chunk := make([]byte, 1024*1024) // 1MiB
	for i := 0; i < 60; i++ {        // 60MiB > 50MiB 上限
		cb.append(chunk)
	}
	if !cb.truncated {
		t.Error("超过 captureFieldLimit 应置 truncated")
	}
	if len(cb.buf) > captureFieldLimit {
		t.Errorf("缓冲超过硬上限: %d", len(cb.buf))
	}
	if cb.total != 60*1024*1024 {
		t.Errorf("total 应记录全部字节: %d", cb.total)
	}
	cb.release()
	if budget.Load() != 0 {
		t.Errorf("release 后预算应归零, 实际 %d", budget.Load())
	}
}

func TestCaptureBufferGlobalBudget(t *testing.T) {
	var budget atomic.Int64
	// 预置占用到接近上限
	budget.Store(captureInflightBudget - 1024)
	cb := newCaptureBuffer(&budget)
	cb.append(make([]byte, 4096)) // 触及全局预算
	if !cb.budgetSkipped {
		t.Error("触及在途预算应置 budget_skipped")
	}
	cb.release()
}

// 导出按类别裁剪：只选响应体时，请求体/URL 不进导出
func TestCaptureExportCategoryProjection(t *testing.T) {
	db := setupCaptureDBEnv(t)
	relay := newCaptureTestRelay(t)
	if err := relay.SetRequestCapture(true); err != nil {
		t.Fatal(err)
	}
	sid := relay.captureSessionID.Load()
	if _, err := db.Exec(`INSERT INTO request_log (platform, provider, model, http_code, request_url, request_headers, request_body, response_body, capture_session_id)
		VALUES ('claude','p','m',200,'https://secret-url/v1','{"Authorization":["Bearer sk-xxx"]}','{"prompt":"secret"}','{"answer":1}',?)`, sid); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir() + "/resp-only.json"
	// 仅导出响应体
	count, err := relay.streamCaptureExport(db, sid, CaptureExportOptions{ResponseBody: true}, dest)
	if err != nil || count != 1 {
		t.Fatalf("导出失败: count=%d err=%v", count, err)
	}
	data, _ := os.ReadFile(dest)
	s := string(data)
	if strings.Contains(s, "secret-url") || strings.Contains(s, "sk-xxx") || strings.Contains(s, "prompt") {
		t.Errorf("未选类别不应出现在导出中: %s", s)
	}
	if !strings.Contains(s, "answer") {
		t.Errorf("已选响应体应出现: %s", s)
	}
	// 全 false 应报错
	if _, err := relay.ExportCaptureSessionWithDialog(sid, CaptureExportOptions{}); err == nil {
		t.Error("全 false 导出应报错")
	}
	// 封套元数据
	var env struct {
		Meta struct {
			Version       int  `json:"version"`
			RawUnredacted bool `json:"raw_unredacted"`
			Count         int  `json:"count"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("导出非合法 JSON: %v", err)
	}
	if env.Meta.Version != 1 || !env.Meta.RawUnredacted || env.Meta.Count != 1 {
		t.Errorf("封套元数据异常: %+v", env.Meta)
	}
}

// GetCaptureTotalBytes 统计抓包字节
func TestCaptureTotalBytes(t *testing.T) {
	db := setupCaptureDBEnv(t)
	relay := newCaptureTestRelay(t)
	if err := relay.SetRequestCapture(true); err != nil {
		t.Fatal(err)
	}
	sid := relay.captureSessionID.Load()
	if _, err := db.Exec(`INSERT INTO request_log (platform, provider, model, request_body, capture_session_id)
		VALUES ('claude','p','m','1234567890', ?)`, sid); err != nil {
		t.Fatal(err)
	}
	total, err := relay.GetCaptureTotalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if total < 10 {
		t.Errorf("总量应至少含 10 字节请求体, 实际 %d", total)
	}
}

// 错误响应体全量入抓包缓冲（非 SSE）：>512 字节的错误体应完整捕获，
// RespBytes 记真实长度而非 512 预览
func TestCaptureFullErrorResponseBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupCaptureDBEnv(t)

	bigErr := `{"error":"` + strings.Repeat("x", 2000) + `"}` // >512 字节
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(bigErr))
	}))
	defer upstream.Close()

	prs := newTestRelayService(NewProviderService())
	provider := Provider{ID: 1, Name: "err-p", APIURL: upstream.URL, APIKey: "k", Enabled: true}
	if err := prs.SetRequestCapture(true); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req, _ := http.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`))
	c.Request = req
	// 400 是客户端类错误，forwardRequest 返回 false
	prs.forwardRequest(c, "claude", provider, "/v1/messages",
		map[string]string{}, map[string]string{"Content-Type": "application/json"}, []byte(`{"model":"m"}`), false, "m", 0)

	var respBody string
	var respBytes int
	if err := db.QueryRow(`SELECT response_body, response_bytes FROM request_log ORDER BY id DESC LIMIT 1`).
		Scan(&respBody, &respBytes); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(respBody) < 2000 || respBytes < 2000 {
		t.Errorf("错误响应体应完整捕获（>512），实际 len(body)=%d bytes=%d", len(respBody), respBytes)
	}
	if !strings.Contains(respBody, strings.Repeat("x", 1000)) {
		t.Errorf("错误体内容不完整")
	}
}
