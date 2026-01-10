<template>
  <PageLayout
    :eyebrow="t('components.logs.eyebrow')"
    :title="t('components.logs.title')"
    :sticky="true"
    :showBackButton="true"
  >
    <template #actions>
      <div class="actions-group">
        <button class="secondary-btn" @click="clearLogs">清空日志</button>
        <label class="auto-scroll-toggle">
          <input type="checkbox" v-model="autoScroll" />
          <span>自动滚动</span>
        </label>
      </div>
    </template>

    <div class="console-container">
      <div v-if="loading" class="loading-state">
        <div class="spinner"></div>
        <p>加载中...</p>
      </div>

      <div v-else class="console-content" ref="logsContainer">
        <div v-if="logs.length === 0" class="empty-state">
          <p>暂无日志</p>
        </div>

        <div v-for="(log, index) in logs" :key="index" class="log-entry" :class="getLevelClass(log.level)">
          <span class="log-timestamp">{{ formatTimestamp(log.timestamp) }}</span>
          <span class="log-level">{{ log.level }}</span>
          <span class="log-message">{{ log.message }}</span>
        </div>
      </div>
    </div>

  </PageLayout>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { Call } from '@wailsio/runtime'
import PageLayout from '../common/PageLayout.vue'

const { t } = useI18n()

interface ConsoleLog {
  timestamp: string
  level: string
  message: string
}

const logs = ref<ConsoleLog[]>([])
const autoScroll = ref(true)
const loading = ref(false)
const logsContainer = ref<HTMLElement>()
let refreshInterval: number | null = null

const loadLogs = async () => {
  try {
    const result = await Call.ByName('codeswitch/services.ConsoleService.GetLogs')
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
  if (!confirm('确定要清空所有控制台日志吗？')) {
    return
  }

  try {
    await Call.ByName('codeswitch/services.ConsoleService.ClearLogs')
    logs.value = []
  } catch (error) {
    console.error('清空日志失败:', error)
    alert('清空失败：' + (error as Error).message)
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

onMounted(async () => {
  loading.value = true
  await loadLogs()
  loading.value = false

  // 每秒刷新一次日志
  refreshInterval = window.setInterval(loadLogs, 1000)
})

onUnmounted(() => {
  if (refreshInterval) {
    clearInterval(refreshInterval)
  }
})
</script>

<style scoped>
.actions-group {
  display: flex;
  align-items: center;
  gap: 12px;
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
  user-select: text;
  -webkit-user-select: text;
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
  to {
    transform: rotate(360deg);
  }
}
</style>
