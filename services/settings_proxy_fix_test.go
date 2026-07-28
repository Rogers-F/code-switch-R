package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// setupTempHome 将家目录指向临时目录，避免测试覆盖真实用户配置
// Windows 上 os.UserHomeDir 读 USERPROFILE，其余平台读 HOME，两者都要设
func setupTempHome(t *testing.T) string {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	return tmpHome
}

// writeProviderSnapshot 写入直连应用所需的 provider 快照文件
func writeProviderSnapshot(t *testing.T, home, filename string, providers []Provider) {
	t.Helper()
	dir := filepath.Join(home, ".code-switch")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	data, err := json.Marshal(providerEnvelope{Providers: providers})
	if err != nil {
		t.Fatalf("序列化 provider 快照失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), data, 0o600); err != nil {
		t.Fatalf("写入 provider 快照失败: %v", err)
	}
}

// 缺陷回归：代理已启用后 settings.json 被手工改坏，再次 EnableProxy 不应无备份地清空用户配置
func TestClaudeEnableProxyBrokenSettingsWithStateReturnsError(t *testing.T) {
	tmpHome := setupTempHome(t)
	css := NewClaudeSettingsService(":18100")

	dir := filepath.Join(tmpHome, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	settingsPath := filepath.Join(dir, "settings.json")
	valid := `{"model":"my-model","env":{"FOO":"bar"}}`
	if err := os.WriteFile(settingsPath, []byte(valid), 0o600); err != nil {
		t.Fatalf("写入 settings.json 失败: %v", err)
	}

	// 首次启用：写入基线与备份
	if err := css.EnableProxy(); err != nil {
		t.Fatalf("首次 EnableProxy 失败: %v", err)
	}

	// 用户手工编辑引入 JSON 语法错误
	broken := "{ \"model\": \"my-model\", // 非法注释\n"
	if err := os.WriteFile(settingsPath, []byte(broken), 0o600); err != nil {
		t.Fatalf("写入损坏内容失败: %v", err)
	}

	// 再次启用：应返回错误而非用空配置覆盖
	if err := css.EnableProxy(); err == nil {
		t.Fatal("期望解析失败时返回错误，实际返回 nil")
	}
	got, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("读取 settings.json 失败: %v", err)
	}
	if string(got) != broken {
		t.Fatalf("损坏文件不应被覆盖，当前内容: %s", got)
	}
}

// 首次启用遇损坏 settings.json 仍应走"备份后降级"路径（原有行为保持）
func TestClaudeEnableProxyBrokenSettingsFirstEnableDegrades(t *testing.T) {
	tmpHome := setupTempHome(t)
	css := NewClaudeSettingsService(":18100")

	dir := filepath.Join(tmpHome, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	settingsPath := filepath.Join(dir, "settings.json")
	broken := "{ 不是合法 JSON"
	if err := os.WriteFile(settingsPath, []byte(broken), 0o600); err != nil {
		t.Fatalf("写入损坏内容失败: %v", err)
	}

	if err := css.EnableProxy(); err != nil {
		t.Fatalf("首次 EnableProxy 应降级继续: %v", err)
	}

	// 备份中应保留损坏前内容
	backup, err := os.ReadFile(filepath.Join(dir, claudeBackupFileName))
	if err != nil {
		t.Fatalf("读取备份失败: %v", err)
	}
	if string(backup) != broken {
		t.Fatalf("备份内容不符: %s", backup)
	}

	// settings.json 应为含代理字段的合法 JSON
	var payload map[string]any
	data, _ := os.ReadFile(settingsPath)
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("降级后 settings.json 应为合法 JSON: %v", err)
	}
}

// 缺陷回归：代理已启用后 config.toml 被手工改坏，再次 EnableProxy 不应静默重写整份文件
func TestCodexEnableProxyBrokenConfigWithStateReturnsError(t *testing.T) {
	tmpHome := setupTempHome(t)
	css := NewCodexSettingsService(":18100", nil)

	dir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte("model = \"gpt-5\"\n"), 0o600); err != nil {
		t.Fatalf("写入 config.toml 失败: %v", err)
	}

	if err := css.EnableProxy(); err != nil {
		t.Fatalf("首次 EnableProxy 失败: %v", err)
	}

	// 用户手工编辑引入 TOML 语法错误（未闭合字符串）
	broken := "model = \"gpt-5\nprofiles = 坏"
	if err := os.WriteFile(configPath, []byte(broken), 0o600); err != nil {
		t.Fatalf("写入损坏内容失败: %v", err)
	}

	if err := css.EnableProxy(); err == nil {
		t.Fatal("期望解析失败时返回错误，实际返回 nil")
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取 config.toml 失败: %v", err)
	}
	if string(got) != broken {
		t.Fatalf("损坏文件不应被覆盖，当前内容: %s", got)
	}
}

// 缺陷回归：auth.json 含非字符串值（ChatGPT 登录态的 tokens 对象）时，
// EnableProxy 仍应捕获原始 OPENAI_API_KEY 基线，DisableProxy 后恢复而非误删
func TestCodexProxyRoundTripPreservesAuthKeyWithTokensObject(t *testing.T) {
	tmpHome := setupTempHome(t)
	css := NewCodexSettingsService(":18100", nil)

	dir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	authPath := filepath.Join(dir, "auth.json")
	authJSON := `{"OPENAI_API_KEY":"sk-user-real","tokens":{"id_token":"abc","account_id":"acc"}}`
	if err := os.WriteFile(authPath, []byte(authJSON), 0o600); err != nil {
		t.Fatalf("写入 auth.json 失败: %v", err)
	}

	if err := css.EnableProxy(); err != nil {
		t.Fatalf("EnableProxy 失败: %v", err)
	}

	// 基线应记录到用户真实 Key
	state, err := LoadProxyState("codex")
	if err != nil {
		t.Fatalf("加载状态文件失败: %v", err)
	}
	if state.OriginalAuthToken == nil || *state.OriginalAuthToken != "sk-user-real" {
		t.Fatalf("基线未捕获原始 API Key: %+v", state.OriginalAuthToken)
	}

	if err := css.DisableProxy(); err != nil {
		t.Fatalf("DisableProxy 失败: %v", err)
	}

	// 恢复后 OPENAI_API_KEY 应回到真实 Key，tokens 对象保留
	data, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("读取恢复后的 auth.json 失败: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("恢复后的 auth.json 解析失败: %v", err)
	}
	if got, _ := payload["OPENAI_API_KEY"].(string); got != "sk-user-real" {
		t.Fatalf("OPENAI_API_KEY 未恢复，当前值: %v", payload["OPENAI_API_KEY"])
	}
	if _, ok := payload["tokens"].(map[string]any); !ok {
		t.Fatalf("tokens 对象丢失: %v", payload)
	}
}

// 缺陷回归：直连应用应按 ConnectivityAuthType 写入对应认证字段
func TestClaudeApplySingleProviderRespectsAuthType(t *testing.T) {
	tests := []struct {
		name        string
		providerID  int
		wantKeyName string
		wantKey     string
		absentName  string
	}{
		{"x-api-key 型写 ANTHROPIC_API_KEY", 1, "ANTHROPIC_API_KEY", "sk-ant-official", "ANTHROPIC_AUTH_TOKEN"},
		{"bearer 型写 ANTHROPIC_AUTH_TOKEN", 2, "ANTHROPIC_AUTH_TOKEN", "sk-relay", "ANTHROPIC_API_KEY"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpHome := setupTempHome(t)
			writeProviderSnapshot(t, tmpHome, "claude-code.json", []Provider{
				{ID: 1, Name: "official", APIURL: "https://api.anthropic.com", APIKey: "sk-ant-official", ConnectivityAuthType: "x-api-key"},
				{ID: 2, Name: "relay", APIURL: "https://relay.example.com", APIKey: "sk-relay"},
			})

			css := NewClaudeSettingsService(":18100")
			if err := css.ApplySingleProvider(tt.providerID); err != nil {
				t.Fatalf("ApplySingleProvider 失败: %v", err)
			}

			data, err := os.ReadFile(filepath.Join(tmpHome, ".claude", "settings.json"))
			if err != nil {
				t.Fatalf("读取 settings.json 失败: %v", err)
			}
			var payload map[string]any
			if err := json.Unmarshal(data, &payload); err != nil {
				t.Fatalf("settings.json 解析失败: %v", err)
			}
			env, _ := payload["env"].(map[string]any)
			if env == nil {
				t.Fatal("env 不存在")
			}
			if got, _ := env[tt.wantKeyName].(string); got != tt.wantKey {
				t.Fatalf("%s 期望 %q，实际 %v", tt.wantKeyName, tt.wantKey, env[tt.wantKeyName])
			}
			if _, exists := env[tt.absentName]; exists {
				t.Fatalf("%s 不应存在: %v", tt.absentName, env[tt.absentName])
			}

			// 反查直连应用的 provider ID 应兼容两种认证字段
			id, err := css.GetDirectAppliedProviderID()
			if err != nil {
				t.Fatalf("GetDirectAppliedProviderID 失败: %v", err)
			}
			if id == nil || *id != int64(tt.providerID) {
				t.Fatalf("期望匹配 provider %d，实际 %v", tt.providerID, id)
			}
		})
	}
}

// 缺陷回归：直连应用应按供应商上游协议写 wire_api
func TestCodexApplySingleProviderWireAPIFollowsUpstreamProtocol(t *testing.T) {
	tests := []struct {
		name        string
		providerID  int
		providerKey string
		wantWireAPI string
	}{
		{"openai_chat 型写 chat", 1, "chat-relay", "chat"},
		{"默认写 responses", 2, "resp-relay", "responses"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpHome := setupTempHome(t)
			writeProviderSnapshot(t, tmpHome, "codex.json", []Provider{
				{ID: 1, Name: "chat-relay", APIURL: "https://relay.example.com/v1", APIKey: "sk-1", UpstreamProtocol: "openai_chat"},
				{ID: 2, Name: "resp-relay", APIURL: "https://resp.example.com", APIKey: "sk-2"},
			})

			css := NewCodexSettingsService(":18100", nil)
			if err := css.ApplySingleProvider(tt.providerID); err != nil {
				t.Fatalf("ApplySingleProvider 失败: %v", err)
			}

			data, err := os.ReadFile(filepath.Join(tmpHome, ".codex", "config.toml"))
			if err != nil {
				t.Fatalf("读取 config.toml 失败: %v", err)
			}
			var cfg codexConfig
			if err := toml.Unmarshal(data, &cfg); err != nil {
				t.Fatalf("config.toml 解析失败: %v", err)
			}
			provider, ok := cfg.ModelProviders[tt.providerKey]
			if !ok {
				t.Fatalf("model_providers.%s 不存在: %+v", tt.providerKey, cfg.ModelProviders)
			}
			if provider.WireAPI != tt.wantWireAPI {
				t.Fatalf("wire_api 期望 %q，实际 %q", tt.wantWireAPI, provider.WireAPI)
			}
		})
	}
}

// AtomicWriteBytes 复用带 fsync 的写入路径后，成功写入不应残留临时文件
func TestAtomicWriteBytesWritesContentWithoutTempResidue(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "config.json")

	if err := AtomicWriteBytes(target, []byte(`{"a":1}`)); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	// 覆盖写
	if err := AtomicWriteBytes(target, []byte(`{"a":2}`)); err != nil {
		t.Fatalf("覆盖写入失败: %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("读取目标文件失败: %v", err)
	}
	if string(data) != `{"a":2}` {
		t.Fatalf("内容不符: %s", data)
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("读取目录失败: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("目录中存在临时残留: %v", names)
	}
}
