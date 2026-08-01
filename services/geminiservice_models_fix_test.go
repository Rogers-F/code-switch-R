package services

import (
	"testing"
)

// 复制供应商必须深拷贝白名单/映射，副本改动不得影响源
func TestGeminiDuplicateCopiesModelConfig(t *testing.T) {
	setupGeminiTestHome(t)
	svc := NewGeminiService("127.0.0.1:18100", nil)

	if err := svc.AddProvider(GeminiProvider{
		Name:            "Src",
		SupportedModels: map[string]bool{"gemini-2.5-pro": true},
		ModelMapping:    map[string]string{"gemini-*": "vendor/gemini-*"},
	}); err != nil {
		t.Fatalf("添加供应商失败: %v", err)
	}
	srcID := svc.GetProviders()[0].ID

	cloned, err := svc.DuplicateProvider(srcID)
	if err != nil {
		t.Fatalf("复制供应商失败: %v", err)
	}
	if !cloned.SupportedModels["gemini-2.5-pro"] {
		t.Error("副本应携带白名单")
	}
	if cloned.ModelMapping["gemini-*"] != "vendor/gemini-*" {
		t.Error("副本应携带映射")
	}

	// 改副本不影响源（深拷贝）
	cloned.SupportedModels["injected"] = true
	cloned.ModelMapping["injected"] = "x"
	for _, p := range svc.GetProviders() {
		if p.ID == srcID {
			if p.SupportedModels["injected"] || p.ModelMapping["injected"] != "" {
				t.Error("副本与源共享了 map 引用")
			}
		}
	}
}

// 名称含 Google 但带凭据的供应商不得按名称误判为 OAuth
// （OAuth 分支会清空 .env，把刚配置的凭据丢掉）
func TestDetectGeminiAuthTypeCredentialsBeatName(t *testing.T) {
	withKey := &GeminiProvider{Name: "Google API Key", APIKey: "sk-x"}
	if got := detectGeminiAuthType(withKey); got == GeminiAuthOAuth {
		t.Errorf("带 APIKey 的 Google 命名供应商不应判为 OAuth，得到 %s", got)
	}

	withBase := &GeminiProvider{Name: "Google Proxy", BaseURL: "https://proxy.example.com"}
	if got := detectGeminiAuthType(withBase); got == GeminiAuthOAuth {
		t.Errorf("带 BaseURL 的 Google 命名供应商不应判为 OAuth，得到 %s", got)
	}

	withEnv := &GeminiProvider{Name: "Google Mirror", EnvConfig: map[string]string{"GEMINI_API_KEY": "sk-e"}}
	if got := detectGeminiAuthType(withEnv); got == GeminiAuthOAuth {
		t.Errorf("EnvConfig 带 Key 的 Google 命名供应商不应判为 OAuth，得到 %s", got)
	}

	// 无凭据时名称启发保持原行为
	bare := &GeminiProvider{Name: "Google"}
	if got := detectGeminiAuthType(bare); got != GeminiAuthOAuth {
		t.Errorf("无凭据的 Google 应判为 OAuth，得到 %s", got)
	}

	// 显式合作方标记优先级不变
	official := &GeminiProvider{Name: "Whatever", PartnerPromotionKey: "google-official"}
	if got := detectGeminiAuthType(official); got != GeminiAuthOAuth {
		t.Errorf("google-official 标记应判为 OAuth，得到 %s", got)
	}
}

// 批量导入：名称查重、URL+Key 组合查重、一次落盘
func TestGeminiImportProvidersDedup(t *testing.T) {
	setupGeminiTestHome(t)
	svc := NewGeminiService("127.0.0.1:18100", nil)

	if err := svc.AddProvider(GeminiProvider{Name: "Existing", BaseURL: "https://a.example.com", APIKey: "k1"}); err != nil {
		t.Fatalf("预置供应商失败: %v", err)
	}

	added, err := svc.importProviders([]GeminiProvider{
		{Name: "existing", BaseURL: "https://other.example.com", APIKey: "kx"},   // 名称重复（大小写不敏感）→ 跳过
		{Name: "SameURLSameKey", BaseURL: "https://a.example.com", APIKey: "k1"}, // URL+Key 均重复 → 跳过
		{Name: "SameURLNewKey", BaseURL: "https://a.example.com", APIKey: "k2"},  // 同 URL 不同 Key → 导入
		{Name: "Fresh", BaseURL: "https://b.example.com", APIKey: "k3"},          // 全新 → 导入
		{Name: "  ", BaseURL: "https://c.example.com", APIKey: "k4"},             // 空名 → 跳过
	})
	if err != nil {
		t.Fatalf("importProviders 失败: %v", err)
	}
	if added != 2 {
		t.Fatalf("added = %d, 期望 2", added)
	}

	providers := svc.GetProviders()
	if len(providers) != 3 {
		t.Fatalf("总数 = %d, 期望 3: %+v", len(providers), providers)
	}
	seenIDs := map[string]bool{}
	for _, p := range providers {
		if p.ID == "" || seenIDs[p.ID] {
			t.Errorf("ID 缺失或重复: %+v", p)
		}
		seenIDs[p.ID] = true
	}

	// 重跑同一批候选：全部查重跳过
	again, err := svc.importProviders([]GeminiProvider{
		{Name: "SameURLNewKey", BaseURL: "https://a.example.com", APIKey: "k2"},
		{Name: "Fresh", BaseURL: "https://b.example.com", APIKey: "k3"},
	})
	if err != nil {
		t.Fatalf("二次 importProviders 失败: %v", err)
	}
	if again != 0 {
		t.Errorf("二次导入应为 0，得到 %d", again)
	}
}
