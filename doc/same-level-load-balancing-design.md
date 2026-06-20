# 同 Level 内负载均衡设计方案

> **状态**: 设计完成，待实施
> **设计时间**: 2024-12-18
> **设计方法**: Claude + Codex 3 轮辩论确认

---

## 背景

### 当前行为

cc-r 的 Level 分组调度机制：
- Level 1-10 优先级分组
- **跨 Level**: 确定性降级（Level 1 全失败 → Level 2 → Level 3...）
- **同 Level 内**: 按数组顺序依次尝试（第一个失败 → 第二个 → ...）

### 用户需求

在同一 Level 内增加负载均衡能力：
- 保留跨 Level 的确定性降级（故障容错）
- 同 Level 内支持轮询或权重随机（负载均衡）

---

## 设计方案

### 三种调度策略

| 策略 | 说明 | 适用场景 |
|------|------|----------|
| `sequential` | 顺序尝试（默认） | 明确优先级，确定性调试 |
| `round_robin` | 轮询 | 均匀分配流量 |
| `weighted_random` | 权重随机 | 按偏好分配流量 |

### 调度流程

```
请求进入
    ↓
┌─────────────────────────────────────────────────┐
│ 外层循环：按 Level 升序遍历（1 → 2 → 3...）     │
│   ↓                                             │
│ ┌─────────────────────────────────────────────┐ │
│ │ 内层：orderProvidersInLevel(kind, level)    │ │
│ │   ├── sequential: 原数组顺序                │ │
│ │   ├── round_robin: rotate 起点              │ │
│ │   └── weighted_random: 加权无放回抽样       │ │
│ └─────────────────────────────────────────────┘ │
│   ↓                                             │
│ 依次尝试排序后的 provider 列表                  │
│   ↓                                             │
│ 全部失败 → 进入下一 Level                       │
└─────────────────────────────────────────────────┘
    ↓
所有 Level 全部失败 → 502
```

---

## 配置设计

### 配置位置

**决策**: 放在 Provider 配置文件的 envelope 中

**理由**:
- 语义直观：调度策略与 provider 列表同源
- 可移植性：备份/迁移时一起带走
- 不依赖数据库

**风险与对策**:
- 风险：`SaveProviders()` 可能覆盖 `schedulingMode`
- 对策：改造为 `LoadProviderConfig()` / `SaveProviderConfig()`，保护 envelope 字段

### 配置文件结构

```json
{
  "schedulingMode": "weighted_random",
  "providers": [
    {
      "id": 1,
      "name": "Provider A",
      "level": 1,
      "weight": 0,
      ...
    },
    {
      "id": 2,
      "name": "Provider B",
      "level": 1,
      "weight": 0,
      ...
    }
  ]
}
```

### 字段说明

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `schedulingMode` | string | `"sequential"` | 同 Level 调度策略 |
| `weight` | int | 0 | 显式权重（0 = 使用位置权重） |

---

## 权重计算

### 第一期：基于位置的隐式权重（推荐）

**零配置方案**：复用现有拖拽排序，位置即权重。

```go
// 同 Level 内 Provider 的相对位置 → 隐式权重
func positionWeight(index int) int {
    weights := []int{10, 8, 5, 3, 1}
    if index < len(weights) {
        return weights[index]
    }
    return 1
}
```

**概率分布示例**（4 个 Provider）：

| 位置 | 权重 | 概率 |
|------|------|------|
| 第 1 个 | 10 | 38.5% |
| 第 2 个 | 8 | 30.8% |
| 第 3 个 | 5 | 19.2% |
| 第 4 个 | 3 | 11.5% |

### 第二期：显式权重覆盖（可选）

**Hybrid 方案**：
- `weight: 0` 或缺失 → 使用位置权重
- `weight: > 0` → 使用显式值

**注意**：需要 UI 配合，否则前端保存时可能丢失 `weight` 字段。

---

## 核心实现

### 数据结构扩展

```go
// Provider 结构体新增字段
type Provider struct {
    // ... 现有字段 ...

    // 权重（仅权重随机模式使用）
    // 0 = 使用位置权重，>0 = 显式权重
    Weight int `json:"weight,omitempty"`
}

// Provider 配置 envelope
type ProviderConfig struct {
    SchedulingMode string     `json:"schedulingMode,omitempty"`
    Providers      []Provider `json:"providers"`
}
```

### ProviderRelayService 扩展

```go
type ProviderRelayService struct {
    // ... 现有字段 ...

    // Round-robin 状态（内存态，按 kind:level 维度）
    rrMu   sync.Mutex
    rrNext map[string]uint64

    // 随机数生成器（并发安全）
    rngMu sync.Mutex
    rng   *rand.Rand
}

// 调度模式枚举
type ProviderSchedulingMode string

const (
    ProviderSchedulingSequential     ProviderSchedulingMode = "sequential"
    ProviderSchedulingRoundRobin     ProviderSchedulingMode = "round_robin"
    ProviderSchedulingWeightedRandom ProviderSchedulingMode = "weighted_random"
)
```

### 核心调度函数

```go
// orderProvidersInLevel 根据调度策略排序同 Level 的 provider
func (prs *ProviderRelayService) orderProvidersInLevel(
    kind string,
    level int,
    providers []Provider,
) []Provider {
    mode := prs.getSchedulingMode(kind)

    switch mode {
    case ProviderSchedulingRoundRobin:
        return prs.roundRobinOrder(kind, level, providers)
    case ProviderSchedulingWeightedRandom:
        return prs.weightedShuffleOrder(providers)
    default:
        return providers // sequential: 原顺序
    }
}

// roundRobinOrder 轮询：rotate 起点
func (prs *ProviderRelayService) roundRobinOrder(
    kind string,
    level int,
    providers []Provider,
) []Provider {
    if len(providers) <= 1 {
        return providers
    }

    key := fmt.Sprintf("%s:%d", kind, level)
    prs.rrMu.Lock()
    start := int(prs.rrNext[key] % uint64(len(providers)))
    prs.rrNext[key]++
    prs.rrMu.Unlock()

    if start == 0 {
        return providers
    }

    // Rotate: [start:] + [:start]
    ordered := make([]Provider, 0, len(providers))
    ordered = append(ordered, providers[start:]...)
    ordered = append(ordered, providers[:start]...)
    return ordered
}

// weightedShuffleOrder 加权无放回抽样（Weighted Shuffle）
// 使用 Efraimidis-Spirakis 算法
func (prs *ProviderRelayService) weightedShuffleOrder(providers []Provider) []Provider {
    if len(providers) <= 1 {
        return providers
    }

    type item struct {
        provider Provider
        key      float64
    }

    items := make([]item, len(providers))
    prs.rngMu.Lock()
    for i, p := range providers {
        w := prs.getEffectiveWeight(i, p.Weight)
        u := prs.rng.Float64()
        // key = u^(1/w)，key 越大越靠前
        items[i] = item{
            provider: p,
            key:      math.Pow(u, 1.0/float64(w)),
        }
    }
    prs.rngMu.Unlock()

    // 按 key 降序排序
    sort.Slice(items, func(i, j int) bool {
        return items[i].key > items[j].key
    })

    ordered := make([]Provider, len(items))
    for i, it := range items {
        ordered[i] = it.provider
    }
    return ordered
}

// getEffectiveWeight 获取有效权重
func (prs *ProviderRelayService) getEffectiveWeight(index, explicitWeight int) int {
    if explicitWeight > 0 {
        return explicitWeight
    }
    // 位置权重
    positionWeights := []int{10, 8, 5, 3, 1}
    if index < len(positionWeights) {
        return positionWeights[index]
    }
    return 1
}
```

### ProviderService 改造

```go
// LoadProviderConfig 读取配置（包含 envelope）
func (ps *ProviderService) LoadProviderConfig(kind string) (*ProviderConfig, error) {
    path, err := providerFilePath(kind)
    if err != nil {
        return nil, err
    }

    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return &ProviderConfig{Providers: []Provider{}}, nil
        }
        return nil, err
    }

    cfg := &ProviderConfig{}
    if err := json.Unmarshal(data, cfg); err != nil {
        // 兼容旧格式：纯数组
        var providers []Provider
        if err2 := json.Unmarshal(data, &providers); err2 == nil {
            cfg.Providers = providers
        } else {
            return nil, err
        }
    }

    return cfg, nil
}

// SaveProviderConfig 保存配置（保护 envelope 字段）
func (ps *ProviderService) SaveProviderConfig(kind string, cfg *ProviderConfig) error {
    ps.mu.Lock()
    defer ps.mu.Unlock()

    path, err := providerFilePath(kind)
    if err != nil {
        return err
    }

    data, err := json.MarshalIndent(cfg, "", "  ")
    if err != nil {
        return err
    }

    // 原子写入
    tmp := path + ".tmp"
    if err := os.WriteFile(tmp, data, 0o644); err != nil {
        return err
    }
    return os.Rename(tmp, path)
}

// SaveProviders 兼容旧 API（保护 schedulingMode 不被覆盖）
func (ps *ProviderService) SaveProviders(kind string, providers []Provider) error {
    // 先读取现有配置（获取 schedulingMode）
    cfg, err := ps.LoadProviderConfig(kind)
    if err != nil {
        cfg = &ProviderConfig{}
    }

    // 只更新 providers，保留 schedulingMode
    cfg.Providers = providers

    return ps.SaveProviderConfig(kind, cfg)
}
```

---

## UI 设计

### 设置页面

```
┌─────────────────────────────────────────────────────┐
│ Claude 供应商设置                                    │
├─────────────────────────────────────────────────────┤
│                                                     │
│ 同级调度策略                                        │
│ ┌─────────────────────────────────┐                │
│ │ 顺序                         ▼ │                │
│ └─────────────────────────────────┘                │
│   ○ 顺序 - 按列表顺序依次尝试（默认）              │
│   ○ 轮询 - 每次请求轮换起点                        │
│   ○ 权重随机 - 按位置权重随机选择                  │
│                                                     │
│ ℹ️ 仅在同一 Level 内生效                            │
│ ℹ️ 与 Fixed Mode 可同时生效                         │
│                                                     │
└─────────────────────────────────────────────────────┘
```

### 前端类型定义

```typescript
// types.ts
export type ProviderSchedulingMode = 'sequential' | 'round_robin' | 'weighted_random';

export interface ProviderConfig {
  schedulingMode?: ProviderSchedulingMode;
  providers: Provider[];
}

// API 调用
export async function loadProviderConfig(kind: string): Promise<ProviderConfig>;
export async function saveProviderConfig(kind: string, config: ProviderConfig): Promise<void>;
```

---

## 实施路线

### 第一期（最小可用）

1. ✅ 设计完成
2. ⬜ 后端：`ProviderConfig` envelope 结构
3. ⬜ 后端：`LoadProviderConfig` / `SaveProviderConfig`
4. ⬜ 后端：`orderProvidersInLevel()` 三种策略
5. ⬜ 前端：设置页面下拉选择
6. ⬜ 测试：各策略验证

### 第二期（可选增强）

1. ⬜ 显式 `weight` 字段支持
2. ⬜ 前端：权重输入框（仅权重随机模式显示）
3. ⬜ 前端：保存时保留未知字段（防止 weight 丢失）

---

## 注意事项

### Fixed Mode 兼容性

黑名单 Fixed Mode 开启时，同 Provider 会重试到阈值/拉黑再切换到下一个 Provider。

**轮询与 Fixed Mode 现已支持同时生效**：
- 如果启用轮询（`enable_round_robin = true`），同 Level 内的 Provider 遍历顺序会按轮询排序
- Fixed Mode 的"同 Provider 重试到拉黑"逻辑不受影响
- 两种功能正交组合：轮询决定"从哪个 Provider 开始"，Fixed Mode 决定"单个 Provider 失败后的处理策略"

### 并发安全

- Round-robin 状态使用 `sync.Mutex` 保护
- 随机数生成器使用独立锁，避免竞争

### 向后兼容

- 旧配置文件（只有 `{providers: [...]}` 格式）自动兼容
- `schedulingMode` 缺失时默认为 `sequential`
- `weight: 0` 或缺失时使用位置权重

---

## 参考资源

- [claude-code-hub provider-selector.ts](../claude-code-hub/src/app/v1/_lib/proxy/provider-selector.ts)
- [Efraimidis-Spirakis 加权无放回抽样算法](https://en.wikipedia.org/wiki/Reservoir_sampling#Algorithm_A-Res)
- [cc-r Level 分组调度文档](./CLAUDE.md#优先级分组调度配置指南)
