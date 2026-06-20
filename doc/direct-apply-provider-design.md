# 单个供应商直接应用功能设计文档

> 版本: v1.0
> 日期: 2024-12-18
> 状态: 待实施

## 1. 功能概述

### 1.1 背景
用户在**不开启本地转发代理**的情况下，希望能够直接点击某个供应商将其配置应用到 CLI 工具的配置文件中。

### 1.2 目标
- 三平台（Claude Code、Codex、Gemini）功能行为完全一致
- 提供一键直连应用功能，无需手动编辑配置文件
- 与代理模式互斥：代理开启时禁止直连应用

### 1.3 核心原则
- **代理互斥**：本地转发开启时，禁止直连应用
- **CLI 配置为真源**：状态查询从 CLI 配置文件反推，不额外维护状态
- **最小侵入**：只修改必要字段，保留用户其他配置

---

## 2. 技术设计

### 2.1 写入目标

| 平台 | 配置文件路径 | 写入字段 |
|------|-------------|----------|
| Claude Code | `~/.claude/settings.json` | `env.ANTHROPIC_BASE_URL`<br>`env.ANTHROPIC_AUTH_TOKEN` |
| Codex | `~/.codex/config.toml`<br>`~/.codex/auth.json` | `model_provider`, `preferred_auth_method`<br>`model_providers.code-switch-r.base_url`<br>`OPENAI_API_KEY` (auth.json) |
| Gemini | `~/.gemini/.env`<br>`~/.gemini/settings.json` | `.env`: `GOOGLE_GEMINI_BASE_URL`, `GEMINI_API_KEY`, `GEMINI_MODEL`<br>`settings.json`: `security.auth.selectedType` |

#### 2.1.1 Gemini 认证类型说明

`~/.gemini/settings.json` 中的 `security.auth.selectedType` 字段用于指定认证方式：

| 值 | 说明 | 使用场景 |
|---|------|---------|
| `gemini-api-key` | API Key 认证 | 第三方供应商（需要 `GEMINI_API_KEY`） |
| `oauth-personal` | OAuth 认证 | Google 官方（无需 API Key） |

写入逻辑：
- 若 provider 有 `GEMINI_API_KEY` → 设置为 `"gemini-api-key"`
- 若 provider 无 API Key（OAuth 模式）→ 设置为 `"oauth-personal"`

### 2.2 后端 API 设计

#### 2.2.1 新增文件: `services/directapply_helpers.go`

通用辅助函数，供三个平台共用：

```go
package services

// providerFilePathNoCreate 只计算 providers 文件路径，不创建目录
func providerFilePathNoCreate(kind string) (string, error)

// providerFileEnvelope 最小解析结构
type providerFileEnvelope struct {
    Providers []Provider `json:"providers"`
}

// loadProviderSnapshot 只读加载 providers（不触发迁移保存）
func loadProviderSnapshot(kind string) ([]Provider, error)

// findProviderByID 按 ID 查找 provider
func findProviderByID(providers []Provider, id int64) (Provider, bool)

// normalizeURLTrimSlash 标准化 URL（去除尾部斜杠）
func normalizeURLTrimSlash(value string) string

// urlsEqualFold URL 不区分大小写比较
func urlsEqualFold(a, b string) bool

// matchProviderIDByConfig 用 CLI 配置反查 provider ID
func matchProviderIDByConfig(providers []Provider, apiURL string, apiKey string) *int64
```

#### 2.2.2 Claude Code: `services/claudesettings.go`

新增方法：

```go
// ApplySingleProvider 直连应用单一供应商（仅在代理关闭时可用）
func (css *ClaudeSettingsService) ApplySingleProvider(providerID int) error

// GetDirectAppliedProviderID 返回当前直连应用的 Provider ID
// - 若配置指向本地代理 → 返回 nil
// - 若无法匹配 provider → 返回 nil
func (css *ClaudeSettingsService) GetDirectAppliedProviderID() (*int64, error)
```

**实现逻辑**：
1. 检查代理状态，若启用则返回错误
2. 从 `~/.code-switch/claude-code.json` 加载 providers
3. 按 providerID 查找目标 provider
4. 备份现有 settings.json
5. 最小侵入写入 `env.ANTHROPIC_BASE_URL` 和 `env.ANTHROPIC_AUTH_TOKEN`

#### 2.2.3 Codex: `services/codexsettings.go`

新增方法：

```go
// ApplySingleProvider 直连应用单一供应商（仅在代理关闭时可用）
func (css *CodexSettingsService) ApplySingleProvider(providerID int) error

// GetDirectAppliedProviderID 返回当前直连应用的 Provider ID
func (css *CodexSettingsService) GetDirectAppliedProviderID() (*int64, error)
```

**实现逻辑**：
1. 检查代理状态，若启用则返回错误
2. 从 `~/.code-switch/codex.json` 加载 providers
3. 按 providerID 查找目标 provider
4. 备份 config.toml 和 auth.json
5. 写入 config.toml: `model_provider`, `preferred_auth_method`, `model_providers.code-switch-r.*`
6. 写入 auth.json: `OPENAI_API_KEY`

#### 2.2.4 Gemini: `services/geminiservice.go`

修改/新增方法：

```go
// SwitchProvider 增加代理检查（与 Claude/Codex 保持一致）
func (s *GeminiService) SwitchProvider(id string) error {
    // 新增：代理开启时拒绝
    proxyStatus, err := s.ProxyStatus()
    if proxyStatus != nil && proxyStatus.Enabled {
        return fmt.Errorf("Gemini 本地代理已启用，请先关闭代理再切换")
    }
    // ... 现有逻辑
}

// ApplySingleProvider 别名，统一 API 命名
func (s *GeminiService) ApplySingleProvider(id string) error {
    return s.SwitchProvider(id)
}

// GetDirectAppliedProviderID 返回当前直连应用的 Provider ID
func (s *GeminiService) GetDirectAppliedProviderID() (*string, error)
```

### 2.3 前端 API 调用

```typescript
// Claude Code
await Call.ByName('codeswitch/services.ClaudeSettingsService.ApplySingleProvider', providerID)
await Call.ByName('codeswitch/services.ClaudeSettingsService.GetDirectAppliedProviderID')

// Codex
await Call.ByName('codeswitch/services.CodexSettingsService.ApplySingleProvider', providerID)
await Call.ByName('codeswitch/services.CodexSettingsService.GetDirectAppliedProviderID')

// Gemini
await Call.ByName('codeswitch/services.GeminiService.ApplySingleProvider', providerID)
await Call.ByName('codeswitch/services.GeminiService.GetDirectAppliedProviderID')
```

---

## 3. 前端设计

### 3.1 UI 组件

#### 3.1.1 直连应用按钮

**位置**：Provider 卡片的 `.card-actions` 区域，放在最前面

**HTML 结构**：
```html
<button
  class="ghost-icon apply-btn"
  :class="{
    'is-active': isDirectApplied(card),
    'is-disabled': activeProxyState
  }"
  :disabled="activeProxyState"
  @click.stop="handleDirectApply(card)"
>
  <!-- 使用中状态：显示文字 -->
  <span v-if="isDirectApplied(card)" class="apply-status-text">
    {{ t('components.main.providers.statusInUse') }}
  </span>

  <!-- 正常状态：显示闪电图标 -->
  <svg v-else viewBox="0 0 24 24">
    <path
      d="M3.75 13.5l10.5-11.25L12 10.5h8.25L9.75 21.75 12 13.5H3.75z"
      stroke="currentColor"
      stroke-width="1.5"
      stroke-linecap="round"
      stroke-linejoin="round"
      fill="none"
    />
  </svg>
</button>
```

#### 3.1.2 "当前使用" Badge

**位置**：卡片标题（`.card-title`）右侧

**HTML 结构**：
```html
<span v-if="isDirectApplied(card)" class="current-use-badge">
  {{ t('components.main.providers.currentBadge') }}
</span>
```

### 3.2 按钮状态

| 状态 | CSS 类 | 样式 | 触发条件 |
|------|--------|------|----------|
| 正常 | `.apply-btn` | 灰色图标，hover 时黄色 | 代理关闭 + 非当前使用 |
| 禁用 | `.is-disabled` | opacity 0.3，灰度 100% | 代理开启 |
| 使用中 | `.is-active` | 绿色边框 + "使用中" 文字 | 是当前直连应用的 provider |

### 3.3 CSS 样式

```css
/* 直连应用按钮 */
.apply-btn {
  position: relative;
  transition: all 0.2s ease;
  color: var(--mac-text-secondary);
  min-width: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
}

/* 正常状态 Hover */
.apply-btn:not(:disabled):not(.is-active):hover {
  color: #f59e0b; /* Amber */
  background: rgba(245, 158, 11, 0.1);
}

/* 禁用状态（代理开启） */
.apply-btn:disabled,
.apply-btn.is-disabled {
  opacity: 0.3;
  cursor: not-allowed;
  filter: grayscale(100%);
}

/* 使用中状态 */
.apply-btn.is-active {
  border: 1px solid #10b981; /* Emerald 500 */
  background: rgba(16, 185, 129, 0.1);
  color: #10b981;
  width: auto;
  padding: 0 8px;
  border-radius: 6px;
  gap: 4px;
}

.apply-status-text {
  font-size: 11px;
  font-weight: 600;
  white-space: nowrap;
}

/* 深色模式适配 */
:global(.dark) .apply-btn.is-active {
  border-color: #34d399; /* Emerald 400 */
  background: rgba(52, 211, 153, 0.15);
  color: #34d399;
}

/* 当前使用 Badge */
.current-use-badge {
  display: inline-flex;
  align-items: center;
  padding: 2px 6px;
  margin-left: 8px;
  border-radius: 4px;
  font-size: 10px;
  font-weight: 600;
  background: linear-gradient(135deg, #10b981 0%, #059669 100%);
  color: white;
  box-shadow: 0 2px 4px rgba(16, 185, 129, 0.2);
}

:global(.dark) .current-use-badge {
  background: linear-gradient(135deg, #059669 0%, #047857 100%);
}
```

### 3.4 状态管理

```typescript
// 状态：追踪各平台直连应用的 provider
const directAppliedProvider = reactive<Record<ProviderTab, number | string | null>>({
  claude: null,
  codex: null,
  gemini: null,
  others: null,
})

// 判断卡片是否为当前直连应用
const isDirectApplied = (card: AutomationCard) => {
  if (activeProxyState.value) return false
  const current = directAppliedProvider[activeTab.value]
  return current != null && current == card.id
}

// 处理直连应用点击
const handleDirectApply = async (card: AutomationCard) => {
  if (activeProxyState.value) {
    showToast(t('components.main.errors.proxyMustBeOff'), 'warning')
    return
  }

  const tab = activeTab.value
  try {
    if (tab === 'gemini') {
      await Call.ByName('codeswitch/services.GeminiService.ApplySingleProvider', String(card.id))
    } else if (tab === 'claude') {
      await Call.ByName('codeswitch/services.ClaudeSettingsService.ApplySingleProvider', Number(card.id))
    } else if (tab === 'codex') {
      await Call.ByName('codeswitch/services.CodexSettingsService.ApplySingleProvider', Number(card.id))
    }

    directAppliedProvider[tab] = card.id
    showToast(t('components.main.providers.appliedSuccess', { name: card.name }), 'success')
  } catch (error) {
    console.error('Failed to apply provider:', error)
    showToast(t('components.main.providers.appliedFailed'), 'error')
  }
}

// 初始化：加载当前直连状态
const loadDirectAppliedProviders = async () => {
  try {
    // Claude
    const claudeID = await Call.ByName('codeswitch/services.ClaudeSettingsService.GetDirectAppliedProviderID')
    if (claudeID) directAppliedProvider.claude = claudeID

    // Codex
    const codexID = await Call.ByName('codeswitch/services.CodexSettingsService.GetDirectAppliedProviderID')
    if (codexID) directAppliedProvider.codex = codexID

    // Gemini
    const geminiID = await Call.ByName('codeswitch/services.GeminiService.GetDirectAppliedProviderID')
    if (geminiID) directAppliedProvider.gemini = geminiID
  } catch (e) {
    console.error('Failed to load direct applied status:', e)
  }
}

// 在 onMounted 中调用
onMounted(async () => {
  // ... 现有逻辑
  await loadDirectAppliedProviders()
})
```

---

## 4. i18n 翻译

### 4.1 中文 (zh.json)

```json
{
  "components": {
    "main": {
      "providers": {
        "directApply": "直接应用",
        "statusInUse": "使用中",
        "currentBadge": "当前使用",
        "appliedSuccess": "{name} 已应用",
        "appliedFailed": "应用失败"
      },
      "errors": {
        "proxyMustBeOff": "请先关闭本地转发再进行直接应用"
      }
    }
  }
}
```

### 4.2 英文 (en.json)

```json
{
  "components": {
    "main": {
      "providers": {
        "directApply": "Direct Apply",
        "statusInUse": "In Use",
        "currentBadge": "Current",
        "appliedSuccess": "{name} applied",
        "appliedFailed": "Failed to apply"
      },
      "errors": {
        "proxyMustBeOff": "Please disable local proxy before direct apply"
      }
    }
  }
}
```

---

## 5. 交互流程

```
┌─────────────────────────────────────────────────────────────┐
│                     用户点击 ⚡ 按钮                          │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
                    ┌─────────────────┐
                    │  代理是否开启？   │
                    └─────────────────┘
                       │           │
                      是           否
                       │           │
                       ▼           ▼
              ┌────────────┐   ┌─────────────────────┐
              │  显示警告   │   │ 调用后端 API        │
              │  Toast     │   │ ApplySingleProvider │
              └────────────┘   └─────────────────────┘
                                        │
                                        ▼
                              ┌─────────────────┐
                              │  写入 CLI 配置   │
                              │  文件           │
                              └─────────────────┘
                                        │
                                        ▼
                              ┌─────────────────┐
                              │  更新前端状态    │
                              │  显示成功 Toast │
                              └─────────────────┘
                                        │
                                        ▼
                    ┌─────────────────────────────────┐
                    │  按钮变为 "使用中"               │
                    │  显示 "当前使用" Badge          │
                    │  其他卡片清除 "使用中" 状态      │
                    └─────────────────────────────────┘
```

---

## 6. 实施计划

### 6.1 后端实施顺序

1. 新增 `services/directapply_helpers.go`
2. 修改 `services/claudesettings.go`
3. 修改 `services/codexsettings.go`
4. 修改 `services/geminiservice.go`

### 6.2 前端实施顺序

1. 添加 i18n 翻译
2. 添加状态管理逻辑
3. 添加 UI 组件（按钮 + Badge）
4. 添加 CSS 样式

### 6.3 测试要点

- [ ] 代理开启时，按钮灰色禁用
- [ ] 代理关闭时，点击按钮成功写入配置
- [ ] 写入后 CLI 工具能正常使用该配置
- [ ] 切换平台 Tab 时，正确显示各平台的直连状态
- [ ] 刷新页面后，正确恢复直连状态显示

---

## 7. 风险与注意事项

1. **配置文件格式兼容**：确保写入的格式与 CLI 工具期望的格式一致
2. **并发安全**：多个操作同时进行时，确保文件写入的原子性
3. **备份机制**：写入前创建备份，便于用户恢复
4. **错误处理**：API 调用失败时，提供清晰的错误提示
