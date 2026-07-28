package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupGeminiTestHome 把家目录指到临时目录，隔离 ~/.gemini 与 ~/.code-switch 的读写
func setupGeminiTestHome(t *testing.T) string {
	t.Helper()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	// Windows 的 os.UserHomeDir() 读的是 USERPROFILE,只设 HOME 会写到真实用户目录
	t.Setenv("USERPROFILE", tmpHome)
	assertHomeIsolated(t, tmpHome)
	return tmpHome
}

// 回归：删除供应商后再新增，自动生成的 ID 不得与现存 ID 重复
// （旧实现用 len+1 生成，删 gemini-1 后新增会再次得到 gemini-2）
func TestGeminiAddProviderUniqueIDAfterDelete(t *testing.T) {
	setupGeminiTestHome(t)
	svc := NewGeminiService("127.0.0.1:18100", nil)

	for _, name := range []string{"A", "B"} {
		if err := svc.AddProvider(GeminiProvider{Name: name}); err != nil {
			t.Fatalf("添加供应商 %s 失败: %v", name, err)
		}
	}

	var idA string
	for _, p := range svc.GetProviders() {
		if p.Name == "A" {
			idA = p.ID
		}
	}
	if idA == "" {
		t.Fatal("未找到供应商 A")
	}
	if err := svc.DeleteProvider(idA); err != nil {
		t.Fatalf("删除供应商 A 失败: %v", err)
	}

	if err := svc.AddProvider(GeminiProvider{Name: "C"}); err != nil {
		t.Fatalf("删除后再新增失败: %v", err)
	}

	seen := make(map[string]bool)
	for _, p := range svc.GetProviders() {
		if p.ID == "" {
			t.Fatalf("供应商 %s 未生成 ID", p.Name)
		}
		if seen[p.ID] {
			t.Fatalf("生成了重复 ID: %s", p.ID)
		}
		seen[p.ID] = true
	}
}

// 回归：GetProviders 必须返回拷贝，调用方在锁外修改不得污染服务内部状态
func TestGeminiGetProvidersReturnsCopy(t *testing.T) {
	setupGeminiTestHome(t)
	svc := NewGeminiService("127.0.0.1:18100", nil)

	if err := svc.AddProvider(GeminiProvider{Name: "A"}); err != nil {
		t.Fatalf("添加供应商失败: %v", err)
	}

	got := svc.GetProviders()
	got[0].Name = "外部改名"
	got[0].Enabled = true

	internal := svc.GetProviders()
	if internal[0].Name != "A" || internal[0].Enabled {
		t.Fatalf("GetProviders 返回了内部切片，外部修改污染了服务状态: %+v", internal[0])
	}
}

// 回归：writeGeminiEnv 重建文件时不得丢弃注释行、export 前缀行与空值键
func TestWriteGeminiEnvPreservesUserLines(t *testing.T) {
	tmpHome := setupGeminiTestHome(t)
	geminiDir := filepath.Join(tmpHome, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o700); err != nil {
		t.Fatalf("创建 .gemini 目录失败: %v", err)
	}

	original := "# 生产 key，勿动\nexport GOOGLE_CLOUD_PROJECT=my-project\nSOME_FLAG=\nGEMINI_API_KEY=old-key\n"
	envPath := filepath.Join(geminiDir, ".env")
	if err := os.WriteFile(envPath, []byte(original), 0o600); err != nil {
		t.Fatalf("写入初始 .env 失败: %v", err)
	}

	// 模拟 EnableProxy 的读-改-写：解析现有内容后注入代理配置
	envConfig := parseEnvFile(original)
	envConfig["GOOGLE_GEMINI_BASE_URL"] = "http://127.0.0.1:18100/gemini"
	envConfig["GEMINI_API_KEY"] = "code-switch-r"
	if err := writeGeminiEnv(envConfig); err != nil {
		t.Fatalf("writeGeminiEnv 失败: %v", err)
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("读取写回后的 .env 失败: %v", err)
	}
	content := string(data)

	for _, want := range []string{
		"# 生产 key，勿动",
		"export GOOGLE_CLOUD_PROJECT=my-project",
		"SOME_FLAG=",
		"GEMINI_API_KEY=code-switch-r",
		"GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:18100/gemini",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("写回后丢失内容 %q，完整文件:\n%s", want, content)
		}
	}
	if strings.Contains(content, "old-key") {
		t.Errorf("旧 API Key 未被替换，完整文件:\n%s", content)
	}
}

// 回归：启用/禁用代理一个来回后，用户手写的注释与直连配置必须原样恢复
func TestGeminiProxyToggleKeepsUserEnv(t *testing.T) {
	tmpHome := setupGeminiTestHome(t)
	geminiDir := filepath.Join(tmpHome, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o700); err != nil {
		t.Fatalf("创建 .gemini 目录失败: %v", err)
	}

	original := "# 备注\nGEMINI_MODEL=gemini-2.5-pro\nGOOGLE_GEMINI_BASE_URL=https://direct.example.com\nGEMINI_API_KEY=direct-key\n"
	envPath := filepath.Join(geminiDir, ".env")
	if err := os.WriteFile(envPath, []byte(original), 0o600); err != nil {
		t.Fatalf("写入初始 .env 失败: %v", err)
	}

	svc := NewGeminiService("127.0.0.1:18100", nil)
	if err := svc.EnableProxy(); err != nil {
		t.Fatalf("EnableProxy 失败: %v", err)
	}
	status, err := svc.ProxyStatus()
	if err != nil || status == nil || !status.Enabled {
		t.Fatalf("启用后代理状态异常: status=%+v err=%v", status, err)
	}
	if err := svc.DisableProxy(); err != nil {
		t.Fatalf("DisableProxy 失败: %v", err)
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("读取恢复后的 .env 失败: %v", err)
	}
	content := string(data)

	for _, want := range []string{
		"# 备注",
		"GEMINI_MODEL=gemini-2.5-pro",
		"GOOGLE_GEMINI_BASE_URL=https://direct.example.com",
		"GEMINI_API_KEY=direct-key",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("代理开关一个来回后丢失内容 %q，完整文件:\n%s", want, content)
		}
	}
	if strings.Contains(content, "code-switch-r") {
		t.Errorf("代理占位配置未被清除，完整文件:\n%s", content)
	}
}

// 回归：selectedType=oauth-personal 时启用代理应直接拒绝（OAuth 通道不读 .env，注入后代理静默失效）
func TestGeminiEnableProxyRejectsOAuth(t *testing.T) {
	tmpHome := setupGeminiTestHome(t)
	geminiDir := filepath.Join(tmpHome, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o700); err != nil {
		t.Fatalf("创建 .gemini 目录失败: %v", err)
	}
	settingsPath := filepath.Join(geminiDir, "settings.json")

	writeSelectedType := func(selectedType string) {
		t.Helper()
		content := `{"security":{"auth":{"selectedType":"` + selectedType + `"}}}`
		if err := os.WriteFile(settingsPath, []byte(content), 0o600); err != nil {
			t.Fatalf("写入 settings.json 失败: %v", err)
		}
	}

	svc := NewGeminiService("127.0.0.1:18100", nil)

	writeSelectedType("oauth-personal")
	if err := svc.EnableProxy(); err == nil {
		t.Fatal("OAuth 认证下 EnableProxy 应拒绝启用")
	}

	writeSelectedType("gemini-api-key")
	if err := svc.EnableProxy(); err != nil {
		t.Fatalf("API Key 认证下 EnableProxy 失败: %v", err)
	}
	if err := svc.DisableProxy(); err != nil {
		t.Fatalf("DisableProxy 失败: %v", err)
	}
}

// TestRenderGeminiEnvContentPreservesUserLines .env 按行合并必须保留注释与 export 行；
// 预览与落盘走同一条逻辑，否则配置编辑器里显示的是被抹掉注释的版本，
// 用户一点"应用"就把手写内容永久写没了。
func TestRenderGeminiEnvContentPreservesUserLines(t *testing.T) {
	existing := "# 我的自定义配置\n" +
		"export GEMINI_EXTRA=1\n" +
		"GOOGLE_GEMINI_BASE_URL=http://old:18100/gemini\n" +
		"\n" +
		"# 尾部注释\n"

	got := renderGeminiEnvContent(existing, map[string]string{
		"GOOGLE_GEMINI_BASE_URL": "http://127.0.0.1:18100/gemini",
		"GEMINI_API_KEY":         "k",
	})

	for _, want := range []string{"# 我的自定义配置", "export GEMINI_EXTRA=1", "# 尾部注释"} {
		if !strings.Contains(got, want) {
			t.Errorf("用户手写行 %q 丢失\n实际内容:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:18100/gemini") {
		t.Errorf("受管键未被更新为新值\n实际内容:\n%s", got)
	}
	if strings.Contains(got, "http://old:18100/gemini") {
		t.Errorf("受管键的旧值未被替换\n实际内容:\n%s", got)
	}
	if !strings.Contains(got, "GEMINI_API_KEY=k") {
		t.Errorf("缺失的键未被追加\n实际内容:\n%s", got)
	}
}

// TestRenderGeminiEnvContentOnEmptyFile 文件不存在时按 envConfig 直接生成。
func TestRenderGeminiEnvContentOnEmptyFile(t *testing.T) {
	got := renderGeminiEnvContent("", map[string]string{"GEMINI_API_KEY": "k"})
	if got != "GEMINI_API_KEY=k\n" {
		t.Errorf("空文件生成结果 = %q, want %q", got, "GEMINI_API_KEY=k\n")
	}
}

// TestGeminiDuplicateProviderUniqueID 复制按钮无防抖，同一秒内连点会生成相同 ID
// （原实现用 time.Now().Unix() 秒级时间戳），导致两条供应商 ID 冲突。
func TestGeminiDuplicateProviderUniqueID(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	assertHomeIsolated(t, tmpHome)

	svc := &GeminiService{
		providers: []GeminiProvider{{ID: "src", Name: "A", BaseURL: "https://a", APIKey: "k"}},
	}

	seen := map[string]bool{"src": true}
	for i := 0; i < 3; i++ {
		cloned, err := svc.DuplicateProvider("src")
		if err != nil {
			t.Fatalf("第 %d 次复制失败: %v", i+1, err)
		}
		if seen[cloned.ID] {
			t.Fatalf("第 %d 次复制生成了重复 ID %q", i+1, cloned.ID)
		}
		seen[cloned.ID] = true
	}
}
