package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// seedExportTestData 造一套覆盖各扩展字段与脱敏载体的配置数据
func seedExportTestData(t *testing.T) (*ProviderService, *GeminiService, *MCPService, *PromptService) {
	t.Helper()

	ps := NewProviderService()
	if err := ps.SaveProviders("claude", []Provider{
		{
			ID: 1, Name: "ClaudeVendor", APIURL: "https://claude.example.com", APIKey: "sk-claude-secret",
			Site: "https://claude.example.com", Enabled: true, Level: 3,
			SupportedModels:    map[string]bool{"claude-*": true},
			ModelMapping:       map[string]string{"claude-x": "claude-*"},
			InsecureSkipVerify: true,
		},
		// 同 URL 不同 Key：多账号场景，脱敏往返后也必须两条都保留
		{
			ID: 2, Name: "ClaudeAcct2", APIURL: "https://claude.example.com", APIKey: "sk-claude-secret2",
			Enabled: true, Level: 3,
		},
	}); err != nil {
		t.Fatalf("预置 claude 供应商失败: %v", err)
	}
	if err := ps.SaveProviders("codex", []Provider{{
		ID: 1, Name: "CodexVendor", APIURL: "https://codex.example.com", APIKey: "sk-codex-secret",
		Enabled: false, Level: 2,
	}}); err != nil {
		t.Fatalf("预置 codex 供应商失败: %v", err)
	}

	gs := NewGeminiService("127.0.0.1:18100", nil)
	if err := gs.AddProvider(GeminiProvider{
		Name: "GemVendor", BaseURL: "https://gem.example.com", APIKey: "sk-gem-secret",
		Model: "gemini-2.5-pro", Enabled: true, Level: 2,
		SupportedModels: map[string]bool{"gemini-*": true},
		ModelMapping:    map[string]string{"gemini-x": "gemini-*"},
		EnvConfig: map[string]string{
			"GEMINI_API_KEY": "sk-gem-secret",
			// 键名不敏感但值是带敏感查询参数的 URL
			"GOOGLE_GEMINI_BASE_URL": "https://gem.example.com?access_token=envurlsecret",
			// 大写 scheme 同样是合法 URL，不得绕过清洗
			"MIRROR_URL": "HTTPS://u:p@mirror.example.com/?api_key=upperschemesecret",
		},
		// 嵌套结构里的凭据：脱敏必须递归处理，含敏感父键下的
		// 复合对象与数组两种载体
		SettingsConfig: map[string]any{
			"security": map[string]any{
				"auth": map[string]any{
					"apiKey": "settings-secret",
					"value":  "settings-composite-secret",
				},
			},
			"tokens": []any{"settings-array-secret"},
		},
	}); err != nil {
		t.Fatalf("预置 gemini 供应商失败: %v", err)
	}

	ms := NewMCPService()
	if err := ms.SaveServers([]MCPServer{
		{
			Name: "mcp-both-off", Type: "stdio", Command: "npx",
			Args: []string{"tool", "--api-key", "mcp-args-secret", "--verbose"},
			Env:  map[string]string{"MY_TOKEN": "mcp-env-secret", "KEYBOARD_LAYOUT": "us"},
		},
		{
			Name: "mcp-claude-on", Type: "http",
			URL:            "https://user:pass@mcp.example.com/sse?api_key=urlsecret&x=1",
			EnablePlatform: []string{platClaudeCode},
		},
		{
			Name: "mcp-gem-on", Type: "http",
			URL:            "https://mcpgem.example.com/sse",
			EnablePlatform: []string{platGemini},
		},
	}); err != nil {
		t.Fatalf("预置 MCP 失败: %v", err)
	}

	prs := NewPromptService()
	if _, err := prs.ImportPrompts(map[string][]Prompt{
		"claude": {{ID: "p1", Name: "Reviewer", Content: "You review code"}},
	}); err != nil {
		t.Fatalf("预置提示词失败: %v", err)
	}

	return ps, gs, ms, prs
}

func newTestExportService(ps *ProviderService, gs *GeminiService, ms *MCPService, prs *PromptService) *ExportService {
	es := NewExportService(ps, gs, ms, prs, func() string { return "v0.0.0-test" })
	es.now = func() time.Time { return time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC) }
	return es
}

// switchToFreshHome 在不重建 app.db 连接的前提下切到全新家目录：
// 同一测试内二次调用 setupRenameTestEnv 会让首个 app.db 句柄泄漏，
// Windows 上 TempDir 清理删不掉文件直接判测试失败
func switchToFreshHome(t *testing.T) string {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	assertHomeIsolated(t, tmpHome)
	return tmpHome
}

func mustBuildBundle(t *testing.T, es *ExportService, includeSecrets bool) (*exportBundle, []string) {
	t.Helper()
	snap, err := es.collectSnapshot()
	if err != nil {
		t.Fatalf("collectSnapshot 失败: %v", err)
	}
	bundle, redacted, err := buildExportBundle(snap, includeSecrets, "v0.0.0-test", es.now())
	if err != nil {
		t.Fatalf("buildExportBundle 失败: %v", err)
	}
	return bundle, redacted
}

// 明文导出 → 全新环境导入：字段全量保真
func TestExportRoundTripFullFidelity(t *testing.T) {
	setupRenameTestEnv(t)
	ps, gs, ms, prs := seedExportTestData(t)
	es := newTestExportService(ps, gs, ms, prs)

	bundle, redacted := mustBuildBundle(t, es, true)
	if len(redacted) != 0 {
		t.Fatalf("明文导出不应有脱敏字段: %v", redacted)
	}
	exportPath := filepath.Join(t.TempDir(), "bundle.json")
	if err := es.writeBundleFile(exportPath, bundle); err != nil {
		t.Fatalf("写导出文件失败: %v", err)
	}

	// 切换到全新 home 再导入
	switchToFreshHome(t)
	is, gs2, prs2 := newTestImportService(t)
	result, err := is.ImportFromPath(exportPath)
	if err != nil {
		t.Fatalf("导入导出包失败: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("导入不应有阶段错误: %v", result.Errors)
	}
	if result.ImportedProviders != 4 {
		t.Errorf("ImportedProviders = %d, 期望 4（claude 2 + codex 1 + gemini 1）", result.ImportedProviders)
	}
	if result.ImportedMCP != 3 {
		t.Errorf("ImportedMCP = %d, 期望 3", result.ImportedMCP)
	}
	if result.ImportedPrompts != 1 {
		t.Errorf("ImportedPrompts = %d, 期望 1", result.ImportedPrompts)
	}

	claude, err := is.providerService.LoadProviders("claude")
	if err != nil || len(claude) != 2 {
		t.Fatalf("claude 供应商恢复失败: %v %+v", err, claude)
	}
	byName := map[string]Provider{}
	for _, p := range claude {
		byName[p.Name] = p
	}
	got := byName["ClaudeVendor"]
	if got.APIKey != "sk-claude-secret" || got.Level != 3 || !got.InsecureSkipVerify ||
		!got.SupportedModels["claude-*"] || got.ModelMapping["claude-x"] != "claude-*" || !got.Enabled {
		t.Errorf("claude 字段未保真: %+v", got)
	}
	if byName["ClaudeAcct2"].APIKey != "sk-claude-secret2" {
		t.Errorf("同 URL 第二账号未保真: %+v", byName["ClaudeAcct2"])
	}
	codex, _ := is.providerService.LoadProviders("codex")
	if len(codex) != 1 || codex[0].Enabled {
		t.Errorf("codex Enabled 应保真为 false: %+v", codex)
	}
	gem := gs2.GetProviders()
	if len(gem) != 1 || gem[0].APIKey != "sk-gem-secret" || !gem[0].SupportedModels["gemini-*"] ||
		gem[0].ModelMapping["gemini-x"] != "gemini-*" || gem[0].Level != 2 || !gem[0].Enabled {
		t.Errorf("gemini 字段未保真: %+v", gem)
	}
	claudePrompts, _ := prs2.StoredPrompts("claude")
	if p, ok := claudePrompts["p1"]; !ok || p.Enabled || p.Content != "You review code" {
		t.Errorf("提示词恢复异常: %+v", claudePrompts)
	}
	// MCP 三平台状态保真（全新 home 会带内置示例服务器，只查导入的三条）
	servers, _ := is.mcpService.ListServers()
	mcpByName := map[string]MCPServer{}
	for _, s := range servers {
		mcpByName[s.Name] = s
	}
	if got := mcpByName["mcp-gem-on"]; !containsPlatform(got.EnablePlatform, platGemini) || containsPlatform(got.EnablePlatform, platClaudeCode) {
		t.Errorf("mcp-gem-on 的平台态未保真: %+v", got.EnablePlatform)
	}
	if got := mcpByName["mcp-claude-on"]; !containsPlatform(got.EnablePlatform, platClaudeCode) {
		t.Errorf("mcp-claude-on 的平台态未保真: %+v", got.EnablePlatform)
	}
	if got, ok := mcpByName["mcp-both-off"]; !ok || len(got.EnablePlatform) != 0 {
		t.Errorf("mcp-both-off 应存在且保持全平台禁用: %+v", got.EnablePlatform)
	}
}

// 脱敏导出：所有已识别凭据被清空，导入恢复为禁用待补，多账号不合并
func TestExportRedactedBundle(t *testing.T) {
	setupRenameTestEnv(t)
	ps, gs, ms, prs := seedExportTestData(t)
	es := newTestExportService(ps, gs, ms, prs)

	bundle, redactedFields := mustBuildBundle(t, es, false)
	if len(redactedFields) == 0 {
		t.Fatal("脱敏导出应产出 redactedFields 清单")
	}
	if !bundle.ExportMeta.Redacted {
		t.Fatal("exportMeta.redacted 应为 true")
	}

	// 序列化结果中不得出现任何密钥哨兵
	data, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	sentinels := []string{
		"sk-claude-secret", "sk-claude-secret2", "sk-codex-secret", "sk-gem-secret",
		"mcp-env-secret", "mcp-args-secret", "urlsecret", "user:pass@",
		"envurlsecret", "settings-secret", "settings-composite-secret", "settings-array-secret",
		"upperschemesecret", "u:p@",
	}
	for _, sentinel := range sentinels {
		if strings.Contains(string(data), sentinel) {
			t.Errorf("脱敏包中仍含密钥哨兵 %q", sentinel)
		}
	}
	// 非敏感 env 键不应误伤
	if !strings.Contains(string(data), "KEYBOARD_LAYOUT") || !strings.Contains(string(data), `"us"`) {
		t.Error("KEYBOARD_LAYOUT=us 不是凭据，不应被清空")
	}

	// 源数据必须原封不动（脱敏只发生在深拷贝快照上）
	claude, _ := ps.LoadProviders("claude")
	for _, p := range claude {
		if p.APIKey == "" {
			t.Errorf("脱敏导出改动了源 claude 配置: %+v", p)
		}
	}
	gemSrc := gs.GetProviders()[0]
	if gemSrc.APIKey != "sk-gem-secret" {
		t.Error("脱敏导出改动了源 gemini APIKey")
	}
	if gemSrc.EnvConfig["GOOGLE_GEMINI_BASE_URL"] != "https://gem.example.com?access_token=envurlsecret" ||
		gemSrc.EnvConfig["MIRROR_URL"] != "HTTPS://u:p@mirror.example.com/?api_key=upperschemesecret" {
		t.Error("脱敏导出改动了源 gemini EnvConfig")
	}
	if auth, ok := gemSrc.SettingsConfig["security"].(map[string]any)["auth"].(map[string]any); !ok || auth["apiKey"] != "settings-secret" {
		t.Error("脱敏导出改动了源 gemini SettingsConfig 嵌套结构")
	}
	servers, _ := ms.ListServers()
	for _, s := range servers {
		if s.Name == "mcp-both-off" && s.Env["MY_TOKEN"] != "mcp-env-secret" {
			t.Error("脱敏导出改动了源 MCP 配置")
		}
	}

	// 导入脱敏包：供应商恢复为禁用待补，同 URL 多账号不得被合并
	exportPath := filepath.Join(t.TempDir(), "redacted.json")
	if err := es.writeBundleFile(exportPath, bundle); err != nil {
		t.Fatalf("写导出文件失败: %v", err)
	}
	switchToFreshHome(t)
	is, gs2, _ := newTestImportService(t)
	result, err := is.ImportFromPath(exportPath)
	if err != nil {
		t.Fatalf("导入脱敏包失败: %v", err)
	}
	if result.ImportedProviders != 4 {
		t.Errorf("脱敏包应恢复全部 4 个供应商（多账号不合并）, got %d", result.ImportedProviders)
	}
	if len(result.Warnings) == 0 {
		t.Error("脱敏包导入应返回待补密钥提醒")
	}
	claude2, _ := is.providerService.LoadProviders("claude")
	if len(claude2) != 2 {
		t.Fatalf("脱敏包同 URL 双账号应都恢复, got %d: %+v", len(claude2), claude2)
	}
	for _, p := range claude2 {
		if p.Enabled || p.APIKey != "" {
			t.Errorf("脱敏 claude 供应商应为禁用且无 Key: %+v", p)
		}
	}
	gem2 := gs2.GetProviders()
	if len(gem2) != 1 || gem2[0].Enabled || gem2[0].APIKey != "" {
		t.Errorf("脱敏 gemini 供应商应为禁用且无 Key: %+v", gem2)
	}
}

// schema 门禁：高版本拒绝并提示升级；缺失/0/负数按非法包拒绝
func TestImportSchemaVersionGate(t *testing.T) {
	setupRenameTestEnv(t)
	is, _, _ := newTestImportService(t)

	cases := []struct {
		name    string
		content string
		wantSub string
	}{
		{"高版本", `{"exportMeta":{"app":"code-switch-r","schemaVersion":99}}`, "升级"},
		{"缺失版本", `{"exportMeta":{"app":"code-switch-r"}}`, "版本号"},
		{"零版本", `{"exportMeta":{"app":"code-switch-r","schemaVersion":0}}`, "版本号"},
		{"负版本", `{"exportMeta":{"app":"code-switch-r","schemaVersion":-3}}`, "版本号"},
		// 类型畸形不得静默降级成 legacy 导入
		{"版本号类型畸形", `{"exportMeta":{"app":"code-switch-r","schemaVersion":"1"}}`, "元数据解析失败"},
		{"redacted 类型畸形", `{"exportMeta":{"app":"code-switch-r","schemaVersion":1,"redacted":"false"}}`, "元数据解析失败"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bundle.json")
			if err := os.WriteFile(path, []byte(tc.content), 0644); err != nil {
				t.Fatalf("写文件失败: %v", err)
			}
			_, err := is.ImportFromPath(path)
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("期望包含 %q 的错误, got %v", tc.wantSub, err)
			}
		})
	}
}

// v1 固定 fixture：锁死导入端兼容性
func TestImportV1GoldenFixture(t *testing.T) {
	setupRenameTestEnv(t)
	is, gs, prs := newTestImportService(t)

	fixture := `{
	  "exportMeta": {"app": "code-switch-r", "schemaVersion": 1, "appVersion": "v0.0.0", "exportedAt": "2026-08-01T12:00:00Z", "redacted": false},
	  "claude": {"providers": {"1": {
	    "id": "1", "name": "GoldClaude", "websiteUrl": "https://gc.example.com",
	    "settingsConfig": {"env": {"ANTHROPIC_BASE_URL": "https://gc.example.com", "ANTHROPIC_AUTH_TOKEN": "sk-gold"}},
	    "extra": {"id": 0, "name": "GoldClaude", "apiUrl": "https://gc.example.com", "apiKey": "sk-gold", "officialSite": "https://gc.example.com", "icon": "", "tint": "", "accent": "", "enabled": true, "level": 5, "supportedModels": {"claude-*": true}}
	  }}},
	  "codex": {"providers": {}},
	  "gemini": {"providers": {"g": {
	    "id": "g", "name": "GoldGem",
	    "settingsConfig": {"env": {"GOOGLE_GEMINI_BASE_URL": "https://gg.example.com", "GEMINI_API_KEY": "sk-gg"}},
	    "extra": {"id": "", "name": "GoldGem", "baseUrl": "https://gg.example.com", "apiKey": "sk-gg", "enabled": false, "level": 4, "modelMapping": {"gemini-a": "gemini-b"}, "supportedModels": {"gemini-a": true, "gemini-b": true}}
	  }}},
	  "mcp": {"claude": {"servers": {}}, "codex": {"servers": {}}},
	  "prompts": {"claude": [{"id": "gp", "name": "GoldPrompt", "content": "hello", "enabled": true}]}
	}`
	path := filepath.Join(t.TempDir(), "golden.json")
	if err := os.WriteFile(path, []byte(fixture), 0644); err != nil {
		t.Fatalf("写 fixture 失败: %v", err)
	}

	result, err := is.ImportFromPath(path)
	if err != nil {
		t.Fatalf("导入 v1 fixture 失败: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("fixture 导入不应有错误: %v", result.Errors)
	}
	claude, _ := is.providerService.LoadProviders("claude")
	if len(claude) != 1 || claude[0].Level != 5 || !claude[0].SupportedModels["claude-*"] || !claude[0].Enabled {
		t.Errorf("fixture claude 恢复异常: %+v", claude)
	}
	gem := gs.GetProviders()
	if len(gem) != 1 || gem[0].Level != 4 || gem[0].Enabled || gem[0].ModelMapping["gemini-a"] != "gemini-b" {
		t.Errorf("fixture gemini 恢复异常（明文包应保真 Enabled=false）: %+v", gem)
	}
	// 包里 enabled=true 的提示词必须落库为禁用
	prompts, _ := prs.StoredPrompts("claude")
	if p, ok := prompts["gp"]; !ok || p.Enabled {
		t.Errorf("fixture 提示词应强制禁用: %+v", prompts)
	}
}

// 导出端 golden：固定快照的规范化输出与基准比对，防止导出端悄悄改形
func TestExportGoldenShape(t *testing.T) {
	snap := &exportSnapshot{
		claudeProviders: []Provider{{
			ID: 7, Name: "G", APIURL: "https://g.example.com", APIKey: "sk-g",
			Enabled: true, Level: 1,
		}},
		prompts: map[string][]Prompt{},
	}
	bundle, _, err := buildExportBundle(snap, true, "v9.9.9", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("buildExportBundle 失败: %v", err)
	}
	data, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	golden := `{"exportMeta":{"app":"code-switch-r","schemaVersion":1,"appVersion":"v9.9.9","exportedAt":"2026-08-01T00:00:00Z","redacted":false},"claude":{"providers":{"7":{"id":"7","name":"G","websiteUrl":"","settingsConfig":{"env":{"ANTHROPIC_AUTH_TOKEN":"sk-g","ANTHROPIC_BASE_URL":"https://g.example.com"},"auth":{},"config":""},"extra":{"id":0,"name":"G","apiUrl":"https://g.example.com","apiKey":"sk-g","officialSite":"","icon":"","tint":"","accent":"","enabled":true,"level":1}}}},"codex":{"providers":{}},"gemini":{"providers":{}},"mcp":{"claude":{"servers":{}},"codex":{"servers":{}},"gemini":{"servers":{}}}}`
	if string(data) != golden {
		t.Errorf("导出形态与 v1 golden 不一致（破坏性变更必须提升 schemaVersion）\n got: %s\nwant: %s", data, golden)
	}
}

// extra 损坏时回退基础字段解析，不崩溃、不拖垮其它条目；
// 脱敏包的坏 extra 条目也必须以禁用态恢复而不是被丢弃
func TestImportCorruptExtraFallsBack(t *testing.T) {
	setupRenameTestEnv(t)
	is, _, _ := newTestImportService(t)

	bundleJSON := `{
	  "exportMeta": {"app": "code-switch-r", "schemaVersion": 1, "redacted": true},
	  "claude": {"providers": {
	    "bad": {"id": "bad", "name": "BadExtra",
	      "settingsConfig": {"env": {"ANTHROPIC_BASE_URL": "https://bad.example.com", "ANTHROPIC_AUTH_TOKEN": ""}},
	      "extra": 42},
	    "good": {"id": "good", "name": "GoodExtra",
	      "settingsConfig": {"env": {"ANTHROPIC_BASE_URL": "https://good.example.com", "ANTHROPIC_AUTH_TOKEN": ""}},
	      "extra": {"name": "GoodExtra", "apiUrl": "https://good.example.com", "apiKey": "", "enabled": true, "level": 2}}
	  }},
	  "codex": {"providers": {}}, "gemini": {"providers": {}},
	  "mcp": {"claude": {"servers": {}}, "codex": {"servers": {}}}
	}`
	path := filepath.Join(t.TempDir(), "corrupt-extra.json")
	if err := os.WriteFile(path, []byte(bundleJSON), 0644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	result, err := is.ImportFromPath(path)
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if result.ImportedProviders != 2 {
		t.Errorf("脱敏包坏 extra 应回退基础字段导入为禁用态，期望 2 个，got %d (errors=%v)", result.ImportedProviders, result.Errors)
	}
	claude, _ := is.providerService.LoadProviders("claude")
	names := map[string]Provider{}
	for _, p := range claude {
		names[p.Name] = p
	}
	if p, ok := names["BadExtra"]; !ok || p.Enabled || p.APIKey != "" {
		t.Errorf("BadExtra 应经基础字段导入为禁用待补: %+v", claude)
	}
	if names["GoodExtra"].Level != 2 || names["GoodExtra"].Enabled {
		t.Errorf("GoodExtra 应经 extra 保真导入且脱敏禁用: %+v", names["GoodExtra"])
	}
}

// MCP 平台状态在导出包中的编码
func TestExportMCPPlatformStates(t *testing.T) {
	setupRenameTestEnv(t)
	ps, gs, ms, prs := seedExportTestData(t)
	es := newTestExportService(ps, gs, ms, prs)

	bundle, _ := mustBuildBundle(t, es, true)

	// 全平台未启用的进 claude+codex 双桶且 Enabled=false
	offClaude, ok1 := bundle.MCP.Claude.Servers["mcp-both-off"]
	offCodex, ok2 := bundle.MCP.Codex.Servers["mcp-both-off"]
	if !ok1 || !ok2 || offClaude.Enabled || offCodex.Enabled {
		t.Errorf("全禁用 MCP 应双桶存在且 Enabled=false: %v %v", ok1, ok2)
	}
	// 仅 claude 启用的只进 claude 桶且 Enabled=true
	onClaude, ok3 := bundle.MCP.Claude.Servers["mcp-claude-on"]
	_, ok4 := bundle.MCP.Codex.Servers["mcp-claude-on"]
	if !ok3 || !onClaude.Enabled || ok4 {
		t.Errorf("仅 claude 启用的 MCP 编码错误: ok3=%v enabled=%v ok4=%v", ok3, onClaude.Enabled, ok4)
	}
	// 仅 gemini 启用的只进 gemini 桶且 Enabled=true
	onGem, ok5 := bundle.MCP.Gemini.Servers["mcp-gem-on"]
	_, ok6 := bundle.MCP.Claude.Servers["mcp-gem-on"]
	if !ok5 || !onGem.Enabled || ok6 {
		t.Errorf("仅 gemini 启用的 MCP 编码错误: ok5=%v enabled=%v ok6=%v", ok5, onGem.Enabled, ok6)
	}
}

// skill.json 损坏时中止导出，不落文件
func TestExportAbortsOnCorruptSkillStore(t *testing.T) {
	tmpHome := setupRenameTestEnv(t)
	ps, gs, ms, prs := seedExportTestData(t)
	es := newTestExportService(ps, gs, ms, prs)

	skillPath := filepath.Join(tmpHome, ".code-switch", "skill.json")
	if err := os.WriteFile(skillPath, []byte("{broken"), 0644); err != nil {
		t.Fatalf("写坏 skill.json 失败: %v", err)
	}
	if _, err := es.collectSnapshot(); err == nil || !strings.Contains(err.Error(), "skill.json") {
		t.Fatalf("skill.json 损坏应中止导出, got %v", err)
	}
}

// 原生导入：同 URL 不同 Key 都保留；空 Key 只按名称去重
func TestImportNativeProvidersSameURLDifferentKey(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()

	added, err := ps.importNativeProviders("claude", []Provider{
		{Name: "AcctOne", APIURL: "https://same.example.com", APIKey: "sk-1", Enabled: true},
		{Name: "AcctTwo", APIURL: "https://same.example.com", APIKey: "sk-2", Enabled: true},
		{Name: "AcctDup", APIURL: "https://same.example.com", APIKey: "sk-1", Enabled: true}, // URL+Key 重复
		{Name: "EmptyOne", APIURL: "https://same.example.com", APIKey: ""},                   // 空 Key 只按名称去重
		{Name: "EmptyTwo", APIURL: "https://same.example.com", APIKey: ""},
	})
	if err != nil {
		t.Fatalf("importNativeProviders 失败: %v", err)
	}
	if added != 4 {
		t.Fatalf("added = %d, 期望 4（同 URL 不同 Key 保留；空 Key 不按 URL 合并）", added)
	}
	list, _ := ps.LoadProviders("claude")
	seenIDs := map[int64]bool{}
	for _, p := range list {
		if p.ID == 0 || seenIDs[p.ID] {
			t.Errorf("ID 分配异常: %+v", p)
		}
		seenIDs[p.ID] = true
	}
}

// 明文包空 Key 供应商保留原 Enabled（无鉴权本地上游合法）；仅脱敏包强制禁用
func TestImportPlaintextKeepsEnabledForEmptyKey(t *testing.T) {
	setupRenameTestEnv(t)
	is, _, _ := newTestImportService(t)

	bundleJSON := `{
	  "exportMeta": {"app": "code-switch-r", "schemaVersion": 1, "redacted": false},
	  "claude": {"providers": {"local": {
	    "id": "local", "name": "LocalNoAuth",
	    "settingsConfig": {"env": {"ANTHROPIC_BASE_URL": "http://127.0.0.1:9999", "ANTHROPIC_AUTH_TOKEN": ""}},
	    "extra": {"name": "LocalNoAuth", "apiUrl": "http://127.0.0.1:9999", "apiKey": "", "enabled": true, "level": 1}
	  }}},
	  "codex": {"providers": {}}, "gemini": {"providers": {}},
	  "mcp": {"claude": {"servers": {}}, "codex": {"servers": {}}}
	}`
	path := filepath.Join(t.TempDir(), "plain-empty-key.json")
	if err := os.WriteFile(path, []byte(bundleJSON), 0644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	result, err := is.ImportFromPath(path)
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if result.ImportedProviders != 1 {
		t.Fatalf("ImportedProviders = %d, 期望 1", result.ImportedProviders)
	}
	claude, _ := is.providerService.LoadProviders("claude")
	if len(claude) != 1 || !claude[0].Enabled || claude[0].APIKey != "" {
		t.Errorf("明文包空 Key 供应商应保留 Enabled=true: %+v", claude)
	}
}

// SettingsConfig 含不可 JSON 序列化的值时快照必须整体失败，不产出部分备份
func TestDeepCopyGeminiProviderUnserializableSettings(t *testing.T) {
	_, err := deepCopyGeminiProvider(GeminiProvider{
		Name:           "Bad",
		SettingsConfig: map[string]any{"oops": make(chan int)},
	})
	if err == nil || !strings.Contains(err.Error(), "settings") {
		t.Fatalf("不可序列化的 SettingsConfig 应报错, got %v", err)
	}
}

// hasHTTPScheme 各长度边界不得越界 panic（"enabled" 恰为 7 字符）
func TestHasHTTPSchemeBoundaries(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"h", false},
		{"http:/", false},
		{"enabled", false}, // 长度 7 的普通字符串
		{"http://", true},
		{"HTTP://x", true},
		{"https://", true},
		{"HTTPS://u:p@h", true},
		{"httpsX//", false},
		{"ftp://x", false},
	}
	for _, tc := range cases {
		if got := hasHTTPScheme(tc.in); got != tc.want {
			t.Errorf("hasHTTPScheme(%q) = %v, 期望 %v", tc.in, got, tc.want)
		}
	}
}
