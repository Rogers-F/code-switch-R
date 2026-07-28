package services

import (
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
)

// newDeepLinkTestService 复用改名测试的环境搭建（隔离家目录 + 初始化 alias 等表）
func newDeepLinkTestService(t *testing.T) *DeepLinkService {
	t.Helper()
	setupRenameTestEnv(t)
	return NewDeepLinkService(NewProviderService())
}

func newDeepLinkRequest(name string) *DeepLinkImportRequest {
	return &DeepLinkImportRequest{
		Version:  "v1",
		Resource: "provider",
		App:      "claude",
		Name:     name,
		Homepage: "https://example.com",
		Endpoint: "https://api.example.com",
		APIKey:   "sk-test",
	}
}

// 回归：深链导入生成的 ID 必须在前端浮点安全整数范围内
// （旧实现用 UnixNano，约 1.78e18，JSON 往返后被取整导致按 ID 的操作全部失配）
func TestDeepLinkImportGeneratesFrontendSafeID(t *testing.T) {
	svc := newDeepLinkTestService(t)

	idStr, err := svc.ImportProviderFromDeepLink(newDeepLinkRequest("DL-One"))
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		t.Fatalf("返回的 ID 不是整数: %q", idStr)
	}
	const maxSafeInteger = int64(1) << 53
	if id >= maxSafeInteger {
		t.Fatalf("ID %d 超出前端浮点安全整数范围，JSON 往返会取整漂移", id)
	}

	req2 := newDeepLinkRequest("DL-Two")
	req2.Endpoint = "https://api2.example.com"
	idStr2, err := svc.ImportProviderFromDeepLink(req2)
	if err != nil {
		t.Fatalf("第二次导入失败: %v", err)
	}
	if idStr2 == idStr {
		t.Fatalf("连续导入生成了重复 ID: %s", idStr2)
	}
}

// 回归：深链的 model 参数不得写成排他白名单，否则 Claude 实际请求的模型全被跳过
func TestDeepLinkImportDoesNotWriteExclusiveWhitelist(t *testing.T) {
	svc := newDeepLinkTestService(t)

	req := newDeepLinkRequest("DL-Model")
	model := "deepseek-v3"
	req.Model = &model
	if _, err := svc.ImportProviderFromDeepLink(req); err != nil {
		t.Fatalf("导入失败: %v", err)
	}

	providers, err := svc.providerService.LoadProviders("claude")
	if err != nil {
		t.Fatalf("加载供应商失败: %v", err)
	}
	var found *Provider
	for i := range providers {
		if providers[i].Name == "DL-Model" {
			found = &providers[i]
		}
	}
	if found == nil {
		t.Fatal("未找到导入的供应商")
	}
	if len(found.SupportedModels) != 0 {
		t.Fatalf("model 参数被写成排他白名单，会拒绝其他模型的请求: %v", found.SupportedModels)
	}
}

// 回归：同名或同端点的供应商重复导入应被拒绝（黑名单与用量统计按 name 键控）
func TestDeepLinkImportRejectsDuplicateProvider(t *testing.T) {
	svc := newDeepLinkTestService(t)

	if _, err := svc.ImportProviderFromDeepLink(newDeepLinkRequest("DL-Dup")); err != nil {
		t.Fatalf("首次导入失败: %v", err)
	}

	// 同名
	if _, err := svc.ImportProviderFromDeepLink(newDeepLinkRequest("DL-Dup")); err == nil {
		t.Fatal("同名供应商重复导入应被拒绝")
	}

	// 同端点、不同名
	req := newDeepLinkRequest("DL-Dup2")
	if _, err := svc.ImportProviderFromDeepLink(req); err == nil {
		t.Fatal("相同端点的供应商重复导入应被拒绝")
	}
}

// 回归：来自内联 config 的 Endpoint 也必须通过 http(s) URL 校验
// （旧实现只校验 URL 查询参数，config 注入的任意 scheme 会被直接落盘）
func TestDeepLinkImportValidatesConfigProvidedEndpoint(t *testing.T) {
	svc := newDeepLinkTestService(t)

	cfg := `{"env":{"ANTHROPIC_AUTH_TOKEN":"sk-x","ANTHROPIC_BASE_URL":"ftp://10.0.0.5:8080"}}`
	encoded := base64.StdEncoding.EncodeToString([]byte(cfg))
	req := &DeepLinkImportRequest{
		Version:  "v1",
		Resource: "provider",
		App:      "claude",
		Name:     "DL-Evil",
		Config:   &encoded,
	}

	_, err := svc.ImportProviderFromDeepLink(req)
	if err == nil {
		t.Fatal("config 注入的非 http(s) Endpoint 应被拒绝")
	}
	if !strings.Contains(err.Error(), "scheme") {
		t.Fatalf("期望 scheme 校验错误，得到: %v", err)
	}
}

// 回归：config 参数的 Base64 需兼容 '+' 被 URL 解码成空格、无填充与 URL-safe 变体
func TestDecodeBase64ConfigVariants(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "标准编码", input: "Pj4+YQ==", want: ">>>a"},
		{name: "加号被 URL 解码成空格", input: "Pj4 YQ==", want: ">>>a"},
		{name: "无填充标准编码", input: "Pj4+YQ", want: ">>>a"},
		{name: "URL-safe 编码", input: "Pj4-YQ==", want: ">>>a"},
		{name: "无填充 URL-safe 编码", input: "Pj4-YQ", want: ">>>a"},
		{name: "非法内容", input: "@@@@", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeBase64Config(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("期望解码失败，实际得到 %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("解码失败: %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("解码结果 %q, 期望 %q", got, tt.want)
			}
		})
	}
}
