package services

import (
	"encoding/json"
	"strings"
	"testing"
)

// 粘贴导入显式声明 type="sse"/"streamable-http" 的远程服务器应按 http 解析，
// 不得被归为 stdio 后以"需要提供 command"整批拒绝
func TestParseMCPJSONRemoteTypes(t *testing.T) {
	setupMCPSyncFixHome(t)
	is := NewImportService(NewProviderService(), NewMCPService())

	cases := []struct {
		name  string
		input string
	}{
		{"sse", `{"mcpServers":{"ctx7":{"type":"sse","url":"https://mcp.example/sse"}}}`},
		{"streamable-http", `{"mcpServers":{"ctx7":{"type":"streamable-http","url":"https://mcp.example/mcp"}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := is.ParseMCPJSON(tc.input)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if result == nil || len(result.Servers) != 1 {
				t.Fatalf("期望解析出 1 个服务器，实际: %+v", result)
			}
			server := result.Servers[0]
			if server.Name != "ctx7" {
				t.Fatalf("服务器名错误: %q", server.Name)
			}
			if server.Type != "http" {
				t.Fatalf("远程类型应归一为 http，实际: %q", server.Type)
			}
			if !strings.HasPrefix(server.URL, "https://mcp.example/") {
				t.Fatalf("url 丢失: %q", server.URL)
			}
		})
	}
}

// 同一份 JSON 里 sse 服务器与合法 stdio 服务器应一起导入成功
func TestParseMCPJSONMixedRemoteAndStdio(t *testing.T) {
	setupMCPSyncFixHome(t)
	is := NewImportService(NewProviderService(), NewMCPService())

	input := `{"mcpServers":{
		"ctx7":{"type":"sse","url":"https://mcp.example/sse"},
		"local":{"command":"npx","args":["-y","some-mcp"]}
	}}`
	result, err := is.ParseMCPJSON(input)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(result.Servers) != 2 {
		t.Fatalf("期望解析出 2 个服务器，实际 %d 个", len(result.Servers))
	}
}

// cc-switch 配置里 args 为单个字符串或含数字时不得让整份配置解析失败
func TestCcMCPServerConfigTolerantArgs(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantArgs []string
	}{
		{"字符串 args", `{"type":"stdio","command":"npx","args":"-y @scope/pkg"}`, []string{"-y @scope/pkg"}},
		{"数组含数字与布尔", `{"command":"npx","args":["-y",8080,true]}`, []string{"-y", "8080", "true"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cfg ccMCPServerConfig
			if err := json.Unmarshal([]byte(tc.input), &cfg); err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if len(cfg.Args) != len(tc.wantArgs) {
				t.Fatalf("args 数量不符: %v", cfg.Args)
			}
			for i := range tc.wantArgs {
				if cfg.Args[i] != tc.wantArgs[i] {
					t.Fatalf("args[%d] = %q, 期望 %q", i, cfg.Args[i], tc.wantArgs[i])
				}
			}
		})
	}

	// 整份 ccSwitchConfig 也应容忍该脏数据，不能因单个 server 导致导入功能整体不可用
	full := `{"mcp":{"claude":{"servers":{"s":{"name":"s","server":{"command":"npx","args":"-y pkg"}}}}}}`
	var cfg ccSwitchConfig
	if err := json.Unmarshal([]byte(full), &cfg); err != nil {
		t.Fatalf("整份配置解析失败: %v", err)
	}
}

// 同批候选里名字相同、URL 不同的 provider 只应保留第一个，
// 否则黑名单/统计/轮询按名字互相串扰
func TestDiffProviderCandidatesRejectsDuplicateNames(t *testing.T) {
	entries := map[string]ccProviderEntry{
		"uuid1": {ID: "uuid1", Name: "中转站", Settings: ccProviderSetting{Env: stringMap{
			"ANTHROPIC_BASE_URL":   "https://a.example",
			"ANTHROPIC_AUTH_TOKEN": "k1",
		}}},
		"uuid2": {ID: "uuid2", Name: "中转站", Settings: ccProviderSetting{Env: stringMap{
			"ANTHROPIC_BASE_URL":   "https://b.example",
			"ANTHROPIC_AUTH_TOKEN": "k2",
		}}},
	}
	candidates := diffProviderCandidates("claude", entries, nil)
	if len(candidates) != 1 {
		t.Fatalf("同批重名应只保留一个候选，实际 %d 个: %+v", len(candidates), candidates)
	}
	if candidates[0].Name != "中转站" {
		t.Fatalf("候选名字错误: %q", candidates[0].Name)
	}
}

// 生成最小 Codex TOML 时 name/url 中的引号与反斜杠必须转义，
// 否则 resolveCodexAPIURL 解析失败导致供应商被静默跳过
func TestBuildMinimalCodexConfigEscaping(t *testing.T) {
	cases := []struct {
		name   string
		urlIn  string
		provIn string
	}{
		{"普通名字", "https://api.example/v1", "relay"},
		{"名字含双引号", "https://api.example/v1", `My "Fast" API`},
		{"名字含反斜杠", "https://api.example/v1", `a\b"c`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := buildMinimalCodexConfig(tc.urlIn, tc.provIn)
			got := resolveCodexAPIURL(raw)
			if got != tc.urlIn {
				t.Fatalf("解析 base_url 失败, 生成的 TOML:\n%s\n解析结果: %q, 期望: %q", raw, got, tc.urlIn)
			}
		})
	}
}

// 导入保存前应在锁内按当前配置重新查重，
// 候选集生成与保存之间已存在同 URL/同名 provider 时不得重复写入
func TestSaveProvidersRechecksDuplicatesInsideLock(t *testing.T) {
	setupRenameTestEnv(t)

	ps := NewProviderService()
	seed := []Provider{{ID: 1, Name: "Existing", APIURL: "https://dup.example", APIKey: "k", Enabled: true}}
	if err := ps.SaveProviders("claude", seed); err != nil {
		t.Fatalf("写入初始 provider 失败: %v", err)
	}

	is := NewImportService(ps, NewMCPService())
	added, err := is.saveProviders("claude", []providerCandidate{
		{Name: "Existing2", APIURL: "https://dup.example", APIKey: "k2"}, // URL 重复
		{Name: "existing", APIURL: "https://new.example", APIKey: "k3"},  // 名字重复（大小写不同）
		{Name: "Fresh", APIURL: "https://fresh.example", APIKey: "k4"},
	})
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if added != 1 {
		t.Fatalf("期望仅导入 1 个，实际 %d 个", added)
	}

	after, err := ps.LoadProviders("claude")
	if err != nil {
		t.Fatalf("读取 providers 失败: %v", err)
	}
	if len(after) != 2 {
		t.Fatalf("期望共 2 个 provider，实际 %d 个: %+v", len(after), after)
	}
	names := map[string]struct{}{}
	for _, p := range after {
		names[p.Name] = struct{}{}
	}
	if _, ok := names["Fresh"]; !ok {
		t.Fatalf("Fresh 未导入: %+v", after)
	}
}

// MCP 导入同样应在锁内查重，已存在同名 server 时跳过且不覆盖既有配置
func TestImportMCPServersRechecksDuplicatesInsideLock(t *testing.T) {
	setupMCPSyncFixHome(t)

	ms := NewMCPService()
	is := NewImportService(NewProviderService(), ms)
	if err := ms.SaveServers([]MCPServer{{Name: "dup", Type: "stdio", Command: "npx"}}); err != nil {
		t.Fatalf("写入初始 server 失败: %v", err)
	}

	added, err := is.importMCPServers([]MCPServer{
		{Name: "dup", Type: "stdio", Command: "other"},
		{Name: "fresh", Type: "http", URL: "https://mcp.example/endpoint"},
	})
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if added != 1 {
		t.Fatalf("期望仅导入 1 个，实际 %d 个", added)
	}

	list, err := ms.ListServers()
	if err != nil {
		t.Fatalf("读取 server 列表失败: %v", err)
	}
	byName := map[string]MCPServer{}
	for _, server := range list {
		byName[server.Name] = server
	}
	if server, ok := byName["dup"]; !ok || server.Command != "npx" {
		t.Fatalf("既有 dup 配置被覆盖或丢失: %+v", byName["dup"])
	}
	if _, ok := byName["fresh"]; !ok {
		t.Fatalf("fresh 未导入: %+v", list)
	}
}
