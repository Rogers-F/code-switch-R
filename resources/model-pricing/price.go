package modelpricing

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed model_prices_and_context_window.json
var pricingFile []byte

var (
	defaultOnce    sync.Once
	defaultService *Service
	defaultErr     error
	nameReplacer   = strings.NewReplacer("-", "", "_", "", ".", "", ":", "", "/", "", " ", "")
)

// Service 提供模型价格相关的计算能力。
type Service struct {
	pricingMap   map[string]*PricingEntry
	normalized   map[string]string
	ephemeral1h  map[string]float64
	longContexts map[string]LongContextPricing
}

// PricingEntry 映射 JSON 内的字段。
type PricingEntry struct {
	InputCostPerToken                   float64 `json:"input_cost_per_token"`
	OutputCostPerToken                  float64 `json:"output_cost_per_token"`
	OutputCostPerReasoningToken         float64 `json:"output_cost_per_reasoning_token"`
	CacheCreationInputTokenCost         float64 `json:"cache_creation_input_token_cost"`
	CacheCreationInputTokenCostAbove1Hr float64 `json:"cache_creation_input_token_cost_above_1hr"`
	CacheCreationInputTokenCostAbove200 float64 `json:"cache_creation_input_token_cost_above_200k_tokens"`
	CacheReadInputTokenCost             float64 `json:"cache_read_input_token_cost"`
	InputCostPerTokenAbove200k          float64 `json:"input_cost_per_token_above_200k_tokens"`
	InputCostPerTokenAbove128k          float64 `json:"input_cost_per_token_above_128k_tokens"`
	OutputCostPerTokenAbove200k         float64 `json:"output_cost_per_token_above_200k_tokens"`
}

// UsageSnapshot 描述一次请求的 token 用量。
type UsageSnapshot struct {
	InputTokens       int
	OutputTokens      int
	ReasoningTokens   int
	CacheCreateTokens int
	CacheReadTokens   int
	CacheCreation     *CacheCreationDetail
}

// CacheCreationDetail 细分缓存创建 tokens。
type CacheCreationDetail struct {
	Ephemeral5mTokens int
	Ephemeral1hTokens int
}

// CostBreakdown 表示一次费用计算的结果。
type CostBreakdown struct {
	InputCost       float64 `json:"input_cost"`
	OutputCost      float64 `json:"output_cost"`
	ReasoningCost   float64 `json:"reasoning_cost"`
	CacheCreateCost float64 `json:"cache_create_cost"`
	CacheReadCost   float64 `json:"cache_read_cost"`
	Ephemeral5mCost float64 `json:"ephemeral_5m_cost"`
	Ephemeral1hCost float64 `json:"ephemeral_1h_cost"`
	TotalCost       float64 `json:"total_cost"`
	HasPricing      bool    `json:"has_pricing"`
	IsLongContext   bool    `json:"is_long_context"`
}

// LongContextPricing 描述 1M 上下文模型的单价。
type LongContextPricing struct {
	Input  float64
	Output float64
}

// DefaultService 返回单例。
func DefaultService() (*Service, error) {
	defaultOnce.Do(func() {
		defaultService, defaultErr = NewService()
	})
	return defaultService, defaultErr
}

// NewService 从嵌入的 JSON 创建服务实例。
func NewService() (*Service, error) {
	raw := make(map[string]PricingEntry)
	if err := json.Unmarshal(pricingFile, &raw); err != nil {
		return nil, fmt.Errorf("parse pricing file: %w", err)
	}
	pricing := make(map[string]*PricingEntry, len(raw))
	normalized := make(map[string]string, len(raw))
	for key, entry := range raw {
		item := entry
		ensureCachePricing(&item)
		pricing[key] = &item
		norm := normalizeName(key)
		if _, exists := normalized[norm]; !exists {
			normalized[norm] = key
		}
	}
	return &Service{
		pricingMap:   pricing,
		normalized:   normalized,
		ephemeral1h:  buildEphemeral1hPricing(),
		longContexts: buildLongContextPricing(),
	}, nil
}

// CalculateCost 根据模型与 token 用量返回费用明细（美元）。
func (s *Service) CalculateCost(model string, usage UsageSnapshot) CostBreakdown {
	if s == nil || model == "" {
		return CostBreakdown{}
	}
	entry, hasPricing := s.getPricing(model)
	breakdown := CostBreakdown{HasPricing: hasPricing}
	if entry == nil && !strings.Contains(strings.ToLower(model), "[1m]") {
		return breakdown
	}
	longTier, useLong := s.longContextTier(model, usage)
	if entry == nil {
		entry = &PricingEntry{}
	}
	if useLong {
		breakdown.IsLongContext = true
		breakdown.InputCost = float64(usage.InputTokens) * longTier.Input
		breakdown.OutputCost = float64(usage.OutputTokens) * longTier.Output
	} else {
		breakdown.InputCost = float64(usage.InputTokens) * entry.InputCostPerToken
		breakdown.OutputCost = float64(usage.OutputTokens) * entry.OutputCostPerToken
	}
	// Reasoning tokens cost (for Gemini thinking models, Codex o1/o3, etc.)
	if usage.ReasoningTokens > 0 && entry.OutputCostPerReasoningToken > 0 {
		breakdown.ReasoningCost = float64(usage.ReasoningTokens) * entry.OutputCostPerReasoningToken
	}
	cacheCreateTokens, cache1hTokens := resolveCacheTokens(usage)
	cache5mCost := float64(cacheCreateTokens) * entry.CacheCreationInputTokenCost
	cache1hCost := float64(cache1hTokens) * s.getEphemeral1hPricing(model)
	breakdown.Ephemeral5mCost = cache5mCost
	breakdown.Ephemeral1hCost = cache1hCost
	breakdown.CacheCreateCost = cache5mCost + cache1hCost
	breakdown.CacheReadCost = float64(usage.CacheReadTokens) * entry.CacheReadInputTokenCost
	breakdown.TotalCost = breakdown.InputCost + breakdown.OutputCost + breakdown.ReasoningCost + breakdown.CacheCreateCost + breakdown.CacheReadCost
	if breakdown.TotalCost > 0 {
		breakdown.HasPricing = true
	}
	return breakdown
}

func (s *Service) getPricing(model string) (*PricingEntry, bool) {
	if model == "" {
		return nil, false
	}
	if entry, ok := s.pricingMap[model]; ok {
		return entry, true
	}
	if model == "gpt-5-codex" {
		if entry, ok := s.pricingMap["gpt-5"]; ok {
			return entry, true
		}
	}
	withoutRegion := stripRegionPrefix(model)
	if entry, ok := s.pricingMap[withoutRegion]; ok {
		return entry, true
	}
	withoutProvider := strings.TrimPrefix(withoutRegion, "anthropic.")
	if entry, ok := s.pricingMap[withoutProvider]; ok {
		return entry, true
	}
	normalizedTarget := normalizeName(model)
	if key, ok := s.normalized[normalizedTarget]; ok {
		return s.pricingMap[key], true
	}
	for key, entry := range s.pricingMap {
		normKey := normalizeName(key)
		if strings.Contains(normKey, normalizedTarget) || strings.Contains(normalizedTarget, normKey) {
			return entry, true
		}
	}
	return nil, false
}

func (s *Service) longContextTier(model string, usage UsageSnapshot) (LongContextPricing, bool) {
	totalInput := usage.InputTokens + usage.CacheCreateTokens + usage.CacheReadTokens
	if strings.Contains(strings.ToLower(model), "[1m]") && totalInput > 200000 && len(s.longContexts) > 0 {
		if tier, ok := s.longContexts[model]; ok {
			return tier, true
		}
		for _, tier := range s.longContexts {
			return tier, true
		}
	}
	return LongContextPricing{}, false
}

func (s *Service) getEphemeral1hPricing(model string) float64 {
	if price, ok := s.ephemeral1h[model]; ok {
		return price
	}
	name := strings.ToLower(model)
	switch {
	case strings.Contains(name, "opus"):
		return 0.00003
	case strings.Contains(name, "sonnet"):
		return 0.000006
	case strings.Contains(name, "haiku"):
		return 0.0000016
	default:
		return 0
	}
}

func ensureCachePricing(entry *PricingEntry) {
	if entry == nil {
		return
	}
	if entry.CacheCreationInputTokenCost == 0 && entry.InputCostPerToken > 0 {
		entry.CacheCreationInputTokenCost = entry.InputCostPerToken * 1.25
	}
	if entry.CacheReadInputTokenCost == 0 && entry.InputCostPerToken > 0 {
		entry.CacheReadInputTokenCost = entry.InputCostPerToken * 0.1
	}
}

func stripRegionPrefix(name string) string {
	for _, prefix := range []string{"us.", "eu.", "apac."} {
		if strings.HasPrefix(strings.ToLower(name), prefix) {
			return name[len(prefix):]
		}
	}
	return name
}

func normalizeName(name string) string {
	return nameReplacer.Replace(strings.ToLower(name))
}

func resolveCacheTokens(usage UsageSnapshot) (fiveMin int, oneHour int) {
	if usage.CacheCreation == nil {
		return usage.CacheCreateTokens, 0
	}
	five := usage.CacheCreation.Ephemeral5mTokens
	one := usage.CacheCreation.Ephemeral1hTokens
	remaining := usage.CacheCreateTokens - five - one
	if remaining > 0 {
		five += remaining
	}
	if five < 0 {
		five = 0
	}
	if one < 0 {
		one = 0
	}
	return five, one
}

func buildEphemeral1hPricing() map[string]float64 {
	return map[string]float64{
		"claude-opus-4-5":            0.00001,
		"claude-opus-4-5-20251101":   0.00001,
		"claude-opus-4-5-20250929":   0.00001,
		"claude-opus-4-1":            0.00003,
		"claude-opus-4-1-20250805":   0.00003,
		"claude-opus-4":              0.00003,
		"claude-opus-4-20250514":     0.00003,
		"claude-3-opus":              0.00003,
		"claude-3-opus-latest":       0.00003,
		"claude-3-opus-20240229":     0.00003,
		"claude-3-5-sonnet":          0.000006,
		"claude-3-5-sonnet-latest":   0.000006,
		"claude-3-5-sonnet-20241022": 0.000006,
		"claude-3-5-sonnet-20240620": 0.000006,
		"claude-3-sonnet":            0.000006,
		"claude-3-sonnet-20240307":   0.000006,
		"claude-sonnet-3":            0.000006,
		"claude-sonnet-3-5":          0.000006,
		"claude-sonnet-3-7":          0.000006,
		"claude-sonnet-4":            0.000006,
		"claude-sonnet-4-20250514":   0.000006,
		"claude-3-5-haiku":           0.0000016,
		"claude-3-5-haiku-latest":    0.0000016,
		"claude-3-5-haiku-20241022":  0.0000016,
		"claude-3-haiku":             0.0000016,
		"claude-3-haiku-20240307":    0.0000016,
		"claude-haiku-3":             0.0000016,
		"claude-haiku-3-5":           0.0000016,
		"claude-haiku-4-5":           0.000002,
		"claude-haiku-4-5-20251001":  0.000002,
	}
}

func buildLongContextPricing() map[string]LongContextPricing {
	return map[string]LongContextPricing{
		"claude-sonnet-4-20250514[1m]": {
			Input:  0.000006,
			Output: 0.0000225,
		},
	}
}
