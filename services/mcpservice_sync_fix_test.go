package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// setupMCPSyncFixHome 把 HOME/USERPROFILE 指到临时目录，避免测试覆盖真实用户配置
func setupMCPSyncFixHome(t *testing.T) string {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	// Windows 的 os.UserHomeDir() 读的是 USERPROFILE,只设 HOME 会让测试写到真实用户配置目录
	t.Setenv("USERPROFILE", tmpHome)
	assertHomeIsolated(t, tmpHome)
	return tmpHome
}

// 目标配置文件解析失败时必须中止同步，不得按空配置重建后覆盖原文件
// (否则 ~/.claude.json 等文件里 mcpServers 之外的全部内容会无备份丢失)
func TestSyncServersAbortOnCorruptedTargetConfig(t *testing.T) {
	servers := []MCPServer{{
		Name:           "srv",
		Type:           "http",
		URL:            "https://mcp.example/endpoint",
		EnablePlatform: []string{platClaudeCode, platCodex, platGemini},
	}}

	cases := []struct {
		name     string
		relParts []string
		content  string
		sync     func(*MCPService, []MCPServer) error
	}{
		{
			name:     "claude 半截 JSON",
			relParts: []string{claudeMcpFile},
			content:  `{"oauthAccount":{"email":"a@b.c"},"mcpSer`,
			sync:     (*MCPService).syncClaudeServers,
		},
		{
			name:     "codex 非法 TOML",
			relParts: []string{codexDirName, codexConfigFile},
			content:  "model = \"gpt-5\"\nmcp_servers = [unclosed",
			sync:     (*MCPService).syncCodexServers,
		},
		{
			name:     "gemini 半截 JSON",
			relParts: []string{geminiDirName, geminiConfigFile},
			content:  `{"theme":"dark","mcpSer`,
			sync:     (*MCPService).syncGeminiServers,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmpHome := setupMCPSyncFixHome(t)
			path := filepath.Join(append([]string{tmpHome}, tc.relParts...)...)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("创建目录失败: %v", err)
			}
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatalf("写入损坏配置失败: %v", err)
			}

			ms := NewMCPService()
			if err := tc.sync(ms, servers); err == nil {
				t.Fatalf("解析失败时应返回错误中止同步，实际返回 nil")
			}

			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("读取目标文件失败: %v", err)
			}
			if string(after) != tc.content {
				t.Fatalf("解析失败时不得改写原文件，改写前:\n%s\n改写后:\n%s", tc.content, string(after))
			}
		})
	}
}

// 目标配置文件合法时，同步只应替换 mcpServers 键，其余键必须原样保留
func TestSyncClaudeServersPreservesOtherKeys(t *testing.T) {
	tmpHome := setupMCPSyncFixHome(t)
	path := filepath.Join(tmpHome, claudeMcpFile)
	original := `{"oauthAccount":{"email":"a@b.c"},"projects":{"/tmp/x":{"history":["cmd"]}},"mcpServers":{}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("写入初始配置失败: %v", err)
	}

	ms := NewMCPService()
	servers := []MCPServer{{
		Name:           "srv",
		Type:           "http",
		URL:            "https://mcp.example/endpoint",
		EnablePlatform: []string{platClaudeCode},
	}}
	if err := ms.syncClaudeServers(servers); err != nil {
		t.Fatalf("同步失败: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取结果失败: %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("结果不是合法 JSON: %v", err)
	}
	if _, ok := payload["oauthAccount"]; !ok {
		t.Fatalf("oauthAccount 键丢失: %s", string(data))
	}
	if _, ok := payload["projects"]; !ok {
		t.Fatalf("projects 键丢失: %s", string(data))
	}
	var mcp map[string]claudeDesktopServer
	if err := json.Unmarshal(payload["mcpServers"], &mcp); err != nil {
		t.Fatalf("mcpServers 解析失败: %v", err)
	}
	if _, ok := mcp["srv"]; !ok {
		t.Fatalf("mcpServers 未写入 srv: %s", string(data))
	}
}

// sse / streamable-http 等基于 url 的远程类型应归一为 http，而非落到 stdio
func TestNormalizeServerTypeRemoteAliases(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"http", "http"},
		{"HTTP", "http"},
		{"sse", "http"},
		{"SSE", "http"},
		{"streamable-http", "http"},
		{"streamable_http", "http"},
		{"streamablehttp", "http"},
		{"stdio", "stdio"},
		{"", "stdio"},
		{"unknown", "stdio"},
	}
	for _, tc := range cases {
		if got := normalizeServerType(tc.input); got != tc.want {
			t.Errorf("normalizeServerType(%q) = %q, 期望 %q", tc.input, got, tc.want)
		}
	}
}
