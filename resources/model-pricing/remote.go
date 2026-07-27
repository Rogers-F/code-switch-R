package modelpricing

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"math"
	"regexp"
	"strings"
	"time"
)

// 远程模型目录(llm-metadata / models.dev 体系)的解析、校验与到价格条目的转换。
// 网络、调度与批次级校验在 services 层;这里只做数据形状与单条目合法性,
// 供运行时同步与内置种子共用同一套逻辑。

const (
	// MaxUSDPerMillionTokens 基础单价上限(USD/百万 token),超出视为脏数据。
	MaxUSDPerMillionTokens = 1000.0
	// MaxRemoteModelsPerProvider 单厂商模型数上限。
	MaxRemoteModelsPerProvider = 2000
	// remoteDateLayout release_date/last_updated 的日期格式(UTC 日历日)。
	remoteDateLayout = "2006-01-02"
)

// RemoteProviderIDs 同步的厂商固定 allowlist,与 key 前缀规则一一对应。
var RemoteProviderIDs = []string{
	"anthropic", "openai", "google", "deepseek", "alibaba", "moonshotai", "zhipuai",
}

// remoteKeyPrefixes 把厂商目录里的裸模型 id 映射为价格表既有的 key 约定
// (与 familyRules、overlay aliases 对齐)。
var remoteKeyPrefixes = map[string]string{
	"anthropic":  "",
	"openai":     "",
	"google":     "",
	"deepseek":   "",
	"alibaba":    "dashscope/",
	"moonshotai": "moonshot/",
	"zhipuai":    "zai/",
}

var remoteModelIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:\[\]-]{0,127}$`)

// RemoteEntry 是远程条目转换后的基础四价(USD/token),presence 用指针表达:
// nil = 源未提供(保持未知),显式 0 = 免费。
type RemoteEntry struct {
	Input      *float64
	Output     *float64
	CacheRead  *float64
	CacheWrite *float64
}

func (r RemoteEntry) hasPositiveBase() bool {
	return (r.Input != nil && *r.Input > 0) || (r.Output != nil && *r.Output > 0)
}

// RemoteCost 目录内价格,单位 USD/百万 token。
type RemoteCost struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	CacheRead  *float64 `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
}

// RemoteLimit 上下文与输出窗口。
type RemoteLimit struct {
	Context int64 `json:"context"`
	Output  int64 `json:"output"`
}

// RemoteModalities 输入输出模态。
type RemoteModalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

// RemoteModel 目录内单个模型条目。
type RemoteModel struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Family      string            `json:"family"`
	ReleaseDate string            `json:"release_date"`
	LastUpdated string            `json:"last_updated"`
	Reasoning   bool              `json:"reasoning"`
	ToolCall    *bool             `json:"tool_call"`
	Cost        *RemoteCost       `json:"cost"`
	Limit       *RemoteLimit      `json:"limit"`
	Modalities  *RemoteModalities `json:"modalities"`
}

// IsTextModel 判断文本入出能力;modalities 缺失视为纯文本(旧数据兼容)。
func (m *RemoteModel) IsTextModel() bool {
	if m == nil {
		return false
	}
	if m.Modalities == nil {
		return true
	}
	return containsFold(m.Modalities.Input, "text") && containsFold(m.Modalities.Output, "text")
}

// ToolCallAllowed 区分"缺失"与显式 false:缺失不排除,false 排除。
func (m *RemoteModel) ToolCallAllowed() bool {
	if m == nil {
		return false
	}
	return m.ToolCall == nil || *m.ToolCall
}

// ReleaseTime 解析 release_date(UTC);缺失或非法返回 ok=false。
func (m *RemoteModel) ReleaseTime() (time.Time, bool) {
	if m == nil {
		return time.Time{}, false
	}
	return ParseRemoteDate(m.ReleaseDate)
}

// ParseRemoteDate 解析目录日期字段。
func ParseRemoteDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(remoteDateLayout, s, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// RemoteCatalog 单厂商目录。
type RemoteCatalog struct {
	ID     string                 `json:"id"`
	Models map[string]RemoteModel `json:"models"`
	// DroppedModels 记录 Sanitize 阶段被剔除的非法条目数,供批次校验与状态展示。
	DroppedModels int `json:"-"`
}

// ParseRemoteCatalog 解析并清洗一份厂商目录。providerID 用于回显校验。
func ParseRemoteCatalog(providerID string, data []byte) (*RemoteCatalog, error) {
	var catalog RemoteCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("解析 %s 目录失败: %w", providerID, err)
	}
	if catalog.ID != "" && catalog.ID != providerID {
		return nil, fmt.Errorf("目录 id 回显不符: 期望 %s 实际 %s", providerID, catalog.ID)
	}
	if len(catalog.Models) == 0 {
		return nil, fmt.Errorf("%s 目录为空", providerID)
	}
	if len(catalog.Models) > MaxRemoteModelsPerProvider {
		return nil, fmt.Errorf("%s 目录模型数 %d 超过上限 %d", providerID, len(catalog.Models), MaxRemoteModelsPerProvider)
	}
	sanitizeCatalog(&catalog)
	if len(catalog.Models) == 0 {
		return nil, fmt.Errorf("%s 目录清洗后为空", providerID)
	}
	return &catalog, nil
}

// sanitizeCatalog 剔除单条目级非法数据:id 非法/键值不符、价格非有限或越界。
func sanitizeCatalog(catalog *RemoteCatalog) {
	for key, model := range catalog.Models {
		if !remoteModelIDPattern.MatchString(key) ||
			(model.ID != "" && model.ID != key) ||
			!costWithinBounds(model.Cost) {
			delete(catalog.Models, key)
			catalog.DroppedModels++
		}
	}
}

// costWithinBounds 校验价格字段有限、非负且不超上限;cost 缺失视为合法(仅目录用途)。
func costWithinBounds(cost *RemoteCost) bool {
	if cost == nil {
		return true
	}
	for _, v := range []*float64{cost.Input, cost.Output, cost.CacheRead, cost.CacheWrite} {
		if v == nil {
			continue
		}
		if math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 || *v > MaxUSDPerMillionTokens {
			return false
		}
	}
	return true
}

// ConvertCatalogs 把各厂商目录转换为价格表远程层(key 已按前缀规则对齐,单位 USD/token)。
// 无 cost 或 input/output 均非正的条目不产出价格(目录仍可供默认模型解析器使用)。
func ConvertCatalogs(catalogs map[string]*RemoteCatalog) map[string]RemoteEntry {
	out := make(map[string]RemoteEntry)
	for _, providerID := range sortedKeys(catalogs) {
		catalog := catalogs[providerID]
		if catalog == nil {
			continue
		}
		prefix, ok := remoteKeyPrefixes[providerID]
		if !ok {
			continue
		}
		for _, id := range sortedKeys(catalog.Models) {
			model := catalog.Models[id]
			if model.Cost == nil {
				continue
			}
			entry := RemoteEntry{
				Input:      perTokenPrice(model.Cost.Input),
				Output:     perTokenPrice(model.Cost.Output),
				CacheRead:  perTokenPrice(model.Cost.CacheRead),
				CacheWrite: perTokenPrice(model.Cost.CacheWrite),
			}
			if !entry.hasPositiveBase() {
				continue
			}
			out[prefix+id] = entry
		}
	}
	return out
}

// perTokenPrice 把 USD/百万 token 转为 USD/token,并做除法后的二次防御。
func perTokenPrice(v *float64) *float64 {
	if v == nil {
		return nil
	}
	perTok := *v / 1e6
	if math.IsNaN(perTok) || math.IsInf(perTok, 0) || perTok < 0 {
		return nil
	}
	return &perTok
}

// EmbeddedSeedCatalogs 返回随二进制内置的目录种子(离线首启即可解析默认模型并有价可查)。
// 种子异常属打包/生成器缺陷,必须留下诊断日志而非静默缺失。
func EmbeddedSeedCatalogs() map[string]*RemoteCatalog {
	out := make(map[string]*RemoteCatalog, len(RemoteProviderIDs))
	for _, providerID := range RemoteProviderIDs {
		data, err := fs.ReadFile(seedFS, "seed/"+providerID+".json")
		if err != nil {
			log.Printf("[ModelPricing] 内置种子缺失 %s: %v", providerID, err)
			continue
		}
		catalog, err := ParseRemoteCatalog(providerID, data)
		if err != nil {
			log.Printf("[ModelPricing] 内置种子解析失败 %s: %v", providerID, err)
			continue
		}
		out[providerID] = catalog
	}
	return out
}

func containsFold(values []string, target string) bool {
	for _, v := range values {
		if strings.EqualFold(strings.TrimSpace(v), target) {
			return true
		}
	}
	return false
}
