# Header 透传优化与硬编码修复 - 最终计划

> **版本**: v1.4  
> **状态**: P0-P5 全部完成 ✅  
> **涉及文件**: `providerrelay.go`, `providerservice.go`, `healthcheckservice.go`, `connectivitytestservice.go`, `constants.go`, `header_config_test.go`, `frontend/`

---

## 📋 背景

本计划整合两个相关改进：

1. **Header 透传优化** - 移除代理对请求 Header 的过度干预，支持 Provider 级别配置
2. **硬编码清理** - 集中管理分散的 API 版本号和默认模型配置

### ~~当前问题~~ (已修复)

| 问题 | 位置 | 状态 |
|------|------|------|
| ~~`Content-Type` 被强制覆盖为 `application/json`~~ | ~~`providerrelay.go:1169`~~ | ✅ 已移除 |
| ~~`anthropic-version` 硬编码 `"2023-06-01"`~~ | ~~多处~~ | ✅ 已集中至 `constants.go` |
| ~~默认模型名含日期 `claude-3-5-haiku-20241022`~~ | ~~`healthcheckservice.go`~~ | ✅ 已改用 `-latest` 别名 |

### 无需处理的部分

| 项目 | 原因 |
|------|------|
| 主代理的 `anthropic-version` 透传 | ✅ `cloneHeaders()` 已透传所有自定义 Headers |
| Provider 配置 TestModel | ✅ `getEffectiveModel()` 已实现优先读取配置 |

---

## 🎯 优先级排序

| 优先级 | 内容 | 工作量 | 状态 |
|--------|------|--------|------|
| **P0** | Phase 1: 移除强制 `Content-Type` 覆盖 | 15 分钟 | ✅ 完成 |
| **P1** | Phase 1.5: 集中管理 API 版本常量 | 30 分钟 | ✅ 完成 |
| **P2** | Phase 1.6: 默认模型改用 `-latest` 别名 | 10 分钟 | ✅ 完成 |
| **P3** | Phase 2: Provider Header 配置扩展（后端） | 30 分钟 | ✅ 完成 |
| **P4** | Phase 3: 前端 UI 支持 | 1-2 小时 | ✅ 完成 |
| **P5** | Phase 4: 文档与测试 | 2-3 小时 | ✅ 完成 |

---

## 🔧 Phase 1: 移除强制 Content-Type 覆盖 (P0) ✅

**状态**: 已完成

### 修改内容

已删除 `providerrelay.go` 中强制设置 `Content-Type` 的代码，改为依赖 `cloneHeaders()` 透传原请求的 `Content-Type`。

### 原因

- `cloneHeaders()` 已复制原请求的所有 Headers（包括 `Content-Type`）
- 强制覆盖破坏了原请求的语义
- Claude/Codex CLI 发出的请求本身就是 `application/json`，无需强制设置

---

## 🔧 Phase 1.5: 集中管理 API 版本常量 (P1) ✅

**状态**: 已完成

### 新建文件

已创建 `services/constants.go`：

```go
package services

import "os"

const DefaultAnthropicAPIVersion = "2023-06-01"

func GetAnthropicAPIVersion() string {
    if v := os.Getenv("ANTHROPIC_API_VERSION"); v != "" {
        return v
    }
    return DefaultAnthropicAPIVersion
}
```

### 修改调用处

已更新以下文件使用 `GetAnthropicAPIVersion()`:
- `services/healthcheckservice.go`
- `services/connectivitytestservice.go`

### 好处

- 一处修改，全局生效
- 用户可通过 `ANTHROPIC_API_VERSION` 环境变量覆盖，无需重新编译
- 方便未来跟进 Anthropic API 版本更新

---

## 🔧 Phase 1.6: 默认模型使用 -latest 别名 (P2) ✅

**状态**: 已完成

### 修改内容

已更新 `services/healthcheckservice.go` 中的 `getEffectiveModel()` 函数：

| 平台 | 修改前 | 修改后 |
|------|--------|--------|
| Claude | `claude-3-5-haiku-20241022` | `claude-3-5-haiku-latest` |
| Codex | `gpt-4o-mini` | `gpt-4o-mini` (不变) |
| Gemini | `gemini-1.5-flash` | `gemini-1.5-flash-latest` |
| Default | `gpt-3.5-turbo` | `gpt-4o-mini` |

### 说明

- Anthropic 支持 `-latest` 别名自动指向最新版本
- Gemini 同样支持 `-latest` 后缀
- OpenAI 模型名通常不含日期，保持不变

---

## 🔧 Phase 2: Provider Header 配置扩展 (P3) ✅

**状态**: 后端已完成 (2026-01-08)

**目标**: 支持 Provider 级别的 Header 自定义

### 数据结构扩展

```go
// services/providerservice.go - Provider 结构体
type Provider struct {
    // ... 现有字段 ...
    
    // Header 配置（高级设置）
    ExtraHeaders    map[string]string `json:"extraHeaders,omitempty"`    // 不存在才添加
    OverrideHeaders map[string]string `json:"overrideHeaders,omitempty"` // 强制覆盖
    StripHeaders    []string          `json:"stripHeaders,omitempty"`    // 需要移除
}
```

### Header 处理优先级

```
1. 复制原请求所有 Headers（除 hop-by-hop 和认证头）  ← cloneHeaders() 已实现
   - 过滤: Authorization, X-Api-Key, X-Goog-Api-Key
2. 移除 StripHeaders 指定的 Headers
3. 应用 OverrideHeaders（覆盖同名 key）
   - ⚠️ 认证头会被 Step 5 覆盖，不应在此配置
4. 应用 ExtraHeaders（仅当 key 不存在时添加）
5. 最后替换认证头（Authorization / x-api-key / x-goog-api-key）
   - 认证头由 Provider.APIKey + authMethod 决定，不受 OverrideHeaders 影响
```

### 设计说明

- `buildForwardHeaders()` 只处理非认证头，认证头继续在调用方设置（保持现有架构）
- Claude/Codex: 根据 `authMethod` 设置 `Authorization` 或 `X-Api-Key`
- Gemini: 设置 `x-goog-api-key`

### 前置修改: cloneHeaders() 补充过滤

在实施 Phase 2 前，需先修复 `cloneHeaders()` 遗漏 `X-Goog-Api-Key` 的问题：

```go
// services/providerrelay.go - 修改 cloneHeaders()
func cloneHeaders(header http.Header) http.Header {
    cloned := make(http.Header)
    for key, values := range header {
        canonicalKey := http.CanonicalHeaderKey(key)

        // 跳过认证相关的头（会在转发时根据 authMethod 重新设置）
        if canonicalKey == "Authorization" || 
           canonicalKey == "X-Api-Key" || 
           canonicalKey == "X-Goog-Api-Key" {  // ← 新增 Gemini 认证头
            continue
        }
        // ... 其他逻辑 ...
    }
    return cloned
}
```

### 实现函数

```go
// services/providerrelay.go - 新增函数
// buildForwardHeaders 只处理非认证头，认证头在调用方设置
func buildForwardHeaders(original http.Header, provider *Provider) http.Header {
    headers := cloneHeaders(original)  // 已过滤 Authorization, X-Api-Key, X-Goog-Api-Key
    
    // Step 2: 移除指定 headers
    for _, h := range provider.StripHeaders {
        headers.Del(h)
    }
    
    // Step 3: 强制覆盖（注意：不应包含认证头）
    for k, v := range provider.OverrideHeaders {
        headers.Set(k, v)
    }
    
    // Step 4: 额外添加（不存在才加）
    for k, v := range provider.ExtraHeaders {
        if headers.Get(k) == "" {
            headers.Set(k, v)
        }
    }
    
    return headers
}
```

---

## 🔧 Phase 3: 前端 UI 支持 (P4) ✅

**状态**: 已完成 (2026-01-08)

**目标**: Provider 编辑界面添加 Header 配置

### UI 设计

```
┌─ Provider 编辑 ─────────────────────────────────┐
│ 名称: [________________]                        │
│ API URL: [________________]                     │
│ API Key: [________________]                     │
│                                                 │
│ ▼ 高级设置                                       │
│ ┌─────────────────────────────────────────────┐ │
│ │ 额外 Headers (ExtraHeaders)                 │ │
│ │ ┌──────────────┬──────────────────┐ [+]    │ │
│ │ │ Key          │ Value            │        │ │
│ │ ├──────────────┼──────────────────┤        │ │
│ │ │ X-Custom     │ my-value         │ [×]    │ │
│ │ └──────────────┴──────────────────┘        │ │
│ │                                             │ │
│ │ 覆盖 Headers (OverrideHeaders)              │ │
│ │ ┌──────────────┬──────────────────┐ [+]    │ │
│ │ │ Content-Type │ application/json │ [×]    │ │
│ │ └──────────────┴──────────────────┘        │ │
│ │                                             │ │
│ │ 移除 Headers (StripHeaders)                 │ │
│ │ ┌────────────────────────────────┐ [+]     │ │
│ │ │ X-Forwarded-For                │ [×]     │ │
│ │ └────────────────────────────────┘         │ │
│ └─────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────┘
```

### 实现内容

已新增 `HeaderConfigEditor.vue` 组件，支持：
- **ExtraHeaders**: 键值对编辑，仅当原请求中不存在该 key 时添加
- **OverrideHeaders**: 键值对编辑，强制覆盖
- **StripHeaders**: 字符串列表编辑，转发前移除
- 折叠面板设计，配置数量徽标
- 认证头警告提示

### 涉及文件

- `frontend/src/components/common/HeaderConfigEditor.vue` - 新增组件
- `frontend/src/components/Main/Index.vue` - 集成到 Provider 编辑表单
- `frontend/src/data/cards.ts` - AutomationCard 类型扩展
- `frontend/src/locales/en.json` / `zh.json` - 国际化文案
- `frontend/bindings/` - 类型定义（Wails 自动生成）

---

## 🔧 Phase 4: 文档与测试 (P5) ✅

**状态**: 已完成 (2026-01-08)

**目标**: 完善单元测试覆盖，确保 Header 配置功能稳定可靠

### 新增测试文件

`services/header_config_test.go`:

| 测试类别 | 测试内容 |
|---------|---------|
| `buildForwardHeaders` | nil Provider、空 Provider、StripHeaders、OverrideHeaders、ExtraHeaders、优先级处理、深拷贝 |
| `cloneHeaders` | 认证头过滤（Authorization/X-Api-Key/X-Goog-Api-Key）、hop-by-hop 头过滤、多值 Header 复制 |
| Provider JSON 序列化 | omitempty 行为、反序列化、完整 round-trip |
| 真实场景集成测试 | OpenRouter HTTP-Referer、非标准 API Content-Type、移除代理头、复合配置 |
| 性能基准测试 | `BenchmarkBuildForwardHeaders_Simple`、`BenchmarkBuildForwardHeaders_WithConfig`、`BenchmarkCloneHeaders` |

### 测试运行

```bash
# 运行单元测试
go test -v ./services/ -run "TestBuildForwardHeaders|TestCloneHeaders|TestProviderHeaderConfig" -count=1

# 运行性能测试
go test -bench="BenchmarkBuildForwardHeaders|BenchmarkCloneHeaders" -run="^$" ./services/ -benchtime=1s
```

### 测试覆盖要点

1. **功能正确性**: 验证 Strip → Override → Extra 的处理优先级
2. **安全性**: 验证认证头（Authorization, X-Api-Key, X-Goog-Api-Key）和 hop-by-hop 头被正确过滤
3. **数据完整性**: 验证深拷贝不会影响原始 Header
4. **JSON 兼容性**: 验证 omitempty 行为和序列化/反序列化完整性
5. **性能**: 基准测试确保无性能回归

---

## 📊 Header 行为对照表

| Header 类型 | 改前 | 改后 |
|------------|------|------|
| `Content-Type` | 强制 `application/json` | **保留原请求** (Phase 1) |
| `Accept` | 为空则添加 | 保持不变 |
| `Authorization` / `X-Api-Key` | 替换为 Provider APIKey | 保持不变（由 Provider.APIKey 控制） |
| `X-Goog-Api-Key` | 替换为 Provider APIKey | 保持不变（Gemini 认证头，**需补充过滤**） |
| `anthropic-version` | 透传（主代理）/ 硬编码（测试） | 透传 / **集中常量** (Phase 1.5) |
| `anthropic-beta` | 透传 | 透传 |
| `OpenAI-Beta` | 透传 | 透传 |
| Provider `ExtraHeaders` | 无 | **新增** (Phase 2) |
| Provider `OverrideHeaders` | 无 | **新增** (Phase 2)，不含认证头 |
| Provider `StripHeaders` | 无 | **新增** (Phase 2) |

---

## 🚨 风险与回滚

### Phase 1 风险

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|----------|
| 某些 Provider 依赖强制 `Content-Type` | 低 | 请求失败 | 用户可在 Phase 2 的 `OverrideHeaders` 中配置 |

### 回滚方案

```go
// 如需回滚 Phase 1，恢复这行代码:
httpReq.Header.Set("Content-Type", "application/json")

// 或在 Phase 2 后，建议用户在 Provider 配置中添加:
// OverrideHeaders: {"Content-Type": "application/json"}
```

---

## 📅 实施顺序

```
Week 1: ✅ 已完成 (2026-01-08)
  ├── Phase 1: 移除强制 Content-Type ✅
  ├── Phase 1.5: 集中 API 版本常量 ✅
  ├── Phase 1.6: 默认模型 -latest ✅
  ├── Phase 2: Provider Header 配置扩展（后端）✅
  ├── Phase 3: 前端 UI 支持 ✅
  └── Phase 4: 文档与测试 ✅
  
Week 2+: 观察效果，收集反馈
```

---

## ✅ 验收标准

- [x] `Content-Type` 不再被强制覆盖
- [x] `anthropic-version` 集中于 `constants.go`
- [x] 环境变量 `ANTHROPIC_API_VERSION` 可覆盖默认值
- [x] 健康检查默认模型使用 `-latest` 别名
- [x] (Phase 2 前置) `cloneHeaders()` 过滤 `X-Goog-Api-Key`
- [x] (Phase 2) Provider Header 配置生效
- [x] (Phase 2) OverrideHeaders 不影响认证头
- [x] (Phase 3) 前端可编辑 Header 配置
- [x] (Phase 3) 前端 UI 提示：OverrideHeaders 中的认证头会被覆盖
- [x] (Phase 4) `buildForwardHeaders()` 单元测试覆盖
- [x] (Phase 4) `cloneHeaders()` 单元测试覆盖
- [x] (Phase 4) Provider Header 配置 JSON 序列化测试
- [x] (Phase 4) 性能基准测试