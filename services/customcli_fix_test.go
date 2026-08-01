package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCustomCliEnableProxyKeepsOriginalBackup 重复启用代理不应用注入值覆盖原始配置备份
func TestCustomCliEnableProxyKeepsOriginalBackup(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)

	svc := NewCustomCliService(":18100", nil)

	cfgDir := filepath.Join(tmp, "tool")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	cfgPath := filepath.Join(cfgDir, "config.json")
	original := `{"base_url":"https://orig.example.com","token":"secret"}`
	if err := os.WriteFile(cfgPath, []byte(original), 0o600); err != nil {
		t.Fatalf("写入原始配置失败: %v", err)
	}

	tool, err := svc.CreateTool(CustomCliTool{
		Name: "demo",
		ConfigFiles: []ConfigFile{
			{Label: "主配置", Path: "~/tool/config.json", Format: "json"},
		},
		ProxyInjection: []ProxyInjection{
			{TargetFileID: "file-1", BaseUrlField: "base_url", AuthTokenField: "token"},
		},
	})
	if err != nil {
		t.Fatalf("CreateTool 失败: %v", err)
	}

	if err := svc.EnableProxy(tool.ID); err != nil {
		t.Fatalf("首次 EnableProxy 失败: %v", err)
	}

	backupPath := cfgPath + ".code-switch.backup"
	backup, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("读取备份失败: %v", err)
	}
	if !strings.Contains(string(backup), "https://orig.example.com") {
		t.Fatalf("首次启用后备份应为原始配置, got: %s", backup)
	}

	// 重复启用（模拟前端状态误判后用户再次点击开关）
	if err := svc.EnableProxy(tool.ID); err != nil {
		t.Fatalf("重复 EnableProxy 失败: %v", err)
	}

	backup, err = os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("读取备份失败: %v", err)
	}
	if !strings.Contains(string(backup), "https://orig.example.com") {
		t.Fatalf("重复启用后备份被注入值覆盖: %s", backup)
	}

	if err := svc.DisableProxy(tool.ID); err != nil {
		t.Fatalf("DisableProxy 失败: %v", err)
	}

	restored, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("读取恢复后配置失败: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(restored, &data); err != nil {
		t.Fatalf("解析恢复后配置失败: %v", err)
	}
	if data["base_url"] != "https://orig.example.com" || data["token"] != "secret" {
		t.Fatalf("禁用代理后应恢复原始配置, got: %s", restored)
	}
}
