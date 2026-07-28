package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// newCliConfigTestService 构造使用临时家目录的服务实例
func newCliConfigTestService(t *testing.T) *CliConfigService {
	t.Helper()
	return &CliConfigService{
		relayAddr: ":18100",
		homeDir:   t.TempDir(),
	}
}

// TestSetTemplateCorruptedFileAborts 模板文件损坏时 SetTemplate 应中止而非用空模板覆盖
func TestSetTemplateCorruptedFileAborts(t *testing.T) {
	svc := newCliConfigTestService(t)

	tplPath := svc.getTemplatesPath()
	if err := os.MkdirAll(filepath.Dir(tplPath), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	corrupted := "{invalid json"
	if err := os.WriteFile(tplPath, []byte(corrupted), 0o600); err != nil {
		t.Fatalf("写入损坏模板失败: %v", err)
	}

	err := svc.SetTemplate("claude", map[string]interface{}{"model": "x"}, true)
	if err == nil {
		t.Fatalf("模板文件损坏时 SetTemplate 应返回错误")
	}

	content, readErr := os.ReadFile(tplPath)
	if readErr != nil {
		t.Fatalf("读取模板文件失败: %v", readErr)
	}
	if string(content) != corrupted {
		t.Fatalf("损坏的模板文件不应被覆盖, got: %s", content)
	}
}

// TestSaveConfigReadFailureAborts 配置文件存在但读取失败时应中止保存，避免整体覆盖
// 用同名目录制造非 ErrNotExist 的读取错误
func TestSaveConfigReadFailureAborts(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		relPath  []string
	}{
		{"claude", "claude", []string{".claude", "settings.json"}},
		{"codex", "codex", []string{".codex", "config.toml"}},
		{"gemini", "gemini", []string{".gemini", ".env"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newCliConfigTestService(t)

			configPath := filepath.Join(append([]string{svc.homeDir}, tt.relPath...)...)
			if err := os.MkdirAll(configPath, 0o755); err != nil {
				t.Fatalf("创建目录失败: %v", err)
			}

			err := svc.SaveConfig(tt.platform, map[string]interface{}{})
			if err == nil {
				t.Fatalf("读取失败时 SaveConfig 应返回错误")
			}
			if !strings.Contains(err.Error(), "读取") {
				t.Fatalf("应返回读取错误而非继续覆盖, got: %v", err)
			}
		})
	}
}

// TestRestoreDefaultClaudeLegacyBackup Claude 平台应能回退到旧格式备份 cc-studio.back.settings.json
func TestRestoreDefaultClaudeLegacyBackup(t *testing.T) {
	svc := newCliConfigTestService(t)

	claudeDir := filepath.Join(svc.homeDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	backup := `{"env":{"ANTHROPIC_BASE_URL":"https://orig.example.com"}}`
	if err := os.WriteFile(filepath.Join(claudeDir, "cc-studio.back.settings.json"), []byte(backup), 0o600); err != nil {
		t.Fatalf("写入旧格式备份失败: %v", err)
	}
	if err := os.WriteFile(svc.getClaudeConfigPath(), []byte(`{"env":{}}`), 0o600); err != nil {
		t.Fatalf("写入当前配置失败: %v", err)
	}

	if err := svc.RestoreDefault("claude"); err != nil {
		t.Fatalf("存在旧格式备份时 RestoreDefault 应成功: %v", err)
	}

	content, err := os.ReadFile(svc.getClaudeConfigPath())
	if err != nil {
		t.Fatalf("读取恢复后配置失败: %v", err)
	}
	if string(content) != backup {
		t.Fatalf("恢复后的配置应等于备份内容, got: %s", content)
	}
}

// TestSaveGeminiConfigPreservesUnmanagedLines 保存 Gemini 配置时应保留注释与 export 等非常规行
func TestSaveGeminiConfigPreservesUnmanagedLines(t *testing.T) {
	svc := newCliConfigTestService(t)

	envPath := svc.getGeminiEnvPath()
	if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	original := strings.Join([]string{
		"# team key, rotate monthly",
		"GEMINI_API_KEY=real-key",
		"export GEMINI_EXTRA=1",
		"GEMINI_MODEL=old-model",
	}, "\n")
	if err := os.WriteFile(envPath, []byte(original), 0o600); err != nil {
		t.Fatalf("写入 .env 失败: %v", err)
	}

	if err := svc.saveGeminiConfig(map[string]interface{}{"GEMINI_MODEL": "new-model"}); err != nil {
		t.Fatalf("saveGeminiConfig 失败: %v", err)
	}

	content, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("读取 .env 失败: %v", err)
	}
	got := string(content)
	for _, want := range []string{
		"# team key, rotate monthly",
		"export GEMINI_EXTRA=1",
		"GEMINI_MODEL=new-model",
		"GEMINI_API_KEY=real-key",
		"GOOGLE_GEMINI_BASE_URL=" + svc.geminiBaseURL(),
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("保存后 .env 缺少 %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "GEMINI_MODEL=old-model") {
		t.Fatalf("旧值未被替换:\n%s", got)
	}
}

// TestUpsertEnvContent 按行更新 .env 的辅助函数
func TestUpsertEnvContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		updates map[string]string
		want    string
	}{
		{
			name:    "替换已有键并保留注释",
			content: "# note\nA=1\nB=2",
			updates: map[string]string{"A": "9"},
			want:    "# note\nA=9\nB=2",
		},
		{
			name:    "缺失键按序追加",
			content: "A=1",
			updates: map[string]string{"C": "3", "B": "2"},
			want:    "A=1\nB=2\nC=3",
		},
		{
			name:    "export 行原样保留",
			content: "export A=1\nB=2",
			updates: map[string]string{"B": "9"},
			want:    "export A=1\nB=9",
		},
		{
			name:    "CRLF 输入统一为 LF 且保留末尾换行",
			content: "A=1\r\nB=2\r\n",
			updates: map[string]string{"A": "9"},
			want:    "A=9\nB=2\n",
		},
		{
			name:    "空内容仅输出更新键",
			content: "",
			updates: map[string]string{"A": "1"},
			want:    "A=1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := upsertEnvContent(tt.content, tt.updates); got != tt.want {
				t.Fatalf("upsertEnvContent = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSaveClaudeConfigConcurrentMerge 并发保存不同字段时不应互相覆盖
func TestSaveClaudeConfigConcurrentMerge(t *testing.T) {
	svc := newCliConfigTestService(t)

	const n = 16
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("key_%02d", i)
			if err := svc.saveClaudeConfig(map[string]interface{}{key: "v"}); err != nil {
				t.Errorf("saveClaudeConfig(%s) 失败: %v", key, err)
			}
		}(i)
	}
	wg.Wait()

	content, err := os.ReadFile(svc.getClaudeConfigPath())
	if err != nil {
		t.Fatalf("读取 settings.json 失败: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(content, &data); err != nil {
		t.Fatalf("解析 settings.json 失败: %v", err)
	}
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("key_%02d", i)
		if _, ok := data[key]; !ok {
			t.Fatalf("并发保存丢失字段 %s:\n%s", key, content)
		}
	}
}
