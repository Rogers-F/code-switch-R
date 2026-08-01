package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// GeminiAuthType 认证类型
type GeminiAuthType string

const (
	GeminiAuthOAuth      GeminiAuthType = "oauth-personal"   // Google 官方 OAuth
	GeminiAuthAPIKey     GeminiAuthType = "gemini-api-key"   // API Key 认证
	GeminiAuthPackycode  GeminiAuthType = "packycode"        // PackyCode 合作方
	GeminiAuthGeneric    GeminiAuthType = "generic"          // 通用第三方
)

// GeminiProvider Gemini 供应商配置
type GeminiProvider struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	WebsiteURL          string            `json:"websiteUrl,omitempty"`
	APIKeyURL           string            `json:"apiKeyUrl,omitempty"`
	BaseURL             string            `json:"baseUrl,omitempty"`
	APIKey              string            `json:"apiKey,omitempty"`
	Model               string            `json:"model,omitempty"`
	Description         string            `json:"description,omitempty"`
	Category            string            `json:"category,omitempty"`            // official, third_party, custom
	PartnerPromotionKey string            `json:"partnerPromotionKey,omitempty"` // 用于识别供应商类型
	Enabled             bool              `json:"enabled"`
	Level               int               `json:"level,omitempty"`               // 优先级分组 (1-10, 默认 1)
	MaxConcurrency      int               `json:"maxConcurrency,omitempty"`      // 最大并发请求数（0=不限，仅代理转发，单进程）
	InsecureSkipVerify  bool              `json:"insecureSkipVerify,omitempty"`  // 跳过上游 TLS 证书验证（仅该供应商，存在中间人风险）
	SupportedModels     map[string]bool   `json:"supportedModels,omitempty"`     // 模型白名单（精确或通配符），空表示不限制
	ModelMapping        map[string]string `json:"modelMapping,omitempty"`        // 模型映射：外部模型名 -> 供应商内部模型名（支持通配符）
	EnvConfig           map[string]string `json:"envConfig,omitempty"`           // .env 配置
	SettingsConfig      map[string]any    `json:"settingsConfig,omitempty"`      // settings.json 配置
}

// IsModelSupported 检查供应商是否支持指定模型（与 claude/codex 的 Provider 同一套逻辑）
func (p *GeminiProvider) IsModelSupported(modelName string) bool {
	return modelSupportedBy(p.SupportedModels, p.ModelMapping, modelName)
}

// GetEffectiveModel 获取实际应该使用的模型名（命中映射时返回映射结果）
func (p *GeminiProvider) GetEffectiveModel(requestedModel string) string {
	return effectiveModelFor(p.ModelMapping, requestedModel)
}

// ValidateConfiguration 验证模型白名单/映射与并发配置
func (p *GeminiProvider) ValidateConfiguration() []string {
	errs := validateModelConfig(p.SupportedModels, p.ModelMapping)
	if p.MaxConcurrency < 0 {
		errs = append(errs, "最大并发数不能为负（0 表示不限）")
	}
	return errs
}

// GeminiPreset 预设供应商
type GeminiPreset struct {
	Name                string            `json:"name"`
	WebsiteURL          string            `json:"websiteUrl"`
	APIKeyURL           string            `json:"apiKeyUrl,omitempty"`
	BaseURL             string            `json:"baseUrl,omitempty"`
	Model               string            `json:"model,omitempty"`
	Description         string            `json:"description,omitempty"`
	Category            string            `json:"category"`
	PartnerPromotionKey string            `json:"partnerPromotionKey,omitempty"`
	EnvConfig           map[string]string `json:"envConfig,omitempty"`
}

// GeminiStatus Gemini 配置状态
type GeminiStatus struct {
	Enabled        bool           `json:"enabled"`
	CurrentProvider string         `json:"currentProvider,omitempty"`
	AuthType       GeminiAuthType `json:"authType"`
	HasAPIKey      bool           `json:"hasApiKey"`
	HasBaseURL     bool           `json:"hasBaseUrl"`
	Model          string         `json:"model,omitempty"`
}

// GeminiService Gemini 配置管理服务
type GeminiService struct {
	mu        sync.Mutex
	providers []GeminiProvider
	policy    *DefaultModelPolicy
	relayAddr string
	// configGen 配置代数：并发限流按它接受容量热更新
	configGen atomic.Int64
}

// configGeneration 返回当前配置代数
func (s *GeminiService) configGeneration() int64 {
	return s.configGen.Load()
}

// NewGeminiService 创建 Gemini 服务
func NewGeminiService(relayAddr string, policy *DefaultModelPolicy) *GeminiService {
	if relayAddr == "" {
		relayAddr = ":18100"
	}
	svc := &GeminiService{
		policy:    policy,
		relayAddr: relayAddr,
	}
	// 加载已保存的供应商配置
	_ = svc.loadProviders()
	return svc
}

// currentPresets 基于当前解析出的默认模型即时生成预设(随同步数据自动更新)。
func (s *GeminiService) currentPresets() []GeminiPreset {
	model := FallbackGeminiDefaultModel
	if s.policy != nil {
		if m := s.policy.GeminiDefaultModel(); m != "" {
			model = m
		}
	}
	return getGeminiPresets(model)
}

// getGeminiPresets 获取预设供应商列表
func getGeminiPresets(defaultModel string) []GeminiPreset {
	return []GeminiPreset{
		{
			Name:                "Google Official",
			WebsiteURL:          "https://ai.google.dev/",
			APIKeyURL:           "https://aistudio.google.com/apikey",
			Description:         "Google 官方 Gemini API (OAuth)",
			Category:            "official",
			PartnerPromotionKey: "google-official",
			EnvConfig:           map[string]string{}, // 空 env，使用 OAuth
		},
		{
			Name:                "PackyCode",
			WebsiteURL:          "https://www.packyapi.com",
			APIKeyURL:           "https://www.packyapi.com/register?aff=cc-switch",
			BaseURL:             "https://www.packyapi.com",
			Model:               defaultModel,
			Description:         "PackyCode 中转服务",
			Category:            "third_party",
			PartnerPromotionKey: "packycode",
			EnvConfig: map[string]string{
				"GOOGLE_GEMINI_BASE_URL": "https://www.packyapi.com",
				"GEMINI_MODEL":           defaultModel,
			},
		},
		{
			Name:        "自定义",
			WebsiteURL:  "",
			Description: "自定义 Gemini API 端点",
			Category:    "custom",
			EnvConfig: map[string]string{
				"GOOGLE_GEMINI_BASE_URL": "",
				"GEMINI_MODEL":           defaultModel,
			},
		},
	}
}

// Start Wails生命周期方法
func (s *GeminiService) Start() error {
	return nil
}

// Stop Wails生命周期方法
func (s *GeminiService) Stop() error {
	return nil
}

// GetPresets 获取预设供应商列表
func (s *GeminiService) GetPresets() []GeminiPreset {
	return s.currentPresets()
}

// GetProviders 获取已配置的供应商列表
func (s *GeminiService) GetProviders() []GeminiProvider {
	providers, _ := s.providersWithGen()
	return providers
}

// providersWithGen 锁内一致地返回 (providers, 配置代数)，
// 供并发限流配对使用（分两步读取会让旧配置带上新代数）
func (s *GeminiService) providersWithGen() ([]GeminiProvider, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 返回拷贝，避免调用方在锁外与写路径共享同一底层数组造成数据竞争
	return append([]GeminiProvider(nil), s.providers...), s.configGen.Load()
}

// AddProvider 添加供应商
func (s *GeminiService) AddProvider(provider GeminiProvider) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 检查 ID 是否重复
	for _, p := range s.providers {
		if provider.ID != "" && p.ID == provider.ID {
			return fmt.Errorf("供应商 ID '%s' 已存在", provider.ID)
		}
	}

	// 生成 ID（如果没有）：从 len+1 起递增直到唯一，避免删除后与残留旧 ID 撞车
	if provider.ID == "" {
		existing := make(map[string]bool, len(s.providers))
		for _, p := range s.providers {
			existing[p.ID] = true
		}
		for n := len(s.providers) + 1; ; n++ {
			candidate := fmt.Sprintf("gemini-%d", n)
			if !existing[candidate] {
				provider.ID = candidate
				break
			}
		}
	}

	original := s.providers
	s.providers = append(append([]GeminiProvider(nil), s.providers...), provider)
	if err := s.saveProviders(); err != nil {
		// 落盘失败必须回滚：否则内存已变而代数未增，产生同代不同容量
		s.providers = original
		return err
	}
	return nil
}

// UpdateProvider 更新供应商
func (s *GeminiService) UpdateProvider(provider GeminiProvider) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, p := range s.providers {
		if p.ID == provider.ID {
			// 允许更新 API Key；若未提供（空/全空白）则保留旧值，避免误清空
			if strings.TrimSpace(provider.APIKey) == "" {
				provider.APIKey = p.APIKey
			}
			original := s.providers
			updated := append([]GeminiProvider(nil), s.providers...)
			updated[i] = provider
			s.providers = updated
			if err := s.saveProviders(); err != nil {
				s.providers = original
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("未找到 ID 为 '%s' 的供应商", provider.ID)
}

// DeleteProvider 删除供应商
func (s *GeminiService) DeleteProvider(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, p := range s.providers {
		if p.ID == id {
			original := s.providers
			updated := append([]GeminiProvider(nil), s.providers[:i]...)
			updated = append(updated, s.providers[i+1:]...)
			s.providers = updated
			if err := s.saveProviders(); err != nil {
				s.providers = original
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("未找到 ID 为 '%s' 的供应商", id)
}

// SwitchProvider 切换到指定供应商
// 注意：代理启用时禁止切换（与 Claude/Codex 保持一致）
func (s *GeminiService) SwitchProvider(id string) error {
	// 代理检查：启用时禁止切换
	proxyStatus, err := s.ProxyStatus()
	if err != nil {
		return fmt.Errorf("检查代理状态失败: %w", err)
	}
	if proxyStatus != nil && proxyStatus.Enabled {
		return fmt.Errorf("本地代理已启用，请先关闭代理再切换供应商")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var provider *GeminiProvider
	for i := range s.providers {
		if s.providers[i].ID == id {
			provider = &s.providers[i]
			break
		}
	}
	if provider == nil {
		return fmt.Errorf("未找到 ID 为 '%s' 的供应商", id)
	}

	// 检测认证类型
	authType := detectGeminiAuthType(provider)

	// 根据认证类型写入配置
	switch authType {
	case GeminiAuthOAuth:
		// OAuth：清空 .env
		if err := writeGeminiEnv(map[string]string{}); err != nil {
			return fmt.Errorf("写入 .env 失败: %w", err)
		}
		// 写入 OAuth 认证标志
		if err := writeGeminiSettings(map[string]any{
			"security": map[string]any{
				"auth": map[string]any{
					"selectedType": string(GeminiAuthOAuth),
				},
			},
		}); err != nil {
			return fmt.Errorf("写入 settings.json 失败: %w", err)
		}

	case GeminiAuthPackycode, GeminiAuthAPIKey, GeminiAuthGeneric:
		// 配置验证：API Key 认证需要 API Key 或 BaseURL
		if provider.APIKey == "" && provider.BaseURL == "" {
			// 检查 EnvConfig 中是否有配置
			hasAPIKey := provider.EnvConfig != nil && provider.EnvConfig["GEMINI_API_KEY"] != ""
			hasBaseURL := provider.EnvConfig != nil && provider.EnvConfig["GOOGLE_GEMINI_BASE_URL"] != ""
			if !hasAPIKey && !hasBaseURL {
				return fmt.Errorf("供应商 '%s' 配置不完整：需要 API Key 或 Base URL", provider.Name)
			}
		}

		// 构建 envConfig：先从预设获取，再用 provider 字段覆盖
		envConfig := make(map[string]string)

		// 1. 先查找预设并复制其 EnvConfig
		for _, preset := range s.currentPresets() {
			if preset.Name == provider.Name || preset.PartnerPromotionKey == provider.PartnerPromotionKey {
				for k, v := range preset.EnvConfig {
					if v != "" {
						envConfig[k] = v
					}
				}
				break
			}
		}

		// 2. 再用 provider.EnvConfig 覆盖
		if provider.EnvConfig != nil {
			for k, v := range provider.EnvConfig {
				if v != "" {
					envConfig[k] = v
				}
			}
		}

		// 3. 最后用 provider 顶级字段覆盖（优先级最高）
		if provider.BaseURL != "" {
			envConfig["GOOGLE_GEMINI_BASE_URL"] = provider.BaseURL
		}
		if provider.APIKey != "" {
			envConfig["GEMINI_API_KEY"] = provider.APIKey
		}
		if provider.Model != "" {
			envConfig["GEMINI_MODEL"] = provider.Model
		}

		if err := writeGeminiEnv(envConfig); err != nil {
			return fmt.Errorf("写入 .env 失败: %w", err)
		}

		// 按认证类型区分 selectedType 值
		var selectedType string
		switch authType {
		case GeminiAuthPackycode:
			selectedType = string(GeminiAuthPackycode) // "packycode"
		case GeminiAuthAPIKey:
			selectedType = string(GeminiAuthAPIKey) // "gemini-api-key"
		case GeminiAuthGeneric:
			selectedType = string(GeminiAuthGeneric) // "generic"
		}

		// 写入认证标志
		if err := writeGeminiSettings(map[string]any{
			"security": map[string]any{
				"auth": map[string]any{
					"selectedType": selectedType,
				},
			},
		}); err != nil {
			return fmt.Errorf("写入 settings.json 失败: %w", err)
		}
	}

	// 更新启用状态（副本上改，落盘失败回滚）
	original := s.providers
	updated := append([]GeminiProvider(nil), s.providers...)
	for i := range updated {
		updated[i].Enabled = (updated[i].ID == id)
	}
	s.providers = updated
	if err := s.saveProviders(); err != nil {
		s.providers = original
		return err
	}
	return nil
}

// GetStatus 获取当前 Gemini 配置状态
func (s *GeminiService) GetStatus() (*GeminiStatus, error) {
	status := &GeminiStatus{}

	// 读取 .env
	envConfig, err := readGeminiEnv()
	if err != nil {
		// 文件不存在时返回默认状态
		return status, nil
	}

	status.HasAPIKey = envConfig["GEMINI_API_KEY"] != ""
	status.HasBaseURL = envConfig["GOOGLE_GEMINI_BASE_URL"] != ""
	status.Model = envConfig["GEMINI_MODEL"]

	// 读取 settings.json 判断认证类型
	settings, err := readGeminiSettings()
	if err == nil {
		if security, ok := settings["security"].(map[string]any); ok {
			if auth, ok := security["auth"].(map[string]any); ok {
				if selectedType, ok := auth["selectedType"].(string); ok {
					status.AuthType = GeminiAuthType(selectedType)
				}
			}
		}
	}

	// 判断是否启用
	status.Enabled = status.HasAPIKey || status.AuthType == GeminiAuthOAuth

	// 查找当前启用的供应商
	s.mu.Lock()
	for _, p := range s.providers {
		if p.Enabled {
			status.CurrentProvider = p.Name
			break
		}
	}
	s.mu.Unlock()

	return status, nil
}

// detectGeminiAuthType 检测供应商认证类型
func detectGeminiAuthType(provider *GeminiProvider) GeminiAuthType {
	// 优先级 1: 检查 partner_promotion_key
	switch strings.ToLower(provider.PartnerPromotionKey) {
	case "google-official":
		return GeminiAuthOAuth
	case "packycode":
		return GeminiAuthPackycode
	}

	// 优先级 2: 检查供应商名称
	// 仅当没有任何凭据配置时才按名称判 OAuth：导入或手工添加的
	// "Google API Key" 之类条目带着 BaseURL/APIKey，若按名称误判为 OAuth，
	// SwitchProvider 的 OAuth 分支会清空 .env，把刚配置的凭据直接丢掉
	nameLower := strings.ToLower(provider.Name)
	if nameLower == "google" || strings.HasPrefix(nameLower, "google ") {
		hasCreds := provider.APIKey != "" || provider.BaseURL != ""
		if !hasCreds && provider.EnvConfig != nil {
			hasCreds = provider.EnvConfig["GEMINI_API_KEY"] != "" || provider.EnvConfig["GOOGLE_GEMINI_BASE_URL"] != ""
		}
		if !hasCreds {
			return GeminiAuthOAuth
		}
	}

	// 优先级 3: 检查 PackyCode 关键词
	keywords := []string{"packycode", "packyapi", "packy"}
	for _, kw := range keywords {
		if strings.Contains(nameLower, kw) {
			return GeminiAuthPackycode
		}
		if strings.Contains(strings.ToLower(provider.WebsiteURL), kw) {
			return GeminiAuthPackycode
		}
		if strings.Contains(strings.ToLower(provider.BaseURL), kw) {
			return GeminiAuthPackycode
		}
	}

	// 默认：通用 API Key 认证
	return GeminiAuthGeneric
}

// getConfigDir 获取 CodeSwitch 配置目录
func getConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".code-switch")
}

// getGeminiDir 获取 Gemini 配置目录
func getGeminiDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini")
}

// getGeminiEnvPath 获取 .env 文件路径
func getGeminiEnvPath() string {
	return filepath.Join(getGeminiDir(), ".env")
}

// getGeminiSettingsPath 获取 settings.json 路径
func getGeminiSettingsPath() string {
	return filepath.Join(getGeminiDir(), "settings.json")
}

// getGeminiProvidersPath 获取供应商配置文件路径
func getGeminiProvidersPath() string {
	return filepath.Join(getConfigDir(), "gemini-providers.json")
}

// readGeminiEnv 读取 .env 文件
func readGeminiEnv() (map[string]string, error) {
	path := getGeminiEnvPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return parseEnvFile(string(data)), nil
}

// parseEnvFile 解析 .env 文件内容
func parseEnvFile(content string) map[string]string {
	result := make(map[string]string)
	// 统一处理 Windows 和 Unix 行尾
	normalizedContent := strings.ReplaceAll(content, "\r\n", "\n")
	normalizedContent = strings.ReplaceAll(normalizedContent, "\r", "\n")
	lines := strings.Split(normalizedContent, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// 跳过空行和注释
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 解析 KEY=VALUE
		idx := strings.Index(line, "=")
		if idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			// 验证 key 有效性
			if key != "" && isValidEnvKey(key) {
				result[key] = value
			}
		}
	}

	return result
}

// isValidEnvKey 验证环境变量名是否有效
func isValidEnvKey(key string) bool {
	for _, c := range key {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

// buildGeminiEnvContent 构建 .env 文件内容（用于预览，不写入磁盘）。
// 必须与 writeGeminiEnv 走同一条按行合并逻辑：配置编辑器把预览内容灌进可编辑文本框，
// 用户点"应用"时原样落盘。若预览是从 map 全量重建的版本，注释与 export 行在预览里就没了，
// 一按应用就把用户手写内容永久抹掉。
func buildGeminiEnvContent(envConfig map[string]string) string {
	existing, _ := os.ReadFile(getGeminiEnvPath())
	return renderGeminiEnvContent(string(existing), envConfig)
}

// renderGeminiEnvContent 把 envConfig 按行合并进现有 .env 内容。
// 仅改写/删除可解析的 KEY=VALUE 行，注释、export 前缀等无法解析的行原样保留，
// 避免重建文件破坏用户手写内容。existingRaw 为空表示文件尚不存在。
func renderGeminiEnvContent(existingRaw string, envConfig map[string]string) string {
	var lines []string
	written := make(map[string]bool)

	// 逐行处理现有内容（为空时跳过，直接按 envConfig 生成）
	if existingRaw != "" {
		normalized := strings.ReplaceAll(existingRaw, "\r\n", "\n")
		normalized = strings.ReplaceAll(normalized, "\r", "\n")
		rawLines := strings.Split(normalized, "\n")
		// 去掉结尾换行符切分出的最后一个空元素
		if n := len(rawLines); n > 0 && rawLines[n-1] == "" {
			rawLines = rawLines[:n-1]
		}
		for _, raw := range rawLines {
			key := ""
			trimmed := strings.TrimSpace(raw)
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				if idx := strings.Index(trimmed, "="); idx > 0 {
					k := strings.TrimSpace(trimmed[:idx])
					if k != "" && isValidEnvKey(k) {
						key = k
					}
				}
			}
			// 注释 / 空行 / 无法解析的行原样保留
			if key == "" {
				lines = append(lines, raw)
				continue
			}
			value, ok := envConfig[key]
			if !ok {
				// 目标配置中已不存在的键，删除该行
				continue
			}
			if written[key] {
				// 重复键只保留第一处
				continue
			}
			lines = append(lines, fmt.Sprintf("%s=%s", key, value))
			written[key] = true
		}
	}

	// 追加文件中尚不存在的键：先按固定顺序，再写其余键
	keys := []string{"GOOGLE_GEMINI_BASE_URL", "GEMINI_API_KEY", "GEMINI_MODEL"}
	for _, key := range keys {
		if value, ok := envConfig[key]; ok && !written[key] {
			lines = append(lines, fmt.Sprintf("%s=%s", key, value))
			written[key] = true
		}
	}
	for key, value := range envConfig {
		if !written[key] {
			lines = append(lines, fmt.Sprintf("%s=%s", key, value))
			written[key] = true
		}
	}

	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	return content
}

// writeGeminiEnv 写入 .env 文件（原子操作），内容由 renderGeminiEnvContent 按行合并生成
func writeGeminiEnv(envConfig map[string]string) error {
	dir := getGeminiDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	path := getGeminiEnvPath()
	existing, _ := os.ReadFile(path)

	// 原子写入（带 fsync 与 Windows 共享冲突重试）
	return atomicWriteFile(path, []byte(renderGeminiEnvContent(string(existing), envConfig)), 0600)
}

// readGeminiSettings 读取 settings.json
func readGeminiSettings() (map[string]any, error) {
	path := getGeminiSettingsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// writeGeminiSettings 写入 settings.json（智能合并）
func writeGeminiSettings(newSettings map[string]any) error {
	dir := getGeminiDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	path := getGeminiSettingsPath()

	// 读取现有配置
	existingSettings := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &existingSettings)
	}

	// 深度合并
	mergedSettings := deepMerge(existingSettings, newSettings)

	// 原子写入（带 fsync 与 Windows 共享冲突重试）
	data, err := json.MarshalIndent(mergedSettings, "", "  ")
	if err != nil {
		return err
	}

	return atomicWriteFile(path, data, 0600)
}

// deepMerge 深度合并两个 map
func deepMerge(dst, src map[string]any) map[string]any {
	result := make(map[string]any)

	// 复制 dst
	for k, v := range dst {
		result[k] = v
	}

	// 合并 src
	for k, v := range src {
		if srcMap, ok := v.(map[string]any); ok {
			if dstMap, ok := result[k].(map[string]any); ok {
				result[k] = deepMerge(dstMap, srcMap)
			} else {
				result[k] = srcMap
			}
		} else {
			result[k] = v
		}
	}

	return result
}

// loadProviders 加载供应商配置
func (s *GeminiService) loadProviders() error {
	path := getGeminiProvidersPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.providers = []GeminiProvider{}
			return nil
		}
		return err
	}

	return json.Unmarshal(data, &s.providers)
}

// saveProviders 保存供应商配置
func (s *GeminiService) saveProviders() error {
	path := getGeminiProvidersPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s.providers, "", "  ")
	if err != nil {
		return err
	}

	// 原子写入（带 fsync 与 Windows 共享冲突重试）
	if err := atomicWriteFile(path, data, 0644); err != nil {
		return err
	}
	s.configGen.Add(1)
	return nil
}

// CreateProviderFromPreset 从预设创建供应商
func (s *GeminiService) CreateProviderFromPreset(presetName string, apiKey string) (*GeminiProvider, error) {
	var preset *GeminiPreset
	presets := s.currentPresets()
	for i := range presets {
		if presets[i].Name == presetName {
			preset = &presets[i]
			break
		}
	}
	if preset == nil {
		return nil, fmt.Errorf("未找到预设 '%s'", presetName)
	}

	// 创建供应商（ID 基于纳秒时间戳保证唯一，len+1 在删除后会与已有 ID 撞车）
	provider := GeminiProvider{
		ID:                  fmt.Sprintf("gemini-%s-%d", strings.ToLower(strings.ReplaceAll(presetName, " ", "-")), time.Now().UnixNano()),
		Name:                preset.Name,
		WebsiteURL:          preset.WebsiteURL,
		APIKeyURL:           preset.APIKeyURL,
		BaseURL:             preset.BaseURL,
		APIKey:              apiKey,
		Model:               preset.Model,
		Description:         preset.Description,
		Category:            preset.Category,
		PartnerPromotionKey: preset.PartnerPromotionKey,
		Enabled:             false,
		EnvConfig:           make(map[string]string),
	}

	// 复制环境配置
	for k, v := range preset.EnvConfig {
		provider.EnvConfig[k] = v
	}

	// 设置 API Key
	if apiKey != "" {
		provider.EnvConfig["GEMINI_API_KEY"] = apiKey
	}

	// 添加供应商
	if err := s.AddProvider(provider); err != nil {
		return nil, err
	}

	return &provider, nil
}

// GeminiProxyStatus Gemini 代理状态
type GeminiProxyStatus struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"base_url"`
}

// ProxyStatus 获取代理状态
func (s *GeminiService) ProxyStatus() (*GeminiProxyStatus, error) {
	status := &GeminiProxyStatus{
		Enabled: false,
		BaseURL: buildProxyURL(s.relayAddr),
	}

	// 读取 .env 文件
	envConfig, err := readGeminiEnv()
	if err != nil {
		// 文件不存在时返回默认状态
		if os.IsNotExist(err) {
			return status, nil
		}
		return status, err
	}

	// 检查是否指向代理
	baseURL := envConfig["GOOGLE_GEMINI_BASE_URL"]
	proxyURL := buildProxyURL(s.relayAddr)
	status.Enabled = strings.EqualFold(baseURL, proxyURL)

	return status, nil
}

// geminiBaseURLKey 和 geminiAPIKeyKey 用于代理注入
const (
	geminiBaseURLKey    = "GOOGLE_GEMINI_BASE_URL"
	geminiAPIKeyKey     = "GEMINI_API_KEY"
	geminiProxyTokenVal = "code-switch-r"
)

// EnableProxy 启用代理
func (s *GeminiService) EnableProxy() error {
	// 与 SwitchProvider 等写路径互斥，防止并发读改写 .env 时把过期内容存为基线
	s.mu.Lock()
	defer s.mu.Unlock()

	// OAuth 认证不读 .env 的 BaseURL/API Key，注入代理配置后 CLI 会继续走
	// OAuth 通道导致代理静默失效，这里直接拒绝并提示先切换认证方式
	if settings, err := readGeminiSettings(); err == nil {
		if security, ok := settings["security"].(map[string]any); ok {
			if auth, ok := security["auth"].(map[string]any); ok {
				if selectedType, ok := auth["selectedType"].(string); ok && selectedType == string(GeminiAuthOAuth) {
					return fmt.Errorf("当前 Gemini 使用 Google OAuth 认证，代理不会生效；请先切换到 API Key 类型的供应商再启用代理")
				}
			}
		}
	}

	dir := getGeminiDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	envPath := getGeminiEnvPath()
	backupPath := envPath + ".code-switch.backup"

	// 幂等化检查：状态文件存在则视为已启用，不覆盖基线
	stateExists, err := ProxyStateExists("gemini")
	if err != nil {
		return err
	}

	// 读取现有配置（如果有）
	// 注意：不能忽略非 ErrNotExist 的错误，避免覆盖/破坏现有文件
	existingEnv, readErr := readGeminiEnv()
	fileExisted := false
	if readErr != nil {
		if !os.IsNotExist(readErr) {
			return fmt.Errorf("读取现有 .env 失败: %w", readErr)
		}
		// 文件不存在，使用空 map
		existingEnv = make(map[string]string)
	} else {
		fileExisted = true // 文件存在（即使内容为空也算存在）
	}
	if existingEnv == nil {
		existingEnv = make(map[string]string)
	}

	// 仅首次启用时创建备份和状态文件
	if !stateExists {
		// 备份现有 .env（如果存在）
		if _, statErr := os.Stat(envPath); statErr == nil {
			content, readFileErr := os.ReadFile(envPath)
			if readFileErr != nil {
				return fmt.Errorf("读取现有 .env 失败: %w", readFileErr)
			}
			if err := os.WriteFile(backupPath, content, 0600); err != nil {
				return fmt.Errorf("备份 .env 失败: %w", err)
			}
		}

		// 记录启用前的基线到状态文件
		state := &ProxyState{
			TargetPath:        envPath,
			FileExisted:       fileExisted,
			EnvExisted:        len(existingEnv) > 0,
			InjectedBaseURL:   buildProxyURL(s.relayAddr),
			InjectedAuthToken: geminiProxyTokenVal,
		}

		// 记录原始 BASE_URL（如果 key 存在，即使是空值也记录）
		// 与 Claude 保持一致：指针表示"是否存在"，空字符串也是有效值
		if v, ok := existingEnv[geminiBaseURLKey]; ok {
			state.OriginalBaseURL = &v
		}
		// 记录原始 API_KEY（如果 key 存在，即使是空值也记录）
		if v, ok := existingEnv[geminiAPIKeyKey]; ok {
			state.OriginalAuthToken = &v
		}

		if err := SaveProxyState("gemini", state); err != nil {
			return err
		}
	}

	// 设置代理 URL 和占位 API Key（与 Claude/Codex 保持一致）
	existingEnv[geminiBaseURLKey] = buildProxyURL(s.relayAddr)
	existingEnv[geminiAPIKeyKey] = geminiProxyTokenVal

	// 写入 .env
	if err := writeGeminiEnv(existingEnv); err != nil {
		return fmt.Errorf("写入 .env 失败: %w", err)
	}

	return nil
}

// DisableProxy 禁用代理（手术式撤销：仅移除注入的代理配置，保留用户其他编辑）
func (s *GeminiService) DisableProxy() error {
	// 与 SwitchProvider 等写路径互斥，避免恢复基线时覆盖并发写入的 .env
	s.mu.Lock()
	defer s.mu.Unlock()

	envPath := getGeminiEnvPath()

	// 读取当前 .env（保留用户在代理期间的所有编辑）
	envConfig, err := readGeminiEnv()
	if err != nil {
		if os.IsNotExist(err) {
			// 配置文件不存在，清理状态文件后返回
			return DeleteProxyState("gemini")
		}
		return fmt.Errorf("读取 .env 失败: %w", err)
	}
	if envConfig == nil {
		envConfig = make(map[string]string)
	}

	// 尝试加载状态文件
	state, stateErr := LoadProxyState("gemini")
	if stateErr != nil {
		// 兜底：状态文件缺失/损坏时，仅在字段仍等于代理值时才删除
		// 避免误删用户自定义的直连配置
		changed := s.fallbackCleanupEnv(envConfig)
		if changed {
			if err := writeGeminiEnv(envConfig); err != nil {
				return fmt.Errorf("写入 .env 失败: %w", err)
			}
		}
		return DeleteProxyState("gemini")
	}

	// 有状态文件：按基线做"手术式"恢复

	// 1. 恢复或删除 GOOGLE_GEMINI_BASE_URL
	if state.OriginalBaseURL != nil {
		envConfig[geminiBaseURLKey] = *state.OriginalBaseURL
	} else {
		delete(envConfig, geminiBaseURLKey)
	}

	// 2. 恢复或删除 GEMINI_API_KEY
	if state.OriginalAuthToken != nil {
		envConfig[geminiAPIKeyKey] = *state.OriginalAuthToken
	} else {
		delete(envConfig, geminiAPIKeyKey)
	}

	// 3. 若 .env 变空且启用前不存在文件，则删除文件
	if len(envConfig) == 0 && !state.FileExisted {
		if err := os.Remove(envPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("删除空 .env 失败: %w", err)
		}
	} else {
		// 写入修改后的配置
		if err := writeGeminiEnv(envConfig); err != nil {
			return fmt.Errorf("写入 .env 失败: %w", err)
		}
	}

	return DeleteProxyState("gemini")
}

// fallbackCleanupEnv 兜底清理：仅删除仍等于代理值的字段
func (s *GeminiService) fallbackCleanupEnv(envConfig map[string]string) bool {
	changed := false
	proxyURL := buildProxyURL(s.relayAddr)

	// 检查 GOOGLE_GEMINI_BASE_URL 是否仍指向代理
	if v, ok := envConfig[geminiBaseURLKey]; ok {
		if strings.EqualFold(
			strings.TrimSuffix(strings.TrimSpace(v), "/"),
			strings.TrimSuffix(strings.TrimSpace(proxyURL), "/"),
		) {
			delete(envConfig, geminiBaseURLKey)
			changed = true
		}
	}

	// 检查 GEMINI_API_KEY 是否为代理占位值
	if v, ok := envConfig[geminiAPIKeyKey]; ok && v == geminiProxyTokenVal {
		delete(envConfig, geminiAPIKeyKey)
		changed = true
	}

	return changed
}

// buildProxyURL 构建代理 URL（包含 /gemini 前缀）
func buildProxyURL(relayAddr string) string {
	addr := strings.TrimSpace(relayAddr)
	if addr == "" {
		addr = ":18100"
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr + "/gemini"
	}
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	return host + "/gemini"
}

// DuplicateProvider 复制供应商
func (s *GeminiService) DuplicateProvider(sourceID string) (*GeminiProvider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. 查找源供应商
	var source *GeminiProvider
	for i := range s.providers {
		if s.providers[i].ID == sourceID {
			source = &s.providers[i]
			break
		}
	}
	if source == nil {
		return nil, fmt.Errorf("未找到 ID 为 '%s' 的供应商", sourceID)
	}

	// 2. 生成新 ID。用纳秒而非秒：复制按钮无防抖，同一秒内连点两次会生成完全相同的 ID。
	// 再扫一遍现有列表做兜底去重（此处已持有 s.mu，成本可忽略）
	newID := fmt.Sprintf("%s-copy-%d", sourceID, time.Now().UnixNano())
	for seq := 2; s.providerIDExists(newID); seq++ {
		newID = fmt.Sprintf("%s-copy-%d-%d", sourceID, time.Now().UnixNano(), seq)
	}

	// 3. 克隆配置（深拷贝）
	cloned := GeminiProvider{
		ID:                  newID,
		Name:                s.uniqueDuplicateName(source.Name),
		WebsiteURL:          source.WebsiteURL,
		APIKeyURL:           source.APIKeyURL,
		BaseURL:             source.BaseURL,
		APIKey:              source.APIKey,
		Model:               source.Model,
		Description:         source.Description,
		Category:            source.Category,
		PartnerPromotionKey: source.PartnerPromotionKey,
		Enabled:             false, // 默认禁用，避免与源供应商冲突
		Level:               source.Level,
		MaxConcurrency:      source.MaxConcurrency,
		InsecureSkipVerify:  source.InsecureSkipVerify, // 复制 TLS 跳验开关
	}

	// 4. 深拷贝 map（避免共享引用）
	if source.EnvConfig != nil {
		cloned.EnvConfig = make(map[string]string, len(source.EnvConfig))
		for k, v := range source.EnvConfig {
			cloned.EnvConfig[k] = v
		}
	}

	if source.SupportedModels != nil {
		cloned.SupportedModels = make(map[string]bool, len(source.SupportedModels))
		for k, v := range source.SupportedModels {
			cloned.SupportedModels[k] = v
		}
	}

	if source.ModelMapping != nil {
		cloned.ModelMapping = make(map[string]string, len(source.ModelMapping))
		for k, v := range source.ModelMapping {
			cloned.ModelMapping[k] = v
		}
	}

	if source.SettingsConfig != nil {
		cloned.SettingsConfig = make(map[string]any, len(source.SettingsConfig))
		for k, v := range source.SettingsConfig {
			// 对于 map/slice 类型的值，需要深拷贝（简化处理，直接赋值）
			cloned.SettingsConfig[k] = v
		}
	}

	// 5. 添加到列表并保存（copy-on-write，落盘失败回滚，
	// 不留下内存已变而配置代数未增的状态）
	original := s.providers
	s.providers = append(append([]GeminiProvider(nil), s.providers...), cloned)
	if err := s.saveProviders(); err != nil {
		s.providers = original
		return nil, fmt.Errorf("保存副本失败: %w", err)
	}

	return &cloned, nil
}

// uniqueDuplicateName 基于源供应商名生成副本名；若与现有供应商重名（如同一个源被连续复制多次），
// 追加序号直到不冲突，避免出现完全同名的条目——前端 Gemini 分支以 name 作为增删改的匹配键，
// 重名会导致删除静默失效、编辑写到错误的供应商上。调用方需已持有 s.mu。
func (s *GeminiService) uniqueDuplicateName(sourceName string) string {
	baseName := sourceName + " (副本)"

	existing := make(map[string]bool, len(s.providers))
	for _, p := range s.providers {
		existing[p.Name] = true
	}

	if !existing[baseName] {
		return baseName
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s %d", baseName, n)
		if !existing[candidate] {
			return candidate
		}
	}
}

// providerIDExists 判断 ID 是否已被占用。调用方需已持有 s.mu。
func (s *GeminiService) providerIDExists(id string) bool {
	for _, p := range s.providers {
		if p.ID == id {
			return true
		}
	}
	return false
}

// importProviders 批量导入供应商：锁内一次性查重、补 ID，单次落盘。
// 名称（不区分大小写）已存在的候选跳过；BaseURL 与 APIKey 都相同的候选
// 视为重复跳过——同一地址配不同 Key 属合法多账号场景，不能只按 URL 去重。
// 保存失败时回滚内存状态，不留下半导入结果。返回实际新增数量。
func (s *GeminiService) importProviders(candidates []GeminiProvider) (int, error) {
	if len(candidates) == 0 {
		return 0, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existingNames := make(map[string]bool, len(s.providers))
	existingIDs := make(map[string]bool, len(s.providers))
	existingURLKey := make(map[string]bool, len(s.providers))
	for _, p := range s.providers {
		existingNames[normalizeName(p.Name)] = true
		existingIDs[p.ID] = true
		if url := normalizeURL(p.BaseURL); url != "" {
			existingURLKey[url+"\x00"+p.APIKey] = true
		}
	}

	original := s.providers
	merged := append([]GeminiProvider(nil), s.providers...)
	added := 0
	seq := len(merged) + 1
	for _, candidate := range candidates {
		name := normalizeName(candidate.Name)
		if name == "" || existingNames[name] {
			continue
		}
		// 空 Key 只按名称去重（脱敏包/OAuth 类条目），
		// 按 URL+空Key 去重会把同地址多账号合并掉
		if url := normalizeURL(candidate.BaseURL); url != "" && candidate.APIKey != "" {
			key := url + "\x00" + candidate.APIKey
			if existingURLKey[key] {
				continue
			}
			existingURLKey[key] = true
		}
		if candidate.ID == "" || existingIDs[candidate.ID] {
			for {
				id := fmt.Sprintf("gemini-%d", seq)
				seq++
				if !existingIDs[id] {
					candidate.ID = id
					break
				}
			}
		}
		existingIDs[candidate.ID] = true
		existingNames[name] = true
		merged = append(merged, candidate)
		added++
	}

	if added == 0 {
		return 0, nil
	}

	s.providers = merged
	if err := s.saveProviders(); err != nil {
		s.providers = original
		return 0, err
	}
	return added, nil
}

// ReorderProviders 重新排序供应商（按传入的 ID 顺序）
func (s *GeminiService) ReorderProviders(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(ids) == 0 {
		return nil
	}

	// 创建 ID -> Provider 的映射
	providerMap := make(map[string]GeminiProvider)
	for _, p := range s.providers {
		providerMap[p.ID] = p
	}

	// 按传入的 ID 顺序重新排列
	newProviders := make([]GeminiProvider, 0, len(ids))
	for _, id := range ids {
		if p, ok := providerMap[id]; ok {
			newProviders = append(newProviders, p)
			delete(providerMap, id) // 标记已处理
		}
	}

	// 如果有遗漏的 provider（不在 ids 中），追加到末尾
	for _, p := range s.providers {
		if _, ok := providerMap[p.ID]; ok {
			newProviders = append(newProviders, p)
		}
	}

	original := s.providers
	s.providers = newProviders
	if err := s.saveProviders(); err != nil {
		s.providers = original
		return err
	}
	return nil
}

// ApplySingleProvider 直连应用单一供应商（别名，统一 API 命名）
// 与 SwitchProvider 功能相同，仅在代理关闭时可用
func (s *GeminiService) ApplySingleProvider(id string) error {
	return s.SwitchProvider(id)
}

// GetDirectAppliedProviderID 返回当前直连应用的 Provider ID
// 通过读取 CLI 配置文件反推当前使用的 provider
// 返回值：
//   - nil: 配置指向本地代理 或 无法匹配到 provider
//   - *string: 匹配到的 provider ID
func (s *GeminiService) GetDirectAppliedProviderID() (*string, error) {
	// 1. 检查代理状态
	proxyStatus, err := s.ProxyStatus()
	if err != nil {
		return nil, fmt.Errorf("检查代理状态失败: %w", err)
	}
	// 代理启用时，直连状态无意义
	if proxyStatus != nil && proxyStatus.Enabled {
		return nil, nil
	}

	// 2. 读取当前 .env 配置
	envConfig, err := readGeminiEnv()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 .env 失败: %w", err)
	}

	currentBaseURL := envConfig["GOOGLE_GEMINI_BASE_URL"]
	currentAPIKey := envConfig["GEMINI_API_KEY"]

	// 3. 遍历所有供应商进行匹配（CLI 配置为真源，不依赖 Enabled 状态）
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, p := range s.providers {
		// 匹配 BaseURL（来自 provider 顶级字段或 EnvConfig）
		providerBaseURL := p.BaseURL
		if providerBaseURL == "" && p.EnvConfig != nil {
			providerBaseURL = p.EnvConfig["GOOGLE_GEMINI_BASE_URL"]
		}

		// 匹配 APIKey（来自 provider 顶级字段或 EnvConfig）
		providerAPIKey := p.APIKey
		if providerAPIKey == "" && p.EnvConfig != nil {
			providerAPIKey = p.EnvConfig["GEMINI_API_KEY"]
		}

		// URL + Key 双重匹配（使用 TrimRight 去除所有尾斜杠）
		urlMatch := strings.EqualFold(
			strings.TrimRight(strings.TrimSpace(currentBaseURL), "/"),
			strings.TrimRight(strings.TrimSpace(providerBaseURL), "/"),
		)
		keyMatch := currentAPIKey == providerAPIKey

		// OAuth 模式特殊处理：无 API Key 时只匹配 URL
		if providerAPIKey == "" && currentAPIKey == "" {
			if urlMatch || (currentBaseURL == "" && providerBaseURL == "") {
				id := p.ID
				return &id, nil
			}
		} else if urlMatch && keyMatch {
			id := p.ID
			return &id, nil
		}
	}

	return nil, nil
}
