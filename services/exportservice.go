package services

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// exportAppID / exportSchemaVersion 是导入端识别自有导出包的门禁：
// 只有 exportMeta.app 与 schemaVersion 命中时才解析 extra/prompts/skills 扩展段，
// 普通 cc-switch JSON 永远走 legacy 解析路径。破坏性格式变更必须提升版本号。
const (
	exportAppID         = "code-switch-r"
	exportSchemaVersion = 1
)

// exportMeta 导出包元数据
type exportMeta struct {
	App            string   `json:"app"`
	SchemaVersion  int      `json:"schemaVersion"`
	AppVersion     string   `json:"appVersion"`
	ExportedAt     string   `json:"exportedAt"`
	Redacted       bool     `json:"redacted"`
	RedactedFields []string `json:"redactedFields,omitempty"`
}

// exportBundle 导出包。基础段（claude/codex/gemini/mcp）是 cc-switch 兼容形态，
// 现有导入不识别 exportMeta 也能按 legacy 路径吃掉基础字段；
// prompts/skills 与各条目的 extra 为自有扩展。
type exportBundle struct {
	ExportMeta exportMeta          `json:"exportMeta"`
	Claude     ccProviderSection   `json:"claude"`
	Codex      ccProviderSection   `json:"codex"`
	Gemini     ccProviderSection   `json:"gemini"`
	MCP        ccMCPSection        `json:"mcp"`
	Prompts    map[string][]Prompt `json:"prompts,omitempty"`
	// Skills 仅为元数据附件（安装状态+仓库列表，不含技能文件），导入不消费
	Skills *skillStore `json:"skills,omitempty"`
}

// exportSnapshot 深拷贝后的数据快照：脱敏在快照上进行，绝不改动运行中的配置
type exportSnapshot struct {
	claudeProviders []Provider
	codexProviders  []Provider
	geminiProviders []GeminiProvider
	mcpServers      []MCPServer
	prompts         map[string][]Prompt
	skills          *skillStore
}

// ExportResult 导出结果
type ExportResult struct {
	Path           string `json:"path"`
	Canceled       bool   `json:"canceled"`
	Providers      int    `json:"providers"`
	MCP            int    `json:"mcp"`
	Prompts        int    `json:"prompts"`
	Redacted       bool   `json:"redacted"`
	RedactedFields int    `json:"redactedFields"`
}

// ExportService 配置一键导出（issue #13）：把供应商/MCP/提示词/技能元数据
// 打成单文件 JSON 包，包可被 ImportService 直接导回。
type ExportService struct {
	providerService *ProviderService
	geminiService   *GeminiService
	mcpService      *MCPService
	promptService   *PromptService
	appVersion      func() string
	now             func() time.Time
}

func NewExportService(ps *ProviderService, gs *GeminiService, ms *MCPService, prs *PromptService, appVersion func() string) *ExportService {
	if appVersion == nil {
		appVersion = func() string { return "" }
	}
	return &ExportService{
		providerService: ps,
		geminiService:   gs,
		mcpService:      ms,
		promptService:   prs,
		appVersion:      appVersion,
		now:             time.Now,
	}
}

func (es *ExportService) Start() error { return nil }
func (es *ExportService) Stop() error  { return nil }

// ExportWithDialog 弹系统保存对话框并导出。这是唯一暴露给前端的导出入口：
// 接受路径参数的写文件方法一律不导出，避免 RPC 变成任意路径写入原语。
// includeSecrets=false（默认脱敏）时清空所有已识别的凭据字段。
func (es *ExportService) ExportWithDialog(includeSecrets bool) (ExportResult, error) {
	// 先取快照：数据源有问题就不弹对话框，也绝不落半成品文件
	snap, err := es.collectSnapshot()
	if err != nil {
		return ExportResult{}, err
	}
	bundle, redactedFields, err := buildExportBundle(snap, includeSecrets, es.appVersion(), es.now())
	if err != nil {
		return ExportResult{}, err
	}

	dialog := application.SaveFileDialog().
		SetFilename(fmt.Sprintf("code-switch-export-%s.json", es.now().Format("20060102"))).
		AddFilter("JSON 配置包 (*.json)", "*.json").
		CanCreateDirectories(true)
	// 绑定当前（发起调用的）窗口，避免对话框失焦或弹到别的窗口后面
	if app := application.Get(); app != nil {
		if w := app.Window.Current(); w != nil {
			dialog.AttachToWindow(w)
		}
	}
	path, err := dialog.PromptForSingleSelection()
	if err != nil {
		return ExportResult{}, fmt.Errorf("打开保存对话框失败: %w", err)
	}
	if strings.TrimSpace(path) == "" {
		return ExportResult{Canceled: true}, nil
	}
	// 不追加扩展名：系统对话框只对用户选中的原路径做过覆盖确认，
	// 事后改写路径可能静默覆盖已存在的同名 .json

	if err := es.writeBundleFile(path, bundle); err != nil {
		return ExportResult{}, err
	}
	return ExportResult{
		Path:           path,
		Providers:      len(snap.claudeProviders) + len(snap.codexProviders) + len(snap.geminiProviders),
		MCP:            len(snap.mcpServers),
		Prompts:        countPrompts(snap.prompts),
		Redacted:       !includeSecrets,
		RedactedFields: len(redactedFields),
	}, nil
}

// writeBundleFile 序列化并原子写入（0600；Windows 上文件权限继承目标目录 ACL，
// POSIX 位不构成额外保护，明文包提示见前端文案）。非导出：不进 RPC 面。
func (es *ExportService) writeBundleFile(path string, bundle *exportBundle) error {
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化导出包失败: %w", err)
	}
	if err := atomicWriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("写入导出文件失败: %w", err)
	}
	return nil
}

// collectSnapshot 读取全部数据源并深拷贝。任一数据源失败即整体失败，
// 不生成缺段的"部分备份"。
func (es *ExportService) collectSnapshot() (*exportSnapshot, error) {
	snap := &exportSnapshot{prompts: map[string][]Prompt{}}

	for _, kind := range []string{"claude", "codex"} {
		providers, err := es.providerService.LoadProviders(kind)
		if err != nil {
			return nil, fmt.Errorf("读取 %s 供应商失败: %w", kind, err)
		}
		copied := make([]Provider, len(providers))
		for i := range providers {
			copied[i] = deepCopyProvider(providers[i])
		}
		if kind == "claude" {
			snap.claudeProviders = copied
		} else {
			snap.codexProviders = copied
		}
	}

	if es.geminiService != nil {
		src := es.geminiService.GetProviders()
		snap.geminiProviders = make([]GeminiProvider, len(src))
		for i := range src {
			copied, err := deepCopyGeminiProvider(src[i])
			if err != nil {
				return nil, err
			}
			snap.geminiProviders[i] = copied
		}
	}

	if es.mcpService != nil {
		servers, err := es.mcpService.ListServers()
		if err != nil {
			return nil, fmt.Errorf("读取 MCP 配置失败: %w", err)
		}
		snap.mcpServers = make([]MCPServer, len(servers))
		for i := range servers {
			snap.mcpServers[i] = deepCopyMCPServer(servers[i])
		}
	}

	if es.promptService != nil {
		for _, platform := range []string{"claude", "codex", "gemini"} {
			stored, err := es.promptService.StoredPrompts(platform)
			if err != nil {
				return nil, fmt.Errorf("读取 %s 提示词失败: %w", platform, err)
			}
			list := make([]Prompt, 0, len(stored))
			for _, p := range stored {
				list = append(list, deepCopyPrompt(p))
			}
			sort.SliceStable(list, func(i, j int) bool {
				return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
			})
			if len(list) > 0 {
				snap.prompts[platform] = list
			}
		}
	}

	skills, err := loadSkillStoreForExport()
	if err != nil {
		return nil, err
	}
	snap.skills = skills

	return snap, nil
}

// loadSkillStoreForExport 读取 skill.json 元数据。
// 文件不存在是正常空状态；存在但解析失败必须中止导出（不产出部分包）。
func loadSkillStoreForExport() (*skillStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("获取用户目录失败: %w", err)
	}
	path := filepath.Join(home, skillStoreDir, skillStoreFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 skill.json 失败: %w", err)
	}
	var store skillStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("skill.json 解析失败，中止导出: %w", err)
	}
	return &store, nil
}

// buildExportBundle 纯构建：快照 → 导出包。includeSecrets=false 时在快照上
// 就地脱敏（快照已深拷贝，不影响运行数据），返回被清空字段的路径清单。
func buildExportBundle(snap *exportSnapshot, includeSecrets bool, appVersion string, now time.Time) (*exportBundle, []string, error) {
	var redacted []string
	track := func(path string) { redacted = append(redacted, path) }

	bundle := &exportBundle{
		Claude: ccProviderSection{Providers: map[string]ccProviderEntry{}},
		Codex:  ccProviderSection{Providers: map[string]ccProviderEntry{}},
		Gemini: ccProviderSection{Providers: map[string]ccProviderEntry{}},
		MCP: ccMCPSection{
			Claude: ccMCPPlatform{Servers: map[string]ccMCPServerEntry{}},
			Codex:  ccMCPPlatform{Servers: map[string]ccMCPServerEntry{}},
			Gemini: ccMCPPlatform{Servers: map[string]ccMCPServerEntry{}},
		},
		Prompts: snap.prompts,
		Skills:  snap.skills,
	}

	for i := range snap.claudeProviders {
		p := &snap.claudeProviders[i]
		if !includeSecrets && p.APIKey != "" {
			p.APIKey = ""
			track(fmt.Sprintf("claude.%s.apiKey", p.Name))
		}
		if !includeSecrets {
			p.APIURL = scrubURLCredentials(p.APIURL, fmt.Sprintf("claude.%s.apiUrl", p.Name), track)
		}
		entry, err := claudeProviderToEntry(p)
		if err != nil {
			return nil, nil, err
		}
		bundle.Claude.Providers[strconv.FormatInt(p.ID, 10)] = entry
	}
	for i := range snap.codexProviders {
		p := &snap.codexProviders[i]
		if !includeSecrets && p.APIKey != "" {
			p.APIKey = ""
			track(fmt.Sprintf("codex.%s.apiKey", p.Name))
		}
		if !includeSecrets {
			p.APIURL = scrubURLCredentials(p.APIURL, fmt.Sprintf("codex.%s.apiUrl", p.Name), track)
		}
		entry, err := codexProviderToEntry(p)
		if err != nil {
			return nil, nil, err
		}
		bundle.Codex.Providers[strconv.FormatInt(p.ID, 10)] = entry
	}
	for i := range snap.geminiProviders {
		p := &snap.geminiProviders[i]
		if !includeSecrets {
			if p.APIKey != "" {
				p.APIKey = ""
				track(fmt.Sprintf("gemini.%s.apiKey", p.Name))
			}
			for k, v := range p.EnvConfig {
				if v == "" {
					continue
				}
				if sensitiveKeyName(k) {
					p.EnvConfig[k] = ""
					track(fmt.Sprintf("gemini.%s.env.%s", p.Name, k))
					continue
				}
				// 键名不敏感但值是 URL（如 GOOGLE_GEMINI_BASE_URL）：
				// 清掉其中的 userinfo 与敏感查询参数
				if hasHTTPScheme(v) {
					p.EnvConfig[k] = scrubURLCredentials(v, fmt.Sprintf("gemini.%s.env.%s", p.Name, k), track)
				}
			}
			p.BaseURL = scrubURLCredentials(p.BaseURL, fmt.Sprintf("gemini.%s.baseUrl", p.Name), track)
			redactAnyMap(p.SettingsConfig, fmt.Sprintf("gemini.%s.settingsConfig", p.Name), track)
		}
		entry, err := geminiProviderToEntry(p)
		if err != nil {
			return nil, nil, err
		}
		bundle.Gemini.Providers[nonEmptyOr(p.ID, p.Name)] = entry
	}

	for i := range snap.mcpServers {
		s := &snap.mcpServers[i]
		if !includeSecrets {
			redactMCPServer(s, track)
		}
		entry := mcpServerToEntry(s)
		enabledClaude := containsPlatform(s.EnablePlatform, platClaudeCode)
		enabledCodex := containsPlatform(s.EnablePlatform, platCodex)
		enabledGemini := containsPlatform(s.EnablePlatform, platGemini)
		// 与 SQLite 装载约定一致：启用进对应平台桶；所有平台都未启用时
		// 进 claude+codex 双桶且 Enabled=false，保证条目不会在导入侧完全消失
		if enabledClaude || enabledCodex || enabledGemini {
			if enabledClaude {
				e := entry
				e.Enabled = true
				bundle.MCP.Claude.Servers[s.Name] = e
			}
			if enabledCodex {
				e := entry
				e.Enabled = true
				bundle.MCP.Codex.Servers[s.Name] = e
			}
			if enabledGemini {
				e := entry
				e.Enabled = true
				bundle.MCP.Gemini.Servers[s.Name] = e
			}
		} else {
			bundle.MCP.Claude.Servers[s.Name] = entry
			bundle.MCP.Codex.Servers[s.Name] = entry
		}
	}

	sort.Strings(redacted)
	bundle.ExportMeta = exportMeta{
		App:            exportAppID,
		SchemaVersion:  exportSchemaVersion,
		AppVersion:     appVersion,
		ExportedAt:     now.Format(time.RFC3339),
		Redacted:       !includeSecrets,
		RedactedFields: redacted,
	}
	return bundle, redacted, nil
}

// redactAnyMap 递归清理任意 JSON 树里的凭据（gemini SettingsConfig 等
// 自由结构）：敏感键的字符串值置空；敏感键下的 map/数组子树整体进入
// 敏感上下文，其中所有字符串值全部清空（复合凭据如 auth:{value:...}）；
// 非敏感上下文中的 URL 字符串清 userinfo/敏感查询参数。
func redactAnyMap(m map[string]any, path string, track func(string)) {
	redactAnyTree(m, false, path, track)
}

func redactAnyTree(v any, sensitiveCtx bool, path string, track func(string)) any {
	switch val := v.(type) {
	case string:
		if val == "" {
			return val
		}
		if sensitiveCtx {
			track(path)
			return ""
		}
		if hasHTTPScheme(val) {
			return scrubURLCredentials(val, path, track)
		}
		return val
	case map[string]any:
		for k, child := range val {
			childSensitive := sensitiveCtx || sensitiveKeyName(k)
			val[k] = redactAnyTree(child, childSensitive, path+"."+k, track)
		}
		return val
	case []any:
		for i, child := range val {
			val[i] = redactAnyTree(child, sensitiveCtx, fmt.Sprintf("%s[%d]", path, i), track)
		}
		return val
	default:
		return v
	}
}

// claudeProviderToEntry 构建 claude 条目；extra 序列化失败必须整体中止，
// 静默省略会产出悄悄丢字段的"部分备份"
func claudeProviderToEntry(p *Provider) (ccProviderEntry, error) {
	entry := ccProviderEntry{
		ID:         strconv.FormatInt(p.ID, 10),
		Name:       p.Name,
		WebsiteURL: p.Site,
		Settings: ccProviderSetting{
			Env: stringMap{
				"ANTHROPIC_BASE_URL":   p.APIURL,
				"ANTHROPIC_AUTH_TOKEN": p.APIKey,
			},
			Auth: stringMap{},
		},
	}
	extra, err := marshalProviderExtra(*p)
	if err != nil {
		return ccProviderEntry{}, fmt.Errorf("序列化供应商 %s 失败: %w", p.Name, err)
	}
	entry.Extra = extra
	return entry, nil
}

func codexProviderToEntry(p *Provider) (ccProviderEntry, error) {
	entry := ccProviderEntry{
		ID:         strconv.FormatInt(p.ID, 10),
		Name:       p.Name,
		WebsiteURL: p.Site,
		Settings: ccProviderSetting{
			Env:    stringMap{},
			Auth:   stringMap{"OPENAI_API_KEY": p.APIKey},
			Config: buildMinimalCodexConfig(p.APIURL, p.Name),
		},
	}
	extra, err := marshalProviderExtra(*p)
	if err != nil {
		return ccProviderEntry{}, fmt.Errorf("序列化供应商 %s 失败: %w", p.Name, err)
	}
	entry.Extra = extra
	return entry, nil
}

func marshalProviderExtra(p Provider) (json.RawMessage, error) {
	p.ID = 0
	return json.Marshal(p)
}

func geminiProviderToEntry(p *GeminiProvider) (ccProviderEntry, error) {
	env := stringMap{
		"GOOGLE_GEMINI_BASE_URL": p.BaseURL,
		"GEMINI_API_KEY":         p.APIKey,
	}
	if p.Model != "" {
		env["GEMINI_MODEL"] = p.Model
	}
	entry := ccProviderEntry{
		ID:         p.ID,
		Name:       p.Name,
		WebsiteURL: p.WebsiteURL,
		Settings:   ccProviderSetting{Env: env, Auth: stringMap{}},
	}
	clone := *p
	clone.ID = ""
	extra, err := json.Marshal(clone)
	if err != nil {
		return ccProviderEntry{}, fmt.Errorf("序列化供应商 %s 失败: %w", p.Name, err)
	}
	entry.Extra = extra
	return entry, nil
}

func mcpServerToEntry(s *MCPServer) ccMCPServerEntry {
	return ccMCPServerEntry{
		ID:          s.Name,
		Name:        s.Name,
		Homepage:    s.Website,
		Description: s.Tips,
		Server: ccMCPServerConfig{
			Type:    s.Type,
			Command: s.Command,
			Args:    stringSlice(append([]string(nil), s.Args...)),
			Env:     stringMap(cloneStringMap(s.Env)),
			URL:     s.URL,
		},
	}
}

// redactMCPServer 清理 MCP 条目里已识别的凭据载体：env 敏感键、
// 命令行敏感旗标的取值、URL 中的 userinfo 与敏感查询参数。
// 自由文本（description 等）不做扫描。
func redactMCPServer(s *MCPServer, track func(string)) {
	for k, v := range s.Env {
		if v != "" && sensitiveKeyName(k) {
			s.Env[k] = ""
			track(fmt.Sprintf("mcp.%s.env.%s", s.Name, k))
		}
	}
	for i := 0; i < len(s.Args); i++ {
		arg := s.Args[i]
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		flag := strings.TrimLeft(arg, "-")
		if eq := strings.Index(flag, "="); eq >= 0 {
			if sensitiveKeyName(flag[:eq]) && flag[eq+1:] != "" {
				s.Args[i] = arg[:strings.Index(arg, "=")+1]
				track(fmt.Sprintf("mcp.%s.args[%d]", s.Name, i))
			}
			continue
		}
		if sensitiveKeyName(flag) && i+1 < len(s.Args) && !strings.HasPrefix(s.Args[i+1], "-") {
			if s.Args[i+1] != "" {
				s.Args[i+1] = ""
				track(fmt.Sprintf("mcp.%s.args[%d]", s.Name, i+1))
			}
			i++
		}
	}
	s.URL = scrubURLCredentials(s.URL, fmt.Sprintf("mcp.%s.url", s.Name), track)
}

// hasHTTPScheme 大小写无关地判断字符串是否 http/https URL——
// "HTTPS://user:pass@host" 是合法 URL，只认小写会被绕过
func hasHTTPScheme(s string) bool {
	if len(s) >= 7 && strings.EqualFold(s[:7], "http://") {
		return true
	}
	return len(s) >= 8 && strings.EqualFold(s[:8], "https://")
}

// scrubURLCredentials 去掉 URL 中的 userinfo 并清空敏感查询参数值
func scrubURLCredentials(raw, path string, track func(string)) string {
	if raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	changed := false
	if u.User != nil {
		u.User = nil
		changed = true
	}
	if u.RawQuery != "" {
		q := u.Query()
		for k, vals := range q {
			if !sensitiveKeyName(k) {
				continue
			}
			for _, v := range vals {
				if v != "" {
					q.Set(k, "")
					changed = true
					break
				}
			}
		}
		if changed {
			u.RawQuery = q.Encode()
		}
	}
	if !changed {
		return raw
	}
	track(path)
	return u.String()
}

// sensitiveKeyName 分段判断键名是否为凭据类：按非字母数字切段后整段比对，
// 避免 "keyboard"/"monkey" 之类子串误伤
func sensitiveKeyName(name string) bool {
	sensitive := map[string]bool{
		"key": true, "apikey": true, "token": true, "secret": true,
		"password": true, "passwd": true, "credential": true, "credentials": true,
		"auth": true, "authorization": true, "bearer": true, "cookie": true, "session": true,
	}
	seg := strings.Builder{}
	segments := []string{}
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			seg.WriteRune(r)
			continue
		}
		if seg.Len() > 0 {
			segments = append(segments, seg.String())
			seg.Reset()
		}
	}
	if seg.Len() > 0 {
		segments = append(segments, seg.String())
	}
	for i, s := range segments {
		if sensitive[s] {
			return true
		}
		// 复数形式（tokens/keys/secrets…）同样按凭据处理
		if strings.HasSuffix(s, "s") && sensitive[strings.TrimSuffix(s, "s")] {
			return true
		}
		// "api"+"key" 连段（api_key / api-key 已被切开）
		if s == "api" && i+1 < len(segments) && sensitive[segments[i+1]] {
			return true
		}
	}
	return false
}

func deepCopyProvider(p Provider) Provider {
	out := p
	if p.SupportedModels != nil {
		out.SupportedModels = make(map[string]bool, len(p.SupportedModels))
		for k, v := range p.SupportedModels {
			out.SupportedModels[k] = v
		}
	}
	if p.ModelMapping != nil {
		out.ModelMapping = make(map[string]string, len(p.ModelMapping))
		for k, v := range p.ModelMapping {
			out.ModelMapping[k] = v
		}
	}
	if p.AvailabilityConfig != nil {
		cfg := *p.AvailabilityConfig
		out.AvailabilityConfig = &cfg
	}
	if p.SanitizeConfig != nil {
		out.SanitizeConfig = &SanitizeConfig{
			BlockedBodyFields: cloneStringListPtr(p.SanitizeConfig.BlockedBodyFields),
			BlockedHeaders:    cloneStringListPtr(p.SanitizeConfig.BlockedHeaders),
			BlockedBetaValues: cloneStringListPtr(p.SanitizeConfig.BlockedBetaValues),
		}
	}
	return out
}

func deepCopyGeminiProvider(p GeminiProvider) (GeminiProvider, error) {
	out := p
	if p.EnvConfig != nil {
		out.EnvConfig = make(map[string]string, len(p.EnvConfig))
		for k, v := range p.EnvConfig {
			out.EnvConfig[k] = v
		}
	}
	if p.SettingsConfig != nil {
		// SettingsConfig 可嵌套任意层 map（如 security.auth 段），
		// 浅拷贝会让脱敏改写穿透到运行中的配置，必须 JSON 往返深拷贝；
		// 编解码失败不能吞掉——那会产出静默缺段的部分备份
		data, err := json.Marshal(p.SettingsConfig)
		if err != nil {
			return GeminiProvider{}, fmt.Errorf("复制供应商 %s 的 settings 配置失败: %w", p.Name, err)
		}
		out.SettingsConfig = make(map[string]any, len(p.SettingsConfig))
		if err := json.Unmarshal(data, &out.SettingsConfig); err != nil {
			return GeminiProvider{}, fmt.Errorf("复制供应商 %s 的 settings 配置失败: %w", p.Name, err)
		}
	}
	if p.SupportedModels != nil {
		out.SupportedModels = make(map[string]bool, len(p.SupportedModels))
		for k, v := range p.SupportedModels {
			out.SupportedModels[k] = v
		}
	}
	if p.ModelMapping != nil {
		out.ModelMapping = make(map[string]string, len(p.ModelMapping))
		for k, v := range p.ModelMapping {
			out.ModelMapping[k] = v
		}
	}
	return out, nil
}

func deepCopyMCPServer(s MCPServer) MCPServer {
	out := s
	out.Args = append([]string(nil), s.Args...)
	out.Env = cloneStringMap(s.Env)
	out.EnablePlatform = append([]string(nil), s.EnablePlatform...)
	out.MissingPlaceholders = append([]string(nil), s.MissingPlaceholders...)
	return out
}

func deepCopyPrompt(p Prompt) Prompt {
	out := p
	if p.Description != nil {
		v := *p.Description
		out.Description = &v
	}
	if p.CreatedAt != nil {
		v := *p.CreatedAt
		out.CreatedAt = &v
	}
	if p.UpdatedAt != nil {
		v := *p.UpdatedAt
		out.UpdatedAt = &v
	}
	return out
}

func countPrompts(m map[string][]Prompt) int {
	total := 0
	for _, list := range m {
		total += len(list)
	}
	return total
}

func nonEmptyOr(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}
