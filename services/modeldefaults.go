package services

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	modelpricing "codeswitch/resources/model-pricing"
)

// 默认模型策略:从已同步的厂商目录动态解析各平台"最新可用"模型,
// 数据不可用或解析不出时退回静态兜底常量。
//
// 频道规则:版本号(数字分段比较)优先,同版本内 stable 优先于 preview,
// 再按 release_date、带日期变体、字典序决胜;探测模型额外要求发布满 30 天
// (给第三方中转留适配期),产品默认模型只要求稳定频道语义与 tool_call 能力。

// 静态兜底常量。探测值刻意保守(稳定窗内),默认值取数据源当前最新。
const (
	FallbackClaudeProbeModel = "claude-haiku-4-5-20251001"
	// FallbackClaudeDefaultModel 产品默认(Sonnet 家族),供 opencode 预设等
	// "写进外部工具配置"的场景;探测仍走 Haiku
	FallbackClaudeDefaultModel = "claude-sonnet-4-5"
	FallbackCodexDefaultModel  = "gpt-5.6"
	FallbackCodexProbeModel    = "gpt-5.5"
	FallbackGeminiDefaultModel = "gemini-3.1-pro-preview"
	FallbackGeminiProbeModel   = "gemini-3.5-flash"
)

// probeStabilityWindow 探测模型的最小发布年龄。
const probeStabilityWindow = 30 * 24 * time.Hour

// resolverMaxFutureSkew release_date 允许的最大未来偏移(UTC),超过视为脏数据不入选。
const resolverMaxFutureSkew = 3 * 24 * time.Hour

// CatalogSource 提供当前生效的厂商目录(由模型同步服务实现)。
type CatalogSource interface {
	Catalogs() map[string]*modelpricing.RemoteCatalog
}

// DefaultModels 各平台当前解析结果,供前端与配置写入方使用。
type DefaultModels struct {
	ClaudeProbe   string `json:"claudeProbe"`
	CodexDefault  string `json:"codexDefault"`
	CodexProbe    string `json:"codexProbe"`
	GeminiDefault string `json:"geminiDefault"`
	GeminiProbe   string `json:"geminiProbe"`
}

// DefaultModelPolicy 解析入口。并发安全:source 替换与读取加锁,解析本身无状态。
type DefaultModelPolicy struct {
	mu      sync.RWMutex
	source  CatalogSource
	pricing *modelpricing.Service
	now     func() time.Time
}

// NewDefaultModelPolicy 创建策略;目录源由模型同步服务构造后注入(SetSource)。
func NewDefaultModelPolicy() *DefaultModelPolicy {
	pricing, _ := modelpricing.DefaultService()
	return &DefaultModelPolicy{pricing: pricing, now: time.Now}
}

// SetSource 注入目录源。
func (p *DefaultModelPolicy) SetSource(source CatalogSource) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.source = source
	p.mu.Unlock()
}

func (p *DefaultModelPolicy) catalog(providerID string) *modelpricing.RemoteCatalog {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	source := p.source
	p.mu.RUnlock()
	if source == nil {
		return nil
	}
	catalogs := source.Catalogs()
	if catalogs == nil {
		return nil
	}
	return catalogs[providerID]
}

// CodexDefaultModel 写入 Codex 配置的默认模型:
// 主线最大版本 M 与 codex 专线最大版本 V 比较,V>=M 才选专线,否则选主线。
func (p *DefaultModelPolicy) CodexDefaultModel() string {
	mainline, okMain := p.selectModel("openai", codexMainlinePattern, selectOpts{requireToolCall: true})
	codexLine, okCodex := p.selectModel("openai", codexLinePattern, selectOpts{requireToolCall: true})
	switch {
	case okMain && okCodex:
		if compareVersionSegments(codexLine.version, mainline.version) >= 0 {
			return codexLine.id
		}
		return mainline.id
	case okMain:
		return mainline.id
	case okCodex:
		return codexLine.id
	default:
		return FallbackCodexDefaultModel
	}
}

// GeminiDefaultModel 写入 Gemini 配置的默认模型(pro 家族)。
func (p *DefaultModelPolicy) GeminiDefaultModel() string {
	if c, ok := p.selectModel("google", geminiProPattern, selectOpts{requireToolCall: true}); ok {
		return c.id
	}
	return FallbackGeminiDefaultModel
}

// ClaudeDefaultModel 产品默认 Claude 模型(稳定 Sonnet 家族,要求 tool_call)。
// 与 ProbeModel("claude") 的语义区别:探测选低成本 Haiku,这里选面向实际
// 编码/对话体验的默认型号,供 opencode 预设等写入外部工具配置的场景使用。
func (p *DefaultModelPolicy) ClaudeDefaultModel() string {
	if c, ok := p.selectModel("anthropic", claudeSonnetPattern, selectOpts{requireToolCall: true}); ok {
		return c.id
	}
	return FallbackClaudeDefaultModel
}

// ProbeModel 各平台健康/连通性探测的默认模型(30 天稳定窗)。
func (p *DefaultModelPolicy) ProbeModel(platform string) string {
	switch strings.ToLower(platform) {
	case "claude":
		if c, ok := p.selectModel("anthropic", claudeHaikuPattern, selectOpts{stabilityWindow: probeStabilityWindow, preferDated: true}); ok {
			return c.id
		}
		return FallbackClaudeProbeModel
	case "codex":
		if c, ok := p.selectModel("openai", codexMainlinePattern, selectOpts{stabilityWindow: probeStabilityWindow}); ok {
			return c.id
		}
		return FallbackCodexProbeModel
	case "gemini":
		if c, ok := p.selectModel("google", geminiFlashPattern, selectOpts{stabilityWindow: probeStabilityWindow}); ok {
			return c.id
		}
		return FallbackGeminiProbeModel
	default:
		return ""
	}
}

// ProbeCandidates 探测候选链(去重):动态解析值 → 静态兜底。
// 供白名单交集选择:声明了 supportedModels 的供应商取链上首个其支持的模型。
func (p *DefaultModelPolicy) ProbeCandidates(platform string) []string {
	var fallback string
	switch strings.ToLower(platform) {
	case "claude":
		fallback = FallbackClaudeProbeModel
	case "codex":
		fallback = FallbackCodexProbeModel
	case "gemini":
		fallback = FallbackGeminiProbeModel
	default:
		return nil
	}
	dynamic := p.ProbeModel(platform)
	if dynamic == "" || dynamic == fallback {
		return []string{fallback}
	}
	return []string{dynamic, fallback}
}

// DefaultModels 汇总当前解析结果。
func (p *DefaultModelPolicy) DefaultModels() DefaultModels {
	return DefaultModels{
		ClaudeProbe:   p.ProbeModel("claude"),
		CodexDefault:  p.CodexDefaultModel(),
		CodexProbe:    p.ProbeModel("codex"),
		GeminiDefault: p.GeminiDefaultModel(),
		GeminiProbe:   p.ProbeModel("gemini"),
	}
}

// —— 家族匹配 ——

// familyPattern 从模型 id 提取版本段与频道信息;不匹配则该 id 不属于此家族。
type familyPattern func(id string) (version []int, dated string, preview bool, ok bool)

var (
	codexMainlineRe      = regexp.MustCompile(`^gpt-(\d+(?:\.\d+)*)$`)
	codexLineRe          = regexp.MustCompile(`^gpt-(\d+(?:\.\d+)*)-codex$`)
	geminiProRe          = regexp.MustCompile(`^gemini-(\d+(?:\.\d+)*)-pro(-preview)?$`)
	geminiFlashRe        = regexp.MustCompile(`^gemini-(\d+(?:\.\d+)*)-flash(-preview)?$`)
	claudeHaikuNewRe     = regexp.MustCompile(`^claude-haiku-(\d+(?:-\d+)*?)(?:-(\d{8}))?$`)
	claudeHaikuLegacyRe  = regexp.MustCompile(`^claude-(\d+(?:-\d+)*?)-haiku(?:-(\d{8}))?$`)
	claudeSonnetNewRe    = regexp.MustCompile(`^claude-sonnet-(\d+(?:-\d+)*?)(?:-(\d{8}))?$`)
	claudeSonnetLegacyRe = regexp.MustCompile(`^claude-(\d+(?:-\d+)*?)-sonnet(?:-(\d{8}))?$`)
)

func codexMainlinePattern(id string) ([]int, string, bool, bool) {
	m := codexMainlineRe.FindStringSubmatch(id)
	if m == nil {
		return nil, "", false, false
	}
	return parseVersionSegments(m[1]), "", false, true
}

func codexLinePattern(id string) ([]int, string, bool, bool) {
	m := codexLineRe.FindStringSubmatch(id)
	if m == nil {
		return nil, "", false, false
	}
	return parseVersionSegments(m[1]), "", false, true
}

func geminiProPattern(id string) ([]int, string, bool, bool) {
	m := geminiProRe.FindStringSubmatch(id)
	if m == nil {
		return nil, "", false, false
	}
	return parseVersionSegments(m[1]), "", m[2] != "", true
}

func geminiFlashPattern(id string) ([]int, string, bool, bool) {
	m := geminiFlashRe.FindStringSubmatch(id)
	if m == nil {
		return nil, "", false, false
	}
	return parseVersionSegments(m[1]), "", m[2] != "", true
}

func claudeHaikuPattern(id string) ([]int, string, bool, bool) {
	if m := claudeHaikuNewRe.FindStringSubmatch(id); m != nil {
		if version := saneClaudeVersion(parseVersionSegments(m[1])); version != nil {
			return version, m[2], false, true
		}
	}
	if m := claudeHaikuLegacyRe.FindStringSubmatch(id); m != nil {
		if version := saneClaudeVersion(parseVersionSegments(m[1])); version != nil {
			return version, m[2], false, true
		}
	}
	return nil, "", false, false
}

func claudeSonnetPattern(id string) ([]int, string, bool, bool) {
	if m := claudeSonnetNewRe.FindStringSubmatch(id); m != nil {
		if version := saneClaudeVersion(parseVersionSegments(m[1])); version != nil {
			return version, m[2], false, true
		}
	}
	if m := claudeSonnetLegacyRe.FindStringSubmatch(id); m != nil {
		if version := saneClaudeVersion(parseVersionSegments(m[1])); version != nil {
			return version, m[2], false, true
		}
	}
	return nil, "", false, false
}

// saneClaudeVersion 拒绝把疑似日期/异常数字吸收进版本段的解析结果
// (惰性正则在非 8 位数字尾缀时可能回溯出 [4,5,2025] 这类伪版本)。
func saneClaudeVersion(version []int) []int {
	if len(version) == 0 || len(version) > 3 {
		return nil
	}
	for _, seg := range version {
		if seg >= 1000 {
			return nil
		}
	}
	return version
}

// parseVersionSegments 把 "5.10"/"4-5" 拆成数字段。
func parseVersionSegments(s string) []int {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == '-' })
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// compareVersionSegments 数字段逐段比较(5.10 > 5.9),缺段按 0。
func compareVersionSegments(a, b []int) int {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av != bv {
			if av > bv {
				return 1
			}
			return -1
		}
	}
	return 0
}

type selectOpts struct {
	requireToolCall bool
	stabilityWindow time.Duration
	preferDated     bool
}

type resolvedCandidate struct {
	id      string
	version []int
	dated   string
	preview bool
	release time.Time
}

// selectModel 在指定厂商目录内按家族规则选出最优候选。
// 过滤:文本入出、正价、release_date 合法且非未来、稳定窗、tool_call(按需)。
// 排序:版本降序 → stable 优先 → release 降序 → 带日期变体(按需) → id 升序。
func (p *DefaultModelPolicy) selectModel(providerID string, pattern familyPattern, opts selectOpts) (resolvedCandidate, bool) {
	catalog := p.catalog(providerID)
	if catalog == nil || len(catalog.Models) == 0 {
		return resolvedCandidate{}, false
	}
	now := time.Now()
	if p.now != nil {
		now = p.now()
	}

	candidates := make([]resolvedCandidate, 0, 8)
	for id := range catalog.Models {
		model := catalog.Models[id]
		version, dated, preview, ok := pattern(id)
		if !ok || len(version) == 0 {
			continue
		}
		if !model.IsTextModel() {
			continue
		}
		if opts.requireToolCall && !model.ToolCallAllowed() {
			continue
		}
		release, hasRelease := model.ReleaseTime()
		if !hasRelease {
			continue
		}
		if release.After(now.Add(resolverMaxFutureSkew)) {
			continue
		}
		if opts.stabilityWindow > 0 && now.Sub(release) < opts.stabilityWindow {
			continue
		}
		if p.pricing == nil || !p.pricing.HasPositivePricing(id) {
			continue
		}
		candidates = append(candidates, resolvedCandidate{
			id: id, version: version, dated: dated, preview: preview, release: release,
		})
	}
	if len(candidates) == 0 {
		return resolvedCandidate{}, false
	}

	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if cmp := compareVersionSegments(a.version, b.version); cmp != 0 {
			return cmp > 0
		}
		if a.preview != b.preview {
			return !a.preview // 同版本 stable 优先
		}
		if !a.release.Equal(b.release) {
			return a.release.After(b.release)
		}
		if opts.preferDated && (a.dated != "") != (b.dated != "") {
			return a.dated != ""
		}
		return a.id < b.id
	})
	return candidates[0], true
}
