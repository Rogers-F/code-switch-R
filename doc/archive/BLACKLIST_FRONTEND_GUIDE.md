# 供应商黑名单功能 - 前端集成指南

## 📝 概述

由于前端文件 `Index.vue` 较大（1693行），手动修改容易出错。此指南提供完整的修改步骤，或者您可以跳过此步骤，先测试后端功能。

---

## ⚡ 快速方案：先测试后端

如果您想快速验证后端功能，可以先跳过前端 UI 修改，直接测试：

1. **运行应用**：`wails3 task dev`
2. **查看后端日志**：在控制台观察是否有拉黑相关日志
3. **检查数据库**：查看 `~/.code-switch/app.db` 中的 `provider_blacklist` 表

---

## 🛠️ 完整方案：集成前端 UI

### 修改清单

| 文件 | 修改内容 | 优先级 |
|------|---------|--------|
| `frontend/src/components/Main/Index.vue` | 添加黑名单 UI 和逻辑 | 高 |
| `frontend/src/locales/zh-CN.json` | 中文文案 | 中 |
| `frontend/src/locales/en-US.json` | 英文文案 | 低 |

---

## 📄 详细修改步骤

### 1. 修改 `Index.vue` - 导入部分

**位置**：第 580 行之后

**添加**：
```typescript
import { getBlacklistStatus, manualUnblock, type BlacklistStatus } from '../../services/blacklist'
```

---

### 2. 修改 `Index.vue` - 添加状态变量

**位置**：第 637 行之后

**添加**：
```typescript
// 黑名单状态
const blacklistStatusMap = reactive<Record<ProviderTab, Record<string, BlacklistStatus>>>({
  claude: {},
  codex: {},
})
let blacklistTimer: number | undefined
```

---

### 3. 修改 `Index.vue` - 添加方法

**位置**：在 `loadProviderStats` 方法附近

**添加以下 4 个方法**：

```typescript
// 加载黑名单状态
const loadBlacklistStatus = async (tab: ProviderTab) => {
  try {
    const statuses = await getBlacklistStatus(tab)
    const map: Record<string, BlacklistStatus> = {}
    statuses.forEach(status => {
      map[status.providerName] = status
    })
    blacklistStatusMap[tab] = map
  } catch (err) {
    console.error(`加载 ${tab} 黑名单状态失败:`, err)
  }
}

// 手动解禁
const handleUnblock = async (providerName: string) => {
  try {
    await manualUnblock(activeTab.value, providerName)
    showToast(t('components.main.blacklist.unblockSuccess', { name: providerName }), 'success')
    await loadBlacklistStatus(activeTab.value)
  } catch (err) {
    console.error('解除拉黑失败:', err)
    showToast(t('components.main.blacklist.unblockFailed'), 'error')
  }
}

// 格式化倒计时
const formatBlacklistCountdown = (remainingSeconds: number): string => {
  const minutes = Math.floor(remainingSeconds / 60)
  const seconds = remainingSeconds % 60
  return `${minutes}${t('components.main.blacklist.minutes')}${seconds}${t('components.main.blacklist.seconds')}`
}

// 获取 provider 黑名单状态
const getProviderBlacklistStatus = (providerName: string): BlacklistStatus | null => {
  return blacklistStatusMap[activeTab.value][providerName] || null
}
```

---

### 4. 修改 `Index.vue` - 修改生命周期钩子

#### 4.1 在 `onMounted` 中添加定时器

**位置**：在现有定时器之后

**添加**：
```typescript
// 加载初始黑名单状态
loadBlacklistStatus(activeTab.value)

// 每秒更新黑名单倒计时
blacklistTimer = window.setInterval(() => {
  const tab = activeTab.value
  Object.keys(blacklistStatusMap[tab]).forEach(providerName => {
    const status = blacklistStatusMap[tab][providerName]
    if (status && status.isBlacklisted && status.remainingSeconds > 0) {
      status.remainingSeconds--
      if (status.remainingSeconds <= 0) {
        loadBlacklistStatus(tab)
      }
    }
  })
}, 1000)
```

#### 4.2 在 `onUnmounted` 中清理定时器

**位置**：在现有清理代码之后

**添加**：
```typescript
if (blacklistTimer) {
  window.clearInterval(blacklistTimer)
}
```

---

### 5. 修改 `Index.vue` - 模板部分

**位置**：第 353 行的 `</p>` 之后

**添加**：
```vue
<!-- 黑名单横幅 -->
<div
  v-if="getProviderBlacklistStatus(card.name)?.isBlacklisted"
  :class="['blacklist-banner', { dark: resolvedTheme === 'dark' }]"
>
  <span class="blacklist-icon">⛔</span>
  <span class="blacklist-text">
    {{ t('components.main.blacklist.blocked') }} |
    {{ t('components.main.blacklist.remaining') }}:
    {{ formatBlacklistCountdown(getProviderBlacklistStatus(card.name)!.remainingSeconds) }}
  </span>
  <button
    class="unblock-btn"
    type="button"
    @click.stop="handleUnblock(card.name)"
  >
    {{ t('components.main.blacklist.unblock') }}
  </button>
</div>
```

---

### 6. 修改 `Index.vue` - 样式部分

**位置**：在 `</style>` 标签之前

**添加**：
```scss
.blacklist-banner {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  margin-top: 8px;
  background: rgba(239, 68, 68, 0.1);
  border-left: 3px solid #ef4444;
  border-radius: 6px;
  font-size: 13px;
  color: #dc2626;

  &.dark {
    background: rgba(239, 68, 68, 0.15);
    color: #f87171;
  }
}

.blacklist-icon {
  font-size: 16px;
  flex-shrink: 0;
}

.blacklist-text {
  flex: 1;
  font-weight: 500;
}

.unblock-btn {
  padding: 4px 12px;
  font-size: 12px;
  font-weight: 500;
  color: #fff;
  background: #ef4444;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  transition: background 0.2s;

  &:hover {
    background: #dc2626;
  }

  &:active {
    transform: scale(0.98);
  }
}
```

---

### 7. 修改 `zh-CN.json` - 中文文案

**位置**：在 `components.main` 对象中添加

**添加**：
```json
"blacklist": {
  "blocked": "已拉黑",
  "remaining": "剩余",
  "minutes": "分",
  "seconds": "秒",
  "unblock": "立即解禁",
  "unblockSuccess": "已解除 {name} 的拉黑",
  "unblockFailed": "解除拉黑失败，请稍后重试"
}
```

---

### 8. 修改 `en-US.json` - 英文文案

**添加**：
```json
"blacklist": {
  "blocked": "Blocked",
  "remaining": "Remaining",
  "minutes": "m",
  "seconds": "s",
  "unblock": "Unblock",
  "unblockSuccess": "{name} has been unblocked",
  "unblockFailed": "Failed to unblock"
}
```

---

## 🧪 测试步骤

### 后端功能测试

1. **启动应用**：
   ```bash
   cd G:\claude-lit\cc-r
   wails3 task dev
   ```

2. **检查数据库表**：
   - 打开 `~/.code-switch/app.db`
   - 确认 `provider_blacklist` 和 `app_settings` 表已创建

3. **触发拉黑**：
   - 添加一个故意配置错误的 provider（错误的 API Key）
   - 向该 provider 发送 3 次请求
   - 查看控制台日志，应该看到 "⛔ Provider XXX 已拉黑 30 分钟"

### 前端 UI 测试（如果完成了前端修改）

1. **验证拉黑横幅**：
   - Provider 卡片下方应出现红色横幅
   - 显示 "⛔ 已拉黑 | 剩余: 29分59秒"

2. **验证倒计时**：
   - 每秒递减
   - 格式正确

3. **验证手动解禁**：
   - 点击"立即解禁"按钮
   - 横幅消失
   - Provider 恢复可用

---

## 🐛 故障排除

**问题：拉黑不生效**
- 检查后端日志是否有错误
- 确认数据库表已创建
- 验证 provider 确实失败了

**问题：前端横幅不显示**
- 检查浏览器控制台是否有 API 调用错误
- 确认导入和状态变量已正确添加
- 验证模板代码位置正确

---

作者：Half open flowers
日期：2025-01-14
