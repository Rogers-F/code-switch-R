package services

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	modelpricing "codeswitch/resources/model-pricing"
)

// TestDetermineStatusModelRejection 模型拒绝类 400/404 记为 validation_failed(不进拉黑计数)。
func TestDetermineStatusModelRejection(t *testing.T) {
	hcs := &HealthCheckService{}

	status, _ := hcs.determineStatus(400, 100, []byte(`{"error":{"message":"The model gpt-5.6 does not exist"}}`))
	if status != HealthStatusValidationError {
		t.Errorf("模型不存在的 400 应为 validation_failed,实际 %s", status)
	}

	status, _ = hcs.determineStatus(404, 100, []byte(`{"error":"model not found: gemini-3.5-flash"}`))
	if status != HealthStatusValidationError {
		t.Errorf("模型不存在的 404 应为 validation_failed,实际 %s", status)
	}

	status, _ = hcs.determineStatus(400, 100, []byte(`{"error":"missing required field max_tokens"}`))
	if status != HealthStatusFailed {
		t.Errorf("普通 400 应保持 failed,实际 %s", status)
	}

	status, _ = hcs.determineStatus(404, 100, []byte(`<html>nginx 404</html>`))
	if status != HealthStatusFailed {
		t.Errorf("端点 404 应为 failed,实际 %s", status)
	}
}

// TestBuildTestRequestByEndpoint /responses 用 Responses 协议,/messages 用 Anthropic,其余 Chat。
func TestBuildTestRequestByEndpoint(t *testing.T) {
	hcs := &HealthCheckService{}

	decode := func(data []byte) map[string]any {
		payload := map[string]any{}
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return payload
	}

	payload := decode(hcs.buildTestRequest("codex", "gpt-5.6", "/responses"))
	if payload["input"] != "hi" || payload["max_output_tokens"] == nil || payload["messages"] != nil {
		t.Errorf("/responses 应使用 Responses 协议体: %v", payload)
	}

	payload = decode(hcs.buildTestRequest("codex", "gpt-5.6", "/v1/chat/completions"))
	if payload["messages"] == nil || payload["input"] != nil {
		t.Errorf("chat 端点应使用 messages 体: %v", payload)
	}

	payload = decode(hcs.buildTestRequest("claude", "claude-haiku-4-5-20251001", "/v1/messages"))
	if payload["max_tokens"] == nil || payload["messages"] == nil {
		t.Errorf("claude 应使用 Anthropic 体: %v", payload)
	}
}

// TestGetEffectiveModelWhitelistIntersection 声明白名单的供应商取候选链上首个其支持的模型。
func TestGetEffectiveModelWhitelistIntersection(t *testing.T) {
	pricing, err := modelpricing.NewService()
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	catalogs := fakeCatalogSource{
		"openai": {ID: "openai", Models: map[string]modelpricing.RemoteModel{
			"gpt-5.6": catalogModel("gpt-5.6", "2026-07-09"),
			"gpt-5.5": catalogModel("gpt-5.5", "2026-04-23"),
		}},
	}
	if _, err := pricing.Rebuild(modelpricing.ConvertCatalogs(catalogs)); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	fixed, _ := time.ParseInLocation("2006-01-02", "2026-09-01", time.UTC) // 5.6 已过稳定窗
	policy := &DefaultModelPolicy{pricing: pricing, source: catalogs, now: func() time.Time { return fixed }}
	hcs := &HealthCheckService{policy: policy}

	// 无白名单:直接用动态解析值
	open := &Provider{}
	if got := hcs.getEffectiveModel(open, "codex"); got != "gpt-5.6" {
		t.Errorf("无白名单应取动态值 gpt-5.6,实际 %s", got)
	}

	// 白名单只含 gpt-5.5:取交集
	limited := &Provider{SupportedModels: map[string]bool{"gpt-5.5": true}}
	if got := hcs.getEffectiveModel(limited, "codex"); got != "gpt-5.5" {
		t.Errorf("白名单交集应取 gpt-5.5,实际 %s", got)
	}

	// 用户配置最高优先
	custom := &Provider{AvailabilityConfig: &AvailabilityConfig{TestModel: "my-model"}}
	if got := hcs.getEffectiveModel(custom, "codex"); got != "my-model" {
		t.Errorf("用户配置应最高优先,实际 %s", got)
	}
}

// TestIsModelRejectionBody 关键词识别边界。
func TestIsModelRejectionBody(t *testing.T) {
	if isModelRejectionBody([]byte(`{"error":"rate limit exceeded"}`)) {
		t.Error("非模型错误不应命中")
	}
	if !isModelRejectionBody([]byte(`{"message":"不支持的模型: glm-5"}`)) {
		t.Error("中文模型拒绝应命中")
	}
	if isModelRejectionBody([]byte(strings.Repeat("x", 10))) {
		t.Error("无关文本不应命中")
	}
}
