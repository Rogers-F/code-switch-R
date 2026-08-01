package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupPresetTestHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	assertHomeIsolated(t, tmp)
	return tmp
}

func writeToolWithInjection(t *testing.T, svc *CustomCliService, cfgPath, format string, injections []ProxyInjection) *CustomCliTool {
	t.Helper()
	tool, err := svc.CreateTool(CustomCliTool{
		Name: "t",
		ConfigFiles: []ConfigFile{{
			ID: "f1", Label: "cfg", Path: cfgPath, Format: format, IsPrimary: true,
		}},
		ProxyInjection: injections,
	})
	if err != nil {
		t.Fatalf("创建工具失败: %v", err)
	}
	return tool
}

// ==================== 解析 fail-stop ====================

// 畸形 JSON 启用注入必须报错且零写入（旧实现会重置成空配置再写回，
// 等于把用户配置清空）
func TestEnableProxyMalformedConfigZeroWrite(t *testing.T) {
	tmp := setupPresetTestHome(t)
	svc := NewCustomCliService(":18100", nil)

	cfgPath := filepath.Join(tmp, "broken.json")
	original := "{ this is not json"
	if err := os.WriteFile(cfgPath, []byte(original), 0o644); err != nil {
		t.Fatalf("写夹具失败: %v", err)
	}

	tool := writeToolWithInjection(t, svc, cfgPath, "json", []ProxyInjection{{
		TargetFileID: "f1", BaseUrlField: "base_url", AuthTokenField: "token",
	}})
	err := svc.EnableProxy(tool.ID)
	if err == nil {
		t.Fatal("畸形 JSON 应报错")
	}
	got, _ := os.ReadFile(cfgPath)
	if string(got) != original {
		t.Errorf("畸形配置被改写: %q", got)
	}
	if FileExists(cfgPath + ".code-switch.backup") {
		t.Error("预检失败不应残留备份")
	}
}

// .jsonc 带注释 → 专属错误提示
func TestEnableProxyJsoncCommentHint(t *testing.T) {
	tmp := setupPresetTestHome(t)
	svc := NewCustomCliService(":18100", nil)

	cfgPath := filepath.Join(tmp, "opencode.jsonc")
	if err := os.WriteFile(cfgPath, []byte("// comment\n{}\n"), 0o644); err != nil {
		t.Fatalf("写夹具失败: %v", err)
	}
	tool := writeToolWithInjection(t, svc, cfgPath, "json", []ProxyInjection{{
		TargetFileID: "f1", BaseUrlField: "provider.p.options.baseURL",
	}})
	err := svc.EnableProxy(tool.ID)
	if err == nil || !strings.Contains(err.Error(), "JSONC") {
		t.Errorf("应给出 JSONC 专属提示, 实际: %v", err)
	}
}

// ==================== 嵌套操作严格化 ====================

func TestSetNestedValueStrictConflicts(t *testing.T) {
	data := map[string]interface{}{"provider": "a string"}
	if err := setNestedValueStrict(data, "provider.x.y", 1); err == nil {
		t.Error("中间节点非对象应报错")
	}
	if data["provider"] != "a string" {
		t.Error("冲突时不得覆盖原值")
	}
	if err := setNestedValueStrict(data, "a..b", 1); err == nil {
		t.Error("空段路径应报错")
	}
	if err := setNestedValueStrict(data, "x.y", "v"); err != nil {
		t.Errorf("正常写入失败: %v", err)
	}
}

func TestGetNestedValueExDistinguishesNull(t *testing.T) {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(`{"a":{"b":null}}`), &data); err != nil {
		t.Fatal(err)
	}
	if v, exists := getNestedValueEx(data, "a.b"); !exists || v != nil {
		t.Errorf("null 叶子应 exists=true value=nil, 实际 %v %v", v, exists)
	}
	if _, exists := getNestedValueEx(data, "a.c"); exists {
		t.Error("缺失路径应 exists=false")
	}
}

// ==================== seed 语义 ====================

func TestApplySeedFieldModes(t *testing.T) {
	models := map[string]interface{}{"m1": map[string]interface{}{"name": "M1"}}

	t.Run("exact 缺失写入", func(t *testing.T) {
		data := map[string]interface{}{}
		if err := applySeedField(data, SeedField{Path: "p.npm", Value: "@ai-sdk/anthropic", Mode: "exact"}); err != nil {
			t.Fatal(err)
		}
		if v, _ := getNestedValueEx(data, "p.npm"); v != "@ai-sdk/anthropic" {
			t.Errorf("未写入: %v", v)
		}
	})
	t.Run("exact 等值保留", func(t *testing.T) {
		data := map[string]interface{}{"p": map[string]interface{}{"npm": "@ai-sdk/anthropic"}}
		if err := applySeedField(data, SeedField{Path: "p.npm", Value: "@ai-sdk/anthropic", Mode: "exact"}); err != nil {
			t.Fatalf("等值应通过: %v", err)
		}
	})
	t.Run("exact 异值报错", func(t *testing.T) {
		data := map[string]interface{}{"p": map[string]interface{}{"npm": "@ai-sdk/openai-compatible"}}
		if err := applySeedField(data, SeedField{Path: "p.npm", Value: "@ai-sdk/anthropic", Mode: "exact"}); err == nil {
			t.Fatal("异值应报错（openai-compatible 会请求不存在的端点，静默跳过是假成功）")
		}
	})
	t.Run("fillMap 缺失与空对象写入", func(t *testing.T) {
		for _, data := range []map[string]interface{}{
			{},
			{"p": map[string]interface{}{"models": map[string]interface{}{}}},
		} {
			if err := applySeedField(data, SeedField{Path: "p.models", Value: models, Mode: "fillMap"}); err != nil {
				t.Fatal(err)
			}
			if v, _ := getNestedValueEx(data, "p.models.m1.name"); v != "M1" {
				t.Errorf("models 未写入: %v", v)
			}
		}
	})
	t.Run("fillMap 非空保留", func(t *testing.T) {
		data := map[string]interface{}{"p": map[string]interface{}{"models": map[string]interface{}{"user-model": map[string]interface{}{}}}}
		if err := applySeedField(data, SeedField{Path: "p.models", Value: models, Mode: "fillMap"}); err != nil {
			t.Fatalf("非空应保留: %v", err)
		}
		if _, exists := getNestedValueEx(data, "p.models.m1"); exists {
			t.Error("不得混入 seed 模型")
		}
	})
	t.Run("fillMap null 报错", func(t *testing.T) {
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(`{"p":{"models":null}}`), &data)
		if err := applySeedField(data, SeedField{Path: "p.models", Value: models, Mode: "fillMap"}); err == nil {
			t.Fatal("null 应报错")
		}
	})
	t.Run("未知模式报错", func(t *testing.T) {
		if err := applySeedField(map[string]interface{}{}, SeedField{Path: "x", Value: 1, Mode: "wild"}); err == nil {
			t.Fatal("未知模式应报错")
		}
	})
}

// ==================== 校验 ====================

func TestValidateProxyInjections(t *testing.T) {
	base := func(injections []ProxyInjection, format string) *CustomCliTool {
		return &CustomCliTool{
			ConfigFiles:    []ConfigFile{{ID: "f1", Format: format}},
			ProxyInjection: injections,
		}
	}
	if err := NewCustomCliService(":18100", nil).validateProxyInjections(base([]ProxyInjection{{
		TargetFileID: "f1", BaseUrlField: "a.b",
		SeedFields: []SeedField{{Path: "a.b.c", Value: 1, Mode: "exact"}},
	}}, "json")); err == nil {
		t.Error("祖先/子孙路径重叠应拒绝")
	}
	if err := NewCustomCliService(":18100", nil).validateProxyInjections(base([]ProxyInjection{{
		TargetFileID: "f1", BaseUrlField: "a.b",
		SeedFields: []SeedField{{Path: "c.d", Value: 1, Mode: "exact"}},
	}}, "env")); err == nil {
		t.Error("env 格式携带 seed 应拒绝")
	}
	if err := NewCustomCliService(":18100", nil).validateProxyInjections(base([]ProxyInjection{{
		TargetFileID: "f1", BaseUrlField: "a.b",
		SeedFields: []SeedField{{Path: "c.d", Value: 1, Mode: "nope"}},
	}}, "json")); err == nil {
		t.Error("未知 seed 模式应拒绝")
	}
	if err := NewCustomCliService(":18100", nil).validateProxyInjections(base([]ProxyInjection{{
		TargetFileID: "f1", BaseUrlField: "a.b", AuthTokenField: "a.c",
		SeedFields: []SeedField{{Path: "d.e", Value: 1, Mode: "fillMap"}},
	}}, "json")); err != nil {
		t.Errorf("合法配置不应拒绝: %v", err)
	}
}

// ==================== 启用端到端（聚合 + seed + marker） ====================

func TestEnableProxySeedsAndMarksCreatedFile(t *testing.T) {
	tmp := setupPresetTestHome(t)
	svc := NewCustomCliService("127.0.0.1:18100", nil)

	cfgPath := filepath.Join(tmp, "oc", "opencode.json") // 启用前不存在
	tool := writeToolWithInjection(t, svc, cfgPath, "json", []ProxyInjection{{
		TargetFileID:   "f1",
		BaseUrlField:   "provider.code-switch-r.options.baseURL",
		AuthTokenField: "provider.code-switch-r.options.apiKey",
		SeedFields: []SeedField{
			{Path: "provider.code-switch-r.npm", Value: "@ai-sdk/anthropic", Mode: "exact"},
			{Path: "provider.code-switch-r.models", Value: map[string]interface{}{
				"claude-x": map[string]interface{}{"name": "claude-x"},
			}, Mode: "fillMap"},
		},
	}})

	if err := svc.EnableProxy(tool.ID); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	content, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("配置未创建: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(content, &data); err != nil {
		t.Fatalf("写出的配置不是合法 JSON: %v", err)
	}
	for path, want := range map[string]interface{}{
		"provider.code-switch-r.npm":                  "@ai-sdk/anthropic",
		"provider.code-switch-r.options.baseURL":      "http://127.0.0.1:18100/custom/" + tool.ID,
		"provider.code-switch-r.options.apiKey":       "code-switch-r",
		"provider.code-switch-r.models.claude-x.name": "claude-x",
	} {
		if v, _ := getNestedValueEx(data, path); v != want {
			t.Errorf("%s = %v, 期望 %v", path, v, want)
		}
	}

	// 重复启用应幂等（seed 等值保留 / 非空保留）
	if err := svc.EnableProxy(tool.ID); err != nil {
		t.Fatalf("重复启用失败: %v", err)
	}

	// 未被改动 → 禁用时整文件删除（created-marker 生效）
	if err := svc.DisableProxy(tool.ID); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}
	if FileExists(cfgPath) {
		t.Error("从零创建且未改动的配置应被整体移除")
	}
}

// 用户改过自建文件 → 禁用只做条件清理，保留用户内容
func TestDisableKeepsUserEditsInCreatedFile(t *testing.T) {
	tmp := setupPresetTestHome(t)
	svc := NewCustomCliService("127.0.0.1:18100", nil)
	cfgPath := filepath.Join(tmp, "oc", "opencode.json")
	tool := writeToolWithInjection(t, svc, cfgPath, "json", []ProxyInjection{{
		TargetFileID:   "f1",
		BaseUrlField:   "provider.code-switch-r.options.baseURL",
		AuthTokenField: "provider.code-switch-r.options.apiKey",
		SeedFields: []SeedField{
			{Path: "provider.code-switch-r.npm", Value: "@ai-sdk/anthropic", Mode: "exact"},
		},
	}})
	if err := svc.EnableProxy(tool.ID); err != nil {
		t.Fatalf("启用失败: %v", err)
	}

	// 模拟用户在文件里加了自己的配置
	var data map[string]interface{}
	content, _ := os.ReadFile(cfgPath)
	_ = json.Unmarshal(content, &data)
	data["theme"] = "dark"
	out, _ := json.Marshal(data)
	if err := os.WriteFile(cfgPath, out, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := svc.DisableProxy(tool.ID); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}
	content, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal("被用户改过的文件不得删除")
	}
	var after map[string]interface{}
	_ = json.Unmarshal(content, &after)
	if after["theme"] != "dark" {
		t.Error("用户字段丢失")
	}
	if _, exists := getNestedValueEx(after, "provider.code-switch-r"); exists {
		t.Errorf("注入与 seed 字段应被条件清理并裁剪空壳: %s", content)
	}
}

// 用户手改注入值 → 条件清理保留改过的值
func TestConditionalCleanupKeepsModifiedValues(t *testing.T) {
	tmp := setupPresetTestHome(t)
	svc := NewCustomCliService("127.0.0.1:18100", nil)
	cfgPath := filepath.Join(tmp, "cfg.json")
	if err := os.WriteFile(cfgPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := writeToolWithInjection(t, svc, cfgPath, "json", []ProxyInjection{{
		TargetFileID: "f1", BaseUrlField: "base_url", AuthTokenField: "token",
	}})
	if err := svc.EnableProxy(tool.ID); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	// 用户删掉备份并手改 base_url
	_ = os.Remove(cfgPath + ".code-switch.backup")
	var data map[string]interface{}
	content, _ := os.ReadFile(cfgPath)
	_ = json.Unmarshal(content, &data)
	data["base_url"] = "https://my-own-endpoint"
	out, _ := json.Marshal(data)
	_ = os.WriteFile(cfgPath, out, 0o644)

	if err := svc.DisableProxy(tool.ID); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}
	content, _ = os.ReadFile(cfgPath)
	var after map[string]interface{}
	_ = json.Unmarshal(content, &after)
	if after["base_url"] != "https://my-own-endpoint" {
		t.Errorf("用户手改的值不得删除: %s", content)
	}
	if _, exists := after["token"]; exists {
		t.Error("仍等于注入值的 token 应被清理")
	}
}

// 删除已启用工具：先恢复配置再删（备份还原路径）
func TestDeleteToolRestoresInjectedConfig(t *testing.T) {
	tmp := setupPresetTestHome(t)
	svc := NewCustomCliService("127.0.0.1:18100", nil)
	cfgPath := filepath.Join(tmp, "cfg.json")
	original := `{"base_url":"https://origin","keep":"me"}`
	if err := os.WriteFile(cfgPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := writeToolWithInjection(t, svc, cfgPath, "json", []ProxyInjection{{
		TargetFileID: "f1", BaseUrlField: "base_url",
	}})
	if err := svc.EnableProxy(tool.ID); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	if err := svc.DeleteTool(tool.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	content, _ := os.ReadFile(cfgPath)
	var after map[string]interface{}
	_ = json.Unmarshal(content, &after)
	if after["base_url"] != "https://origin" || after["keep"] != "me" {
		t.Errorf("删除前应恢复原始配置: %s", content)
	}
	if FileExists(cfgPath + ".code-switch.backup") {
		t.Error("备份应随恢复清理")
	}
	if _, err := svc.GetTool(tool.ID); err == nil {
		t.Error("工具记录应已删除")
	}
}

// ==================== 预设与路径探测 ====================

func TestOpencodePresetShape(t *testing.T) {
	setupPresetTestHome(t)
	t.Setenv("OPENCODE_CONFIG", "")
	svc := NewCustomCliService("127.0.0.1:18100", nil)
	presets, err := svc.ListToolPresets()
	if err != nil {
		t.Fatalf("预设列表失败: %v", err)
	}
	if len(presets) != 1 || presets[0].PresetID != "opencode" {
		t.Fatalf("应有 opencode 预设: %+v", presets)
	}
	p := presets[0]
	if p.ConfigState != "none" {
		t.Errorf("干净环境应为 none, 实际 %s", p.ConfigState)
	}
	if len(p.ProxyInjection) != 1 {
		t.Fatalf("应有一条注入: %+v", p.ProxyInjection)
	}
	inj := p.ProxyInjection[0]
	if inj.BaseUrlField != "provider.code-switch-r.options.baseURL" {
		t.Errorf("baseURL 字段路径错误: %s", inj.BaseUrlField)
	}
	var npm, models *SeedField
	for i := range inj.SeedFields {
		switch {
		case strings.HasSuffix(inj.SeedFields[i].Path, ".npm"):
			npm = &inj.SeedFields[i]
		case strings.HasSuffix(inj.SeedFields[i].Path, ".models"):
			models = &inj.SeedFields[i]
		}
	}
	if npm == nil || npm.Value != "@ai-sdk/anthropic" || npm.Mode != "exact" {
		t.Errorf("npm seed 错误: %+v", npm)
	}
	if models == nil || models.Mode != "fillMap" {
		t.Fatalf("models seed 错误: %+v", models)
	}
	m, ok := models.Value.(map[string]interface{})
	if !ok || len(m) == 0 {
		t.Errorf("models seed 应为非空表: %+v", models.Value)
	}
	// nil policy → 静态兜底模型
	if _, exists := m[FallbackClaudeDefaultModel]; !exists {
		t.Errorf("无策略时应使用兜底默认模型 %s: %v", FallbackClaudeDefaultModel, m)
	}
	// 校验预设生成的注入规则自身合法
	if err := svc.validateProxyInjections(&CustomCliTool{
		ConfigFiles:    p.ConfigFiles,
		ProxyInjection: p.ProxyInjection,
	}); err != nil {
		t.Errorf("预设注入规则未通过自家校验: %v", err)
	}
}

func TestResolveOpencodeConfigPathStates(t *testing.T) {
	tmp := setupPresetTestHome(t)
	t.Setenv("OPENCODE_CONFIG", "")
	svc := NewCustomCliService(":18100", nil)
	dir := filepath.Join(tmp, ".config", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	check := func(wantPath, wantState string, wantCandidates int) {
		t.Helper()
		path, state, candidates, err := svc.resolveOpencodeConfigPath()
		if err != nil {
			t.Fatalf("探测失败: %v", err)
		}
		if path != wantPath || state != wantState || len(candidates) != wantCandidates {
			t.Errorf("探测结果 (%s,%s,%d), 期望 (%s,%s,%d)", path, state, len(candidates), wantPath, wantState, wantCandidates)
		}
	}

	check("~/.config/opencode/opencode.json", "none", 0)
	if err := os.WriteFile(filepath.Join(dir, "opencode.jsonc"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	check("~/.config/opencode/opencode.jsonc", "jsonc", 0)
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	check("~/.config/opencode/opencode.json", "both", 2)
	_ = os.Remove(filepath.Join(dir, "opencode.jsonc"))
	check("~/.config/opencode/opencode.json", "json", 0)

	// OPENCODE_CONFIG 覆盖：绝对路径生效
	override := filepath.Join(tmp, "custom-opencode.json")
	t.Setenv("OPENCODE_CONFIG", override)
	path, state, _, err := svc.resolveOpencodeConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != override || state != "none" {
		t.Errorf("OPENCODE_CONFIG 未生效: %s %s", path, state)
	}
	if err := os.WriteFile(override, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, state, _, _ = svc.resolveOpencodeConfigPath(); state != "json" {
		t.Errorf("覆盖路径存在时应为 json, 实际 %s", state)
	}
	// 相对路径 / 非法后缀 → 忽略回退默认
	t.Setenv("OPENCODE_CONFIG", "relative/opencode.json")
	if path, _, _, _ = svc.resolveOpencodeConfigPath(); path != "~/.config/opencode/opencode.json" {
		t.Errorf("相对路径应被忽略: %s", path)
	}
	t.Setenv("OPENCODE_CONFIG", filepath.Join(tmp, "conf.yaml"))
	if path, _, _, _ = svc.resolveOpencodeConfigPath(); path != "~/.config/opencode/opencode.json" {
		t.Errorf("非法后缀应被忽略: %s", path)
	}
}

// ==================== 兼容别名路由 ====================

// /custom/:toolId/messages 与 /custom/:toolId/v1/messages 行为一致
// （ai-sdk 系 Anthropic 客户端按 ${baseURL}/messages 拼 URL）
func TestCustomCliMessagesAliasRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupRenameTestEnv(t)

	var upstreamHits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	// 给 custom:tool-x 写一个供应商
	ps := NewProviderService()
	if err := ps.SaveProviders("custom:tool-x", []Provider{{
		ID: 1, Name: "p", APIURL: upstream.URL, APIKey: "k", Enabled: true,
	}}); err != nil {
		t.Fatalf("保存供应商失败: %v", err)
	}

	prs := newTestRelayService(ps)
	router := gin.New()
	router.POST("/custom/:toolId/v1/messages", prs.customCliProxyHandler())
	router.POST("/custom/:toolId/messages", prs.customCliProxyHandler())

	for _, path := range []string{"/custom/tool-x/v1/messages", "/custom/tool-x/messages"} {
		req := httptest.NewRequest("POST", path, strings.NewReader(`{"model":"m","messages":[]}`))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("%s 应 200, 实际 %d: %s", path, w.Code, w.Body.String())
		}
	}
	if upstreamHits != 2 {
		t.Errorf("上游应收到 2 次请求, 实际 %d", upstreamHits)
	}
}

// ==================== ownership 元数据防护 ====================

// Create/Update 不能写入或清空服务端私有元数据
func TestToolMetaSurvivesClientUpdates(t *testing.T) {
	tmp := setupPresetTestHome(t)
	svc := NewCustomCliService("127.0.0.1:18100", nil)
	cfgPath := filepath.Join(tmp, "oc", "opencode.json")
	tool := writeToolWithInjection(t, svc, cfgPath, "json", []ProxyInjection{{
		TargetFileID: "f1", BaseUrlField: "provider.p.options.baseURL",
	}})
	if err := svc.EnableProxy(tool.ID); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	// 客户端全量更新工具（模拟前端编辑保存）
	updated := *tool
	updated.Name = "renamed"
	if err := svc.UpdateTool(tool.ID, updated); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	// marker 仍应生效：禁用时整文件删除
	if err := svc.DisableProxy(tool.ID); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}
	if FileExists(cfgPath) {
		t.Error("Update 之后 created-marker 丢失（自建文件未被清理）")
	}
}

// F1 回归：用户改过自建文件后重新启用，不得重认领所有权——
// 随后禁用必须保留用户内容（marker 在重启用时被放弃）
func TestReenableAfterUserEditDropsOwnership(t *testing.T) {
	tmp := setupPresetTestHome(t)
	svc := NewCustomCliService("127.0.0.1:18100", nil)
	cfgPath := filepath.Join(tmp, "oc", "opencode.json")
	tool := writeToolWithInjection(t, svc, cfgPath, "json", []ProxyInjection{{
		TargetFileID: "f1", BaseUrlField: "provider.p.options.baseURL",
	}})
	if err := svc.EnableProxy(tool.ID); err != nil {
		t.Fatalf("首次启用失败: %v", err)
	}
	// 用户往自建文件里加字段
	var data map[string]interface{}
	content, _ := os.ReadFile(cfgPath)
	_ = json.Unmarshal(content, &data)
	data["theme"] = "dark"
	out, _ := json.Marshal(data)
	if err := os.WriteFile(cfgPath, out, 0o644); err != nil {
		t.Fatal(err)
	}
	// 重新启用（幂等操作，用户随手点）
	if err := svc.EnableProxy(tool.ID); err != nil {
		t.Fatalf("重启用失败: %v", err)
	}
	// 禁用：文件必须保留（含用户字段），注入段被条件清理
	if err := svc.DisableProxy(tool.ID); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}
	content, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal("重启用后禁用把含用户内容的文件删掉了（重认领缺陷）")
	}
	var after map[string]interface{}
	_ = json.Unmarshal(content, &after)
	if after["theme"] != "dark" {
		t.Errorf("用户字段丢失: %s", content)
	}
}

// F4 回归：多目标启用是"全部预检通过才写入"——
// 第二个文件冲突时第一个文件必须原样未动、零备份
func TestEnableProxyMultiTargetPrecheckAtomic(t *testing.T) {
	tmp := setupPresetTestHome(t)
	svc := NewCustomCliService("127.0.0.1:18100", nil)
	okPath := filepath.Join(tmp, "ok.json")
	badPath := filepath.Join(tmp, "bad.json")
	originalOK := `{"keep":"me"}`
	if err := os.WriteFile(okPath, []byte(originalOK), 0o644); err != nil {
		t.Fatal(err)
	}
	// bad 文件的注入路径中间节点是字符串 → 阶段一冲突
	if err := os.WriteFile(badPath, []byte(`{"provider":"a string"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tool, err := svc.CreateTool(CustomCliTool{
		Name: "t",
		ConfigFiles: []ConfigFile{
			{ID: "f1", Label: "ok", Path: okPath, Format: "json", IsPrimary: true},
			{ID: "f2", Label: "bad", Path: badPath, Format: "json"},
		},
		ProxyInjection: []ProxyInjection{
			{TargetFileID: "f1", BaseUrlField: "base_url"},
			{TargetFileID: "f2", BaseUrlField: "provider.x.baseURL"},
		},
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if err := svc.EnableProxy(tool.ID); err == nil {
		t.Fatal("第二目标冲突应报错")
	}
	got, _ := os.ReadFile(okPath)
	if string(got) != originalOK {
		t.Errorf("冲突时第一个文件也被写入了（未实现整体预检）: %s", got)
	}
	if FileExists(okPath+".code-switch.backup") || FileExists(badPath+".code-switch.backup") {
		t.Error("预检失败不应残留任何备份")
	}
}

// F3 回归：注入生效期间修改注入目标被拒绝；禁用后允许；只改名不受限
func TestUpdateToolRejectsTargetChangeWhileActive(t *testing.T) {
	tmp := setupPresetTestHome(t)
	svc := NewCustomCliService("127.0.0.1:18100", nil)
	cfgPath := filepath.Join(tmp, "cfg.json")
	if err := os.WriteFile(cfgPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := writeToolWithInjection(t, svc, cfgPath, "json", []ProxyInjection{{
		TargetFileID: "f1", BaseUrlField: "base_url",
	}})
	if err := svc.EnableProxy(tool.ID); err != nil {
		t.Fatalf("启用失败: %v", err)
	}

	// 改注入目标路径 → 拒绝
	moved := *tool
	moved.ConfigFiles = []ConfigFile{{ID: "f1", Label: "cfg", Path: filepath.Join(tmp, "other.json"), Format: "json", IsPrimary: true}}
	if err := svc.UpdateTool(tool.ID, moved); err == nil {
		t.Error("启用期间改注入目标应被拒绝")
	}
	// 改注入字段 → 拒绝
	changedField := *tool
	changedField.ProxyInjection = []ProxyInjection{{TargetFileID: "f1", BaseUrlField: "another_field"}}
	if err := svc.UpdateTool(tool.ID, changedField); err == nil {
		t.Error("启用期间改注入字段应被拒绝")
	}
	// 只改名 → 允许
	renamed := *tool
	renamed.Name = "renamed"
	if err := svc.UpdateTool(tool.ID, renamed); err != nil {
		t.Errorf("只改名应允许: %v", err)
	}
	// 禁用后改目标 → 允许
	if err := svc.DisableProxy(tool.ID); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}
	if err := svc.UpdateTool(tool.ID, moved); err != nil {
		t.Errorf("禁用后修改应允许: %v", err)
	}
}

// F5 回归：两个 ConfigFile 条目指向同一物理路径 → 单组处理，
// 两条注入都生效且禁用后完整还原
func TestSamePhysicalPathMergedIntoOneGroup(t *testing.T) {
	tmp := setupPresetTestHome(t)
	svc := NewCustomCliService("127.0.0.1:18100", nil)
	cfgPath := filepath.Join(tmp, "cfg.json")
	original := `{"keep":"me"}`
	if err := os.WriteFile(cfgPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	tool, err := svc.CreateTool(CustomCliTool{
		Name: "t",
		ConfigFiles: []ConfigFile{
			{ID: "f1", Label: "a", Path: cfgPath, Format: "json", IsPrimary: true},
			{ID: "f2", Label: "b", Path: cfgPath, Format: "json"},
		},
		ProxyInjection: []ProxyInjection{
			{TargetFileID: "f1", BaseUrlField: "alpha.url"},
			{TargetFileID: "f2", BaseUrlField: "beta.url"},
		},
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if err := svc.EnableProxy(tool.ID); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	var data map[string]interface{}
	content, _ := os.ReadFile(cfgPath)
	_ = json.Unmarshal(content, &data)
	want := "http://127.0.0.1:18100/custom/" + tool.ID
	if v, _ := getNestedValueEx(data, "alpha.url"); v != want {
		t.Errorf("第一条注入未生效: %v", v)
	}
	if v, _ := getNestedValueEx(data, "beta.url"); v != want {
		t.Errorf("第二条注入未生效（同路径应合并处理）: %v", v)
	}
	if err := svc.DisableProxy(tool.ID); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}
	content, _ = os.ReadFile(cfgPath)
	if string(content) != original {
		t.Errorf("禁用后应还原原始内容（备份不得被注入态覆盖）: %s", content)
	}
}

// ENV 生命周期：注释/export/用户行保留；注入行条件删除；自建 env 走 marker
func TestEnvInjectionPreservesUserContent(t *testing.T) {
	tmp := setupPresetTestHome(t)
	svc := NewCustomCliService("127.0.0.1:18100", nil)
	cfgPath := filepath.Join(tmp, "tool.env")
	original := "# my comment\nexport SHELL_THING=1\nMY_KEY=mine\n"
	if err := os.WriteFile(cfgPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := writeToolWithInjection(t, svc, cfgPath, "env", []ProxyInjection{{
		TargetFileID: "f1", BaseUrlField: "BASE_URL", AuthTokenField: "AUTH_TOKEN",
	}})
	if err := svc.EnableProxy(tool.ID); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	content, _ := os.ReadFile(cfgPath)
	text := string(content)
	for _, want := range []string{"# my comment", "export SHELL_THING=1", "MY_KEY=mine", "BASE_URL=http://127.0.0.1:18100/custom/" + tool.ID, "AUTH_TOKEN=code-switch-r"} {
		if !strings.Contains(text, want) {
			t.Errorf("启用后缺少 %q:\n%s", want, text)
		}
	}
	// 删掉备份走条件清理路径：注入行删除，用户行原样保留
	_ = os.Remove(cfgPath + ".code-switch.backup")
	if err := svc.DisableProxy(tool.ID); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}
	content, _ = os.ReadFile(cfgPath)
	text = string(content)
	for _, want := range []string{"# my comment", "export SHELL_THING=1", "MY_KEY=mine"} {
		if !strings.Contains(text, want) {
			t.Errorf("禁用后用户内容丢失 %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "BASE_URL=") || strings.Contains(text, "AUTH_TOKEN=") {
		t.Errorf("注入行未被条件清理:\n%s", text)
	}
}

// 自建 ENV 文件也纳入 ownership：未改动禁用即删除整文件
func TestEnvCreatedFileOwnershipLifecycle(t *testing.T) {
	tmp := setupPresetTestHome(t)
	svc := NewCustomCliService("127.0.0.1:18100", nil)
	cfgPath := filepath.Join(tmp, "sub", "tool.env") // 启用前不存在
	tool := writeToolWithInjection(t, svc, cfgPath, "env", []ProxyInjection{{
		TargetFileID: "f1", BaseUrlField: "BASE_URL",
	}})
	if err := svc.EnableProxy(tool.ID); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	if !FileExists(cfgPath) {
		t.Fatal("env 文件应被创建")
	}
	if err := svc.DisableProxy(tool.ID); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}
	if FileExists(cfgPath) {
		t.Error("从零创建且未改动的 env 文件应被整体移除（marker 应覆盖 env 格式）")
	}
}

// 活动态探测遇配置解析错误必须 fail-closed（不能放行目标变更）
func TestUpdateToolFailsClosedOnUnparseableConfig(t *testing.T) {
	tmp := setupPresetTestHome(t)
	svc := NewCustomCliService("127.0.0.1:18100", nil)
	cfgPath := filepath.Join(tmp, "cfg.json")
	if err := os.WriteFile(cfgPath, []byte(`{ broken`), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := writeToolWithInjection(t, svc, cfgPath, "json", []ProxyInjection{{
		TargetFileID: "f1", BaseUrlField: "base_url",
	}})
	moved := *tool
	moved.ConfigFiles = []ConfigFile{{ID: "f1", Label: "cfg", Path: filepath.Join(tmp, "other.json"), Format: "json", IsPrimary: true}}
	err := svc.UpdateTool(tool.ID, moved)
	if err == nil || !strings.Contains(err.Error(), "无法确认") {
		t.Errorf("解析失败应按未知态中止修改, 实际: %v", err)
	}
}
