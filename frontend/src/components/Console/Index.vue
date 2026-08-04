<script setup lang="ts">
import { ref, onMounted, onUnmounted, onActivated, onDeactivated, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { Call } from '@wailsio/runtime'
import { createPoller } from '../../composables/usePoller'

interface ConsoleLog {
  timestamp: string
  level: string
  message: string
}

const { t } = useI18n()
const logs = ref<ConsoleLog[]>([])
const autoScroll = ref(true)
const loading = ref(false)
const logsContainer = ref<HTMLElement>()

const loadLogs = async () => {
  try {
    // 只取最近 200 条：全量拉取在日志堆积后每轮都是大 payload
    const result = await Call.ByName('codeswitch/services.ConsoleService.GetRecentLogs', 200)
    logs.value = result as ConsoleLog[]

    if (autoScroll.value) {
      await nextTick()
      scrollToBottom()
    }
  } catch (error) {
    console.error('加载控制台日志失败:', error)
  }
}

const clearLogs = async () => {
  if (!confirm(t('components.console.clearConfirm'))) {
    return
  }

  try {
    await Call.ByName('codeswitch/services.ConsoleService.ClearLogs')
    logs.value = []
  } catch (error) {
    console.error('清空日志失败:', error)
    alert(t('components.console.clearFailed') + (error as Error).message)
  }
}

const scrollToBottom = () => {
  if (logsContainer.value) {
    logsContainer.value.scrollTop = logsContainer.value.scrollHeight
  }
}

const formatTimestamp = (timestamp: string) => {
  const date = new Date(timestamp)
  return date.toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

const getLevelClass = (level: string) => {
  switch (level.toUpperCase()) {
    case 'ERROR':
      return 'log-error'
    case 'WARN':
      return 'log-warn'
    default:
      return 'log-info'
  }
}

// 每 2 秒刷新一次日志，仅页面激活期间运行（keep-alive 下切走即停）
const logPoller = createPoller(loadLogs, 2000)
// 首载 Promise：keep-alive 下首次进入 mounted 与 activated 均触发，
// activated 等首载完成后再启动轮询，避免双份加载
let initialLoad: Promise<void> | null = null
// 页面是否处于激活状态：等待首载期间被切走时不得启动轮询
let pageActive = false

onMounted(() => {
  initialLoad = (async () => {
    loading.value = true
    await loadLogs()
    loading.value = false
  })()
})

onActivated(async () => {
  pageActive = true
  await initialLoad
  if (!pageActive) return
  logPoller.start()
})

onDeactivated(() => {
  pageActive = false
  logPoller.stop()
})

onUnmounted(() => {
  pageActive = false
  logPoller.stop()
})
</script>

<template>
  <div class="main-shell console-shell">
    <header class="app-page-header">
      <div class="app-page-title-group">
        <h1 class="app-page-title">{{ t('components.console.title') }}</h1>
      </div>
      <div class="app-page-actions">
        <label class="auto-scroll-toggle">
          <input type="checkbox" v-model="autoScroll" />
          <span>{{ t('components.console.autoScroll') }}</span>
        </label>
        <button class="secondary-btn" @click="clearLogs">{{ t('components.console.clear') }}</button>
      </div>
    </header>

    <div class="app-page-container console-page">
      <div class="console-container">
        <div v-if="loading" class="loading-state">
          <div class="spinner"></div>
          <p>{{ t('components.console.loading') }}</p>
        </div>

        <div v-else class="console-content" ref="logsContainer">
          <div v-if="logs.length === 0" class="empty-state">
            <p>{{ t('components.console.empty') }}</p>
          </div>

          <div
            v-for="(log, index) in logs"
            :key="index"
            class="log-entry"
            :class="getLevelClass(log.level)"
          >
            <span class="log-timestamp">{{ formatTimestamp(log.timestamp) }}</span>
            <span class="log-level">{{ log.level }}</span>
            <span class="log-message">{{ log.message }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.console-shell {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

.console-page {
  min-height: 0;
}

.auto-scroll-toggle {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 0.9rem;
  color: var(--mac-text-secondary);
  cursor: pointer;
  user-select: none;
}

.auto-scroll-toggle input[type="checkbox"] {
  cursor: pointer;
}

.console-container {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  background: var(--mac-surface);
  border: 1px solid var(--mac-border);
  border-radius: 12px;
  display: flex;
  flex-direction: column;
}

.console-content {
  flex: 1;
  overflow-y: auto;
  padding: 16px;
  font-family: 'SF Mono', 'Monaco', 'Inconsolata', 'Fira Code', 'Consolas', monospace;
  font-size: 0.85rem;
  line-height: 1.6;
  background: #1e1e1e;
  color: #d4d4d4;
}

html.dark .console-content {
  background: #0d1117;
  color: #e6edf3;
}

.log-entry {
  display: flex;
  gap: 12px;
  padding: 4px 0;
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
}

.log-entry:last-child {
  border-bottom: none;
}

.log-timestamp {
  flex-shrink: 0;
  color: #858585;
  font-weight: 500;
}

.log-level {
  flex-shrink: 0;
  min-width: 50px;
  font-weight: 600;
}

.log-info .log-level {
  color: #4ec9b0;
}

.log-warn .log-level {
  color: #dcdcaa;
}

.log-error .log-level {
  color: #f48771;
}

.log-message {
  flex: 1;
  white-space: pre-wrap;
  word-break: break-word;
}

.loading-state,
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--mac-text-secondary);
}

.spinner {
  width: 32px;
  height: 32px;
  border: 3px solid rgba(0, 0, 0, 0.1);
  border-top-color: var(--mac-accent);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
  margin-bottom: 12px;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}
</style>
