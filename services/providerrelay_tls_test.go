package services

import (
	"context"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newSelfSignedServer 起一个自签名证书的 HTTPS 服务器，模拟需要跳验的上游。
func newSelfSignedServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	ts := httptest.NewTLSServer(handler)
	t.Cleanup(ts.Close)
	return ts
}

func isCertError(err error) bool {
	if err == nil {
		return false
	}
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	return errors.As(err, &unknownAuthority) || errors.As(err, &hostnameErr)
}

func TestRelayClientForSelection(t *testing.T) {
	if relayClientFor(false, "p1") != relayHTTPClient {
		t.Fatal("secure provider must use the default shared client")
	}
	if relayClientFor(true, "p1") != relayHTTPClientInsecure {
		t.Fatal("insecure provider must use the insecure shared client")
	}
	// 再次调用命中告警去重路径，不应 panic
	if relayClientFor(true, "p1") != relayHTTPClientInsecure {
		t.Fatal("selection must be stable across calls")
	}
}

// 转发共享客户端对自签名上游的行为：默认验证失败，insecure 变体成功。
func TestRelayClientsAgainstSelfSignedUpstream(t *testing.T) {
	ts := newSelfSignedServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	if _, err := relayHTTPClient.Get(ts.URL); !isCertError(err) {
		t.Fatalf("secure client should fail certificate verification, got err=%v", err)
	}

	resp, err := relayHTTPClientInsecure.Get(ts.URL)
	if err != nil {
		t.Fatalf("insecure client should succeed, got %v", err)
	}
	resp.Body.Close()
}

// 健康检查：insecureSkipVerify=false 的供应商探测自签名上游必须失败，
// =true 时必须不再因证书失败（避免"转发通、探测挂"引发误拉黑）。
func TestHealthCheckHonorsInsecureSkipVerify(t *testing.T) {
	ts := newSelfSignedServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}],"model":"test-model"}`))
	})

	hcs := NewHealthCheckService(nil, nil, nil, nil)
	base := Provider{
		ID:     1,
		Name:   "self-signed",
		APIURL: ts.URL,
		APIKey: "k",
		AvailabilityConfig: &AvailabilityConfig{
			TestModel:    "test-model",
			TestEndpoint: "/v1/messages",
			Timeout:      5000,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	strict := base
	strict.InsecureSkipVerify = false
	if got := hcs.checkProvider(ctx, strict, "claude"); got.Status != HealthStatusFailed {
		t.Fatalf("strict provider should fail against self-signed upstream, got status=%s (%s)", got.Status, got.ErrorMessage)
	}

	skip := base
	skip.InsecureSkipVerify = true
	if got := hcs.checkProvider(ctx, skip, "claude"); got.Status == HealthStatusFailed {
		t.Fatalf("skip-verify provider should not fail on certificate, got status=%s (%s)", got.Status, got.ErrorMessage)
	}
}

// 连通性测试：同一自签名上游，跳验开关决定成败。
// TestProvider 本身不写黑名单（拉黑联动在 TestAll 里），跳验成功即不会产生失败上报。
func TestConnectivityHonorsInsecureSkipVerify(t *testing.T) {
	ts := newSelfSignedServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	})

	cts := NewConnectivityTestService(nil, nil, nil, nil)
	base := Provider{
		ID:     1,
		Name:   "self-signed",
		APIURL: ts.URL,
		APIKey: "k",
		AvailabilityConfig: &AvailabilityConfig{
			TestModel: "test-model",
			Timeout:   5000,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	strict := base
	strict.InsecureSkipVerify = false
	if got := cts.TestProvider(ctx, strict, "claude"); got.Status != StatusUnavailable || got.SubStatus != SubStatusNetworkError {
		t.Fatalf("strict provider should be unavailable(network_error), got status=%d sub=%s msg=%s", got.Status, got.SubStatus, got.Message)
	}

	skip := base
	skip.InsecureSkipVerify = true
	if got := cts.TestProvider(ctx, skip, "claude"); got.Status != StatusAvailable {
		t.Fatalf("skip-verify provider should be available, got status=%d sub=%s msg=%s", got.Status, got.SubStatus, got.Message)
	}
}
