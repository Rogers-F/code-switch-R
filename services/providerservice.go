package services

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// AvailabilityConfig 可用性监控高级配置
// 在可用性页面的"高级配置"弹窗中设置，可选
type AvailabilityConfig struct {
	TestModel    string `json:"testModel,omitempty"`    // 覆盖默认测试模型
	TestEndpoint string `json:"testEndpoint,omitempty"` // 覆盖默认测试端点
	Timeout      int    `json:"timeout,omitempty"`      // 覆盖默认超时（毫秒）
}

// SanitizeConfig 请求清理高级配置（黑名单模式）。
// 三个列表用指针区分三态：nil 指针（字段缺失/null）= 用内置默认黑名单；
// 指向空数组 = 该维度什么都不删；非空 = 用自定义列表。
type SanitizeConfig struct {
	BlockedBodyFields *[]string `json:"blockedBodyFields,omitempty"` // 要移除的请求体顶层字段
	BlockedHeaders    *[]string `json:"blockedHeaders,omitempty"`    // 要移除的请求头（小写）
	BlockedBetaValues *[]string `json:"blockedBetaValues,omitempty"` // anthropic-beta 中要移除的值
}

// cloneStringListPtr 深拷贝三态列表指针：nil 保持 nil，非 nil 复制底层数组。
func cloneStringListPtr(p *[]string) *[]string {
	if p == nil {
		return nil
	}
	v := append([]string{}, (*p)...)
	return &v
}

type Provider struct {
	ID      int64  `json:"id"` // 修复：使用 int64 支持大 ID 值
	Name    string `json:"name"`
	APIURL  string `json:"apiUrl"`
	APIKey  string `json:"apiKey"`
	Site    string `json:"officialSite"`
	Icon    string `json:"icon"`
	Tint    string `json:"tint"`
	Accent  string `json:"accent"`
	Enabled bool   `json:"enabled"`

	// API 端点路径（可选）- 覆盖平台默认端点
	// 如：GLM 模型需要使用 /v1/chat/completions 而非 /v1/messages
	// 留空则使用平台默认（claude: /v1/messages, codex: /responses）
	APIEndpoint string `json:"apiEndpoint,omitempty"`

	// 备用 API 地址（可选，最多 4 个）- 同一供应商的多入口容灾
	// 主地址（APIURL）失败且属可切换错误时，同一请求内按序改试备用地址；
	// 全部失败才算该供应商一次失败。仅 claude/codex/custom 转发路径生效
	FallbackAPIURLs []string `json:"fallbackApiUrls,omitempty"`

	// 模型白名单 - Provider 原生支持的模型名
	// 使用 map 实现 O(1) 查找，向后兼容（omitempty）
	SupportedModels map[string]bool `json:"supportedModels,omitempty"`

	// 模型映射 - 外部模型名 -> Provider 内部模型名
	// 支持精确匹配和通配符（如 "claude-*" -> "anthropic/claude-*"）
	ModelMapping map[string]string `json:"modelMapping,omitempty"`

	// 优先级分组 - 数字越小优先级越高（1-10，默认 1）
	// 使用 omitempty 确保零值不序列化，向后兼容
	Level int `json:"level,omitempty"`

	// ========== 可用性监控字段（新增 v0.5.0） ==========

	// 可用性监控开关 - 在可用性页面配置
	// 启用后才会执行后台健康检查
	AvailabilityMonitorEnabled bool `json:"availabilityMonitorEnabled,omitempty"`

	// 连通性自动拉黑开关 - 在 Provider 编辑页面配置
	// 前置条件：AvailabilityMonitorEnabled 必须为 true
	// 启用后，健康检查连续失败达到阈值时向拉黑服务上报失败计数，
	// 由拉黑服务按其自身失败阈值决定何时真正拉黑
	ConnectivityAutoBlacklist bool `json:"connectivityAutoBlacklist,omitempty"`

	// 可用性高级配置 - 可选，在可用性页面的"高级配置"中设置
	AvailabilityConfig *AvailabilityConfig `json:"availabilityConfig,omitempty"`

	// 认证方式 - bearer / x-api-key / 自定义 Header 名
	// 空值时使用平台默认（claude: x-api-key, codex: bearer）
	ConnectivityAuthType string `json:"connectivityAuthType,omitempty"`

	// 上游协议类型 - anthropic / openai_chat / auto
	// anthropic: 上游使用 Anthropic Messages API（默认）
	// openai_chat: 上游使用 OpenAI Chat Completions API，自动转换请求/响应格式
	// auto: 根据 APIEndpoint 自动检测（包含 /chat/completions 则为 openai_chat）
	UpstreamProtocol string `json:"upstreamProtocol,omitempty"`

	// 跳过上游 TLS 证书验证 - 仅对该供应商生效（自签名证书/企业代理场景）
	// 默认 false；开启后该供应商的转发与健康/连通性探测都不再校验证书，存在中间人风险
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`

	// 请求清理开关 - 启用后转发前移除非标准字段和不支持的请求头
	// 解决 LiteLLM 等中转服务的 "Extra inputs are not permitted" 兼容性问题
	RequestSanitizeEnabled bool            `json:"requestSanitizeEnabled,omitempty"`
	SanitizeConfig         *SanitizeConfig `json:"sanitizeConfig,omitempty"`

	// ========== 旧字段（已废弃，仅用于读取迁移） ==========
	// 这些字段在保存时不再写入，但读取时会自动迁移到新字段

	// [已废弃] 连通性检测开关 - 迁移到 AvailabilityMonitorEnabled
	ConnectivityCheck bool `json:"connectivityCheck,omitempty"`

	// [已废弃] 连通性检测模型 - 迁移到 AvailabilityConfig.TestModel
	ConnectivityTestModel string `json:"connectivityTestModel,omitempty"`

	// [已废弃] 连通性检测端点 - 迁移到 AvailabilityConfig.TestEndpoint
	ConnectivityTestEndpoint string `json:"connectivityTestEndpoint,omitempty"`

	// 内部字段：配置验证错误（不持久化）
	configErrors []string `json:"-"`
}

type providerEnvelope struct {
	Providers []Provider `json:"providers"`
}

type ProviderService struct {
	mu sync.Mutex
}

var safeCustomToolIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func validateCustomToolID(toolID string) error {
	if !safeCustomToolIDPattern.MatchString(toolID) {
		return fmt.Errorf("invalid custom tool id: %s", toolID)
	}
	return nil
}

func NewProviderService() *ProviderService {
	return &ProviderService{}
}

func (ps *ProviderService) Start() error { return nil }
func (ps *ProviderService) Stop() error  { return nil }

func providerFilePath(kind string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".code-switch")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	var filename string
	switch strings.ToLower(kind) {
	case "claude", "claude-code", "claude_code":
		filename = "claude-code.json"
	case "codex":
		filename = "codex.json"
	default:
		// 支持自定义 CLI 工具的供应商存储：custom:{tool-id}
		if strings.HasPrefix(kind, "custom:") {
			toolId := strings.TrimPrefix(kind, "custom:")
			if err := validateCustomToolID(toolId); err != nil {
				return "", err
			}
			// 存储在 providers 子目录下
			providersDir := filepath.Join(dir, "providers")
			if err := os.MkdirAll(providersDir, 0o755); err != nil {
				return "", err
			}
			return filepath.Join(providersDir, toolId+".json"), nil
		}
		return "", fmt.Errorf("unknown provider type: %s", kind)
	}
	return filepath.Join(dir, filename), nil
}

func (ps *ProviderService) SaveProviders(kind string, providers []Provider) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.saveProvidersLocked(kind, providers)
}

// mutateProviders 在锁内完成"加载→修改→保存"的整段读改写。
// 供跨调用修改 provider 字段的场景使用(如健康检查开关),
// 避免调用方拆分 Load/Save 导致并发保存相互覆盖丢失更新。
func (ps *ProviderService) mutateProviders(kind string, mutate func(providers []Provider) ([]Provider, error)) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	providers, err := ps.loadProvidersRaw(kind)
	if err != nil {
		return err
	}
	// 原样读取不含旧字段迁移,而 saveProvidersLocked 会清除旧字段;
	// 必须先迁移再修改,否则旧配置里的连通性设置会在保存时丢失
	for i := range providers {
		providers[i].migrateFromLegacy()
	}
	updated, err := mutate(providers)
	if err != nil {
		return err
	}
	return ps.saveProvidersLocked(kind, updated)
}

// loadProvidersRaw 原样读取配置文件（不迁移、不保存）
// 用于内部需要读取现有配置但不触发迁移的场景（如名称校验）
func (ps *ProviderService) loadProvidersRaw(kind string) ([]Provider, error) {
	path, err := providerFilePath(kind)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var envelope providerEnvelope
	if len(data) == 0 {
		return []Provider{}, nil
	}

	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}

	return envelope.Providers, nil
}

// saveProvidersLocked 内部保存方法，调用方必须已持有锁
func (ps *ProviderService) saveProvidersLocked(kind string, providers []Provider) error {
	path, err := providerFilePath(kind)
	if err != nil {
		return err
	}

	// 加载现有配置，用于检查 name 是否被修改
	// 使用原样读取，避免触发迁移导致死锁
	existingProviders, err := ps.loadProvidersRaw(kind)
	if err != nil {
		return err
	}
	nameByID := make(map[int64]string, len(existingProviders))
	for _, p := range existingProviders {
		nameByID[p.ID] = p.Name
	}

	// 解析 platform(alias 校验需要)。失败时跳过 alias 校验(比如 gemini/unknown kind)
	aliasPlatform, aliasErr := resolvePlatform(kind)

	// 验证每个 provider 的配置，并清除旧字段
	validationErrors := make([]string, 0)
	for i := range providers {
		p := &providers[i]

		// 规则：name 不可修改（走独立 RenameProvider 路径,SaveProviders 只允许既有 name）
		if oldName, ok := nameByID[p.ID]; ok && oldName != p.Name {
			return fmt.Errorf("provider id %d 的 name 不可修改(请使用 RenameProvider)", p.ID)
		}

		// 规则:名字不得占用其他 provider 的 48h 活动 alias
		// 防止 "A→B 后新建同名 A" 被 alias resolver 静默归并到 B 的历史里
		if aliasErr == nil {
			if err := checkNameNotOccupiedByAlias(aliasPlatform, p.ID, p.Name); err != nil {
				return err
			}
		}

		// 验证模型配置
		if errs := p.ValidateConfiguration(); len(errs) > 0 {
			for _, errMsg := range errs {
				validationErrors = append(validationErrors, fmt.Sprintf("[%s] %s", p.Name, errMsg))
			}
		}

		// 清除旧连通性字段，确保保存时不再写入
		p.clearLegacyFields()
	}

	// 如果有验证错误，返回汇总错误
	if len(validationErrors) > 0 {
		return fmt.Errorf("配置验证失败：\n  - %s", strings.Join(validationErrors, "\n  - "))
	}

	data, err := json.MarshalIndent(providerEnvelope{Providers: providers}, "", "  ")
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (ps *ProviderService) LoadProviders(kind string) ([]Provider, error) {
	path, err := providerFilePath(kind)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var envelope providerEnvelope
	if len(data) == 0 {
		return []Provider{}, nil
	}

	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}

	// 执行字段迁移：将旧字段值迁移到新字段
	migrated := false
	for i := range envelope.Providers {
		if envelope.Providers[i].migrateFromLegacy() {
			migrated = true
		}
	}

	// 如果有迁移，记录日志并持久化到磁盘
	if migrated {
		fmt.Printf("[ProviderService] 已从旧配置迁移可用性字段 (kind=%s)\n", kind)
		// 锁内重新读取+迁移+保存：锁外读到的快照可能已被并发保存覆盖，
		// 直接回写会静默丢弃期间的用户修改（新增供应商消失、字段回滚）
		ps.mu.Lock()
		fresh, freshErr := ps.loadProvidersRaw(kind)
		if freshErr == nil {
			stillMigrated := false
			for i := range fresh {
				if fresh[i].migrateFromLegacy() {
					stillMigrated = true
				}
			}
			// 若并发保存已清除旧字段则无需再写盘，直接返回最新快照
			if stillMigrated {
				if saveErr := ps.saveProvidersLocked(kind, fresh); saveErr != nil {
					log.Printf("[ProviderService] 迁移后写入失败: %v\n", saveErr)
				} else {
					fmt.Printf("[ProviderService] 迁移后的配置已保存到磁盘 (kind=%s)\n", kind)
				}
			}
			envelope.Providers = fresh
		} else {
			log.Printf("[ProviderService] 迁移后重新加载失败: %v\n", freshErr)
		}
		ps.mu.Unlock()
	}

	return envelope.Providers, nil
}

// loadProvidersNoLock 内部加载方法，在持有锁的情况下调用（避免递归加锁）
// 执行配置加载和迁移，如有迁移则直接保存（不再加锁）
// 仅在已持有 ps.mu 锁的上下文中调用（如 DuplicateProvider）
func (ps *ProviderService) loadProvidersNoLock(kind string) ([]Provider, error) {
	path, err := providerFilePath(kind)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var envelope providerEnvelope
	if len(data) == 0 {
		return []Provider{}, nil
	}

	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}

	// 执行字段迁移（但不保存，避免在持锁时再次加锁）
	migrated := false
	for i := range envelope.Providers {
		if envelope.Providers[i].migrateFromLegacy() {
			migrated = true
		}
	}

	if migrated {
		fmt.Printf("[ProviderService] 已从旧配置迁移可用性字段 (kind=%s, 锁内模式)\n", kind)
		// 在锁内模式下，直接保存而不再加锁
		if err := ps.saveProvidersLocked(kind, envelope.Providers); err != nil {
			log.Printf("[ProviderService] 锁内迁移保存失败: %v\n", err)
		}
	}

	return envelope.Providers, nil
}

// migrateFromLegacy 将旧连通性字段迁移到新可用性字段
// 返回 true 表示发生了迁移
func (p *Provider) migrateFromLegacy() bool {
	migrated := false

	// 迁移 ConnectivityCheck -> AvailabilityMonitorEnabled
	// 仅当新字段未设置（false）且旧字段已设置（true）时迁移
	if p.ConnectivityCheck && !p.AvailabilityMonitorEnabled {
		p.AvailabilityMonitorEnabled = true
		migrated = true
	}

	// 迁移测试模型和端点到 AvailabilityConfig
	if p.ConnectivityTestModel != "" || p.ConnectivityTestEndpoint != "" {
		if p.AvailabilityConfig == nil {
			p.AvailabilityConfig = &AvailabilityConfig{}
		}
		// 仅当新字段为空时才从旧字段迁移
		if p.AvailabilityConfig.TestModel == "" && p.ConnectivityTestModel != "" {
			p.AvailabilityConfig.TestModel = p.ConnectivityTestModel
			migrated = true
		}
		if p.AvailabilityConfig.TestEndpoint == "" && p.ConnectivityTestEndpoint != "" {
			p.AvailabilityConfig.TestEndpoint = p.ConnectivityTestEndpoint
			migrated = true
		}
	}

	return migrated
}

// clearLegacyFields 清除旧字段值，使其在序列化时被 omitempty 跳过
func (p *Provider) clearLegacyFields() {
	p.ConnectivityCheck = false
	p.ConnectivityTestModel = ""
	p.ConnectivityTestEndpoint = ""
	// 注意：ConnectivityAuthType 现在是活跃字段，不再清除
}

// uniqueProviderName 保证生成的供应商名字在现有列表中唯一。
// 名字冲突时追加序号（"X (副本) 2"）：黑名单、用量统计与别名迁移都以 name 为键，
// 重名会让两个供应商的状态互相串扰。
func uniqueProviderName(providers []Provider, candidate string) string {
	taken := make(map[string]bool, len(providers))
	for _, p := range providers {
		taken[strings.ToLower(strings.TrimSpace(p.Name))] = true
	}

	name := candidate
	for seq := 2; taken[strings.ToLower(strings.TrimSpace(name))]; seq++ {
		name = fmt.Sprintf("%s %d", candidate, seq)
	}
	return name
}

// DuplicateProvider 复制供应商配置，生成新的副本
// 返回新创建的 Provider 对象
func (ps *ProviderService) DuplicateProvider(kind string, sourceID int64) (*Provider, error) {
	// 1. 先加锁，避免并发修改
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// 2. 加载现有配置（在锁内完成，确保数据一致性）
	// 注意：LoadProviders 内部可能触发迁移保存，会再次尝试加锁导致死锁
	// 因此使用不加锁的内部加载逻辑
	providers, err := ps.loadProvidersNoLock(kind)
	if err != nil {
		return nil, fmt.Errorf("加载供应商配置失败: %w", err)
	}

	// 3. 查找源供应商
	var source *Provider
	for i := range providers {
		if providers[i].ID == sourceID {
			source = &providers[i]
			break
		}
	}
	if source == nil {
		return nil, fmt.Errorf("未找到 ID 为 %d 的供应商", sourceID)
	}

	// 4. 生成新 ID（当前最大 ID + 1）
	maxID := int64(0)
	for _, p := range providers {
		if p.ID > maxID {
			maxID = p.ID
		}
	}
	newID := maxID + 1

	// 5. 克隆配置（深拷贝）
	// 名字必须在现有列表里唯一：黑名单、用量统计、别名迁移全部以 name 为键，
	// 同名供应商会互相串扰（一个被拉黑，另一个也被跳过；用量记到同一条）
	cloned := &Provider{
		ID:                   newID,
		Name:                 uniqueProviderName(providers, source.Name+" (副本)"),
		APIURL:               source.APIURL,
		APIKey:               source.APIKey,
		Site:                 source.Site,
		Icon:                 source.Icon,
		Tint:                 source.Tint,
		Accent:               source.Accent,
		Enabled:              false, // 默认禁用，避免与源供应商冲突
		Level:                source.Level,
		APIEndpoint:          source.APIEndpoint,          // 复制端点配置
		UpstreamProtocol:     source.UpstreamProtocol,     // 复制上游协议配置
		ConnectivityAuthType: source.ConnectivityAuthType, // 复制认证方式
		InsecureSkipVerify:   source.InsecureSkipVerify,   // 复制 TLS 跳验开关
		// 请求清理配置
		RequestSanitizeEnabled: source.RequestSanitizeEnabled,
		// 可用性监控配置
		AvailabilityMonitorEnabled: source.AvailabilityMonitorEnabled,
		ConnectivityAutoBlacklist:  false, // 副本默认关闭自动拉黑
	}

	// 深拷贝 SanitizeConfig（指针三态列表逐个复制，避免副本与源共享底层数组）
	if source.SanitizeConfig != nil {
		cloned.SanitizeConfig = &SanitizeConfig{
			BlockedBodyFields: cloneStringListPtr(source.SanitizeConfig.BlockedBodyFields),
			BlockedHeaders:    cloneStringListPtr(source.SanitizeConfig.BlockedHeaders),
			BlockedBetaValues: cloneStringListPtr(source.SanitizeConfig.BlockedBetaValues),
		}
	}

	// 6. 深拷贝 map（避免共享引用）
	if source.SupportedModels != nil {
		cloned.SupportedModels = make(map[string]bool, len(source.SupportedModels))
		for k, v := range source.SupportedModels {
			cloned.SupportedModels[k] = v
		}
	}

	// 深拷贝 AvailabilityConfig
	if source.AvailabilityConfig != nil {
		cloned.AvailabilityConfig = &AvailabilityConfig{
			TestModel:    source.AvailabilityConfig.TestModel,
			TestEndpoint: source.AvailabilityConfig.TestEndpoint,
			Timeout:      source.AvailabilityConfig.Timeout,
		}
	}

	if source.ModelMapping != nil {
		cloned.ModelMapping = make(map[string]string, len(source.ModelMapping))
		for k, v := range source.ModelMapping {
			cloned.ModelMapping[k] = v
		}
	}

	if len(source.FallbackAPIURLs) > 0 {
		cloned.FallbackAPIURLs = append([]string(nil), source.FallbackAPIURLs...)
	}

	// 7. 添加到列表并保存（使用内部方法避免死锁）
	providers = append(providers, *cloned)
	if err := ps.saveProvidersLocked(kind, providers); err != nil {
		return nil, fmt.Errorf("保存副本失败: %w", err)
	}

	return cloned, nil
}

// modelInWhitelist 检查模型是否命中白名单（精确或通配符）。
// 值为 false 的条目视为未声明，精确与通配符路径都不算命中。
func modelInWhitelist(supported map[string]bool, modelName string) bool {
	if supported[modelName] {
		return true
	}
	for pattern, allowed := range supported {
		if allowed && matchWildcard(pattern, modelName) {
			return true
		}
	}
	return false
}

// modelSupportedBy 判断白名单/映射组合是否支持指定模型。
// Provider（claude/codex）与 GeminiProvider 共用这一份逻辑。
func modelSupportedBy(supported map[string]bool, mapping map[string]string, modelName string) bool {
	// 向后兼容：如果未配置白名单和映射，假设支持所有模型
	if len(supported) == 0 && len(mapping) == 0 {
		return true
	}

	// 场景 A：原生支持该模型（精确或通配符）
	if modelInWhitelist(supported, modelName) {
		return true
	}

	// 场景 B：通过映射支持该模型（精确或通配符）
	if mapping != nil {
		if _, exists := mapping[modelName]; exists {
			return true
		}
		for pattern := range mapping {
			if matchWildcard(pattern, modelName) {
				return true
			}
		}
	}

	// 场景 C：不支持
	return false
}

// effectiveModelFor 返回映射后的模型名；无命中映射时原样返回。
// 精确映射优先；多条通配符同时命中时按"字面量最长优先，等长按字典序"
// 确定性取胜——Go map 迭代序随机，不定序会让同一份配置在不同进程里
// 把同一个请求改写成不同模型。
func effectiveModelFor(mapping map[string]string, requestedModel string) string {
	if len(mapping) == 0 {
		return requestedModel
	}

	// 优先查找精确映射
	if mappedModel, exists := mapping[requestedModel]; exists {
		return mappedModel
	}

	// 通配符映射：收集所有命中项后确定性选择
	bestPattern := ""
	bestLiteral := -1
	for pattern := range mapping {
		if !matchWildcard(pattern, requestedModel) {
			continue
		}
		literal := len(strings.ReplaceAll(pattern, "*", ""))
		if literal > bestLiteral || (literal == bestLiteral && pattern < bestPattern) {
			bestPattern = pattern
			bestLiteral = literal
		}
	}
	if bestPattern == "" {
		return requestedModel
	}
	return applyWildcardMapping(bestPattern, mapping[bestPattern], requestedModel)
}

// IsModelSupported 检查 provider 是否支持指定的模型
// 支持条件：1) 模型在 SupportedModels 中（精确或通配符匹配）
//  2. 模型在 ModelMapping 的 key 中（精确或通配符匹配）
func (p *Provider) IsModelSupported(modelName string) bool {
	return modelSupportedBy(p.SupportedModels, p.ModelMapping, modelName)
}

// GetEffectiveModel 获取实际应该使用的模型名
// 如果存在映射（精确或通配符），返回映射后的模型名；否则返回原模型名
func (p *Provider) GetEffectiveModel(requestedModel string) string {
	return effectiveModelFor(p.ModelMapping, requestedModel)
}

// GetEffectiveEndpoint 获取有效的 API 端点
// 优先使用用户配置的端点，否则使用平台默认
func (p *Provider) GetEffectiveEndpoint(defaultEndpoint string) string {
	ep := strings.TrimSpace(p.APIEndpoint)
	if ep == "" {
		return defaultEndpoint
	}

	// 校验：必须是相对路径，不能是完整 URL
	if strings.HasPrefix(ep, "http://") || strings.HasPrefix(ep, "https://") {
		log.Printf("[Provider] 警告: apiEndpoint 应该是相对路径（如 /v1/chat/completions），而非完整 URL: %s，使用默认端点", ep)
		return defaultEndpoint
	}

	// 确保以 / 开头
	if !strings.HasPrefix(ep, "/") {
		ep = "/" + ep
	}

	return ep
}

// UpstreamProtocolType 上游协议类型
type UpstreamProtocolType string

const (
	// UpstreamProtocolAnthropic Anthropic Messages API（默认）
	UpstreamProtocolAnthropic UpstreamProtocolType = "anthropic"
	// UpstreamProtocolOpenAIChat OpenAI Chat Completions API
	UpstreamProtocolOpenAIChat UpstreamProtocolType = "openai_chat"
	// UpstreamProtocolAuto 自动检测
	UpstreamProtocolAuto UpstreamProtocolType = "auto"
)

// GetUpstreamProtocol 获取上游协议类型
// 空值或无效值默认返回 anthropic
func (p *Provider) GetUpstreamProtocol() UpstreamProtocolType {
	protocol := strings.TrimSpace(strings.ToLower(p.UpstreamProtocol))
	switch protocol {
	case "openai_chat", "openai-chat", "openai":
		return UpstreamProtocolOpenAIChat
	case "auto":
		return UpstreamProtocolAuto
	default:
		return UpstreamProtocolAnthropic
	}
}

// DetectUpstreamProtocol 根据端点自动检测上游协议
// 用于 auto 模式的启发式判断
func DetectUpstreamProtocol(endpoint string) UpstreamProtocolType {
	ep := strings.ToLower(endpoint)
	// 检测 OpenAI Chat Completions 端点
	if strings.Contains(ep, "/chat/completions") {
		return UpstreamProtocolOpenAIChat
	}
	// 默认 Anthropic
	return UpstreamProtocolAnthropic
}

// ResolveUpstreamProtocol 解析最终的上游协议
// 如果是 auto 模式，根据端点自动检测
func (p *Provider) ResolveUpstreamProtocol(effectiveEndpoint string) UpstreamProtocolType {
	protocol := p.GetUpstreamProtocol()
	if protocol == UpstreamProtocolAuto {
		return DetectUpstreamProtocol(effectiveEndpoint)
	}
	return protocol
}

// validateModelConfig 校验白名单/映射组合，返回错误列表（空表示通过）。
// Provider 与 GeminiProvider 的 ValidateConfiguration 共用。
func validateModelConfig(supported map[string]bool, mapping map[string]string) []string {
	errors := make([]string, 0)

	for externalModel, internalModel := range mapping {
		// 空目标会把请求模型改写成空串，上游必拒，直接判为配置错误
		if strings.TrimSpace(internalModel) == "" {
			errors = append(errors, fmt.Sprintf(
				"模型映射无效：'%s' 的目标模型为空", externalModel))
			continue
		}

		// 规则 1：ModelMapping 的 value 必须在 SupportedModels 中
		// 仅当白名单有实际内容时才校验（空白名单不限制映射目标）
		if len(supported) == 0 {
			continue
		}

		// 通配符映射暂不静态验证（需要具体请求才能展开；
		// Gemini 转发路径会对展开后的 effective model 再做白名单校验）
		if strings.Contains(internalModel, "*") {
			continue
		}

		if !modelInWhitelist(supported, internalModel) {
			errors = append(errors, fmt.Sprintf(
				"模型映射无效：'%s' -> '%s'，目标模型 '%s' 不在 supportedModels 中",
				externalModel, internalModel, internalModel,
			))
		}
	}

	// 允许仅配置 modelMapping（无 supportedModels 时不阻塞保存）
	// 用户可能只想映射模型名，不需要白名单过滤

	return errors
}

// ValidateConfiguration 验证 provider 的模型配置
// 返回验证错误列表（空则表示验证通过）
func (p *Provider) ValidateConfiguration() []string {
	errors := validateModelConfig(p.SupportedModels, p.ModelMapping)
	errors = append(errors, validateFallbackURLs(p.FallbackAPIURLs)...)
	p.configErrors = errors
	return errors
}

// matchWildcard 通配符匹配函数
// 支持 * 通配符，如 "claude-*" 匹配 "claude-sonnet-4"
func matchWildcard(pattern, text string) bool {
	// 如果没有通配符，使用精确匹配
	if !strings.Contains(pattern, "*") {
		return pattern == text
	}

	// 简化实现：只支持单个 * 通配符
	parts := strings.Split(pattern, "*")
	if len(parts) == 2 {
		// 前缀 + * 或 * + 后缀
		prefix, suffix := parts[0], parts[1]
		// * 匹配 0 个及以上字符：text 长度不足以同时容纳前后缀时不可能命中。
		// 漏掉该检查时 "gemini-*-pro" 会错误匹配 "gemini-pro"（前后缀在 text
		// 中重叠），随后 applyWildcardMapping 的切片会越界 panic
		if len(text) < len(prefix)+len(suffix) {
			return false
		}
		return strings.HasPrefix(text, prefix) && strings.HasSuffix(text, suffix)
	}

	// 多个 * 的情况（更复杂，暂不支持）
	return false
}

// applyWildcardMapping 应用通配符映射
// 将 pattern 中的 * 匹配部分替换到 replacement 的 * 位置
// 示例: pattern="claude-*", replacement="anthropic/claude-*", input="claude-sonnet-4"
//
//	输出: "anthropic/claude-sonnet-4"
func applyWildcardMapping(pattern, replacement, input string) string {
	// 如果 pattern 或 replacement 没有通配符，直接返回 replacement
	if !strings.Contains(pattern, "*") || !strings.Contains(replacement, "*") {
		return replacement
	}

	// 提取通配符匹配的部分
	parts := strings.Split(pattern, "*")
	if len(parts) != 2 {
		return replacement // 不支持多个通配符
	}

	prefix, suffix := parts[0], parts[1]

	// 验证 input 确实匹配 pattern（含长度检查，防止重叠切片越界）
	if len(input) < len(prefix)+len(suffix) ||
		!strings.HasPrefix(input, prefix) || !strings.HasSuffix(input, suffix) {
		return replacement
	}

	// 提取中间部分
	wildcardPart := input[len(prefix) : len(input)-len(suffix)]

	// 替换 replacement 中的 *
	return strings.Replace(replacement, "*", wildcardPart, 1)
}
