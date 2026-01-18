<template>
  <PageLayout
    :title="t('sidebar.terminal_logs')"
    :sticky="true"
  >
    <template #actions>
      <Button variant="outline" size="sm" type="button" @click="handleCopy">
        <svg class="w-4 h-4 mr-2" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
              <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
              <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
            </svg>
            {{ t('components.terminalLogs.actions.copy') }}
      </Button>
      <Button variant="outline" size="sm" type="button" @click="handleOpenFolder">
        <svg class="w-4 h-4 mr-2" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
              <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 2h9a2 2 0 0 1 2 2z"></path>
            </svg>
            {{ t('components.terminalLogs.actions.openFolder') }}
      </Button>
      <Button variant="destructive" size="sm" type="button" @click="handleClear">
        <svg class="w-4 h-4 mr-2" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
              <path d="M9 3h6m-7 4h8m-6 0v11m4-11v11M5 7h14l-.867 12.138A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.862L5 7z" />
            </svg>
            {{ t('components.terminalLogs.actions.clear') }}
      </Button>
    </template>

    <p class="page-lead">{{ t('components.terminalLogs.subtitle') }}</p>

    <!-- Terminal View -->
    <div class="terminal-container">
      <!-- Terminal Header -->
      <div class="terminal-header">
        <div class="terminal-buttons">
          <div class="terminal-button terminal-button-red"></div>
          <div class="terminal-button terminal-button-yellow"></div>
          <div class="terminal-button terminal-button-green"></div>
        </div>
        <div class="terminal-title">{{ t('components.terminalLogs.filename') }}</div>
        <div class="terminal-auto-scroll" @click="toggleAutoScroll">
          <span class="terminal-auto-scroll-text">{{ t('components.console.actions.autoScroll', 'Auto Scroll') }}</span>
          <div class="terminal-auto-scroll-indicator" :class="{ active: autoScroll }" />
        </div>
      </div>

      <!-- Log Content -->
      <ScrollArea ref="scrollAreaRef" height="calc(100vh - 240px)">
        <div class="terminal-content">
          <div v-if="logs.length === 0" class="terminal-empty">
            <p>{{ t('components.terminalLogs.empty') }}</p>
          </div>

          <div v-for="(log, index) in logs" :key="index" class="log-line">
            <span class="log-timestamp">{{ formatTime(log.timestamp) }}</span>
            <span class="log-level" :class="getLevelClass(log.level)">{{ log.level }}</span>
            <span class="log-message">{{ log.message }}</span>
          </div>
        </div>
      </ScrollArea>
    </div>
  </PageLayout>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { Call } from '@wailsio/runtime'
import PageLayout from '../common/PageLayout.vue'
import Button from '../ui/Button.vue'
import ScrollArea from '../ui/ScrollArea.vue'
import { showToast } from '../../utils/toast'
import { GetMITMLogs } from '../../../bindings/codeswitch/services/mitmservice'
import type { MITMLogEntry } from '../../../bindings/codeswitch/services/models'

const { t } = useI18n()

type LogLevel = 'INFO' | 'WARN' | 'ERROR'
interface TerminalLogLine {
  timestamp: MITMLogEntry['timestamp']
  level: LogLevel
  message: string
}

const AUTO_SCROLL_STORAGE_KEY = 'logs-auto-scroll'
const AUTO_SCROLL_EVENT = 'logs-auto-scroll-change'
const MAX_LOGS = 1000

const logs = ref<TerminalLogLine[]>([])
const autoScroll = ref(true)
const scrollAreaRef = ref<InstanceType<typeof ScrollArea> | null>(null)
let refreshInterval: number | null = null

const onAutoScrollChanged = (event: Event) => {
  autoScroll.value = Boolean((event as CustomEvent<boolean>).detail)
}

const loadLogs = async () => {
  try {
    const newLogs = await GetMITMLogs()
    if (!newLogs.length) return

    const newLines = newLogs.map(toTerminalLine).filter(Boolean) as TerminalLogLine[]
    logs.value = [...logs.value, ...newLines].slice(-MAX_LOGS)

    if (autoScroll.value) {
      await nextTick()
      scrollAreaRef.value?.scrollToBottom()
    }
  } catch (error) {
    console.error('Failed to load logs:', error)
  }
}

const handleClear = async () => {
  if (!confirm(t('components.terminalLogs.clearConfirm'))) {
    return
  }

  try {
    logs.value = []
    // Drain remaining buffered logs (avoid re-appearing right after clear)
    await GetMITMLogs()
  } catch (error) {
    console.error('Failed to clear logs:', error)
  }
}

const handleCopy = async () => {
  const text = logs.value.map((l) => `[${formatTime(l.timestamp)}] [${l.level}] ${l.message}`).join('\n')
  try {
    await navigator.clipboard.writeText(text)
    showToast(t('components.logs.detail.copied', 'Copied'), 'success')
  } catch (error) {
    console.error('Failed to copy logs:', error)
    showToast(t('components.logs.detail.copyFailed', 'Copy failed'), 'error')
  }
}

const handleOpenFolder = async () => {
  try {
    await Call.ByName('codeswitch/services.ConsoleService.OpenLogFolder')
  } catch (error) {
    console.error('Failed to open log folder:', error)
    showToast(t('components.terminalLogs.openFolderFailed', 'Failed to open log folder'), 'error')
  }
}

const toggleAutoScroll = () => {
  const nextValue = !autoScroll.value
  autoScroll.value = nextValue
  localStorage.setItem(AUTO_SCROLL_STORAGE_KEY, String(nextValue))
  window.dispatchEvent(new CustomEvent(AUTO_SCROLL_EVENT, { detail: nextValue }))
}

const formatTime = (timestamp: any) => {
  if (!timestamp) return '--:--:--'
  try {
    const value = String(timestamp)
    // ISO format (2023-11-24T12:00:00)
    if (value.includes('T')) {
      return value.split('T')[1].split('.')[0]
    }
    // Normal format (2023-11-24 12:00:00)
    if (value.includes(' ')) {
      return value.split(' ')[1].split('.')[0]
    }
    return value
  } catch {
    return String(timestamp)
  }
}

const toTerminalLine = (entry: MITMLogEntry): TerminalLogLine | null => {
  const statusCode = entry.statusCode || 0
  const hasError = Boolean(entry.error && String(entry.error).trim())
  const level: LogLevel = hasError || statusCode >= 500 ? 'ERROR' : statusCode >= 400 ? 'WARN' : 'INFO'

  const domain = entry.domain || ''
  const path = entry.path || ''
  const target = entry.target ? ` -> ${entry.target}` : ''
  const status = statusCode ? ` ${statusCode}` : ''
  const latency = entry.latency ? ` ${entry.latency}ms` : ''
  const err = hasError ? ` | ${String(entry.error)}` : ''

  const message = `${entry.method || '-'} ${domain}${path}${target}${status}${latency}${err}`.trim()
  return {
    timestamp: entry.timestamp,
    level,
    message,
  }
}

const getLevelClass = (level: LogLevel) => {
  switch (level) {
    case 'ERROR':
      return 'level-error'
    case 'WARN':
      return 'level-warn'
    case 'INFO':
      return 'level-info'
    default:
      return 'level-default'
  }
}

onMounted(() => {
  const saved = localStorage.getItem(AUTO_SCROLL_STORAGE_KEY)
  if (saved === 'false') {
    autoScroll.value = false
  }
  window.addEventListener(AUTO_SCROLL_EVENT, onAutoScrollChanged)

  loadLogs()
  refreshInterval = window.setInterval(() => {
    loadLogs()
  }, 1000)
})

onUnmounted(() => {
  if (refreshInterval !== null) {
    clearInterval(refreshInterval)
  }
  window.removeEventListener(AUTO_SCROLL_EVENT, onAutoScrollChanged)
})
</script>

<style scoped>
.terminal-container {
  background: #09090b;
  border-radius: 0.75rem;
  border: 1px solid #27272a;
  overflow: hidden;
  box-shadow: 0 10px 40px rgba(0, 0, 0, 0.3);
}

.terminal-header {
  height: 2.25rem;
  background: #18181b;
  border-bottom: 1px solid #27272a;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 1rem;
  flex-shrink: 0;
}

.terminal-buttons {
  display: flex;
  gap: 0.375rem;
}

.terminal-button {
  width: 0.75rem;
  height: 0.75rem;
  border-radius: 50%;
}

.terminal-button-red {
  background: rgba(239, 68, 68, 0.2);
  border: 1px solid rgba(239, 68, 68, 0.5);
}

.terminal-button-yellow {
  background: rgba(234, 179, 8, 0.2);
  border: 1px solid rgba(234, 179, 8, 0.5);
}

.terminal-button-green {
  background: rgba(34, 197, 94, 0.2);
  border: 1px solid rgba(34, 197, 94, 0.5);
}

.terminal-title {
  font-family: 'SF Mono', 'Monaco', 'Consolas', monospace;
  font-size: 0.625rem;
  color: #71717a;
}

.terminal-auto-scroll {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  cursor: pointer;
  transition: opacity 0.2s;
}

.terminal-auto-scroll:hover {
  opacity: 0.8;
}

.terminal-auto-scroll-text {
  font-size: 0.625rem;
  color: #71717a;
}

.terminal-auto-scroll-indicator {
  width: 0.5rem;
  height: 0.5rem;
  border-radius: 50%;
  background: #3f3f46;
  transition: background 0.2s;
}

.terminal-auto-scroll-indicator.active {
  background: #22c55e;
}

.terminal-content {
  padding: 1rem;
  font-family: 'SF Mono', 'Monaco', 'Consolas', monospace;
  font-size: 0.75rem;
  line-height: 1.5;
  color: #a1a1aa;
}

.terminal-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 5rem 0;
  color: #3f3f46;
  opacity: 0.5;
}

.log-line {
  display: flex;
  gap: 0.75rem;
  padding: 0.125rem 0.25rem;
  border-radius: 0.25rem;
  transition: background 0.15s;
  word-break: break-all;
}

.log-line:hover {
  background: rgba(255, 255, 255, 0.03);
}

.log-timestamp {
  color: #52525b;
  flex-shrink: 0;
  user-select: none;
  min-width: 130px;
}

.log-level {
  font-weight: 600;
  flex-shrink: 0;
  min-width: 60px;
  text-transform: uppercase;
}

.level-error {
  color: #ef4444;
}

.level-warn {
  color: #eab308;
}

.level-info {
  color: #3b82f6;
}

.level-default {
  color: #71717a;
}

.log-message {
  color: #d4d4d8;
  flex: 1;
}

/* 覆盖默认主题样式（在终端容器内） */
.terminal-container * {
  color: inherit;
}
</style>
