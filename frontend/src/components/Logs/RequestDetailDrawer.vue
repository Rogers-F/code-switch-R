<template>
  <TransitionRoot as="template" :show="open">
    <div class="drawer-backdrop" @click.self="$emit('close')">
      <TransitionChild as="template" enter="ease-out duration-300" enter-from="translate-x-full"
        enter-to="translate-x-0" leave="ease-in duration-200" leave-from="translate-x-0" leave-to="translate-x-full">
        <div class="drawer-panel">
          <!-- 头部 -->
          <header class="drawer-header">
            <h2 class="drawer-title">{{ t('components.logs.detail.title') }}</h2>
            <button class="drawer-close" @click="$emit('close')" :aria-label="t('common.cancel')">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path d="M6 6l12 12M6 18L18 6" fill="none" stroke="currentColor" stroke-width="2"
                  stroke-linecap="round" />
              </svg>
            </button>
          </header>

          <!-- 内容 -->
          <div class="drawer-body">
            <!-- 无数据提示 -->
            <div v-if="!detail" class="drawer-empty">
              <div class="empty-icon" aria-hidden="true">
                <svg viewBox="0 0 24 24">
                  <path
                    d="M22 12h-6l-2 3h-4l-2-3H2"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.8"
                    stroke-linecap="round"
                    stroke-linejoin="round"
                  />
                  <path
                    d="M5.45 5.11L2 12v7a2 2 0 002 2h16a2 2 0 002-2v-7l-3.45-6.89A2 2 0 0016.76 4H7.24a2 2 0 00-1.79 1.11z"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.8"
                    stroke-linejoin="round"
                  />
                </svg>
              </div>
              <p class="empty-title">{{ t('components.logs.detail.notFound') }}</p>
              <p class="empty-hint">{{ t('components.logs.detail.notFoundHint') }}</p>
            </div>

            <template v-else>
              <!-- 基本信息卡片 -->
              <section class="detail-section">
                <div class="section-header" @click="toggleSection('basic')">
                  <span class="section-icon" aria-hidden="true">
                    <svg viewBox="0 0 24 24">
                      <circle cx="12" cy="12" r="10" fill="none" stroke="currentColor" stroke-width="1.8" />
                      <path
                        d="M12 16v-4"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.8"
                        stroke-linecap="round"
                      />
                      <path
                        d="M12 8h.01"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="2.4"
                        stroke-linecap="round"
                      />
                    </svg>
                  </span>
                  <h3 class="section-title">{{ t('components.logs.detail.basicInfo') }}</h3>
                  <span class="section-toggle" :class="{ expanded: sections.basic }">▾</span>
                </div>
                <div v-show="sections.basic" class="section-content">
                  <div class="info-grid">
                    <div class="info-item">
                      <span class="info-label">{{ t('components.logs.table.time') }}</span>
                      <span class="info-value">{{ formatTime(detail.timestamp) }}</span>
                    </div>
                    <div class="info-item">
                      <span class="info-label">{{ t('components.logs.table.platform') }}</span>
                      <span class="info-value platform-badge">{{ detail.platform }}</span>
                    </div>
                    <div class="info-item">
                      <span class="info-label">{{ t('components.logs.table.provider') }}</span>
                      <span class="info-value">{{ detail.provider }}</span>
                    </div>
                    <div class="info-item">
                      <span class="info-label">{{ t('components.logs.table.model') }}</span>
                      <span class="info-value model-name">{{ detail.model }}</span>
                    </div>
                    <div class="info-item">
                      <span class="info-label">HTTP</span>
                      <span class="info-value" :class="httpCodeClass(detail.http_code)">{{ detail.http_code }}</span>
                    </div>
                    <div class="info-item">
                      <span class="info-label">{{ t('components.logs.table.duration') }}</span>
                      <span class="info-value">{{ formatDuration(detail.duration_ms) }}</span>
                    </div>
                  </div>
                </div>
              </section>

              <!-- 请求头 -->
              <section class="detail-section">
                <div class="section-header" @click="toggleSection('headers')">
                  <span class="section-icon" aria-hidden="true">
                    <svg viewBox="0 0 24 24">
                      <path
                        d="M15 7a5 5 0 11-9.9 1.2A5 5 0 0115 7z"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.8"
                      />
                      <path
                        d="M10 11l9 9m-3-3h-3v3h-3v-3H7v-3h3l2-2"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.8"
                        stroke-linecap="round"
                        stroke-linejoin="round"
                      />
                    </svg>
                  </span>
                  <h3 class="section-title">{{ t('components.logs.detail.headers') }}</h3>
                  <span class="section-toggle" :class="{ expanded: sections.headers }">▾</span>
                </div>
                <div v-show="sections.headers" class="section-content">
                  <div class="headers-list">
                    <div v-for="(value, key) in detail.headers" :key="key" class="header-item">
                      <span class="header-key">{{ key }}</span>
                      <span class="header-value">{{ value }}</span>
                    </div>
                    <div v-if="!Object.keys(detail.headers || {}).length" class="empty-hint">
                      {{ t('components.logs.detail.noHeaders') }}
                    </div>
                  </div>
                </div>
              </section>

              <!-- 响应头 -->
              <section class="detail-section">
                <div class="section-header" @click="toggleSection('responseHeaders')">
                  <span class="section-icon" aria-hidden="true">
                    <svg viewBox="0 0 24 24">
                      <path
                        d="M22 12h-6l-2 3h-4l-2-3H2"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.8"
                        stroke-linecap="round"
                        stroke-linejoin="round"
                      />
                      <path
                        d="M5.45 5.11L2 12v7a2 2 0 002 2h16a2 2 0 002-2v-7l-3.45-6.89A2 2 0 0016.76 4H7.24a2 2 0 00-1.79 1.11z"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.8"
                        stroke-linejoin="round"
                      />
                      <path
                        d="M12 8v6"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.8"
                        stroke-linecap="round"
                      />
                      <path
                        d="M9 11l3 3 3-3"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.8"
                        stroke-linecap="round"
                        stroke-linejoin="round"
                      />
                    </svg>
                  </span>
                  <h3 class="section-title">{{ t('components.logs.detail.responseHeaders') }}</h3>
                  <span class="section-toggle" :class="{ expanded: sections.responseHeaders }">▾</span>
                </div>
                <div v-show="sections.responseHeaders" class="section-content">
                  <div class="headers-list">
                    <div v-for="(value, key) in detail.response_headers" :key="key" class="header-item">
                      <span class="header-key">{{ key }}</span>
                      <span class="header-value">{{ value }}</span>
                    </div>
                    <div v-if="!Object.keys(detail.response_headers || {}).length" class="empty-hint">
                      {{ t('components.logs.detail.noHeaders') }}
                    </div>
                  </div>
                </div>
              </section>

              <!-- 请求体 -->
              <section class="detail-section">
                <div class="section-header" @click="toggleSection('request')">
                  <span class="section-icon" aria-hidden="true">
                    <svg viewBox="0 0 24 24">
                      <path
                        d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.8"
                        stroke-linecap="round"
                        stroke-linejoin="round"
                      />
                      <path
                        d="M17 8l-5-5-5 5"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.8"
                        stroke-linecap="round"
                        stroke-linejoin="round"
                      />
                      <path
                        d="M12 3v12"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.8"
                        stroke-linecap="round"
                      />
                    </svg>
                  </span>
                  <h3 class="section-title">
                    {{ t('components.logs.detail.requestBody') }}
                    <span class="size-badge">{{ formatSize(detail.request_size) }}</span>
                  </h3>
                  <div class="section-actions">
                    <button
                      class="action-btn-small"
                      type="button"
                      @click.stop="copyContent(detail.request_body)"
                      :title="t('components.logs.detail.copy')"
                      :aria-label="t('components.logs.detail.copy')"
                    >
                      <svg viewBox="0 0 24 24" aria-hidden="true">
                        <rect x="9" y="9" width="13" height="13" rx="2" ry="2" fill="none" stroke="currentColor" stroke-width="2" />
                        <path
                          d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                        />
                      </svg>
                    </button>
                    <button
                      class="action-btn-small"
                      type="button"
                      @click.stop="toggleFormat('request')"
                      :title="t('components.logs.detail.format')"
                      :aria-label="t('components.logs.detail.format')"
                    >
                      <svg v-if="formatModes.request" viewBox="0 0 24 24" aria-hidden="true">
                        <path
                          d="M10 6c-2 1-3 2.5-3 6s1 5 3 6"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                          stroke-linecap="round"
                        />
                        <path
                          d="M14 6c2 1 3 2.5 3 6s-1 5-3 6"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                          stroke-linecap="round"
                        />
                      </svg>
                      <svg v-else viewBox="0 0 24 24" aria-hidden="true">
                        <path
                          d="M16 18l6-6-6-6"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                        />
                        <path
                          d="M8 6l-6 6 6 6"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                        />
                      </svg>
                    </button>
                  </div>
                  <span class="section-toggle" :class="{ expanded: sections.request }">▾</span>
                </div>
                <div v-show="sections.request" class="section-content">
                  <pre class="code-block"
                    :class="{ truncated: detail.truncated }"><code>{{ formatJson(detail.request_body, formatModes.request) }}</code></pre>
                  <div v-if="detail.truncated && detail.request_size > 307200" class="truncated-notice">
                    <span class="truncated-icon" aria-hidden="true">
                      <svg viewBox="0 0 24 24">
                        <path
                          d="M12 9v4m0 4h.01M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                        />
                      </svg>
                    </span>
                    <span>{{ t('components.logs.detail.truncated', { size: formatSize(detail.request_size) }) }}</span>
                  </div>
                </div>
              </section>

              <!-- 响应体 -->
              <section class="detail-section">
                <div class="section-header" @click="toggleSection('response')">
                  <span class="section-icon" aria-hidden="true">
                    <svg viewBox="0 0 24 24">
                      <path
                        d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.8"
                        stroke-linecap="round"
                        stroke-linejoin="round"
                      />
                      <path
                        d="M7 10l5 5 5-5"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.8"
                        stroke-linecap="round"
                        stroke-linejoin="round"
                      />
                      <path
                        d="M12 15V3"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.8"
                        stroke-linecap="round"
                      />
                    </svg>
                  </span>
                  <h3 class="section-title">
                    {{ t('components.logs.detail.responseBody') }}
                    <span class="size-badge">{{ formatSize(detail.response_size) }}</span>
                  </h3>
                  <div class="section-actions">
                    <button
                      class="action-btn-small"
                      type="button"
                      @click.stop="copyContent(detail.response_body)"
                      :title="t('components.logs.detail.copy')"
                      :aria-label="t('components.logs.detail.copy')"
                    >
                      <svg viewBox="0 0 24 24" aria-hidden="true">
                        <rect x="9" y="9" width="13" height="13" rx="2" ry="2" fill="none" stroke="currentColor" stroke-width="2" />
                        <path
                          d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                        />
                      </svg>
                    </button>
                    <button
                      class="action-btn-small"
                      type="button"
                      @click.stop="toggleFormat('response')"
                      :title="t('components.logs.detail.format')"
                      :aria-label="t('components.logs.detail.format')"
                    >
                      <svg v-if="formatModes.response" viewBox="0 0 24 24" aria-hidden="true">
                        <path
                          d="M10 6c-2 1-3 2.5-3 6s1 5 3 6"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                          stroke-linecap="round"
                        />
                        <path
                          d="M14 6c2 1 3 2.5 3 6s-1 5-3 6"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                          stroke-linecap="round"
                        />
                      </svg>
                      <svg v-else viewBox="0 0 24 24" aria-hidden="true">
                        <path
                          d="M16 18l6-6-6-6"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                        />
                        <path
                          d="M8 6l-6 6 6 6"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                        />
                      </svg>
                    </button>
                  </div>
                  <span class="section-toggle" :class="{ expanded: sections.response }">▾</span>
                </div>
                <div v-show="sections.response" class="section-content">
                  <pre class="code-block response-block"
                    :class="{ truncated: detail.truncated }"><code>{{ formatJson(detail.response_body, formatModes.response) }}</code></pre>
                  <div v-if="detail.truncated && detail.response_size > 307200" class="truncated-notice">
                    <span class="truncated-icon" aria-hidden="true">
                      <svg viewBox="0 0 24 24">
                        <path
                          d="M12 9v4m0 4h.01M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                        />
                      </svg>
                    </span>
                    <span>{{ t('components.logs.detail.truncated', { size: formatSize(detail.response_size) }) }}</span>
                  </div>
                </div>
              </section>

              <!-- URL -->
              <section class="detail-section">
                <div class="section-header" @click="toggleSection('url')">
                  <span class="section-icon" aria-hidden="true">
                    <svg viewBox="0 0 24 24">
                      <path
                        d="M10 13a5 5 0 007.07 0l1.41-1.41a5 5 0 00-7.07-7.07L10 5"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.8"
                        stroke-linecap="round"
                        stroke-linejoin="round"
                      />
                      <path
                        d="M14 11a5 5 0 00-7.07 0L5.52 12.4a5 5 0 007.07 7.07L14 19"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.8"
                        stroke-linecap="round"
                        stroke-linejoin="round"
                      />
                    </svg>
                  </span>
                  <h3 class="section-title">{{ t('components.logs.detail.requestUrl') }}</h3>
                  <span class="section-toggle" :class="{ expanded: sections.url }">▾</span>
                </div>
                <div v-show="sections.url" class="section-content">
                  <div class="url-display">
                    <code>{{ detail.request_url }}</code>
                    <button
                      class="action-btn-small"
                      type="button"
                      @click="copyContent(detail.request_url)"
                      :title="t('components.logs.detail.copy')"
                      :aria-label="t('components.logs.detail.copy')"
                    >
                      <svg viewBox="0 0 24 24" aria-hidden="true">
                        <rect x="9" y="9" width="13" height="13" rx="2" ry="2" fill="none" stroke="currentColor" stroke-width="2" />
                        <path
                          d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"
                          fill="none"
                          stroke="currentColor"
                          stroke-width="2"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                        />
                      </svg>
                    </button>
                  </div>
                </div>
              </section>
            </template>
          </div>
        </div>
      </TransitionChild>
    </div>
  </TransitionRoot>
</template>

<script setup lang="ts">
import { ref, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { TransitionRoot, TransitionChild } from '@headlessui/vue'
import { getRequestDetail, type RequestDetail } from '../../services/requestDetail'
import { showToast } from '../../utils/toast'

const { t } = useI18n()

const props = defineProps<{
  open: boolean
  sequenceId: number | null
}>()

defineEmits<{
  (e: 'close'): void
}>()

const detail = ref<RequestDetail | null>(null)
const loading = ref(false)

const sections = reactive({
  basic: true,
  headers: false,
  responseHeaders: false,
  request: true,
  response: true,
  url: false,
})

const formatModes = reactive({
  request: true,
  response: true,
})

const toggleSection = (key: keyof typeof sections) => {
  sections[key] = !sections[key]
}

const toggleFormat = (key: 'request' | 'response') => {
  formatModes[key] = !formatModes[key]
}

const loadDetail = async (seqId: number) => {
  loading.value = true
  try {
    const result = await getRequestDetail(seqId)
    detail.value = result
  } catch (error) {
    console.error('Failed to load request detail:', error)
    detail.value = null
  } finally {
    loading.value = false
  }
}

watch(() => [props.open, props.sequenceId], ([open, seqId]) => {
  if (open && seqId) {
    loadDetail(seqId as number)
  } else if (!open) {
    detail.value = null
  }
}, { immediate: true })

const formatTime = (timestamp: string) => {
  if (!timestamp) return '—'
  const date = new Date(timestamp)
  if (isNaN(date.getTime())) return timestamp
  return date.toLocaleString()
}

const formatDuration = (ms: number) => {
  if (!ms || isNaN(ms)) return '—'
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

const formatSize = (bytes: number) => {
  if (!bytes || bytes <= 0) return '0 B'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`
}

const formatJson = (content: string, shouldFormat: boolean): string => {
  if (!content) return ''
  if (!shouldFormat) return content

  // 尝试解析纯 JSON
  try {
    const parsed = JSON.parse(content)
    return JSON.stringify(parsed, null, 2)
  } catch {
    // 不是纯 JSON，尝试解析 SSE 格式
  }

  // 检测 SSE 格式（data: {...}）
  if (content.includes('data: ')) {
    return formatSSE(content)
  }

  return content
}

// 格式化 SSE 响应（Server-Sent Events）
const formatSSE = (content: string): string => {
  const lines = content.split('\n')
  const formattedLines: string[] = []

  for (const line of lines) {
    const trimmed = line.trim()
    if (!trimmed) {
      formattedLines.push('')
      continue
    }

    // 处理 data: 行
    if (trimmed.startsWith('data: ')) {
      const jsonStr = trimmed.slice(6) // 移除 'data: ' 前缀
      if (jsonStr === '[DONE]') {
        formattedLines.push('data: [DONE]')
        continue
      }
      try {
        const parsed = JSON.parse(jsonStr)
        formattedLines.push('data: ' + JSON.stringify(parsed, null, 2))
      } catch {
        formattedLines.push(trimmed)
      }
    } else {
      formattedLines.push(trimmed)
    }
  }

  return formattedLines.join('\n')
}

const httpCodeClass = (code: number) => {
  if (code >= 500) return 'http-server-error'
  if (code >= 400) return 'http-client-error'
  if (code >= 300) return 'http-redirect'
  if (code >= 200) return 'http-success'
  return 'http-info'
}

const copyContent = async (content: string) => {
  if (!content) return
  try {
    await navigator.clipboard.writeText(content)
    showToast(t('components.logs.detail.copied'), 'success')
  } catch (error) {
    console.error('Copy failed:', error)
    showToast(t('components.logs.detail.copyFailed'), 'error')
  }
}
</script>

<style scoped>
.drawer-backdrop {
  position: fixed;
  inset: 0;
  z-index: 2000;
  background: rgba(15, 23, 42, 0.4);
  backdrop-filter: blur(4px);
}

.drawer-panel {
  position: absolute;
  right: 0;
  top: 0;
  bottom: 0;
  width: min(480px, 90vw);
  background: var(--mac-surface);
  border-left: 1px solid var(--mac-border);
  box-shadow: -20px 0 60px rgba(0, 0, 0, 0.2);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.drawer-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 20px 24px;
  border-bottom: 1px solid var(--mac-border);
  background: linear-gradient(180deg, var(--mac-surface-strong) 0%, var(--mac-surface) 100%);
}

.drawer-title {
  margin: 0;
  font-size: 1.1rem;
  font-weight: 600;
  color: var(--mac-text);
}

.drawer-close {
  width: 32px;
  height: 32px;
  border: none;
  background: rgba(15, 23, 42, 0.06);
  border-radius: 8px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.15s ease;
}

.drawer-close:hover {
  background: rgba(15, 23, 42, 0.12);
}

.drawer-close svg {
  width: 16px;
  height: 16px;
  color: var(--mac-text-secondary);
}

html.dark .drawer-close {
  background: rgba(255, 255, 255, 0.08);
}

html.dark .drawer-close:hover {
  background: rgba(255, 255, 255, 0.15);
}

.drawer-body {
  flex: 1;
  overflow-y: auto;
  padding: 16px;
  user-select: text;
  -webkit-user-select: text;
}

/* 空状态 */
.drawer-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 48px 24px;
  text-align: center;
}

.empty-icon {
  width: 56px;
  height: 56px;
  margin-bottom: 16px;
  opacity: 0.6;
  color: var(--mac-text-secondary);
  display: inline-flex;
  align-items: center;
  justify-content: center;
}

.empty-icon svg {
  width: 56px;
  height: 56px;
  display: block;
}

.empty-title {
  font-size: 1rem;
  font-weight: 600;
  color: var(--mac-text);
  margin: 0 0 8px;
}

.empty-hint {
  font-size: 0.85rem;
  color: var(--mac-text-secondary);
  margin: 0;
  line-height: 1.5;
}

/* 详情区块 */
.detail-section {
  margin-bottom: 12px;
  border-radius: 12px;
  border: 1px solid var(--mac-border);
  background: var(--mac-surface-strong);
  overflow: hidden;
}

.section-header {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
  cursor: pointer;
  transition: background 0.15s ease;
  user-select: none;
}

.section-header:hover {
  background: rgba(15, 23, 42, 0.04);
}

html.dark .section-header:hover {
  background: rgba(255, 255, 255, 0.04);
}

.section-icon {
  width: 16px;
  height: 16px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--mac-text-secondary);
  flex-shrink: 0;
}

.section-icon svg {
  width: 16px;
  height: 16px;
  display: block;
}

.section-title {
  flex: 1;
  margin: 0;
  font-size: 0.9rem;
  font-weight: 600;
  color: var(--mac-text);
  display: flex;
  align-items: center;
  gap: 8px;
}

.section-toggle {
  font-size: 0.8rem;
  color: var(--mac-text-secondary);
  transition: transform 0.2s ease;
}

.section-toggle.expanded {
  transform: rotate(180deg);
}

.section-actions {
  display: flex;
  gap: 4px;
}

.action-btn-small {
  width: 28px;
  height: 28px;
  border: none;
  background: rgba(15, 23, 42, 0.06);
  border-radius: 6px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.15s ease;
}

.action-btn-small svg {
  width: 16px;
  height: 16px;
  display: block;
  color: var(--mac-text-secondary);
}

.action-btn-small:hover svg {
  color: var(--mac-text);
}

.action-btn-small:hover {
  background: rgba(15, 23, 42, 0.12);
}

html.dark .action-btn-small {
  background: rgba(255, 255, 255, 0.08);
}

html.dark .action-btn-small:hover {
  background: rgba(255, 255, 255, 0.15);
}

.section-content {
  padding: 0 16px 16px;
}

/* 信息网格 */
.info-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 12px;
}

.info-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.info-label {
  font-size: 0.75rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--mac-text-secondary);
}

.info-value {
  font-size: 0.9rem;
  font-weight: 500;
  color: var(--mac-text);
  word-break: break-all;
}

.platform-badge {
  display: inline-block;
  padding: 2px 8px;
  background: rgba(10, 132, 255, 0.1);
  color: #0a84ff;
  border-radius: 4px;
  font-size: 0.8rem;
  width: fit-content;
}

.model-name {
  font-family: 'SF Mono', Consolas, monospace;
  font-size: 0.85rem;
}

.size-badge {
  font-size: 0.7rem;
  font-weight: 500;
  padding: 2px 6px;
  background: rgba(15, 23, 42, 0.08);
  border-radius: 4px;
  color: var(--mac-text-secondary);
}

html.dark .size-badge {
  background: rgba(255, 255, 255, 0.1);
}

/* HTTP 状态颜色 */
.http-success {
  color: #34d399;
}

.http-redirect {
  color: #60a5fa;
}

.http-client-error {
  color: #fbbf24;
}

.http-server-error {
  color: #f87171;
}

.http-info {
  color: var(--mac-text-secondary);
}

/* 请求头列表 */
.headers-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.header-item {
  display: flex;
  gap: 12px;
  padding: 8px 12px;
  background: rgba(15, 23, 42, 0.04);
  border-radius: 8px;
  font-size: 0.85rem;
}

html.dark .header-item {
  background: rgba(255, 255, 255, 0.04);
}

.header-key {
  font-weight: 600;
  color: #0a84ff;
  min-width: 120px;
  flex-shrink: 0;
}

.header-value {
  color: var(--mac-text);
  word-break: break-all;
  font-family: 'SF Mono', Consolas, monospace;
}

/* 代码块 */
.code-block {
  margin: 0;
  padding: 16px;
  background: #1e1e1e;
  border-radius: 8px;
  overflow-x: auto;
  font-family: 'SF Mono', Monaco, Consolas, monospace;
  font-size: 0.8rem;
  line-height: 1.5;
  color: #d4d4d4;
  max-height: 300px;
  overflow-y: auto;
}

html.dark .code-block {
  background: #0d1117;
  color: #e6edf3;
}

.code-block.truncated {
  border-bottom-left-radius: 0;
  border-bottom-right-radius: 0;
}

.response-block {
  max-height: 400px;
}

.truncated-notice {
  padding: 8px 16px;
  background: rgba(251, 191, 36, 0.1);
  color: #d97706;
  font-size: 0.8rem;
  border-bottom-left-radius: 8px;
  border-bottom-right-radius: 8px;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

html.dark .truncated-notice {
  background: rgba(251, 191, 36, 0.15);
  color: #fbbf24;
}

.truncated-icon {
  width: 14px;
  height: 14px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.truncated-icon svg {
  width: 14px;
  height: 14px;
  display: block;
}

/* URL 显示 */
.url-display {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px;
  background: rgba(15, 23, 42, 0.04);
  border-radius: 8px;
}

html.dark .url-display {
  background: rgba(255, 255, 255, 0.04);
}

.url-display code {
  flex: 1;
  font-family: 'SF Mono', Consolas, monospace;
  font-size: 0.8rem;
  color: var(--mac-text);
  word-break: break-all;
}
</style>
