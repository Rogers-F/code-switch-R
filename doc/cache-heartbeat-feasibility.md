# Cache Heartbeat Feature Feasibility Analysis

## Executive Summary

**Feature**: Cache Heartbeat - 通过定时发送心跳请求保持 Anthropic Prompt Caching TTL，节省缓存重建费用

**Conclusion**: **技术可行**，核心假设已被官方文档证实

> "This lifetime is refreshed each time the cached content is used."
> — [Anthropic Prompt Caching Documentation](https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching)

---

## 1. Research Findings (Official Documentation)

### 1.1 TTL Refresh Mechanism (CONFIRMED)

**Key Discovery**: Cache TTL **会被刷新**，每次缓存命中（cache_read）时重置计时器

| TTL Option | Duration | Write Cost | Read Cost | Beta Header Required |
|------------|----------|------------|-----------|---------------------|
| Default | 5 minutes | 1.25× input | 0.1× input | No |
| Extended | 1 hour | 2× input | 0.1× input | Yes (`anthropic-beta: extended-cache-ttl-2025-04-11`) |

### 1.2 Model Support for 1-Hour TTL

**NOT Supported**:
- Claude 3.7 Sonnet
- Claude 3.5 Sonnet v2
- Claude 3.5 Sonnet
- Claude 3 Opus

**Supported**:
- Claude Sonnet 4
- Claude Opus 4
- Claude 3.5 Haiku
- Other newer models

### 1.3 Cache Control Format

```json
{
  "system": [
    {
      "type": "text",
      "text": "Your system prompt here...",
      "cache_control": {
        "type": "ephemeral",
        "ttl": "5m"  // or "1h"
      }
    }
  ]
}
```

### 1.4 Minimum Token Requirements

- Minimum cacheable content: **1,024 tokens** (varies by model)
- Maximum cache breakpoints per request: **4**

### 1.5 Cache Key Binding (CRITICAL)

**缓存是按「内容 + 模型」绑定的**，不能跨模型复用：

```
Cache Key = hash(system_prompt + messages_prefix) + model_id
```

| 场景 | 能否命中缓存 |
|------|-------------|
| 工作用 sonnet，心跳用 sonnet | ✅ 命中 |
| 工作用 sonnet，心跳用 haiku | ❌ 不命中（不同缓存分区） |
| 工作用 opus，心跳用 sonnet | ❌ 不命中 |

**重要限制**：不能用便宜的 haiku 模型来保活 sonnet 的缓存，必须使用相同模型。

### 1.6 Multi-Platform Support Analysis

#### 1.6.1 Claude (Anthropic) - Primary Target

| 特性 | 详情 |
|------|------|
| 缓存类型 | 显式 `cache_control` 标记 |
| TTL | 5 分钟（默认）/ 1 小时（扩展） |
| TTL 刷新 | ✅ 每次 `cache_read` 自动刷新 |
| 心跳机制 | 发送最小化请求触发 `cache_read` |
| 心跳成本 | 消耗 `cache_read` tokens（0.1× 输入价格） |

#### 1.6.2 OpenAI (Codex) - Not Applicable

| 特性 | 详情 |
|------|------|
| 缓存类型 | **完全自动**，无需代码改动 |
| TTL | 5-10 分钟（默认），GPT-5.1/4.1 支持 24 小时 |
| TTL 刷新 | ❌ 用户无法控制 |
| 心跳机制 | **不可行也不需要** |
| 原因 | OpenAI 自动管理缓存，基于前缀 hash 路由 |

> "Starting in October 2024, OpenAI began offering a 50% discount for input tokens that the model has seen recently, with no action required."
> — [OpenAI Prompt Caching](https://platform.openai.com/docs/guides/prompt-caching)

#### 1.6.3 Google Gemini - Future Support (Different Mechanism)

| 特性 | 详情 |
|------|------|
| 缓存类型 | 隐式（Gemini 2.5 自动）+ 显式（手动声明） |
| 默认 TTL | **60 分钟**（远长于 Anthropic） |
| TTL 刷新 | ✅ **支持！** 通过专用 API `caches.update()` |
| 心跳机制 | 调用 TTL 更新 API（不需要发送实际请求） |
| 心跳成本 | **极低**（仅 API 调用，不消耗 token） |
| 存储计费 | 按存储时间计费（每百万 token/小时） |

**Gemini TTL 刷新示例**：
```python
# 直接更新 TTL，无需发送请求
client.caches.update(
    name="caches/xxx",
    ttl="3600s"  # 延长 1 小时
)
```

> "You can update the TTL using `client.caches.update()` with a `ttl` parameter."
> — [Google Gemini Context Caching](https://ai.google.dev/gemini-api/docs/caching)

#### 1.6.4 Platform Comparison Summary

| 平台 | 心跳可行性 | 心跳成本 | 实现优先级 |
|------|-----------|----------|-----------|
| **Claude** | ✅ 需要且可行 | 中（token 费用） | 🥇 **P0 - 首要目标** |
| **Gemini** | ✅ 可行但机制不同 | 低（仅 API 调用） | 🥈 P1 - 后续支持 |
| **Codex** | ❌ 不可行 | N/A | ❌ 不支持 |

**结论**：
1. **Claude**: 心跳功能的主要目标，通过发送请求刷新 TTL
2. **Gemini**: 未来可支持，但需要单独实现（调用 `caches.update` API）
3. **Codex**: 完全自动管理，用户无法干预，不做心跳

---

## 2. Economic Analysis

### 2.1 Cost Comparison (Claude Sonnet 4, 50K tokens system prompt)

| Scenario | Formula | Cost |
|----------|---------|------|
| Cache Creation (5m) | 50K × $3.75/MTok × 1.25 | **$0.234** |
| Cache Creation (1h) | 50K × $3.75/MTok × 2 | **$0.375** |
| Cache Read (per request) | 50K × $3.75/MTok × 0.1 | **$0.019** |
| Heartbeat (per 4 min) | Same as cache read | **$0.019** |

### 2.2 Break-even Analysis

**5-minute TTL scenario**:
- User takes 10-minute break → cache expires → rebuild cost: $0.234
- With heartbeat: 2 heartbeats × $0.019 = $0.038
- **Savings**: $0.234 - $0.038 = **$0.196 per break**

**ROI Formula**:
```
Savings = (CacheCreateCost - HeartbeatCount × CacheReadCost) × BreakCount
```

**Break-even point**: ~12 heartbeats = 1 cache rebuild
- If user breaks < 48 minutes (12 × 4min), heartbeat is profitable
- If user breaks > 48 minutes, let cache expire and rebuild

### 2.3 Recommended Strategy

| User Behavior | Recommendation |
|---------------|----------------|
| Breaks < 5 min | No heartbeat needed (cache auto-refreshes) |
| Breaks 5-30 min | Heartbeat beneficial |
| Breaks 30-60 min | Consider 1-hour TTL instead |
| Breaks > 60 min | Let cache expire |

---

## 3. Technical Architecture

### 3.1 System Components

```
┌─────────────────────────────────────────────────────────────┐
│                    Code-Switch R                             │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────────┐    ┌─────────────────┐                │
│  │ HeartbeatService│◄───│TemplateStorage  │                │
│  │                 │    │                 │                │
│  │ - Start/Stop    │    │ - SaveTemplate  │                │
│  │ - SendHeartbeat │    │ - LoadTemplate  │                │
│  │ - MonitorCost   │    │ - DeleteTemplate│                │
│  └────────┬────────┘    └─────────────────┘                │
│           │                                                  │
│           ▼                                                  │
│  ┌─────────────────┐    ┌─────────────────┐                │
│  │  HTTP Client    │───▶│  Anthropic API  │                │
│  │ (Independent)   │    │                 │                │
│  └─────────────────┘    └─────────────────┘                │
│           │                                                  │
│           ▼                                                  │
│  ┌─────────────────┐                                        │
│  │ HeartbeatLog    │  (Isolated from request_log)          │
│  └─────────────────┘                                        │
└─────────────────────────────────────────────────────────────┘
```

### 3.2 Data Structures

```go
// HeartbeatTemplate - 用户保存的心跳模板
type HeartbeatTemplate struct {
    ID              string    `json:"id"`
    Name            string    `json:"name"`            // User-defined name
    ProviderID      int       `json:"provider_id"`     // Lock to specific provider
    ProviderKind    string    `json:"provider_kind"`   // "claude" only (Codex/Gemini not supported)

    // Cached request (minimal version)
    SystemPrompt    string    `json:"system_prompt"`   // The cacheable content

    // Model configuration (用户可选 + 智能推荐)
    Model           string    `json:"model"`           // e.g., "claude-sonnet-4-20250514"
    ModelSource     string    `json:"model_source"`    // "auto" | "manual" | "suggested"

    CacheControlTTL string    `json:"cache_control_ttl"` // "5m" or "1h"

    // Heartbeat config
    IntervalSec     int       `json:"interval_sec"`    // Default: 240 (4 minutes)
    MaxHeartbeats   int       `json:"max_heartbeats"`  // Safety limit, default: 15

    // State
    IsActive        bool      `json:"is_active"`
    LastHeartbeat   time.Time `json:"last_heartbeat"`
    HeartbeatCount  int       `json:"heartbeat_count"` // Today's count
    TotalCost       float64   `json:"total_cost"`      // Accumulated cost

    CreatedAt       time.Time `json:"created_at"`
    UpdatedAt       time.Time `json:"updated_at"`
}

// ModelSuggestion - 智能模型推荐
type ModelSuggestion struct {
    Model        string  `json:"model"`
    UsageCount   int     `json:"usage_count"`     // 最近 24h 使用次数
    CacheHitRate float64 `json:"cache_hit_rate"`  // 缓存命中率
    Recommended  bool    `json:"recommended"`     // 是否推荐
    Reason       string  `json:"reason"`          // 推荐原因
}

// HeartbeatLog - 心跳日志（独立于 request_log）
type HeartbeatLog struct {
    ID              int64     `json:"id"`
    TemplateID      string    `json:"template_id"`
    Provider        string    `json:"provider"`
    Model           string    `json:"model"`

    // Result
    Success         bool      `json:"success"`
    CacheReadTokens int       `json:"cache_read_tokens"`
    CacheCreateTokens int     `json:"cache_create_tokens"` // Should be 0 for successful heartbeat
    Cost            float64   `json:"cost"`
    ErrorMessage    string    `json:"error_message,omitempty"`

    CreatedAt       time.Time `json:"created_at"`
}

// HeartbeatStats - 统计信息
type HeartbeatStats struct {
    ActiveTemplates   int     `json:"active_templates"`
    TodayHeartbeats   int     `json:"today_heartbeats"`
    TodayCost         float64 `json:"today_cost"`
    EstimatedSavings  float64 `json:"estimated_savings"`  // Based on avoided cache rebuilds
    LastCheck         time.Time `json:"last_check"`
}
```

### 3.3 Model Selection Strategy

#### 3.3.1 Smart Model Suggestion

```go
// SuggestModels 从最近请求日志中智能推荐模型
func (h *HeartbeatService) SuggestModels() ([]ModelSuggestion, error) {
    // 查询最近 24 小时的 Claude 请求
    query := `
        SELECT model,
               COUNT(*) as usage_count,
               SUM(CASE WHEN cache_read_tokens > 0 THEN 1 ELSE 0 END) as cache_hits,
               COUNT(*) as total_requests
        FROM request_log
        WHERE platform = 'claude'
          AND created_at > datetime('now', '-24 hours')
          AND model != ''
        GROUP BY model
        ORDER BY usage_count DESC
        LIMIT 5
    `
    // 返回按使用频率排序的模型列表
    // 最常用且缓存命中率高的模型标记为 Recommended
}
```

#### 3.3.2 Model Selection UI Flow

```
用户创建心跳模板
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│  模型选择                                                    │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  智能推荐（基于最近 24h 使用记录）：                          │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ ● claude-sonnet-4-20250514    ⭐ 推荐               │   │
│  │   最近 87 次请求 | 缓存命中率 92%                    │   │
│  │                                                     │   │
│  │ ○ claude-opus-4-20250514                           │   │
│  │   最近 12 次请求 | 缓存命中率 78%                    │   │
│  │                                                     │   │
│  │ ○ claude-3-5-haiku-20241022                        │   │
│  │   最近 5 次请求 | 缓存命中率 100%                    │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  或手动输入：                                                │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ [                                                 ] │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  ⚠️ 重要：模型必须与实际使用的模型一致，否则无法命中缓存       │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

#### 3.3.3 Model Validation

```go
// ValidateModel 验证模型是否适合心跳
func (h *HeartbeatService) ValidateModel(model string) error {
    // 1. 检查是否为空
    if model == "" {
        return errors.New("model is required")
    }

    // 2. 检查是否为 Claude 模型（心跳仅支持 Claude）
    if !strings.Contains(model, "claude") {
        return errors.New("heartbeat only supports Claude models")
    }

    // 3. 检查 1-hour TTL 兼容性
    unsupported1h := []string{
        "claude-3-7-sonnet", "claude-3-5-sonnet-v2",
        "claude-3-5-sonnet", "claude-3-opus",
    }
    // 如果用户选择 1h TTL，检查模型是否支持

    return nil
}
```

### 3.4 HeartbeatService Interface

```go
type HeartbeatService struct {
    providerService  *ProviderService
    templates        map[string]*HeartbeatTemplate
    stopChans        map[string]chan struct{}
    mu               sync.RWMutex
    db               *sql.DB
}

// Core methods
func (h *HeartbeatService) SaveTemplate(tpl HeartbeatTemplate) error
func (h *HeartbeatService) DeleteTemplate(id string) error
func (h *HeartbeatService) StartHeartbeat(templateID string) error
func (h *HeartbeatService) StopHeartbeat(templateID string) error
func (h *HeartbeatService) TriggerNow(templateID string) (*HeartbeatLog, error)
func (h *HeartbeatService) GetStats() (*HeartbeatStats, error)
func (h *HeartbeatService) GetLogs(templateID string, limit int) ([]HeartbeatLog, error)

// Model suggestion
func (h *HeartbeatService) SuggestModels() ([]ModelSuggestion, error)
func (h *HeartbeatService) ValidateModel(model string) error

// Safety methods
func (h *HeartbeatService) shouldStop(log *HeartbeatLog) bool {
    // Stop if cache_creation > 0 (TTL expired, template invalid)
    return log.CacheCreateTokens > 0
}
```

### 3.5 Heartbeat Request Construction

> ⚠️ 心跳请求必须模仿 Claude Code 的真实请求格式，包含必需的 `betas`、`metadata` 等字段。

#### 3.5.1 Required Fields

| 参数 | 格式 | 必需 | 说明 |
|------|------|------|------|
| `model` | `string` | ✅ | 模型名称，必须与实际使用的模型一致 |
| `system` | `[{type, text, cache_control}]` | ✅ | 数组格式，包含 cache_control |
| `betas` | `["oauth-2025-04-20"]` | ✅ | Claude Code 使用的 beta 特性 |
| `metadata` | `{user_id: string}` | ✅ | 用户标识 |
| `max_tokens` | `number` | ✅ | 最小化输出成本，设为 1 |
| `stream` | `boolean` | ✅ | 使用流式响应 |
| `messages` | `array` | ✅ | 最小化用户消息 |

#### 3.5.2 Request Body Template

```json
{
  "model": "claude-sonnet-4-20250514",
  "max_tokens": 1,
  "stream": true,
  "betas": ["oauth-2025-04-20"],
  "system": [
    {
      "type": "text",
      "text": "<用户保存的 System Prompt>",
      "cache_control": {
        "type": "ephemeral",
        "ttl": "5m"
      }
    }
  ],
  "messages": [
    {
      "role": "user",
      "content": "."
    }
  ],
  "metadata": {
    "user_id": "heartbeat"
  }
}
```

#### 3.5.3 Go Implementation

```go
func (h *HeartbeatService) buildHeartbeatRequest(tpl *HeartbeatTemplate) ([]byte, error) {
    // 模仿 Claude Code 的真实请求格式
    req := map[string]interface{}{
        "model":      tpl.Model,
        "max_tokens": 1,               // 最小化输出成本
        "stream":     true,            // 使用流式响应
        "betas":      []string{"oauth-2025-04-20"},
        "system": []map[string]interface{}{
            {
                "type": "text",
                "text": tpl.SystemPrompt,
                "cache_control": map[string]string{
                    "type": "ephemeral",
                    "ttl":  tpl.CacheControlTTL, // "5m" or "1h"
                },
            },
        },
        "messages": []map[string]interface{}{
            {
                "role":    "user",
                "content": ".",  // 最小用户消息
            },
        },
        "metadata": map[string]string{
            "user_id": "heartbeat",  // 标识心跳请求
        },
    }
    return json.Marshal(req)
}
```

#### 3.5.4 Request Headers

```go
headers := map[string]string{
    "Content-Type":      "application/json",
    "x-api-key":         provider.APIKey,
    "anthropic-version": "2023-06-01",
    "anthropic-beta":    "oauth-2025-04-20",  // 与 betas 字段对应
}
```

---

## 4. Implementation Plan

### Phase 1: Core Infrastructure (3-4 days)

#### 4.1 Backend

1. **Create HeartbeatService** (`services/heartbeatservice.go`)
   - Template CRUD operations
   - Background heartbeat scheduler
   - Cost tracking and logging

2. **Create Database Tables**
   ```sql
   CREATE TABLE heartbeat_template (
       id TEXT PRIMARY KEY,
       name TEXT NOT NULL,
       provider_id INTEGER,
       provider_kind TEXT,
       system_prompt TEXT,
       model TEXT,
       cache_control_ttl TEXT DEFAULT '5m',
       interval_sec INTEGER DEFAULT 240,
       max_heartbeats INTEGER DEFAULT 15,
       is_active INTEGER DEFAULT 0,
       last_heartbeat DATETIME,
       heartbeat_count INTEGER DEFAULT 0,
       total_cost REAL DEFAULT 0,
       created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
       updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
   );

   CREATE TABLE heartbeat_log (
       id INTEGER PRIMARY KEY AUTOINCREMENT,
       template_id TEXT,
       provider TEXT,
       model TEXT,
       success INTEGER,
       cache_read_tokens INTEGER,
       cache_create_tokens INTEGER,
       cost REAL,
       error_message TEXT,
       created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
       FOREIGN KEY (template_id) REFERENCES heartbeat_template(id)
   );
   ```

3. **Isolation from Main Proxy**
   - Use independent HTTP client
   - Skip blacklist/failover logic
   - Separate logging table

#### 4.2 Frontend

1. **HeartbeatPanel Component** (`frontend/src/components/Heartbeat/Index.vue`)
   - Stats cards (status, active templates, today's heartbeats, estimated savings)
   - Template list with enable/disable toggles
   - Manual trigger button
   - Logs viewer

2. **TemplateForm Component** (`frontend/src/components/Heartbeat/TemplateForm.vue`)
   - System prompt input (or paste from clipboard)
   - Model selection
   - TTL selection (5m/1h)
   - Interval configuration
   - Provider lock selection

### Phase 2: Smart Features (2-3 days)

1. **Auto-capture Template**
   - Option to auto-save the last request with `cache_read > 0`
   - User confirmation before saving

2. **Cost Protection**
   - Auto-stop if `cache_create > 0` detected
   - Daily cost limit setting
   - Alert when approaching limit

3. **Analytics**
   - Cache efficiency chart
   - Savings estimation over time
   - Heartbeat success rate

### Phase 3: Polish (1-2 days)

1. **i18n** - Add translations
2. **Dark mode** support
3. **Export/Import** templates
4. **Documentation** - User guide

---

## 5. UI Design

### 5.1 Main Panel Layout

```
┌─────────────────────────────────────────────────────────────┐
│  ♡ Cache Heartbeat                           [Refresh] [+]  │
│  Keep Anthropic Prompt Cache alive to save rebuild costs    │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐       │
│  │ Status   │ │ Active   │ │ Today's  │ │ Est.     │       │
│  │ ●Enabled │ │ Templates│ │ Beats    │ │ Savings  │       │
│  │          │ │    2     │ │   24     │ │  $1.23   │       │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘       │
│                                                             │
│  Last check: 2024-12-29 10:30:05                           │
├─────────────────────────────────────────────────────────────┤
│  Templates                                                  │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ 📄 Claude Code Default System     [ON]  [▶] [✎] [🗑]│   │
│  │    Provider: Anthropic | TTL: 5m | Every 4min       │   │
│  │    Last: 2min ago | Today: 12 beats | Cost: $0.23   │   │
│  └─────────────────────────────────────────────────────┘   │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ 📄 Project Analysis Prompt        [OFF] [▶] [✎] [🗑]│   │
│  │    Provider: OpenRouter | TTL: 1h | Every 30min     │   │
│  │    Last: — | Today: 0 beats | Cost: $0.00           │   │
│  └─────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

### 5.2 Template Form

```
┌─────────────────────────────────────────────────────────────┐
│  Create Heartbeat Template                           [×]    │
├─────────────────────────────────────────────────────────────┤
│  Name: [________________________]                           │
│                                                             │
│  Provider: [▼ Select Provider           ]                   │
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  Model Selection                                    │   │
│  ├─────────────────────────────────────────────────────┤   │
│  │  智能推荐（基于最近 24h 使用记录）：                  │   │
│  │                                                     │   │
│  │  ● claude-sonnet-4-20250514           ⭐ 推荐       │   │
│  │    87 次请求 | 缓存命中率 92%                       │   │
│  │                                                     │   │
│  │  ○ claude-opus-4-20250514                          │   │
│  │    12 次请求 | 缓存命中率 78%                       │   │
│  │                                                     │   │
│  │  ○ claude-3-5-haiku-20241022                       │   │
│  │    5 次请求 | 缓存命中率 100%                       │   │
│  │                                                     │   │
│  │  ────────────────────────────────────              │   │
│  │  ○ 手动输入: [_________________________]           │   │
│  │                                                     │   │
│  │  ⚠️ 模型必须与实际使用的模型一致，否则无法命中缓存   │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  System Prompt:                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ You are a helpful coding assistant...               │   │
│  │ ...                                                 │   │
│  │                                                     │   │
│  └─────────────────────────────────────────────────────┘   │
│  [Paste from Clipboard] [Capture from Last Request]         │
│                                                             │
│  TTL:      (●) 5 minutes  ( ) 1 hour                       │
│  Interval: [240] seconds (recommended: 240 for 5m TTL)      │
│  Max daily heartbeats: [15]                                 │
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  ⚠️ Experimental Feature                            │   │
│  │  • 心跳功能仅支持 Claude/Anthropic API               │   │
│  │  • Codex/Gemini 有不同的缓存机制，无需心跳           │   │
│  │  • 心跳会产生 cache_read 费用（约 $0.02/次）         │   │
│  │  • 检测到 cache_create 时自动停止（防止白烧钱）       │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│                              [Cancel] [Save Template]       │
└─────────────────────────────────────────────────────────────┘
```

---

## 6. Risk Mitigation

### 6.1 Technical Risks

| Risk | Mitigation |
|------|------------|
| Heartbeat triggers cache_creation instead of cache_read | Auto-stop mechanism + alert |
| Provider rate limiting | Configurable interval + jitter |
| Network failure during heartbeat | Retry with exponential backoff (max 2 retries) |
| Template becomes stale (system prompt changed) | Manual invalidation + usage pattern detection |

### 6.2 Product Risks

| Risk | Mitigation |
|------|------------|
| User expects "session" preservation | Clear UI language: "Cache Object" not "Session" |
| Hidden costs accumulate | Daily cost display + configurable limit |
| Feature doesn't work with all providers | Provider compatibility indicator |

### 6.3 Privacy Considerations

| Concern | Solution |
|---------|----------|
| Storing user prompts | Explicit user consent required |
| Prompt content in logs | Only store token counts, not content |
| Export contains sensitive data | Warning before export |

---

## 7. Success Metrics

### 7.1 Feature Adoption
- Number of active templates
- DAU using heartbeat feature

### 7.2 Cost Efficiency
- Total savings (avoided cache rebuilds)
- Heartbeat ROI ratio (savings / heartbeat cost)

### 7.3 Reliability
- Heartbeat success rate
- Auto-stop trigger rate (lower is better)

---

## 8. Open Questions

1. **Claude Code CLI behavior**: Does it send consistent system prompts? Need to analyze real requests.

2. **Multi-provider caching**: If user switches provider, can the same template work?

3. **1-hour TTL availability**: When will it be available for Claude Sonnet 4?

4. **Rate limits**: What's the minimum safe interval between heartbeats?

---

## 9. Recommendation

**Proceed with implementation** as an **experimental/advanced feature** with:

1. ✅ Clear "Experimental" labeling
2. ✅ Explicit user consent for template storage
3. ✅ Auto-stop safety mechanism
4. ✅ Cost visibility and limits
5. ✅ Isolated from main proxy logic

**Estimated Development Time**: 6-9 days total

---

## 10. Detailed Implementation Plan (Final)

> 本节基于与 Codex 的多轮辩论结论，包含完整的状态机设计、数据库 Schema、安全机制和实施顺序。

### 10.1 Product Decisions (Final)

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 首次 `cache_create` | **(A) 自动预热** | 更符合"自动化"心跳特性，减少用户操作 |
| 运行中再次 `cache_create` | **(B) 停机并提示用户** | 更安全，防止失控成本，用户确认后才重建 |

### 10.2 State Machine (5 States)

#### 10.2.1 State Definitions

| State | Description | `needs_user_action` |
|-------|-------------|---------------------|
| `stopped` | 未运行（新建/手动停止/被系统停止后） | 0 |
| `warming_up` | 预热阶段（允许首次 `cache_create`） | 0 |
| `running` | 稳定保活阶段（期望 `cache_read>0 && cache_create==0`） | 0 |
| `blocked_expired` | 缓存过期风险或已触发二次 `cache_create`，需用户确认 | 1 |
| `blocked_safety` | 安全停机（成本/次数/错误/不支持缓存等） | 0 |

#### 10.2.2 State Transitions

```
                    ┌─────────────────────────────────────────────────────────────┐
                    │                        START                                │
                    └─────────────────────────────────────────────────────────────┘
                                              │
                                              ▼
┌──────────────────────────────────────────────────────────────────────────────────┐
│                              stopped                                              │
│  (新建模板、手动停止、被系统停止后的状态)                                          │
└──────────────────────────────────────────────────────────────────────────────────┘
          │                                                           ▲
          │ [User: Start]                                             │
          │ 校验通过                                                   │
          ▼                                                           │
┌──────────────────────────────────────────────────────────────────────────────────┐
│                            warming_up                                            │
│  预热阶段：允许首次 cache_create，等待 cache_read 确认缓存命中                    │
├──────────────────────────────────────────────────────────────────────────────────┤
│  WarmupResult:                                                                   │
│  • cache_read>0 && cache_create==0 → running                                     │
│  • cache_create>0 (首次) → warmup_cache_create_count++, 继续 warming_up           │
│  • cache_create>0 (二次, 无 cache_read) → blocked_expired                         │
│  • cache_create==0 && cache_read==0 → blocked_safety ("无缓存计量")               │
└──────────────────────────────────────────────────────────────────────────────────┘
          │                                                           │
          │ [cache_read>0]                                            │ [二次 cache_create]
          ▼                                                           ▼
┌─────────────────────────────────────────┐   ┌─────────────────────────────────────┐
│              running                     │   │          blocked_expired            │
│  稳定运行：每次心跳期望 cache_read>0      │   │  缓存过期，需用户确认是否重建        │
├─────────────────────────────────────────┤   ├─────────────────────────────────────┤
│  HeartbeatResult:                        │   │  • User: RewarmConfirm → warming_up │
│  • cache_read>0 → 继续 running           │   │  • User: Stop → stopped             │
│  • cache_create>0 → blocked_expired      │   └─────────────────────────────────────┘
│  • no metrics → blocked_safety           │
│                                          │
│  PrecheckTTLExpired:                     │
│  • now-last_cache_read > ttl             │
│    → blocked_expired (不发请求)          │
└─────────────────────────────────────────┘
          │
          │ [成本/次数/错误阈值]
          ▼
┌──────────────────────────────────────────────────────────────────────────────────┐
│                          blocked_safety                                          │
│  安全停机：成本超限/次数超限/连续失败/Provider 失效                               │
├──────────────────────────────────────────────────────────────────────────────────┤
│  • User: Stop → stopped                                                          │
│  • User: Start (修复问题后) → warming_up                                          │
└──────────────────────────────────────────────────────────────────────────────────┘
```

#### 10.2.3 Transition Rules (Complete)

| From | To | Trigger | Condition | Action |
|------|----|---------|-----------| -------|
| `stopped` | `warming_up` | `Start` | 校验通过 | 清零计数，启动 runner |
| `warming_up` | `running` | `WarmupResult` | `cache_read>0 && cache_create==0` | 更新 `last_cache_read_at` |
| `warming_up` | `warming_up` | `WarmupResult` | `cache_create>0` 且首次 | `warmup_cache_create_count++` |
| `warming_up` | `blocked_expired` | `WarmupResult` | 二次 `cache_create>0` 无 `cache_read` | 停机提示 |
| `warming_up` | `blocked_safety` | `WarmupResult` | `cache_create==0 && cache_read==0` | 停机提示 |
| `running` | `running` | `HeartbeatResult` | `cache_read>0 && cache_create==0` | 更新时间戳 |
| `running` | `blocked_expired` | `PrecheckTTLExpired` | `now - last_cache_read > ttl` | 不发请求直接停机 |
| `running` | `blocked_expired` | `HeartbeatResult` | `cache_create>0` | 停机提示 |
| `running` | `blocked_safety` | `HeartbeatResult` | `no cache metrics` | 停机提示 |
| `*` | `blocked_safety` | `CostLimitHit` | 达到阈值 | 记录原因 |
| `*` | `blocked_safety` | `MaxBeatsHit` | 达到阈值 | 记录原因 |
| `*` | `blocked_safety` | `ConsecutiveFailuresHit` | 达到阈值 | 记录原因 |
| `blocked_expired` | `warming_up` | `RewarmConfirm` | 用户确认 | 清零计数重新预热 |
| `blocked_*` | `stopped` | `Stop` | — | 停止 runner |

### 10.3 Database Schema (Final SQL)

```sql
-- heartbeat_template 表
CREATE TABLE IF NOT EXISTS heartbeat_template (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,

  -- Provider 绑定
  provider_kind TEXT NOT NULL,        -- 固定 "claude"
  provider_id INTEGER NOT NULL,
  provider_name TEXT NOT NULL,

  -- 缓存内容
  system_prompt TEXT NOT NULL,
  model TEXT NOT NULL,
  model_source TEXT NOT NULL DEFAULT 'manual', -- manual|suggested

  -- 配置
  cache_control_ttl TEXT NOT NULL DEFAULT '5m', -- 5m|1h
  interval_sec INTEGER NOT NULL DEFAULT 240,

  -- 限额
  max_heartbeats_per_day INTEGER NOT NULL DEFAULT 15,
  max_cost_per_day_usd REAL NOT NULL DEFAULT 0, -- 0 表示不限制

  -- 状态机
  state TEXT NOT NULL DEFAULT 'stopped',
  stop_reason TEXT,
  needs_user_action INTEGER NOT NULL DEFAULT 0,

  -- 内部计数器
  warmup_cache_create_count INTEGER NOT NULL DEFAULT 0,
  consecutive_failures INTEGER NOT NULL DEFAULT 0,

  -- 时间戳
  last_heartbeat_at DATETIME,
  last_success_at DATETIME,
  last_cache_create_at DATETIME,
  last_cache_read_at DATETIME,

  -- 每日统计（本地时区）
  today_date TEXT, -- YYYY-MM-DD
  today_heartbeat_count INTEGER NOT NULL DEFAULT 0,
  today_cost_usd REAL NOT NULL DEFAULT 0,

  -- 累计统计
  total_heartbeat_count INTEGER NOT NULL DEFAULT 0,
  total_cost_usd REAL NOT NULL DEFAULT 0,

  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_heartbeat_template_state
  ON heartbeat_template(state);

CREATE INDEX IF NOT EXISTS idx_heartbeat_template_provider
  ON heartbeat_template(provider_kind, provider_name);

-- heartbeat_log 表
CREATE TABLE IF NOT EXISTS heartbeat_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  template_id TEXT NOT NULL,

  -- 请求信息
  request_type TEXT NOT NULL,         -- warmup|heartbeat|manual
  provider_kind TEXT NOT NULL,
  provider_name TEXT NOT NULL,
  model TEXT NOT NULL,

  -- 结果
  success INTEGER NOT NULL,
  http_status INTEGER,
  latency_ms INTEGER,

  -- Token 用量
  input_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  cache_create_tokens INTEGER NOT NULL DEFAULT 0,
  cache_read_tokens INTEGER NOT NULL DEFAULT 0,

  -- 成本
  cost_usd REAL NOT NULL DEFAULT 0,
  error_message TEXT,

  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_heartbeat_log_template_time
  ON heartbeat_log(template_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_heartbeat_log_created_at
  ON heartbeat_log(created_at DESC);
```

### 10.4 HeartbeatService Interface (Go)

```go
// services/heartbeatservice.go

package services

import (
    "net/http"
    "sync"
    "time"
)

type HeartbeatService struct {
    // 依赖
    providerService *ProviderService
    pricing         *modelpricing.Service
    client          *http.Client

    // Wails App（用于发送事件）
    app *application.App

    // 运行时
    mu      sync.RWMutex
    runners map[string]*heartbeatRunner // templateID -> runner
}

// 构造函数
func NewHeartbeatService(providerService *ProviderService) *HeartbeatService

// ===== 生命周期 =====
func (h *HeartbeatService) Start() error              // 创建表 + 恢复可恢复的 runner
func (h *HeartbeatService) Stop() error               // 停止所有 runner
func (h *HeartbeatService) SetApp(app *application.App) // 注入 Wails App

// ===== 模板 CRUD =====
func (h *HeartbeatService) ListTemplates() ([]HeartbeatTemplate, error)
func (h *HeartbeatService) GetTemplate(id string) (*HeartbeatTemplate, error)
func (h *HeartbeatService) UpsertTemplate(input HeartbeatTemplateUpsert) (*HeartbeatTemplate, error)
func (h *HeartbeatService) DeleteTemplate(id string) error

// ===== 运行控制 =====
func (h *HeartbeatService) StartHeartbeat(id string) error
func (h *HeartbeatService) StopHeartbeat(id string) error
func (h *HeartbeatService) RewarmHeartbeat(id string) error // blocked_expired → warming_up

// ===== 手动执行 =====
func (h *HeartbeatService) TriggerNow(id string) (*HeartbeatLog, error)

// ===== 查询 =====
func (h *HeartbeatService) GetStats() (*HeartbeatStats, error)
func (h *HeartbeatService) GetLogs(templateID string, limit int) ([]HeartbeatLog, error)
func (h *HeartbeatService) SuggestModels(providerName string) ([]ModelSuggestion, error)

// ===== 校验 =====
func (h *HeartbeatService) ValidateTemplate(input HeartbeatTemplateUpsert) error

// ===== 内部方法 =====
func (h *HeartbeatService) ensureTables() error
func (h *HeartbeatService) loadAndValidateTemplate(id string) (*HeartbeatTemplate, error)
func (h *HeartbeatService) resolveProvider(tpl *HeartbeatTemplate) (*Provider, error)
func (h *HeartbeatService) buildRequest(tpl *HeartbeatTemplate) ([]byte, error)
func (h *HeartbeatService) doBeat(ctx context.Context, tpl *HeartbeatTemplate, requestType string) (*HeartbeatLog, error)
func (h *HeartbeatService) evaluateAndTransition(tpl *HeartbeatTemplate, result *HeartbeatLog) error
```

### 10.5 Safety Mechanisms (Complete)

#### 10.5.1 Auto-Stop Triggers

| Trigger | Condition | New State | `stop_reason` |
|---------|-----------|-----------|---------------|
| TTL 预检 | `now - last_cache_read_at > ttl` | `blocked_expired` | `ttl_elapsed` |
| 二次创建 | `running` 状态收到 `cache_create>0` | `blocked_expired` | `cache_create_after_warm` |
| 预热循环 | `warming_up` 二次 `cache_create` 无 `cache_read` | `blocked_expired` | `warmup_cache_create_loop` |
| 无缓存计量 | `cache_create==0 && cache_read==0` | `blocked_safety` | `no_cache_metrics` |
| 每日次数 | `today_heartbeat_count >= max_heartbeats_per_day` | `blocked_safety` | `daily_count_limit` |
| 每日成本 | `today_cost_usd + next_cost > max_cost_per_day_usd` | `blocked_safety` | `daily_cost_limit` |
| 连续失败 | `consecutive_failures >= 3` | `blocked_safety` | `consecutive_failures` |
| 429 限流 | 连续 429 或 `Retry-After > 300s` | `blocked_safety` | `rate_limited` |
| Provider 失效 | provider 不存在/被禁用/APIKey 为空 | `blocked_safety` | `provider_invalid` |

#### 10.5.2 Safety Evaluation Function (Go)

```go
func shouldBlockExpired(tpl *HeartbeatTemplate, now time.Time, ttl time.Duration, result *HeartbeatLog) (bool, string) {
    // TTL 预检（最省钱）
    if tpl.State == "running" && tpl.LastCacheReadAt != nil {
        if now.Sub(*tpl.LastCacheReadAt) > ttl {
            return true, "ttl_elapsed"
        }
    }

    // 运行中收到 cache_create
    if tpl.State == "running" && result != nil && result.CacheCreateTokens > 0 {
        return true, "cache_create_after_warm"
    }

    // 预热循环重建
    if tpl.State == "warming_up" && result != nil && result.CacheCreateTokens > 0 {
        if tpl.WarmupCacheCreateCount >= 1 {
            return true, "warmup_cache_create_loop"
        }
    }

    return false, ""
}

func shouldBlockSafety(tpl *HeartbeatTemplate, result *HeartbeatLog) (bool, string) {
    // 无缓存计量（极危险：全价心跳）
    if result != nil && result.CacheCreateTokens == 0 && result.CacheReadTokens == 0 {
        return true, "no_cache_metrics"
    }

    // 其他阈值检查由调用方处理
    return false, ""
}
```

### 10.6 Frontend Components (Vue)

#### 10.6.1 Component Structure

```
frontend/src/components/Heartbeat/
├── Index.vue               # 页面容器（拉取 stats+templates、定时刷新）
├── TemplateFormDialog.vue  # 新建/编辑模板表单
├── TemplateCard.vue        # 单模板卡片（状态、开关、操作按钮）
├── LogsDrawer.vue          # 日志抽屉/弹窗
└── ExpiredConfirmModal.vue # blocked_expired 确认弹窗

frontend/src/services/
└── heartbeat.ts            # Wails RPC 封装
```

#### 10.6.2 Route & Navigation

```typescript
// frontend/src/router/index.ts
{
  path: '/heartbeat',
  name: 'heartbeat',
  component: () => import('../components/Heartbeat/Index.vue')
}
```

#### 10.6.3 i18n Keys (zh.json)

```json
{
  "sidebar": {
    "heartbeat": "缓存心跳"
  },
  "heartbeat": {
    "title": "缓存心跳",
    "subtitle": "保持 Anthropic Prompt Cache 存活，节省缓存重建费用",
    "experimental": "实验性功能",
    "stats": {
      "active": "活跃模板",
      "todayBeats": "今日心跳",
      "todayCost": "今日费用",
      "estimatedSavings": "预估节省"
    },
    "template": {
      "new": "新建模板",
      "edit": "编辑模板",
      "name": "模板名称",
      "provider": "供应商",
      "model": "模型",
      "modelSource": {
        "manual": "手动输入",
        "suggested": "智能推荐"
      },
      "systemPrompt": "System Prompt",
      "ttl": "缓存 TTL",
      "interval": "心跳间隔",
      "maxBeats": "每日次数上限",
      "maxCost": "每日成本上限"
    },
    "state": {
      "stopped": "已停止",
      "warming_up": "预热中",
      "running": "运行中",
      "blocked_expired": "已过期",
      "blocked_safety": "安全停止"
    },
    "stopReason": {
      "ttl_elapsed": "缓存 TTL 已过期",
      "cache_create_after_warm": "缓存已失效需重建",
      "warmup_cache_create_loop": "预热失败（循环重建）",
      "no_cache_metrics": "未检测到缓存计量",
      "daily_count_limit": "达到每日次数上限",
      "daily_cost_limit": "达到每日成本上限",
      "consecutive_failures": "连续失败过多",
      "rate_limited": "触发限流",
      "provider_invalid": "供应商无效"
    },
    "action": {
      "start": "启动",
      "stop": "停止",
      "triggerNow": "立即触发",
      "viewLogs": "查看日志",
      "rewarm": "重新预热"
    },
    "expiredConfirm": {
      "title": "缓存已过期",
      "message": "检测到缓存已过期，是否支付一次 cache_create 费用重新预热？",
      "confirm": "重新预热",
      "cancel": "保持停止"
    },
    "warning": {
      "modelMismatch": "模型必须与实际使用的模型一致，否则无法命中缓存",
      "costRisk": "心跳会产生 cache_read 费用（约 $0.02/次）"
    }
  }
}
```

### 10.7 API Endpoints (Wails RPC)

| Method | Parameters | Return | Description |
|--------|------------|--------|-------------|
| `ListTemplates` | — | `HeartbeatTemplate[]` | 列出所有模板 |
| `GetTemplate` | `id: string` | `HeartbeatTemplate` | 获取单个模板 |
| `UpsertTemplate` | `input: HeartbeatTemplateUpsert` | `HeartbeatTemplate` | 创建/更新模板 |
| `DeleteTemplate` | `id: string` | — | 删除模板 |
| `StartHeartbeat` | `id: string` | — | 启动心跳 |
| `StopHeartbeat` | `id: string` | — | 停止心跳 |
| `RewarmHeartbeat` | `id: string` | — | 重新预热（`blocked_expired` → `warming_up`） |
| `TriggerNow` | `id: string` | `HeartbeatLog` | 手动触发一次心跳 |
| `GetStats` | — | `HeartbeatStats` | 获取统计信息 |
| `GetLogs` | `templateID: string, limit: int` | `HeartbeatLog[]` | 获取心跳日志 |
| `SuggestModels` | `providerName: string` | `ModelSuggestion[]` | 智能推荐模型 |
| `ValidateTemplate` | `input: HeartbeatTemplateUpsert` | — | 验证模板配置 |

### 10.8 Wails Events

| Event Name | Payload | Description |
|------------|---------|-------------|
| `heartbeat:blocked` | `{templateId, state, stopReason, message, timestamp}` | 模板被阻止时通知前端 |
| `heartbeat:state_changed` | `{templateId, oldState, newState, timestamp}` | 状态变更通知 |
| `heartbeat:log_appended` | `{templateId, log: HeartbeatLog}` | 新日志追加通知 |

### 10.9 Implementation Order (Step by Step)

#### Phase 1: Backend Core (Day 1-2)

1. **新建** `services/heartbeatservice.go`
   - 实现状态机逻辑
   - 实现 HTTP 请求发送（独立 client）
   - 实现 usage 解析（复用 `providerrelay.go` 的解析逻辑）
   - 实现 DB 写入
   - 实现 runner 管理（goroutine + ticker + stopChan）
   - 实现事件发射

2. **修改** `main.go`
   - 构造 `heartbeatService := services.NewHeartbeatService(providerService)`
   - 在 `application.Options.Services` 中注册
   - 在 app 创建后调用 `heartbeatService.SetApp(app)`
   - 在 `app.OnShutdown` 中调用 `heartbeatService.Stop()`

#### Phase 2: Frontend Service (Day 2)

3. **新建** `frontend/src/services/heartbeat.ts`
   - 封装所有 Wails RPC 调用
   - 定义 TypeScript 类型

#### Phase 3: Frontend UI (Day 3-4)

4. **新建** `frontend/src/components/Heartbeat/Index.vue`
   - 页面容器
   - 统计卡片
   - 模板列表
   - 定时刷新
   - 事件订阅

5. **新建** `frontend/src/components/Heartbeat/TemplateFormDialog.vue`
   - Provider 选择
   - Model 选择（智能推荐 + 手动输入）
   - System Prompt 输入
   - TTL 选择
   - 间隔配置
   - 限额配置

6. **新建** `frontend/src/components/Heartbeat/TemplateCard.vue`
   - 状态显示
   - 开关控制
   - 操作按钮

7. **新建** `frontend/src/components/Heartbeat/LogsDrawer.vue`
   - 日志列表
   - Token 用量显示
   - 错误信息显示

8. **新建** `frontend/src/components/Heartbeat/ExpiredConfirmModal.vue`
   - 过期确认弹窗
   - 重新预热按钮

#### Phase 4: Integration (Day 4-5)

9. **修改** `frontend/src/router/index.ts`
   - 注册 `/heartbeat` 路由

10. **修改** `frontend/src/components/Sidebar.vue`（或导航组件）
    - 添加心跳入口

11. **修改** `frontend/src/locales/zh.json` 和 `en.json`
    - 添加所有 i18n 键

#### Phase 5: Testing & Polish (Day 5-6)

12. **测试**
    - 状态机转换测试
    - 安全机制测试
    - UI 交互测试

13. **文档**
    - 更新 CLAUDE.md
    - 添加用户指南

---

## References

- [Anthropic Prompt Caching Documentation](https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching)
- [Spring AI Anthropic Prompt Caching Blog](https://spring.io/blog/2025/10/27/spring-ai-anthropic-prompt-caching-blog/)
- [PromptHub: Prompt Caching Guide](https://www.prompthub.us/blog/prompt-caching-with-openai-anthropic-and-google-models)
- [OpenRouter Prompt Caching](https://openrouter.ai/docs/guides/best-practices/prompt-caching)

---

*Document created: 2024-12-29*
*Last updated: 2024-12-29*
*Author: Half open flowers*
*Status: Implementation Ready*

## Revision History

| Version | Date | Changes |
|---------|------|---------|
| 1.0 | 2024-12-29 | Initial draft |
| 1.1 | 2024-12-29 | Added: Cache key binding mechanism, multi-platform support analysis, smart model suggestion, model validation |
| 1.2 | 2024-12-29 | Updated: Detailed platform analysis for OpenAI (Codex) and Google Gemini based on web research. Gemini supports TTL refresh via API (future support possible). Codex uses automatic caching (no heartbeat needed). |
| 2.0 | 2024-12-29 | **Final Implementation Plan**: Added Section 10 with complete state machine (5 states), database schema, HeartbeatService interface, safety mechanisms, frontend components, API endpoints, and step-by-step implementation order. Based on multi-round debate with Codex. |
| 2.1 | 2024-12-30 | Updated: Section 3.5 Heartbeat Request Construction - Added required fields (`betas`, `metadata`, `stream`), request body template, and headers to match Claude Code's real request format. |
