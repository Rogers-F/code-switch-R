package services

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// createCcSwitchTestDB 构造一个 cc-switch 风格的 SQLite 库（schema 取自其
// src-tauri/src/database/schema.rs 的字段子集）
func createCcSwitchTestDB(t *testing.T, tmpHome string, withPrompts bool) string {
	t.Helper()

	dir := filepath.Join(tmpHome, ".cc-switch")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建 .cc-switch 目录失败: %v", err)
	}
	dbPath := filepath.Join(dir, "cc-switch.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("创建 SQLite 失败: %v", err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE providers (
			id TEXT NOT NULL, app_type TEXT NOT NULL, name TEXT NOT NULL,
			settings_config TEXT NOT NULL, website_url TEXT,
			PRIMARY KEY (id, app_type)
		)`,
		`CREATE TABLE provider_endpoints (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider_id TEXT NOT NULL, app_type TEXT NOT NULL, url TEXT NOT NULL
		)`,
	}
	if withPrompts {
		stmts = append(stmts, `CREATE TABLE prompts (
			id TEXT NOT NULL, app_type TEXT NOT NULL, name TEXT NOT NULL, content TEXT NOT NULL,
			description TEXT, enabled BOOLEAN NOT NULL DEFAULT 1, created_at INTEGER, updated_at INTEGER,
			PRIMARY KEY (id, app_type)
		)`)
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("建表失败: %v", err)
		}
	}

	providerRows := [][]any{
		{"c1", "claude", "MyClaude", `{"env":{"ANTHROPIC_BASE_URL":"https://claude.example.com","ANTHROPIC_AUTH_TOKEN":"sk-c"}}`, "https://claude.example.com"},
		{"g1", "gemini", "GemOne", `{"env":{"GOOGLE_GEMINI_BASE_URL":"https://gem.example.com","GEMINI_API_KEY":"sk-1","GEMINI_MODEL":"gemini-2.5-pro"}}`, "https://gem.example.com"},
		// 同 URL 不同 Key：合法多账号场景，两条都应导入
		{"g2", "gemini", "GemTwo", `{"env":{"GOOGLE_GEMINI_BASE_URL":"https://gem.example.com","GEMINI_API_KEY":"sk-2"}}`, ""},
		// env 缺 URL，由 provider_endpoints 补
		{"g3", "gemini", "GemEndpoint", `{"env":{"GEMINI_API_KEY":"sk-3"}}`, ""},
		// 非法 BaseURL，应跳过
		{"g4", "gemini", "GemBad", `{"env":{"GOOGLE_GEMINI_BASE_URL":"ftp://bad","GEMINI_API_KEY":"x"}}`, ""},
		// OAuth 官方条目（空 env），走不了本地代理，应跳过
		{"g5", "gemini", "Google Official", `{"env":{}}`, ""},
		// 本应用不支持的平台，应显式跳过而不是错进 claude 桶
		{"gr1", "grokbuild", "GrokThing", `{"env":{"SOME_URL":"https://grok.example.com"}}`, ""},
	}
	for _, row := range providerRows {
		if _, err := db.Exec(`INSERT INTO providers (id, app_type, name, settings_config, website_url) VALUES (?,?,?,?,?)`, row...); err != nil {
			t.Fatalf("插入 provider 失败: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO provider_endpoints (provider_id, app_type, url) VALUES ('g3','gemini','https://ep.example.com')`); err != nil {
		t.Fatalf("插入 endpoint 失败: %v", err)
	}

	if withPrompts {
		promptRows := [][]any{
			// 毫秒时间戳（cc-switch 用 JS Date.now()）
			{"p1", "claude", "Review prompt", "You are a reviewer", "code review", 1, int64(1750000000000), int64(1750000000000)},
			// NULL description / NULL 时间戳
			{"p2", "gemini", "Gem prompt", "Gemini content", nil, 1, nil, nil},
			// 不支持的平台，应跳过
			{"p3", "hermes", "Hermes prompt", "x", "", 1, int64(0), int64(0)},
			// 空名称，应跳过
			{"p4", "claude", "  ", "no name content", "", 1, nil, nil},
		}
		for _, row := range promptRows {
			if _, err := db.Exec(`INSERT INTO prompts (id, app_type, name, content, description, enabled, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?)`, row...); err != nil {
				t.Fatalf("插入 prompt 失败: %v", err)
			}
		}
	}
	return dbPath
}

func newTestImportService(t *testing.T) (*ImportService, *GeminiService, *PromptService) {
	t.Helper()
	gs := NewGeminiService("127.0.0.1:18100", nil)
	ps := NewPromptService()
	is := NewImportService(NewProviderService(), NewMCPService(), gs, ps)
	return is, gs, ps
}

// SQLite 全链路：gemini 供应商与提示词导入 + 平台路由 + 幂等
func TestImportFromSQLiteGeminiAndPrompts(t *testing.T) {
	// claude 供应商保存路径要查 provider_alias 表，需带 app.db 的隔离环境
	tmpHome := setupRenameTestEnv(t)
	dbPath := createCcSwitchTestDB(t, tmpHome, true)
	is, gs, ps := newTestImportService(t)

	// 导入前状态：4 个供应商（claude 1 + gemini 3）、2 个提示词待导入
	status, err := is.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus 失败: %v", err)
	}
	if status.PendingProviderCount != 4 {
		t.Errorf("PendingProviderCount = %d, 期望 4（claude 1 + gemini 3）", status.PendingProviderCount)
	}
	if !status.PendingPrompts || status.PendingPromptCount != 2 {
		t.Errorf("PendingPromptCount = %d (pending=%v), 期望 2", status.PendingPromptCount, status.PendingPrompts)
	}

	result, err := is.ImportFromPath(dbPath)
	if err != nil {
		t.Fatalf("ImportFromPath 失败: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("导入不应有阶段错误: %v", result.Errors)
	}
	if result.ImportedProviders != 4 {
		t.Errorf("ImportedProviders = %d, 期望 4", result.ImportedProviders)
	}
	if result.ImportedPrompts != 2 {
		t.Errorf("ImportedPrompts = %d, 期望 2", result.ImportedPrompts)
	}

	// gemini 供应商落库检查
	providers := gs.GetProviders()
	if len(providers) != 3 {
		t.Fatalf("gemini 供应商数 = %d, 期望 3（g1/g2/g3）: %+v", len(providers), providers)
	}
	byName := map[string]GeminiProvider{}
	for _, p := range providers {
		byName[p.Name] = p
		if !p.Enabled {
			t.Errorf("导入的供应商 %s 应为启用状态", p.Name)
		}
		if p.ID == "" {
			t.Errorf("导入的供应商 %s 未生成 ID", p.Name)
		}
	}
	if byName["GemOne"].Model != "gemini-2.5-pro" {
		t.Errorf("GemOne.Model = %q, 期望 gemini-2.5-pro", byName["GemOne"].Model)
	}
	if byName["GemTwo"].APIKey != "sk-2" {
		t.Errorf("同 URL 不同 Key 的 GemTwo 应导入，APIKey = %q", byName["GemTwo"].APIKey)
	}
	if byName["GemEndpoint"].BaseURL != "https://ep.example.com" {
		t.Errorf("GemEndpoint 应从 provider_endpoints 补 URL，实际 %q", byName["GemEndpoint"].BaseURL)
	}

	// 提示词落库检查：全部禁用、时间戳归一为秒、不写 CLI 提示词文件
	claudePrompts, err := ps.StoredPrompts("claude")
	if err != nil {
		t.Fatalf("读取 claude 提示词失败: %v", err)
	}
	p1, ok := claudePrompts["p1"]
	if !ok {
		t.Fatalf("claude 提示词 p1 未导入: %+v", claudePrompts)
	}
	if p1.Enabled {
		t.Error("导入的提示词必须为禁用状态")
	}
	if p1.Content != "You are a reviewer" {
		t.Errorf("p1 内容不符: %q", p1.Content)
	}
	if p1.CreatedAt == nil || *p1.CreatedAt != 1750000000 {
		t.Errorf("毫秒时间戳应归一为秒，得到 %v", p1.CreatedAt)
	}
	geminiPrompts, err := ps.StoredPrompts("gemini")
	if err != nil {
		t.Fatalf("读取 gemini 提示词失败: %v", err)
	}
	if p2, ok := geminiPrompts["p2"]; !ok || p2.Enabled {
		t.Errorf("gemini 提示词 p2 应导入且禁用: %+v", geminiPrompts)
	}
	if _, err := os.Stat(filepath.Join(tmpHome, ".claude", "CLAUDE.md")); !os.IsNotExist(err) {
		t.Error("导入不得写 CLI 提示词文件 CLAUDE.md")
	}

	// 幂等：重复导入不产生新增
	again, err := is.ImportFromPath(dbPath)
	if err != nil {
		t.Fatalf("二次导入失败: %v", err)
	}
	if again.ImportedProviders != 0 || again.ImportedPrompts != 0 || again.ImportedMCP != 0 {
		t.Errorf("二次导入应为 0 新增，得到 providers=%d prompts=%d mcp=%d",
			again.ImportedProviders, again.ImportedPrompts, again.ImportedMCP)
	}
	if len(gs.GetProviders()) != 3 {
		t.Errorf("二次导入后 gemini 供应商数不应变化")
	}
}

// 旧版 cc-switch 数据库没有 prompts 表：提示词跳过，其余照常导入
func TestImportFromSQLiteWithoutPromptsTable(t *testing.T) {
	tmpHome := setupRenameTestEnv(t)
	dbPath := createCcSwitchTestDB(t, tmpHome, false)
	is, gs, _ := newTestImportService(t)

	result, err := is.ImportFromPath(dbPath)
	if err != nil {
		t.Fatalf("ImportFromPath 失败: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("缺 prompts 表不应产生阶段错误: %v", result.Errors)
	}
	if result.ImportedPrompts != 0 {
		t.Errorf("ImportedPrompts = %d, 期望 0", result.ImportedPrompts)
	}
	if len(gs.GetProviders()) != 3 {
		t.Errorf("gemini 供应商仍应导入 3 个，实际 %d", len(gs.GetProviders()))
	}
}

// 旧 JSON 配置带 gemini 段也能导入
func TestImportFromJSONWithGeminiSection(t *testing.T) {
	tmpHome := setupGeminiTestHome(t)
	dir := filepath.Join(tmpHome, ".cc-switch")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	cfgPath := filepath.Join(dir, "config.json")
	cfgJSON := `{
		"gemini": {
			"providers": {
				"gj1": {
					"id": "gj1",
					"name": "JsonGem",
					"websiteUrl": "https://json-gem.example.com",
					"settingsConfig": {
						"env": {
							"GOOGLE_GEMINI_BASE_URL": "https://json-gem.example.com",
							"GEMINI_API_KEY": "sk-json"
						}
					}
				}
			}
		}
	}`
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0644); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}

	is, gs, _ := newTestImportService(t)
	result, err := is.ImportFromPath(cfgPath)
	if err != nil {
		t.Fatalf("ImportFromPath 失败: %v", err)
	}
	if result.ImportedProviders != 1 {
		t.Errorf("ImportedProviders = %d, 期望 1", result.ImportedProviders)
	}
	providers := gs.GetProviders()
	if len(providers) != 1 || providers[0].Name != "JsonGem" || providers[0].APIKey != "sk-json" {
		t.Errorf("JSON gemini 导入结果不符: %+v", providers)
	}
	// JSON 数据源没有提示词
	if result.ImportedPrompts != 0 {
		t.Errorf("JSON 源不应导入提示词，得到 %d", result.ImportedPrompts)
	}
}
