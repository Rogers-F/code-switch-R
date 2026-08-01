package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/pelletier/go-toml/v2"
)

// CustomCliTool 自定义 CLI 工具配置
type CustomCliTool struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	ConfigFiles    []ConfigFile     `json:"configFiles"`
	ProxyInjection []ProxyInjection `json:"proxyInjection,omitempty"`
}

// ConfigFile 配置文件信息
type ConfigFile struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Path      string `json:"path"`
	Format    string `json:"format"` // json | toml | env
	IsPrimary bool   `json:"isPrimary,omitempty"`
}

// ProxyInjection 代理注入配置
type ProxyInjection struct {
	TargetFileID   string `json:"targetFileId"`
	BaseUrlField   string `json:"baseUrlField"`
	AuthTokenField string `json:"authTokenField,omitempty"`
	// SeedFields 启用代理时按声明式模式补齐的字段（预设用，如 opencode 的
	// provider npm/models）。只在缺失/可安全填充时写入，详见 applySeedField
	SeedFields []SeedField `json:"seedFields,omitempty"`
}

// SeedField 声明式 seed 规则
type SeedField struct {
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
	// Mode 冲突语义：
	//   exact   缺失→写入；等值→保留；异值→报错（如 npm 包名）
	//   fillMap 缺失或空对象→写入；非空对象→保留；非对象→报错（如 models 表）
	Mode string `json:"mode"`
}

// CustomCliProxyStatus 代理状态
type CustomCliProxyStatus struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"baseUrl"`
}

// customCliToolMeta 服务端私有元数据：不进 CustomCliTool，Create/Update 无法
// 读写，防止前端编辑丢失或伪造
type customCliToolMeta struct {
	// CreatedFiles 启用代理时由本应用从零创建的配置文件：路径 → 写入后内容
	// 的 sha256。禁用/删除时哈希未变才允许整文件删除，被用户改过则只做
	// 条件字段清理，保留用户内容
	CreatedFiles map[string]string `json:"createdFiles,omitempty"`
}

// customCliStore 存储结构
type customCliStore struct {
	Tools []CustomCliTool `json:"tools"`
	// ToolMeta 按工具 ID 的服务端私有元数据
	ToolMeta map[string]*customCliToolMeta `json:"toolMeta,omitempty"`
}

// CustomCliService 自定义 CLI 工具服务
type CustomCliService struct {
	mu          sync.RWMutex
	relayAddr   string
	modelPolicy *DefaultModelPolicy
}

// NewCustomCliService 创建服务实例。modelPolicy 供内置预设动态解析默认模型，
// 可为 nil（测试/降级时用静态兜底常量）
func NewCustomCliService(relayAddr string, modelPolicy *DefaultModelPolicy) *CustomCliService {
	return &CustomCliService{relayAddr: relayAddr, modelPolicy: modelPolicy}
}

// ========== 工具 CRUD ==========

// ListTools 获取所有自定义 CLI 工具
func (s *CustomCliService) ListTools() ([]CustomCliTool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	store, err := s.loadStore()
	if err != nil {
		return nil, err
	}
	return store.Tools, nil
}

// GetTool 获取单个工具
func (s *CustomCliService) GetTool(id string) (*CustomCliTool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	store, err := s.loadStore()
	if err != nil {
		return nil, err
	}

	for i := range store.Tools {
		if store.Tools[i].ID == id {
			return &store.Tools[i], nil
		}
	}
	return nil, fmt.Errorf("未找到 ID 为 %s 的工具", id)
}

// CreateTool 创建新工具
func (s *CustomCliService) CreateTool(tool CustomCliTool) (*CustomCliTool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 验证必填字段
	if tool.Name == "" {
		return nil, errors.New("工具名称不能为空")
	}
	if len(tool.ConfigFiles) == 0 {
		return nil, errors.New("至少需要一个配置文件")
	}

	// 生成 ID
	tool.ID = uuid.New().String()

	// 为配置文件生成 ID（如果未设置）
	for i := range tool.ConfigFiles {
		if tool.ConfigFiles[i].ID == "" {
			tool.ConfigFiles[i].ID = fmt.Sprintf("file-%d", i+1)
		}
	}

	// 确保至少有一个主配置文件
	hasPrimary := false
	for _, f := range tool.ConfigFiles {
		if f.IsPrimary {
			hasPrimary = true
			break
		}
	}
	if !hasPrimary && len(tool.ConfigFiles) > 0 {
		tool.ConfigFiles[0].IsPrimary = true
	}

	if err := s.validateProxyInjections(&tool); err != nil {
		return nil, err
	}

	// 加载并追加
	store, err := s.loadStore()
	if err != nil {
		store = &customCliStore{Tools: []CustomCliTool{}}
	}
	store.Tools = append(store.Tools, tool)

	if err := s.saveStore(store); err != nil {
		return nil, err
	}

	// 创建供应商目录
	if err := s.ensureProvidersDir(); err != nil {
		return nil, err
	}

	return &tool, nil
}

// UpdateTool 更新工具
func (s *CustomCliService) UpdateTool(id string, tool CustomCliTool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.validateProxyInjections(&tool); err != nil {
		return err
	}

	store, err := s.loadStore()
	if err != nil {
		return err
	}

	found := false
	for i := range store.Tools {
		if store.Tools[i].ID == id {
			old := store.Tools[i]
			// 代理已启用时不允许改注入目标/规则：直接覆盖会让旧配置文件
			// 与旧备份失去恢复入口（禁用/删除只按新定义遍历）。
			// 仅当注入相关定义变化且旧目标仍处于注入态时拒绝；
			// 状态探测失败按未知处理，同样中止（fail-closed）
			if s.injectionDefinitionChanged(&old, &tool) {
				active, err := s.anyInjectionActiveLocked(store, &old)
				if err != nil {
					return fmt.Errorf("无法确认代理注入状态，已中止修改: %w", err)
				}
				if active {
					return errors.New("代理注入生效期间不能修改注入目标或规则，请先禁用代理再编辑")
				}
			}
			tool.ID = id // 保持 ID 不变
			store.Tools[i] = tool
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("未找到 ID 为 %s 的工具", id)
	}

	return s.saveStore(store)
}

// injectionDefinitionChanged 注入相关定义（注入规则 + 被注入文件的路径/格式）
// 是否发生变化；只改名称等无关字段不受启用态限制
func (s *CustomCliService) injectionDefinitionChanged(old, updated *CustomCliTool) bool {
	if !reflect.DeepEqual(old.ProxyInjection, updated.ProxyInjection) {
		return true
	}
	fileKey := func(t *CustomCliTool) map[string][2]string {
		out := map[string][2]string{}
		targeted := map[string]bool{}
		for _, inj := range t.ProxyInjection {
			targeted[inj.TargetFileID] = true
		}
		for _, cf := range t.ConfigFiles {
			if targeted[cf.ID] {
				out[cf.ID] = [2]string{cf.Path, strings.ToLower(cf.Format)}
			}
		}
		return out
	}
	return !reflect.DeepEqual(fileKey(old), fileKey(updated))
}

// anyInjectionActiveLocked 旧定义的任一目标当前是否处于注入态
// （备份在 / 自建 marker 在 / 注入字段值仍是本代理值）。
// 探测出错返回 error——调用方按未知态中止，不得 fail-open
func (s *CustomCliService) anyInjectionActiveLocked(store *customCliStore, tool *CustomCliTool) (bool, error) {
	meta := store.ToolMeta[tool.ID]
	groups, err := s.groupInjectionsByFile(tool)
	if err != nil {
		return false, err
	}
	for _, g := range groups {
		backupExists, err := statConfigCandidate(g.configPath + ".code-switch.backup")
		if err != nil {
			return false, err
		}
		if backupExists {
			return true, nil
		}
		if meta != nil && meta.CreatedFiles[g.configPath] != "" {
			return true, nil
		}
		content, readErr := os.ReadFile(g.configPath)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				continue
			}
			return false, fmt.Errorf("读取配置失败 (%s): %w", g.file.Label, readErr)
		}
		for _, injection := range g.injections {
			injected, checkErr := s.checkProxyField(content, g.file.Format, injection.BaseUrlField, s.baseURLWithToolPath(tool.ID))
			if checkErr != nil {
				// 解析失败=状态未知，必须 fail-closed（吞掉会让守卫放行覆盖旧定义）
				return false, fmt.Errorf("检查注入状态失败 (%s): %w", g.file.Label, checkErr)
			}
			if injected {
				return true, nil
			}
		}
	}
	return false, nil
}

// DeleteTool 删除工具
func (s *CustomCliService) DeleteTool(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	store, err := s.loadStore()
	if err != nil {
		return err
	}

	// 查找并删除
	found := false
	newTools := make([]CustomCliTool, 0, len(store.Tools))
	var deleted *CustomCliTool
	for i := range store.Tools {
		if store.Tools[i].ID == id {
			found = true
			deleted = &store.Tools[i]
			continue
		}
		newTools = append(newTools, store.Tools[i])
	}

	if !found {
		return fmt.Errorf("未找到 ID 为 %s 的工具", id)
	}

	// 删除前先把注入恢复干净（备份还原/自建文件移除/条件字段清理），
	// 否则外部 CLI 会继续指向已不存在的路由且备份失去恢复入口。
	// 恢复失败中止删除，用户可先手工禁用再删
	if len(deleted.ProxyInjection) > 0 {
		if err := s.restoreProxyTargetsLocked(store, deleted); err != nil {
			return fmt.Errorf("删除前恢复配置失败: %w", err)
		}
	}
	delete(store.ToolMeta, id)

	store.Tools = newTools
	if err := s.saveStore(store); err != nil {
		return err
	}

	// 删除对应的供应商文件
	providersPath, err := s.getProvidersPath(id)
	if err != nil {
		return err
	}
	_ = os.Remove(providersPath) // 忽略错误（文件可能不存在）

	return nil
}

// ========== 代理管理 ==========

// ProxyStatus 获取代理状态
func (s *CustomCliService) ProxyStatus(toolId string) (*CustomCliProxyStatus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tool, err := s.getToolLocked(toolId)
	if err != nil {
		return &CustomCliProxyStatus{Enabled: false, BaseURL: s.baseURLWithToolPath(toolId)}, err
	}

	status := &CustomCliProxyStatus{
		Enabled: false,
		BaseURL: s.baseURLWithToolPath(toolId),
	}

	// 检查所有代理注入配置
	if len(tool.ProxyInjection) == 0 {
		return status, nil
	}

	allEnabled := true
	for _, injection := range tool.ProxyInjection {
		// 找到目标文件
		var targetFile *ConfigFile
		for i := range tool.ConfigFiles {
			if tool.ConfigFiles[i].ID == injection.TargetFileID {
				targetFile = &tool.ConfigFiles[i]
				break
			}
		}
		if targetFile == nil {
			allEnabled = false
			continue
		}

		// 读取并检查配置
		configPath := s.expandPath(targetFile.Path)
		content, err := os.ReadFile(configPath)
		if err != nil {
			allEnabled = false
			continue
		}

		// 检查代理字段是否已设置
		enabled, err := s.checkProxyField(content, targetFile.Format, injection.BaseUrlField, s.baseURLWithToolPath(toolId))
		if err != nil || !enabled {
			allEnabled = false
			continue
		}

		// 校验可选的鉴权字段，避免误判为已启用
		// 向后兼容：同时检查 code-switch-r（新）和 code-switch（旧）两个 token
		if injection.AuthTokenField != "" {
			authOk := false
			for _, token := range []string{"code-switch-r", "code-switch"} {
				authEnabled, err := s.checkProxyField(content, targetFile.Format, injection.AuthTokenField, token)
				if err == nil && authEnabled {
					authOk = true
					break
				}
			}
			if !authOk {
				allEnabled = false
			}
		}
	}

	status.Enabled = allEnabled
	return status, nil
}

// EnableProxy 启用代理
func (s *CustomCliService) EnableProxy(toolId string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	store, err := s.loadStore()
	if err != nil {
		return err
	}
	tool := findToolInStore(store, toolId)
	if tool == nil {
		return fmt.Errorf("未找到 ID 为 %s 的工具", toolId)
	}

	if len(tool.ProxyInjection) == 0 {
		return errors.New("未配置代理注入规则")
	}
	if err := s.validateProxyInjections(tool); err != nil {
		return err
	}

	// 按物理路径聚合：同一文件的全部注入在一棵解析树上完成，
	// 两个 ConfigFile 条目指向同一路径也不会二次解析/互相覆盖
	groups, err := s.groupInjectionsByFile(tool)
	if err != nil {
		return err
	}

	meta := store.ToolMeta[toolId]

	// —— 阶段一：全部目标先在内存中完成解析与应用，任何冲突在此报错，
	// 此时零备份零写入，不会留下部分启用状态 ——
	type plannedWrite struct {
		group   injectionGroup
		data    map[string]interface{} // json/toml 的应用后配置树
		envText string                 // env 的应用后完整内容
		existed bool
	}
	plans := make([]plannedWrite, 0, len(groups))
	for _, g := range groups {
		existed, err := statConfigCandidate(g.configPath)
		if err != nil {
			return err
		}
		if strings.ToLower(g.file.Format) == "env" {
			content, readErr := os.ReadFile(g.configPath)
			if readErr != nil && !os.IsNotExist(readErr) {
				return fmt.Errorf("读取配置文件失败 (%s): %w", g.file.Label, readErr)
			}
			// 行级 upsert 保留注释/export/空行等用户手写内容，
			// 不能走 parseEnvFile+serializeEnvFile 的 map 重建（会整体丢弃）
			updates := map[string]string{}
			for _, injection := range g.injections {
				collectEnvInjection(updates, injection, s.baseURLWithToolPath(toolId))
			}
			plans = append(plans, plannedWrite{group: g, envText: upsertEnvContent(string(content), updates), existed: existed})
			continue
		}
		data, err := s.parseConfigStrict(g.configPath, g.file.Format)
		if err != nil {
			return err
		}
		for _, injection := range g.injections {
			if err := setNestedValueStrict(data, injection.BaseUrlField, s.baseURLWithToolPath(toolId)); err != nil {
				return fmt.Errorf("注入 %s 失败: %w", g.file.Label, err)
			}
			if injection.AuthTokenField != "" {
				if err := setNestedValueStrict(data, injection.AuthTokenField, "code-switch-r"); err != nil {
					return fmt.Errorf("注入 %s 失败: %w", g.file.Label, err)
				}
			}
			for _, sf := range injection.SeedFields {
				if err := applySeedField(data, sf); err != nil {
					return fmt.Errorf("预设字段处理失败 (%s): %w", g.file.Label, err)
				}
			}
		}
		plans = append(plans, plannedWrite{group: g, data: data, existed: existed})
	}

	// —— 阶段二：备份 + 写入 + 所有权记录 ——
	metaChanged := false
	for _, plan := range plans {
		g := plan.group
		markerHash := ""
		if meta != nil {
			markerHash = meta.CreatedFiles[g.configPath]
		}
		if plan.existed {
			if markerHash != "" {
				// 曾由本应用从零创建：内容仍是我们写入的才保持所有权；
				// 被用户改过则立刻放弃 marker（转普通文件语义，禁用走
				// 条件清理），绝不能重认领后在禁用时删掉用户数据
				content, readErr := os.ReadFile(g.configPath)
				if readErr != nil {
					return fmt.Errorf("读取配置文件失败: %w", readErr)
				}
				if sha256Hex(content) != markerHash {
					delete(meta.CreatedFiles, g.configPath)
					markerHash = ""
					metaChanged = true
				}
			}
			if markerHash == "" {
				if err := s.backupIfNeeded(g.configPath, g, toolId); err != nil {
					return err
				}
			}
		}
		if err := EnsureDir(filepath.Dir(g.configPath)); err != nil {
			return err
		}

		var written []byte
		if strings.ToLower(g.file.Format) == "env" {
			if err := AtomicWriteText(g.configPath, plan.envText); err != nil {
				return fmt.Errorf("写入配置失败 (%s): %w", g.file.Label, err)
			}
			written = []byte(plan.envText)
		} else {
			written, err = s.writeConfigTree(g.configPath, g.file.Format, plan.data)
			if err != nil {
				return fmt.Errorf("写入配置失败 (%s): %w", g.file.Label, err)
			}
		}

		if !plan.existed || markerHash != "" {
			// created-marker：从零创建的文件记录内容哈希（未被改动的重复
			// 启用刷新哈希），禁用/删除时未被改动才允许整文件删除
			if meta == nil {
				meta = &customCliToolMeta{}
			}
			if meta.CreatedFiles == nil {
				meta.CreatedFiles = map[string]string{}
			}
			meta.CreatedFiles[g.configPath] = sha256Hex(written)
			metaChanged = true
		}
		// 每个文件写完立即持久化所有权，避免后续文件失败留下无主的新建文件
		if metaChanged {
			if store.ToolMeta == nil {
				store.ToolMeta = map[string]*customCliToolMeta{}
			}
			store.ToolMeta[toolId] = meta
			if err := s.saveStore(store); err != nil {
				return err
			}
			metaChanged = false
		}
	}
	return nil
}

// injectionGroup 同一目标文件（按物理路径归并）的注入集合
type injectionGroup struct {
	file       *ConfigFile
	configPath string // 展开并清理后的绝对路径
	injections []ProxyInjection
}

func (s *CustomCliService) groupInjectionsByFile(tool *CustomCliTool) ([]injectionGroup, error) {
	byPath := map[string]*injectionGroup{}
	order := []string{}
	for _, injection := range tool.ProxyInjection {
		var targetFile *ConfigFile
		for i := range tool.ConfigFiles {
			if tool.ConfigFiles[i].ID == injection.TargetFileID {
				targetFile = &tool.ConfigFiles[i]
				break
			}
		}
		if targetFile == nil {
			return nil, fmt.Errorf("找不到目标文件: %s", injection.TargetFileID)
		}
		absPath, err := filepath.Abs(s.expandPath(targetFile.Path))
		if err != nil {
			return nil, fmt.Errorf("解析配置路径失败 (%s): %w", targetFile.Path, err)
		}
		key := normalizeConfigPathKey(absPath)
		g, ok := byPath[key]
		if !ok {
			g = &injectionGroup{file: targetFile, configPath: absPath}
			byPath[key] = g
			order = append(order, key)
		} else if !strings.EqualFold(g.file.Format, targetFile.Format) {
			return nil, fmt.Errorf("同一路径 %s 被两个不同格式的配置文件条目引用", g.configPath)
		}
		g.injections = append(g.injections, injection)
	}
	groups := make([]injectionGroup, 0, len(order))
	for _, key := range order {
		groups = append(groups, *byPath[key])
	}
	return groups, nil
}

// normalizeConfigPathKey 物理路径归并键：绝对化 + 清理；
// 仅 Windows 做大小写归一（类 Unix 文件系统区分大小写，不能合并）
func normalizeConfigPathKey(absPath string) string {
	cleaned := filepath.Clean(absPath)
	if runtime.GOOS == "windows" {
		return strings.ToLower(cleaned)
	}
	return cleaned
}

// backupIfNeeded 建立/维护 .code-switch.backup 基线。
// 已注入内容绝不能当基线：重复启用时把注入后的文件存为备份，会让"禁用还原"
// 还原出注入态（对自建文件还会挡住 created-marker 的整文件清理）
func (s *CustomCliService) backupIfNeeded(configPath string, g injectionGroup, toolId string) error {
	backupPath := configPath + ".code-switch.backup"
	content, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}
	anyInjected := false
	checkFailed := false
	for _, injection := range g.injections {
		injected, checkErr := s.checkProxyField(content, g.file.Format, injection.BaseUrlField, s.baseURLWithToolPath(toolId))
		if checkErr != nil {
			checkFailed = true
			continue
		}
		if injected {
			anyInjected = true
			break
		}
	}
	if !checkFailed && anyInjected {
		return nil
	}
	// 无基线则建立（含检测失败的未知态：否则禁用后无从还原）；
	// 已有基线时只有确认当前内容是用户原始配置才刷新
	shouldBackup := !FileExists(backupPath) || !checkFailed
	if shouldBackup {
		if err := os.WriteFile(backupPath, content, 0o600); err != nil {
			return fmt.Errorf("创建备份失败: %w", err)
		}
	}
	return nil
}

// parseConfigStrict 严格解析配置：失败即错，绝不静默重置为空
func (s *CustomCliService) parseConfigStrict(configPath, format string) (map[string]interface{}, error) {
	content, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(content) == 0 {
		return map[string]interface{}{}, nil
	}
	var data map[string]interface{}
	switch strings.ToLower(format) {
	case "json":
		if err := json.Unmarshal(content, &data); err != nil {
			if strings.HasSuffix(strings.ToLower(configPath), ".jsonc") {
				return nil, fmt.Errorf("解析 %s 失败：带注释的 JSONC 暂不支持，请移除注释或转存为 .json（原始错误: %v）", configPath, err)
			}
			return nil, fmt.Errorf("解析 %s 失败（为保护原有配置已中止，不会写入任何内容）: %w", configPath, err)
		}
	case "toml":
		if err := toml.Unmarshal(content, &data); err != nil {
			return nil, fmt.Errorf("解析 %s 失败（为保护原有配置已中止，不会写入任何内容）: %w", configPath, err)
		}
	default:
		return nil, fmt.Errorf("不支持的格式: %s", format)
	}
	if data == nil {
		data = map[string]interface{}{}
	}
	return data, nil
}

// writeConfigTree 原子写回配置树，返回实际写入的字节（供 created-marker 记哈希）
func (s *CustomCliService) writeConfigTree(configPath, format string, data map[string]interface{}) ([]byte, error) {
	switch strings.ToLower(format) {
	case "json":
		if err := AtomicWriteJSON(configPath, data); err != nil {
			return nil, err
		}
		written, err := os.ReadFile(configPath)
		return written, err
	case "toml":
		tomlData, err := toml.Marshal(data)
		if err != nil {
			return nil, err
		}
		if err := AtomicWriteBytes(configPath, tomlData); err != nil {
			return nil, err
		}
		return tomlData, nil
	}
	return nil, fmt.Errorf("不支持的格式: %s", format)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func findToolInStore(store *customCliStore, toolId string) *CustomCliTool {
	for i := range store.Tools {
		if store.Tools[i].ID == toolId {
			return &store.Tools[i]
		}
	}
	return nil
}

// DisableProxy 禁用代理
func (s *CustomCliService) DisableProxy(toolId string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	store, err := s.loadStore()
	if err != nil {
		return err
	}
	tool := findToolInStore(store, toolId)
	if tool == nil {
		return fmt.Errorf("未找到 ID 为 %s 的工具", toolId)
	}
	if err := s.restoreProxyTargetsLocked(store, tool); err != nil {
		return err
	}
	return s.saveStore(store)
}

// restoreProxyTargetsLocked 按目标文件恢复注入前状态（调用方持写锁）。
// 三分支：有备份→恢复备份；无备份但文件是本应用从零创建且未被改动→删除
// 整文件；其余→只做条件字段清理（仅删除仍等于本代理注入值/seed 值的字段），
// 保留用户后续添加的内容。任何一步失败即返回错误且不清 marker——
// 调用方（禁用/删除）据此中止，不得留下"记录已删、文件仍指向代理"的状态
func (s *CustomCliService) restoreProxyTargetsLocked(store *customCliStore, tool *CustomCliTool) error {
	meta := store.ToolMeta[tool.ID]
	groups, err := s.groupInjectionsByFile(tool)
	if err != nil {
		return err
	}
	for _, g := range groups {
		configPath := g.configPath
		backupPath := configPath + ".code-switch.backup"

		backupExists, err := statConfigCandidate(backupPath)
		if err != nil {
			return err
		}
		if backupExists {
			if err := RestoreBackup(backupPath, configPath); err != nil {
				return fmt.Errorf("恢复备份失败 (%s): %w", g.file.Label, err)
			}
			if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
				// 备份残留会在下次启用时被误当基线，必须显式失败
				return fmt.Errorf("清理备份失败 (%s): %w", g.file.Label, err)
			}
		} else if meta != nil && meta.CreatedFiles[configPath] != "" {
			content, readErr := os.ReadFile(configPath)
			switch {
			case readErr != nil && os.IsNotExist(readErr):
				// 文件已被外部删除：视为已清理
			case readErr != nil:
				return fmt.Errorf("读取配置失败 (%s): %w", g.file.Label, readErr)
			case sha256Hex(content) == meta.CreatedFiles[configPath]:
				// 从零创建且未被改动：整文件都是本应用产物，直接移除
				if err := os.Remove(configPath); err != nil {
					return fmt.Errorf("移除自建配置失败 (%s): %w", g.file.Label, err)
				}
			default:
				// 被用户改过：只清理仍属于我们的字段
				if err := s.conditionalCleanup(g, tool.ID); err != nil {
					return fmt.Errorf("清理注入字段失败 (%s): %w", g.file.Label, err)
				}
			}
		} else {
			configExists, err := statConfigCandidate(configPath)
			if err != nil {
				return err
			}
			if configExists {
				if err := s.conditionalCleanup(g, tool.ID); err != nil {
					return fmt.Errorf("清理注入字段失败 (%s): %w", g.file.Label, err)
				}
			}
		}
		if meta != nil && meta.CreatedFiles[configPath] != "" {
			delete(meta.CreatedFiles, configPath)
		}
	}
	if meta != nil && len(meta.CreatedFiles) == 0 {
		delete(store.ToolMeta, tool.ID)
	}
	return nil
}

// conditionalCleanup 条件清理：只删除当前取值仍等于本代理注入值/seed 值的
// 字段（用户手改过的字段一律保留），删除后递归裁剪空父对象
func (s *CustomCliService) conditionalCleanup(g injectionGroup, toolId string) error {
	configPath := g.configPath
	format := strings.ToLower(g.file.Format)
	if format == "env" {
		content, err := os.ReadFile(configPath)
		if err != nil {
			return err
		}
		// 行级条件删除：只删值仍等于注入值的 KEY=VALUE 行，
		// 注释/export/用户手改的行原样保留
		expected := map[string]string{}
		for _, injection := range g.injections {
			collectEnvInjection(expected, injection, s.baseURLWithToolPath(toolId))
		}
		return AtomicWriteText(configPath, removeEnvLinesIfEquals(string(content), expected))
	}

	data, err := s.parseConfigStrict(configPath, g.file.Format)
	if err != nil {
		return err
	}
	for _, injection := range g.injections {
		deleteNestedValueIfEquals(data, injection.BaseUrlField, s.baseURLWithToolPath(toolId))
		if injection.AuthTokenField != "" {
			deleteNestedValueIfEquals(data, injection.AuthTokenField, "code-switch-r")
		}
		for _, sf := range injection.SeedFields {
			deleteNestedValueIfEquals(data, sf.Path, sf.Value)
		}
		pruneEmptyObjects(data, injection.BaseUrlField)
		if injection.AuthTokenField != "" {
			pruneEmptyObjects(data, injection.AuthTokenField)
		}
		for _, sf := range injection.SeedFields {
			pruneEmptyObjects(data, sf.Path)
		}
	}
	_, err = s.writeConfigTree(configPath, g.file.Format, data)
	return err
}

// removeEnvLinesIfEquals 行级条件删除：只删 key 命中且值等于期望值的
// KEY=VALUE 行；注释、export、空行与用户手改的行原样保留
func removeEnvLinesIfEquals(content string, expected map[string]string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	hadTrailingNewline := strings.HasSuffix(normalized, "\n")
	normalized = strings.TrimSuffix(normalized, "\n")

	var lines []string
	if normalized != "" {
		lines = strings.Split(normalized, "\n")
	}
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			if idx := strings.Index(trimmed, "="); idx > 0 {
				key := strings.TrimSpace(trimmed[:idx])
				value := strings.TrimSpace(trimmed[idx+1:])
				if want, ok := expected[key]; ok && value == want {
					continue
				}
			}
		}
		kept = append(kept, line)
	}
	out := strings.Join(kept, "\n")
	if out != "" && hadTrailingNewline {
		out += "\n"
	}
	return out
}

// ========== 配置文件读写 ==========

// GetConfigContent 获取配置文件内容
func (s *CustomCliService) GetConfigContent(toolId, fileId string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tool, err := s.getToolLocked(toolId)
	if err != nil {
		return "", err
	}

	var targetFile *ConfigFile
	for i := range tool.ConfigFiles {
		if tool.ConfigFiles[i].ID == fileId {
			targetFile = &tool.ConfigFiles[i]
			break
		}
	}
	if targetFile == nil {
		return "", fmt.Errorf("找不到文件: %s", fileId)
	}

	configPath := s.expandPath(targetFile.Path)
	content, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("配置文件不存在: %s", configPath)
		}
		return "", err
	}

	return string(content), nil
}

// SaveConfigContent 保存配置文件内容
func (s *CustomCliService) SaveConfigContent(toolId, fileId, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tool, err := s.getToolLocked(toolId)
	if err != nil {
		return err
	}

	var targetFile *ConfigFile
	for i := range tool.ConfigFiles {
		if tool.ConfigFiles[i].ID == fileId {
			targetFile = &tool.ConfigFiles[i]
			break
		}
	}
	if targetFile == nil {
		return fmt.Errorf("找不到文件: %s", fileId)
	}

	configPath := s.expandPath(targetFile.Path)

	// 验证格式
	if err := s.validateFormat(content, targetFile.Format); err != nil {
		return fmt.Errorf("格式验证失败: %w", err)
	}

	// 创建备份
	if FileExists(configPath) {
		if _, err := CreateBackup(configPath); err != nil {
			// 备份失败不阻止保存
			fmt.Printf("创建备份失败: %v\n", err)
		}
	}

	// 确保目录存在
	if err := EnsureDir(filepath.Dir(configPath)); err != nil {
		return err
	}

	// 原子写入
	return AtomicWriteText(configPath, content)
}

// GetLockedFields 获取锁定字段列表
func (s *CustomCliService) GetLockedFields(toolId string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tool, err := s.getToolLocked(toolId)
	if err != nil {
		return nil, err
	}

	var locked []string
	for _, injection := range tool.ProxyInjection {
		if injection.BaseUrlField != "" {
			locked = append(locked, injection.BaseUrlField)
		}
		if injection.AuthTokenField != "" {
			locked = append(locked, injection.AuthTokenField)
		}
	}

	return locked, nil
}

// ========== 内部方法 ==========

func (s *CustomCliService) getStorePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".code-switch", "custom-cli.json")
}

func (s *CustomCliService) getProvidersDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".code-switch", "providers")
}

func (s *CustomCliService) getProvidersPath(toolId string) (string, error) {
	if err := validateCustomToolID(toolId); err != nil {
		return "", err
	}
	return filepath.Join(s.getProvidersDir(), toolId+".json"), nil
}

func (s *CustomCliService) ensureProvidersDir() error {
	return EnsureDir(s.getProvidersDir())
}

func (s *CustomCliService) loadStore() (*customCliStore, error) {
	path := s.getStorePath()
	var store customCliStore

	if err := ReadJSONFile(path, &store); err != nil {
		if os.IsNotExist(err) {
			return &customCliStore{Tools: []CustomCliTool{}}, nil
		}
		return nil, err
	}

	return &store, nil
}

func (s *CustomCliService) saveStore(store *customCliStore) error {
	path := s.getStorePath()
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return AtomicWriteJSON(path, store)
}

func (s *CustomCliService) getToolLocked(id string) (*CustomCliTool, error) {
	store, err := s.loadStore()
	if err != nil {
		return nil, err
	}

	for i := range store.Tools {
		if store.Tools[i].ID == id {
			return &store.Tools[i], nil
		}
	}
	return nil, fmt.Errorf("未找到 ID 为 %s 的工具", id)
}

func (s *CustomCliService) baseURL() string {
	addr := strings.TrimSpace(s.relayAddr)
	if addr == "" {
		addr = ":18100"
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	return host
}

// baseURLWithToolPath 返回包含 /custom/{toolId} 路径的完整代理 URL
// 自定义 CLI 工具的路由格式为 /custom/:toolId/v1/messages
func (s *CustomCliService) baseURLWithToolPath(toolId string) string {
	base := s.baseURL()
	// 移除尾部斜杠（如果有）
	base = strings.TrimSuffix(base, "/")
	return base + "/custom/" + toolId
}

func (s *CustomCliService) expandPath(path string) string {
	// 处理 Unix 风格路径 ~/
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	// 处理 Windows 风格路径 ~\
	if strings.HasPrefix(path, "~\\") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	// 处理单独的 ~ (表示家目录)
	if path == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	return path
}

// checkProxyField 检查代理字段是否已正确设置
func (s *CustomCliService) checkProxyField(content []byte, format, fieldPath, expectedValue string) (bool, error) {
	var data map[string]interface{}

	switch strings.ToLower(format) {
	case "json":
		if err := json.Unmarshal(content, &data); err != nil {
			return false, err
		}
	case "toml":
		if err := toml.Unmarshal(content, &data); err != nil {
			return false, err
		}
	case "env":
		envMap := parseEnvFile(string(content))
		// ENV 格式：取字段路径的最后一部分作为键名
		key := fieldPath
		if idx := strings.LastIndex(fieldPath, "."); idx >= 0 {
			key = fieldPath[idx+1:]
		}
		return envMap[key] == expectedValue, nil
	default:
		return false, fmt.Errorf("不支持的格式: %s", format)
	}

	// 检查嵌套字段
	value := getNestedValue(data, fieldPath)
	if str, ok := value.(string); ok {
		return str == expectedValue, nil
	}
	return false, nil
}

// collectEnvInjection 收集 env 注入键值（键取字段路径最后一段）
func collectEnvInjection(updates map[string]string, injection ProxyInjection, baseURL string) {
	baseUrlKey := injection.BaseUrlField
	if idx := strings.LastIndex(baseUrlKey, "."); idx >= 0 {
		baseUrlKey = baseUrlKey[idx+1:]
	}
	updates[baseUrlKey] = baseURL
	if injection.AuthTokenField != "" {
		authKey := injection.AuthTokenField
		if idx := strings.LastIndex(authKey, "."); idx >= 0 {
			authKey = authKey[idx+1:]
		}
		updates[authKey] = "code-switch-r"
	}
}

// validateFormat 验证内容格式
func (s *CustomCliService) validateFormat(content, format string) error {
	switch strings.ToLower(format) {
	case "json":
		var data interface{}
		return json.Unmarshal([]byte(content), &data)
	case "toml":
		var data interface{}
		return toml.Unmarshal([]byte(content), &data)
	case "env":
		// ENV 格式不做严格验证
		return nil
	default:
		return fmt.Errorf("不支持的格式: %s", format)
	}
}

// ========== seed 与校验 ==========

// applySeedField 按声明式模式应用 seed（详见 SeedField.Mode 注释）
func applySeedField(data map[string]interface{}, sf SeedField) error {
	val, exists := getNestedValueEx(data, sf.Path)
	switch sf.Mode {
	case "exact":
		if exists {
			if reflect.DeepEqual(normalizeJSONValue(val), normalizeJSONValue(sf.Value)) {
				return nil
			}
			return fmt.Errorf("字段 %s 已有不同取值（%v），不会覆盖；请手工核对后再启用", sf.Path, val)
		}
		return setNestedValueStrict(data, sf.Path, sf.Value)
	case "fillMap":
		if exists {
			m, ok := val.(map[string]interface{})
			if !ok {
				return fmt.Errorf("字段 %s 已存在但不是对象（%v），请手工核对后再启用", sf.Path, val)
			}
			if len(m) > 0 {
				return nil
			}
		}
		return setNestedValueStrict(data, sf.Path, sf.Value)
	default:
		return fmt.Errorf("未知的 seed 模式: %q", sf.Mode)
	}
}

// validateProxyInjections 注入规则静态校验（Create/Update/Enable 三处共用）：
// seed 模式合法、路径无空段、同一目标文件（按物理路径归并）内全部注入的
// 字段路径无重复或祖先/子孙重叠、env 格式目标不允许携带 seed
func (s *CustomCliService) validateProxyInjections(tool *CustomCliTool) error {
	fileByID := map[string]*ConfigFile{}
	for i := range tool.ConfigFiles {
		fileByID[tool.ConfigFiles[i].ID] = &tool.ConfigFiles[i]
	}
	// 跨注入按物理路径聚合字段路径：后写的祖先叶子会静默覆盖先建的对象，
	// 必须在校验期拦截
	pathsByFile := map[string][]string{}
	for _, injection := range tool.ProxyInjection {
		cf := fileByID[injection.TargetFileID]
		format := ""
		fileKey := injection.TargetFileID
		if cf != nil {
			format = strings.ToLower(cf.Format)
			p := s.expandPath(cf.Path)
			if abs, absErr := filepath.Abs(p); absErr == nil {
				p = abs
			}
			fileKey = normalizeConfigPathKey(p)
		}
		if len(injection.SeedFields) > 0 && format == "env" {
			return fmt.Errorf("env 格式配置不支持预设 seed 字段（目标文件 %s）", injection.TargetFileID)
		}
		paths := []string{}
		if injection.BaseUrlField != "" {
			paths = append(paths, injection.BaseUrlField)
		}
		if injection.AuthTokenField != "" {
			paths = append(paths, injection.AuthTokenField)
		}
		for _, sf := range injection.SeedFields {
			if sf.Mode != "exact" && sf.Mode != "fillMap" {
				return fmt.Errorf("未知的 seed 模式: %q（路径 %s）", sf.Mode, sf.Path)
			}
			if strings.TrimSpace(sf.Path) == "" {
				return errors.New("seed 路径不能为空")
			}
			paths = append(paths, sf.Path)
		}
		for _, p := range paths {
			for _, seg := range strings.Split(p, ".") {
				if strings.TrimSpace(seg) == "" {
					return fmt.Errorf("字段路径 %q 含空段", p)
				}
			}
		}
		pathsByFile[fileKey] = append(pathsByFile[fileKey], paths...)
	}
	for _, paths := range pathsByFile {
		for i := 0; i < len(paths); i++ {
			for j := i + 1; j < len(paths); j++ {
				if nestedPathsOverlap(paths[i], paths[j]) {
					return fmt.Errorf("字段路径 %q 与 %q 重叠", paths[i], paths[j])
				}
			}
		}
	}
	return nil
}

// nestedPathsOverlap 相等或互为祖先/子孙
func nestedPathsOverlap(a, b string) bool {
	return a == b || strings.HasPrefix(b, a+".") || strings.HasPrefix(a, b+".")
}

// ========== 内置预设 ==========

// CustomCliToolPreset 内置预设：前端"从预设开始"入口的预填内容 +
// 目标配置文件的探测状态
type CustomCliToolPreset struct {
	PresetID       string           `json:"presetId"`
	Name           string           `json:"name"`
	ConfigFiles    []ConfigFile     `json:"configFiles"`
	ProxyInjection []ProxyInjection `json:"proxyInjection"`
	// ConfigState 目标配置探测结果: none | json | jsonc | both
	ConfigState string `json:"configState"`
	// ResolvedPath 预填的配置路径（含 ~ 形式，便于跨机展示）
	ResolvedPath string `json:"resolvedPath"`
	// Candidates configState=both 时的可选路径
	Candidates []string `json:"candidates,omitempty"`
}

// ListToolPresets 列出内置预设（当前仅 opencode）
func (s *CustomCliService) ListToolPresets() ([]CustomCliToolPreset, error) {
	preset, err := s.opencodePreset()
	if err != nil {
		return nil, err
	}
	return []CustomCliToolPreset{preset}, nil
}

// opencodePreset 动态构建 opencode 预设。
// 事实依据（官方 config 文档）：全局配置 ~/.config/opencode/opencode.json；
// OPENCODE_CONFIG 环境变量指定的自定义配置在全局之后加载（写它必然生效）；
// json/jsonc 同目录共存的优先级官方未定义（both 态交由用户选择）；
// 配置为合并语义，写全局 provider 块不会被项目配置整体顶掉。
func (s *CustomCliService) opencodePreset() (CustomCliToolPreset, error) {
	resolvedPath, state, candidates, err := s.resolveOpencodeConfigPath()
	if err != nil {
		return CustomCliToolPreset{}, err
	}

	model := FallbackClaudeDefaultModel
	if s.modelPolicy != nil {
		model = s.modelPolicy.ClaudeDefaultModel()
	}

	const fileID = "opencode-config"
	return CustomCliToolPreset{
		PresetID: "opencode",
		Name:     "opencode",
		ConfigFiles: []ConfigFile{{
			ID:        fileID,
			Label:     "opencode.json",
			Path:      resolvedPath,
			Format:    "json",
			IsPrimary: true,
		}},
		ProxyInjection: []ProxyInjection{{
			TargetFileID:   fileID,
			BaseUrlField:   "provider.code-switch-r.options.baseURL",
			AuthTokenField: "provider.code-switch-r.options.apiKey",
			SeedFields: []SeedField{
				{Path: "provider.code-switch-r.npm", Value: "@ai-sdk/anthropic", Mode: "exact"},
				{Path: "provider.code-switch-r.models", Value: map[string]interface{}{
					model: map[string]interface{}{"name": model},
				}, Mode: "fillMap"},
			},
		}},
		ConfigState:  state,
		ResolvedPath: resolvedPath,
		Candidates:   candidates,
	}, nil
}

// resolveOpencodeConfigPath 探测 opencode 配置文件。
// 返回 (预填路径, 状态 none|json|jsonc|both, both 态候选)。
// OPENCODE_CONFIG（非空绝对路径且 .json/.jsonc 后缀）优先；否则探测
// ~/.config/opencode/ 下的 opencode.json 与 opencode.jsonc。
// Stat 出现非"不存在"错误（权限等）直接报错，不猜测状态
func (s *CustomCliService) resolveOpencodeConfigPath() (string, string, []string, error) {
	if override := strings.TrimSpace(os.Getenv("OPENCODE_CONFIG")); override != "" {
		lower := strings.ToLower(override)
		if filepath.IsAbs(override) && (strings.HasSuffix(lower, ".json") || strings.HasSuffix(lower, ".jsonc")) {
			exists, err := statConfigCandidate(override)
			if err != nil {
				return "", "", nil, err
			}
			state := "none"
			if exists {
				state = "json"
				if strings.HasSuffix(lower, ".jsonc") {
					state = "jsonc"
				}
			}
			return override, state, nil, nil
		}
		fmt.Printf("[CustomCLI] OPENCODE_CONFIG 值无效（需绝对路径且 .json/.jsonc 后缀），忽略: %q\n", override)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", nil, err
	}
	dir := filepath.Join(home, ".config", "opencode")
	jsonPath := filepath.Join(dir, "opencode.json")
	jsoncPath := filepath.Join(dir, "opencode.jsonc")
	jsonExists, err := statConfigCandidate(jsonPath)
	if err != nil {
		return "", "", nil, err
	}
	jsoncExists, err := statConfigCandidate(jsoncPath)
	if err != nil {
		return "", "", nil, err
	}
	// 预填统一用 ~ 形式（expandPath 在使用时展开），跨用户展示友好
	displayJSON := "~/.config/opencode/opencode.json"
	displayJSONC := "~/.config/opencode/opencode.jsonc"
	switch {
	case jsonExists && jsoncExists:
		return displayJSON, "both", []string{displayJSON, displayJSONC}, nil
	case jsoncExists:
		return displayJSONC, "jsonc", nil, nil
	case jsonExists:
		return displayJSON, "json", nil, nil
	default:
		return displayJSON, "none", nil, nil
	}
}

func statConfigCandidate(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("检查配置文件 %s 失败: %w", path, err)
	}
	return true, nil
}

// ========== 嵌套字段操作辅助函数 ==========

// getNestedValue 获取嵌套字段值
func getNestedValue(data map[string]interface{}, path string) interface{} {
	v, _ := getNestedValueEx(data, path)
	return v
}

// getNestedValueEx 获取嵌套字段值并区分"路径缺失"与"叶子存在但为 null"
func getNestedValueEx(data map[string]interface{}, path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	current := interface{}(data)
	for i, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		v, exists := m[part]
		if !exists {
			return nil, false
		}
		if i == len(parts)-1 {
			return v, true
		}
		current = v
	}
	return nil, false
}

// setNestedValueStrict 设置嵌套字段值。中间节点已存在但不是对象时报错
// （静默覆盖会丢用户数据），路径含空段也报错
func setNestedValueStrict(data map[string]interface{}, path string, value interface{}) error {
	parts := strings.Split(path, ".")
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("字段路径 %q 含空段", path)
		}
	}
	current := data
	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = value
			return nil
		}
		existing, exists := current[part]
		if !exists {
			next := make(map[string]interface{})
			current[part] = next
			current = next
			continue
		}
		next, ok := existing.(map[string]interface{})
		if !ok {
			return fmt.Errorf("字段 %s 的上级 %q 已存在且不是对象，为避免覆盖用户配置已中止", path, strings.Join(parts[:i+1], "."))
		}
		current = next
	}
	return nil
}

// deleteNestedValueIfEquals 仅当叶子当前取值深等于 expected 时才删除
// （用户手改过的值保留）
func deleteNestedValueIfEquals(data map[string]interface{}, path string, expected interface{}) {
	parts := strings.Split(path, ".")
	current := data
	for i, part := range parts {
		if i == len(parts)-1 {
			if v, exists := current[part]; exists && reflect.DeepEqual(normalizeJSONValue(v), normalizeJSONValue(expected)) {
				delete(current, part)
			}
			return
		}
		next, ok := current[part].(map[string]interface{})
		if !ok {
			return
		}
		current = next
	}
}

// normalizeJSONValue 经 JSON 往返归一化（seed 里的 map[string]any 与解析出的
// 树在数值类型上可能不同：int vs float64），保证 DeepEqual 语义正确
func normalizeJSONValue(v interface{}) interface{} {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}

// pruneEmptyObjects 自底向上裁剪 path 沿途变空的对象壳
func pruneEmptyObjects(data map[string]interface{}, path string) {
	parts := strings.Split(path, ".")
	for cut := len(parts) - 1; cut >= 1; cut-- {
		parent := data
		ok := true
		for _, part := range parts[:cut-1] {
			next, isMap := parent[part].(map[string]interface{})
			if !isMap {
				ok = false
				break
			}
			parent = next
		}
		if !ok {
			continue
		}
		key := parts[cut-1]
		if m, isMap := parent[key].(map[string]interface{}); isMap && len(m) == 0 {
			delete(parent, key)
		}
	}
}
