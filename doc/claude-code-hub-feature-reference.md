# Claude Code Hub 功能借鉴清单

> 基于 [claude-code-hub](https://github.com/ding113/claude-code-hub) 项目分析，整理出适合 cc-r（Code-Switch R）借鉴的功能清单。
>
> **分析时间**: 2024-12-18
> **分析方法**: Claude + Codex + Gemini 多模型协作分析，3 轮辩论确认
> **适用场景**: 单用户桌面应用（Wails + Go + SQLite + Vue）

---

## 项目对比概览

| 维度 | claude-code-hub | cc-r |
|------|-----------------|------|
| 架构 | Next.js 15 + Hono + PostgreSQL + Redis | Wails 3 + Go + Gin + SQLite |
| 部署 | Web 服务（Docker） | 桌面应用 |
| 用户 | 多用户（团队） | 单用户（个人） |
| 核心特性 | Guard Pipeline、多维限流、Session 管理 | Level 分组调度、拉黑服务、模型映射 |

---

## 后端功能清单

### 必做（Must-Have）

#### 1. 熔断半开探测（Circuit Breaker Half-Open Probe）

**核心概念**
在"拉黑到期"后不立刻全量恢复，而进入半开状态，限制性放行少量探测请求；连续成功后恢复正常，失败则快速回到拉黑。

**与 cc-r 现有方案对比**
- cc-r 现有黑名单 ≈ Closed/Open 两态
- 到期恢复是"直接放开"，上游抖动时容易反复拉黑
- 半开能显著降低恢复期雪崩与抖动

**Go 实现要点（无需 Redis）**
```go
// provider 状态扩展
type ProviderStatus struct {
    State           string    // closed/open/half_open
    OpenUntil       time.Time // open 到期时间
    HalfOpenSuccess int       // 半开期成功计数
    HalfOpenInFlight int      // 半开期并发探测数（限制为 1）
}

// 调度逻辑
// - open: 直接跳过
// - half_open: 仅允许 1 个并发 probe，其余走备用 provider
// - probe 成功累计到阈值（如 2）→ closed
// - probe 失败 → open，按 L1-L5 升级/延长冷却
```

**优先级**: 必做
**预估复杂度**: 中

---

#### 2. 错误分类与计数策略（Error Classification & Accounting）

**核心概念**
将失败分为"可归因上游/线路"的错误与"客户端/输入不可重试"错误，决定是否计入失败次数、是否触发拉黑/熔断。

**与 cc-r 现有方案对比**
- 现有失败计数不区分错误类型
- 提示词过长、鉴权错误等"非线路问题"会误算到 provider 头上
- 导致误伤与错误切换

**Go 实现要点（无需 Redis）**
```go
// 错误类型枚举
type ErrorCategory string

const (
    ErrUpstream5xx     ErrorCategory = "upstream_5xx"      // 计入熔断
    ErrTimeout         ErrorCategory = "timeout"          // 计入熔断
    ErrNetwork         ErrorCategory = "network"          // 计入熔断
    ErrRateLimited429  ErrorCategory = "rate_limited"     // 触发冷却，可选计入
    ErrAuth401         ErrorCategory = "auth_error"       // 不计入
    ErrBadRequest4xx   ErrorCategory = "bad_request"      // 不计入
    ErrContentPolicy   ErrorCategory = "content_policy"   // 不计入
)

// 只对 Timeout/Network/5xx 计入熔断
// 对 4xx 不计入或单独统计
```

**优先级**: 必做
**预估复杂度**: 中

---

#### 3. 429 冷却与退避（Rate Limit Cooldown & Backoff）

**核心概念**
遇到 429/限流信号时对该 provider 进入短期冷却（尊重 `Retry-After` 或指数退避），调度时直接跳过，避免"失败即拉黑"的粗粒度处理。

**与 cc-r 现有方案对比**
- 黑名单适合"线路坏/持续失败"
- 429 是"暂时拥塞"，用冷却能更快恢复且更准确

**Go 实现要点（无需 Redis）**
```go
type Provider struct {
    // ... 现有字段
    RateLimitedUntil time.Time `json:"rateLimitedUntil,omitempty"`
}

// 转发返回 429 时
func handleRateLimited(provider *Provider, resp *http.Response) {
    retryAfter := resp.Header.Get("Retry-After")
    if retryAfter != "" {
        seconds, _ := strconv.Atoi(retryAfter)
        provider.RateLimitedUntil = time.Now().Add(time.Duration(seconds) * time.Second)
    } else {
        // 指数退避：默认 30s，最大 5min
        provider.RateLimitedUntil = time.Now().Add(30 * time.Second)
    }
}

// 调度时跳过冷却中的 provider
```

**优先级**: 必做
**预估复杂度**: 低-中

---

#### 4. 日志分页与筛选查询（Paginated Log Query）

**核心概念**
将日志查询改为分页与条件过滤，避免桌面端一次性加载大量日志导致卡顿。

**与 cc-r 现有方案对比**
- 当前全量加载在日志增长后会明显拖慢 UI
- 分页是桌面长期使用的基础设施

**Go 实现要点（无需 Redis）**
```go
type LogQueryParams struct {
    Page       int       `json:"page"`
    PageSize   int       `json:"pageSize"`
    Provider   string    `json:"provider,omitempty"`
    Model      string    `json:"model,omitempty"`
    HttpCode   int       `json:"httpCode,omitempty"`
    StartTime  time.Time `json:"startTime,omitempty"`
    EndTime    time.Time `json:"endTime,omitempty"`
    OnlyFailed bool      `json:"onlyFailed,omitempty"`
}

// SQLite 索引建议
// CREATE INDEX idx_request_log_created ON request_log(created_at);
// CREATE INDEX idx_request_log_provider ON request_log(provider);
// CREATE INDEX idx_request_log_http_code ON request_log(http_code);
```

**优先级**: 必做
**预估复杂度**: 中

---

#### 5. 轻量尝试链记录（Decision Trace / Attempt Chain JSON）

**核心概念**
把"本次尝试/切换/跳过的原因"结构化写入同一条请求日志的 JSON 字段，用于回放与排障；只在失败或发生切换时写入以控制体积。

**与 cc-r 现有方案对比**
- 现有 request_log 记录结果，控制台记录过程但不可查询
- 尝试链让"为什么会切换/为什么没选某个 provider"可回放、可展示

**Go 实现要点（无需 Redis）**
```go
// 在 request_log 增加 attempts_json 字段
type RequestLog struct {
    // ... 现有字段
    AttemptsJSON string `json:"attemptsJson,omitempty"` // TEXT/JSON
}

// JSON 结构
type DecisionTrace struct {
    Attempts []AttemptRecord `json:"attempts"`
    Skipped  []SkipRecord    `json:"skipped"`
}

type AttemptRecord struct {
    ProviderID   int64         `json:"providerId"`
    ProviderName string        `json:"providerName"`
    Level        int           `json:"level"`
    Order        int           `json:"order"`
    Result       string        `json:"result"` // success/failed
    ErrorCategory string       `json:"errorCategory,omitempty"`
    HttpCode     int           `json:"httpCode,omitempty"`
    Duration     float64       `json:"durationSec"`
    ErrorMsg     string        `json:"errorMsg,omitempty"`
}

type SkipRecord struct {
    ProviderID   int64  `json:"providerId"`
    ProviderName string `json:"providerName"`
    Reason       string `json:"reason"` // open/half_open/rate_limited/disabled/model_mismatch
}

// 仅在失败/发生 failover/debug 模式落盘
```

**优先级**: 必做
**预估复杂度**: 低-中

---

### 锦上添花（Nice-to-Have）

#### 6. 调度过滤统一化（Unified Provider Filtering）

**核心概念**
将"禁用/冷却/熔断/模型不匹配"等过滤集中成一套可复用逻辑，并为每个过滤决策产出可记录的原因。

**Go 实现要点**
```go
type FilterResult struct {
    Allowed bool
    Reason  string // 枚举：disabled/circuit_open/half_open_busy/rate_limited/model_mismatch
}

func FilterProvider(p *Provider, requestedModel string) FilterResult {
    if !p.Enabled {
        return FilterResult{false, "disabled"}
    }
    if p.State == "open" && time.Now().Before(p.OpenUntil) {
        return FilterResult{false, "circuit_open"}
    }
    // ... 其他过滤逻辑
    return FilterResult{true, ""}
}
```

**优先级**: 锦上添花
**预估复杂度**: 中

---

### 可选（Optional）

#### 7. 可选调度模式：一致性选择/平滑轮询

**核心概念**
在保留 Level+顺序默认行为前提下，提供可选的"更平滑但仍可预测"的策略（如一致性哈希或平滑加权轮询）。

**注意**: 单用户 + "选中后一直用"策略下，随机权重价值有限且降低可调试性。

**优先级**: 可选
**预估复杂度**: 中

---

#### 8. 预算控制：每日消费上限

**核心概念**
提供"自我预算"能力，用每日上限限制成本，超出后阻止请求或提示。

**注意**: 多维窗口（5h/周/月）对单用户可能过度设计；每日上限是性价比最高的预算特性。

**优先级**: 可选
**预估复杂度**: 中

---

## 前端功能清单

### 必做（Must-Have）

#### 1. 日志列表分页 + 过滤 + 详情（Paginated Logs with Filters & Details）

**核心概念**
日志页改为分页加载，支持过滤与点开详情；详情可展示尝试链 JSON（如有）。

**Vue 实现要点**
```vue
<template>
  <!-- 过滤栏 -->
  <div class="filter-bar">
    <select v-model="filters.provider">...</select>
    <input v-model="filters.model" placeholder="模型">
    <DatePicker v-model="filters.dateRange" />
    <button @click="loadLogs">查询</button>
  </div>

  <!-- 日志表格 -->
  <table>...</table>

  <!-- 分页 -->
  <Pagination :total="total" :page="page" @change="loadLogs" />

  <!-- 详情弹窗 -->
  <LogDetailModal v-if="selectedLog" :log="selectedLog" />
</template>
```

**优先级**: 必做
**预估复杂度**: 中

---

#### 2. Provider 状态面板（Provider Health Panel）

**核心概念**
展示每个 provider 的当前状态（closed/open/half_open/冷却中）、剩余冷却时间、最近错误与统计。

**设计建议**（参考 Gemini 分析）
- 状态指示器：绿色=正常，红色=拉黑，黄色=半开/冷却
- 显示剩余冷却时间（倒计时）
- 点击查看最近错误详情

**优先级**: 必做
**预估复杂度**: 中

---

#### 3. 指标卡片动画（Metric Cards with Animation）

**核心概念**（来自 Gemini 分析）
数字变化时使用 `requestAnimationFrame` 实现平滑过渡动画。

**Vue 实现要点**
```vue
<script setup>
import { ref, watch } from 'vue'

const displayValue = ref(0)
const targetValue = defineModel('value')

watch(targetValue, (newVal) => {
  const start = displayValue.value
  const end = newVal
  const duration = 300
  const startTime = performance.now()

  const animate = (currentTime) => {
    const elapsed = currentTime - startTime
    const progress = Math.min(elapsed / duration, 1)
    displayValue.value = start + (end - start) * progress
    if (progress < 1) requestAnimationFrame(animate)
  }
  requestAnimationFrame(animate)
})
</script>
```

**优先级**: 锦上添花
**预估复杂度**: 低

---

### 锦上添花（Nice-to-Have）

#### 4. 错误详情与可复制诊断包

**核心概念**
失败时提供统一错误详情页/弹窗，一键复制诊断信息（含 attempts JSON、错误分类、关键配置摘要）。

**优先级**: 锦上添花
**预估复杂度**: 低-中

---

#### 5. 统一页面 Section 组件

**核心概念**（来自 Gemini 分析）
标准化页面头部，统一样式与布局。

```vue
<template>
  <section class="mb-6">
    <div class="flex items-center justify-between mb-4">
      <div>
        <h2 class="text-2xl font-bold">{{ title }}</h2>
        <p v-if="description" class="text-sm text-gray-500">{{ description }}</p>
      </div>
      <slot name="actions" />
    </div>
    <slot />
  </section>
</template>
```

**优先级**: 锦上添花
**预估复杂度**: 低

---

### 可选（Optional）

#### 6. 进行中请求视图（In-flight Requests View）

**核心概念**
显示当前正在进行的请求/流式连接、使用的 provider、耗时与是否处于 half-open 探测。

**优先级**: 可选
**预估复杂度**: 中

---

#### 7. 个人统计页（Personal Analytics）

**核心概念**
按 provider 展示成功率、平均延迟、成本趋势，用于自我调优（替代"排行榜"）。

**优先级**: 可选
**预估复杂度**: 中-高

---

## 实施路线图建议

### 第一阶段：核心可靠性（1-2 周）

1. **熔断半开探测** - 在现有 blacklistservice.go 基础上改造
2. **错误分类** - 新增错误类型枚举与判断逻辑
3. **429 冷却** - 简单字段扩展
4. **尝试链记录** - request_log 表增加 JSON 字段

### 第二阶段：前端体验（1-2 周）

1. **日志分页** - 后端接口 + 前端组件
2. **Provider 状态面板** - 展示熔断/冷却状态

### 第三阶段：增强功能（可选）

1. 调度过滤统一化
2. 指标卡片动画
3. 错误诊断包
4. 个人统计页

---

## 不建议借鉴的功能

| 功能 | 原因 |
|------|------|
| 多用户排行榜 | 单用户场景无意义 |
| Guard Pipeline 框架 | 当前 Gin 中间件链已足够，引入抽象层收益有限 |
| 多维限流（5h/周/月） | 对单用户过度设计，每日上限即可 |
| Redis 依赖 | 桌面应用用 SQLite + 内存即可，无需分布式组件 |
| Session 管理 | cc-r 已是"选中后一直用"，显式 session 增量有限 |

---

## 参考资源

- [claude-code-hub 源码](https://github.com/ding113/claude-code-hub)
- [Guard Pipeline 实现](src/app/v1/_lib/proxy/guard-pipeline.ts)
- [Circuit Breaker 实现](src/lib/circuit-breaker.ts)
- [Rate Limit Service](src/lib/rate-limit/service.ts)
- [Session Tracker](src/lib/session-tracker.ts)
