package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupPromptTestHome 把 HOME/USERPROFILE 指向临时目录，避免污染真实用户配置
func setupPromptTestHome(t *testing.T) string {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	return tmpHome
}

// 损坏的 prompts.json 应被保留为 .bad-* 备份，后续保存不会静默覆盖原始数据
func TestPromptServiceLoadCorruptConfigPreservesBackup(t *testing.T) {
	tmpHome := setupPromptTestHome(t)

	cfgDir := filepath.Join(tmpHome, ".code-switch")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	corrupt := `{"claude": {"p1": {"id": "p1", "name": "重要提示词"`
	cfgPath := filepath.Join(cfgDir, "prompts.json")
	if err := os.WriteFile(cfgPath, []byte(corrupt), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewPromptService()

	// 损坏文件应被改名保留
	matches, err := filepath.Glob(cfgPath + ".bad-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("期望 1 个 .bad-* 备份文件，实际 %d 个", len(matches))
	}

	// 后续保存会重建 prompts.json，但备份中的原始数据必须仍然存在
	if err := svc.UpsertPrompt("claude", "new", Prompt{Name: "新提示词", Content: "内容"}); err != nil {
		t.Fatalf("UpsertPrompt 失败: %v", err)
	}
	backup, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != corrupt {
		t.Fatalf("备份内容被改动: %q", string(backup))
	}
}

// 平台字段为 null 时不应导致 nil map 赋值 panic
func TestPromptServiceLoadNullPlatformMap(t *testing.T) {
	tmpHome := setupPromptTestHome(t)

	cfgDir := filepath.Join(tmpHome, ".code-switch")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	data := `{"claude": null, "codex": {}, "gemini": {}}`
	if err := os.WriteFile(filepath.Join(cfgDir, "prompts.json"), []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewPromptService()
	if err := svc.UpsertPrompt("claude", "p1", Prompt{Name: "n", Content: "c"}); err != nil {
		t.Fatalf("UpsertPrompt 失败: %v", err)
	}
	prompts, err := svc.GetPrompts("claude")
	if err != nil {
		t.Fatalf("GetPrompts 失败: %v", err)
	}
	if _, ok := prompts["p1"]; !ok {
		t.Fatal("新增的提示词未保存")
	}
}

// 启用→禁用切换必须清空目标文件，否则 CLI 仍会加载已禁用的提示词
func TestPromptServiceUpsertDisableClearsTargetFile(t *testing.T) {
	tmpHome := setupPromptTestHome(t)
	svc := NewPromptService()

	prompt := Prompt{ID: "p1", Name: "n", Content: "提示词正文", Enabled: true}
	if err := svc.UpsertPrompt("claude", "p1", prompt); err != nil {
		t.Fatalf("启用失败: %v", err)
	}

	filePath := filepath.Join(tmpHome, ".claude", "CLAUDE.md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "提示词正文" {
		t.Fatalf("启用后文件内容错误: %q", string(data))
	}

	prompt.Enabled = false
	if err := svc.UpsertPrompt("claude", "p1", prompt); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}
	data, err = os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("禁用后文件应为空，实际: %q", string(data))
	}
}

// 外部把文件清空后，重启同步不应把空内容回填覆盖已保存的正文
func TestPromptServiceSyncSkipsEmptyFile(t *testing.T) {
	tmpHome := setupPromptTestHome(t)

	svc1 := NewPromptService()
	if err := svc1.UpsertPrompt("claude", "p1", Prompt{ID: "p1", Name: "n", Content: "重要正文", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	// 外部清空文件（非删除）
	filePath := filepath.Join(tmpHome, ".claude", "CLAUDE.md")
	if err := os.WriteFile(filePath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	// 模拟重启：新实例的 lastWriteTime 为空，首次访问必触发同步
	svc2 := NewPromptService()
	prompts, err := svc2.GetPrompts("claude")
	if err != nil {
		t.Fatalf("GetPrompts 失败: %v", err)
	}
	if prompts["p1"].Content != "重要正文" {
		t.Fatalf("空文件被回填，正文丢失: %q", prompts["p1"].Content)
	}
}

// 非 UTF-8 的外部文件不应被同步回填（JSON 序列化会把内容替换成乱码）
func TestPromptServiceSyncSkipsInvalidUTF8(t *testing.T) {
	tmpHome := setupPromptTestHome(t)

	svc1 := NewPromptService()
	if err := svc1.UpsertPrompt("claude", "p1", Prompt{ID: "p1", Name: "n", Content: "原始正文", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	// GBK 编码的"中文"两字，不是合法 UTF-8
	gbk := []byte{0xD6, 0xD0, 0xCE, 0xC4}
	filePath := filepath.Join(tmpHome, ".claude", "CLAUDE.md")
	if err := os.WriteFile(filePath, gbk, 0644); err != nil {
		t.Fatal(err)
	}

	svc2 := NewPromptService()
	prompts, err := svc2.GetPrompts("claude")
	if err != nil {
		t.Fatalf("GetPrompts 失败: %v", err)
	}
	if prompts["p1"].Content != "原始正文" {
		t.Fatalf("非 UTF-8 内容被回填: %q", prompts["p1"].Content)
	}
}

// 导入非 UTF-8 文件应直接报错，而不是存入会被替换成乱码的内容
func TestPromptServiceImportRejectsInvalidUTF8(t *testing.T) {
	tmpHome := setupPromptTestHome(t)

	claudeDir := filepath.Join(tmpHome, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatal(err)
	}
	gbk := []byte{0xD6, 0xD0, 0xCE, 0xC4}
	if err := os.WriteFile(filepath.Join(claudeDir, "CLAUDE.md"), gbk, 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewPromptService()
	if _, err := svc.ImportFromFile("claude"); err == nil {
		t.Fatal("导入非 UTF-8 文件应报错")
	} else if !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("错误信息应说明编码问题: %v", err)
	}
}
