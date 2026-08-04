package services

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daodao97/xgo/xrequest"
)

// startRelayDoTestServer 按路径返回各类响应形态，供新旧发送路径对照
func startRelayDoTestServer(hits *atomic.Int64) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	})
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, "/ok", http.StatusFound)
	})
	for _, code := range []int{400, 401, 408, 429} {
		code := code
		mux.HandleFunc(fmt.Sprintf("/status%d", code), func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			if code == 429 {
				w.Header().Set("Retry-After", "1")
			}
			w.WriteHeader(code)
			fmt.Fprintf(w, `{"error":"status %d"}`, code)
		})
	}
	mux.HandleFunc("/err500", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"boom"}`)
	})
	mux.HandleFunc("/err500-empty", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	mux.HandleFunc("/gzip", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/json")
		gz := gzip.NewWriter(w)
		fmt.Fprint(gz, `{"compressed":true}`)
		gz.Close()
	})
	mux.HandleFunc("/slowheader", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(300 * time.Millisecond)
		fmt.Fprint(w, `{"slow":true}`)
	})
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for i := 0; i < 4; i++ {
			fmt.Fprintf(w, "data: chunk-%d\n\n", i)
			if fl != nil {
				fl.Flush()
			}
			time.Sleep(100 * time.Millisecond)
		}
	})
	return httptest.NewServer(mux)
}

// oldXrequestPost 旧发送路径（xrequest），语义基准
func oldXrequestPost(ctx context.Context, targetURL string, query, headers map[string]string, body []byte, singleAddress bool) (*xrequest.Response, error) {
	req := xrequest.New().
		SetClient(&http.Client{}).
		SetDebug(false).
		WithContext(ctx).
		SetHeaders(headers).
		SetQueryParams(query)
	if singleAddress {
		req = req.SetRetry(1, 10*time.Millisecond)
	}
	req = req.SetBody(bytes.NewReader(body))
	return req.Post(targetURL)
}

// TestRelayDoPostParityWithXrequest 新旧路径逐形态对照：
// (是否返回错误, 状态码, 响应体可读性) 三元组必须一致
func TestRelayDoPostParityWithXrequest(t *testing.T) {
	var hits atomic.Int64
	srv := startRelayDoTestServer(&hits)
	defer srv.Close()

	body := []byte(`{"model":"m","messages":[]}`)
	headers := map[string]string{"x-api-key": "test-key", "Content-Type": "application/json"}
	query := map[string]string{"beta": "true"}

	cases := []struct {
		name       string
		path       string
		single     bool
		wantErr    bool
		wantStatus int
		wantBody   string
	}{
		{"2xx 单地址", "/ok", true, false, 200, `{"ok":true}`},
		{"2xx 多地址", "/ok", false, false, 200, `{"ok":true}`},
		{"3xx 跟随重定向", "/redirect", true, false, 200, `{"ok":true}`},
		{"400 单地址", "/status400", true, false, 400, `{"error":"status 400"}`},
		{"401 单地址", "/status401", true, false, 401, `{"error":"status 401"}`},
		{"408 单地址", "/status408", true, false, 408, `{"error":"status 408"}`},
		{"429 多地址", "/status429", false, false, 429, `{"error":"status 429"}`},
		{"500 非空体 单地址(resp+err)", "/err500", true, true, 500, `{"error":"boom"}`},
		{"500 空体 单地址(resp+err)", "/err500-empty", true, true, 500, ""},
		{"500 非空体 多地址(err=nil)", "/err500", false, false, 500, `{"error":"boom"}`},
		{"gzip 解压", "/gzip", true, false, 200, `{"compressed":true}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldResp, oldErr := oldXrequestPost(context.Background(), srv.URL+tc.path, query, headers, body, tc.single)
			newResp, _, newErr := relayDoPost(context.Background(), &http.Client{}, srv.URL+tc.path, query, headers, body, tc.single, relayFirstByteBudget)

			if (oldErr != nil) != (newErr != nil) {
				t.Fatalf("错误形态不一致: old=%v new=%v", oldErr, newErr)
			}
			if (oldErr != nil) != tc.wantErr {
				t.Fatalf("错误形态与预期不符: old=%v want=%v", oldErr, tc.wantErr)
			}
			if oldResp == nil || newResp == nil {
				t.Fatalf("响应不应为 nil: old=%v new=%v", oldResp, newResp)
			}
			if oldResp.StatusCode() != tc.wantStatus || newResp.StatusCode() != tc.wantStatus {
				t.Fatalf("状态码不一致: old=%d new=%d want=%d", oldResp.StatusCode(), newResp.StatusCode(), tc.wantStatus)
			}
			// Error() 分支命中必须一致（调度按它分类 4xx/5xx）
			if (oldResp.Error() != nil) != (newResp.Error() != nil) {
				t.Fatalf("Error() 分支不一致: old=%v new=%v", oldResp.Error(), newResp.Error())
			}
			// 响应体可读且内容一致（String 自动解压 gzip）
			if got := newResp.String(); got != tc.wantBody {
				t.Fatalf("新路径响应体不符: got=%q want=%q", got, tc.wantBody)
			}
			if got := oldResp.String(); got != tc.wantBody {
				t.Fatalf("旧路径响应体不符: got=%q want=%q", got, tc.wantBody)
			}
		})
	}
}

// TestRelayDoPostTransportError 传输层错误：两路径都返回 err + nil resp
func TestRelayDoPostTransportError(t *testing.T) {
	deadURL := "http://127.0.0.1:1" // 端口 1 拒绝连接
	oldResp, oldErr := oldXrequestPost(context.Background(), deadURL, nil, nil, []byte("{}"), false)
	newResp, _, newErr := relayDoPost(context.Background(), &http.Client{Timeout: 2 * time.Second}, deadURL, nil, nil, []byte("{}"), false, relayFirstByteBudget)
	if oldErr == nil || newErr == nil {
		t.Fatalf("传输错误应返回 err: old=%v new=%v", oldErr, newErr)
	}
	if oldResp != nil || newResp != nil {
		t.Fatalf("传输错误 resp 应为 nil: old=%v new=%v", oldResp, newResp)
	}
	if errors.Is(newErr, errFirstByteBudget) {
		t.Fatalf("传输错误不应归类为首响预算耗尽: %v", newErr)
	}
}

// TestRelayDoPostClientCancel 客户端取消：错误链保留 context.Canceled，
// 且不得归类为首响预算耗尽
func TestRelayDoPostClientCancel(t *testing.T) {
	var hits atomic.Int64
	srv := startRelayDoTestServer(&hits)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := relayDoPost(ctx, &http.Client{}, srv.URL+"/slowheader", nil, nil, []byte("{}"), false, relayFirstByteBudget)
	if err == nil {
		t.Fatal("已取消的 context 应返回错误")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("错误链应保留 context.Canceled: %v", err)
	}
	if errors.Is(err, errFirstByteBudget) {
		t.Fatalf("客户端取消不得归类为首响预算耗尽: %v", err)
	}
}

// TestRelayDoPostBudgetExhausted 预算清零：不发出请求、返回预算哨兵
func TestRelayDoPostBudgetExhausted(t *testing.T) {
	var hits atomic.Int64
	srv := startRelayDoTestServer(&hits)
	defer srv.Close()

	_, _, err := relayDoPost(context.Background(), &http.Client{}, srv.URL+"/ok", nil, nil, []byte("{}"), false, 0)
	if !errors.Is(err, errFirstByteBudget) {
		t.Fatalf("预算清零应返回 errFirstByteBudget: %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("预算清零不得真实上行，实际命中 %d 次", hits.Load())
	}
}

// TestRelayDoPostBudgetCutsSlowHeader 预算在响应头到达前触发：返回预算哨兵，
// 且不是 context.Canceled 分类（不会被当成客户端断开）
func TestRelayDoPostBudgetCutsSlowHeader(t *testing.T) {
	var hits atomic.Int64
	srv := startRelayDoTestServer(&hits)
	defer srv.Close()

	start := time.Now()
	_, _, err := relayDoPost(context.Background(), &http.Client{}, srv.URL+"/slowheader", nil, nil, []byte("{}"), false, 60*time.Millisecond)
	if !errors.Is(err, errFirstByteBudget) {
		t.Fatalf("慢响应头应触发预算哨兵: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("预算触发应在 60ms 左右返回，实际 %v", elapsed)
	}
}

// TestRelayDoPostBudgetReleasedAfterHeaders 响应头到达后预算不再介入：
// 长过预算的流式响应体必须完整可读
func TestRelayDoPostBudgetReleasedAfterHeaders(t *testing.T) {
	var hits atomic.Int64
	srv := startRelayDoTestServer(&hits)
	defer srv.Close()

	// 预算 150ms；/stream 响应头立即返回、响应体持续 ~400ms
	resp, _, err := relayDoPost(context.Background(), &http.Client{}, srv.URL+"/stream", nil, nil, []byte("{}"), false, 150*time.Millisecond)
	if err != nil {
		t.Fatalf("响应头已到，预算不应再报错: %v", err)
	}
	data, err := io.ReadAll(resp.RawResponse.Body)
	resp.RawResponse.Body.Close()
	if err != nil {
		t.Fatalf("流式响应体读取失败（预算不应截断已开始的流）: %v", err)
	}
	for i := 0; i < 4; i++ {
		if want := fmt.Sprintf("chunk-%d", i); !containsStr(string(data), want) {
			t.Fatalf("流式响应体缺失 %s: %q", want, string(data))
		}
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestRelayDoPostFinalURLAndHeaders 最终 URL 合并 query；头部原始赋值不规范化
func TestRelayDoPostFinalURLAndHeaders(t *testing.T) {
	var gotHeader http.Header
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Clone()
		gotQuery = r.URL.RawQuery
		fmt.Fprint(w, "{}")
	}))
	defer srv.Close()

	_, finalURL, err := relayDoPost(context.Background(), &http.Client{},
		srv.URL+"/v1/messages?fixed=1", map[string]string{"beta": "true"},
		map[string]string{"x-api-key": "k"}, []byte("{}"), false, relayFirstByteBudget)
	if err != nil {
		t.Fatal(err)
	}
	if !containsStr(finalURL, "fixed=1") || !containsStr(finalURL, "beta=true") {
		t.Fatalf("最终 URL 未合并既有与新增 query: %s", finalURL)
	}
	if !containsStr(gotQuery, "fixed=1") || !containsStr(gotQuery, "beta=true") {
		t.Fatalf("服务端收到的 query 不完整: %s", gotQuery)
	}
	// 原始键 "x-api-key"（小写）应按原样上行；net/http 服务端会按规范化键收纳，
	// 这里校验值到达即可
	if gotHeader.Get("x-api-key") != "k" {
		t.Fatalf("头部未上行: %v", gotHeader)
	}
}
