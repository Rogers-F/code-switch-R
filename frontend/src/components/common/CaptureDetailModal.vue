<template>
  <BaseModal :open="open" :title="t('components.logs.captureDetail.title')" @close="$emit('close')">
    <div class="capture-detail-modal">
      <p v-if="loading" class="capture-detail-hint">
        {{ t('components.logs.loading') }}
      </p>
      <p v-else-if="error" class="capture-detail-hint capture-detail-error">
        {{ t('components.logs.captureDetail.loadFailed') }}{{ error }}
      </p>
      <template v-else-if="data">
        <div v-if="isEmpty" class="capture-detail-hint">
          {{ t('components.logs.captureDetail.empty') }}
        </div>
        <template v-else>
          <p class="capture-detail-warning">⚠ {{ t('components.logs.captureDetail.sensitiveWarning') }}</p>

          <section v-if="data.request_url" class="capture-section">
            <div class="capture-section__header">
              <h4>{{ t('components.logs.captureDetail.url') }}</h4>
              <button class="capture-copy-btn" @click="copyText(data.request_url)">{{ t('components.logs.captureDetail.copy') }}</button>
            </div>
            <pre class="capture-pre capture-pre--url">{{ data.request_url }}</pre>
          </section>

          <section v-if="data.request_headers" class="capture-section">
            <div class="capture-section__header">
              <h4>{{ t('components.logs.captureDetail.reqHeaders') }}</h4>
              <button class="capture-copy-btn" @click="copyText(prettyJSON(data.request_headers))">{{ t('components.logs.captureDetail.copy') }}</button>
            </div>
            <pre class="capture-pre">{{ prettyJSON(data.request_headers) }}</pre>
          </section>

          <section v-if="data.request_body || data.body_truncated" class="capture-section">
            <div class="capture-section__header">
              <h4>{{ t('components.logs.captureDetail.reqBody') }}</h4>
              <button class="capture-copy-btn" @click="copyText(data.request_body)">{{ t('components.logs.captureDetail.copy') }}</button>
            </div>
            <p v-if="data.body_truncated" class="capture-detail-hint">
              {{ t('components.logs.captureDetail.captureTruncatedHint', { bytes: formatBytes(data.body_bytes) }) }}
            </p>
            <p v-else-if="data.request_body_preview" class="capture-detail-hint">
              {{ t('components.logs.captureDetail.previewHint', { bytes: formatBytes(data.body_bytes) }) }}
            </p>
            <pre v-if="data.request_body" class="capture-pre">{{ data.request_body }}</pre>
          </section>

          <section v-if="data.response_headers" class="capture-section">
            <div class="capture-section__header">
              <h4>{{ t('components.logs.captureDetail.respHeaders') }}</h4>
              <button class="capture-copy-btn" @click="copyText(prettyJSON(data.response_headers))">{{ t('components.logs.captureDetail.copy') }}</button>
            </div>
            <pre class="capture-pre">{{ prettyJSON(data.response_headers) }}</pre>
          </section>

          <section v-if="data.response_body || data.response_truncated || data.budget_skipped" class="capture-section">
            <div class="capture-section__header">
              <h4>{{ t('components.logs.captureDetail.respBody') }}</h4>
              <button v-if="data.response_body" class="capture-copy-btn" @click="copyText(data.response_body)">{{ t('components.logs.captureDetail.copy') }}</button>
            </div>
            <p v-if="data.budget_skipped" class="capture-detail-hint capture-detail-error">
              {{ t('components.logs.captureDetail.budgetSkippedHint') }}
            </p>
            <p v-else-if="data.response_truncated" class="capture-detail-hint">
              {{ t('components.logs.captureDetail.captureTruncatedHint', { bytes: formatBytes(data.response_bytes) }) }}
            </p>
            <p v-else-if="data.response_body_preview" class="capture-detail-hint">
              {{ t('components.logs.captureDetail.previewHint', { bytes: formatBytes(data.response_bytes) }) }}
            </p>
            <pre v-if="data.response_body" class="capture-pre">{{ data.response_body }}</pre>
          </section>
        </template>
      </template>
    </div>
  </BaseModal>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseModal from './BaseModal.vue'
import type { RequestLogDetail } from '../../services/logs'

const props = defineProps<{
  open: boolean
  loading: boolean
  error: string
  data: RequestLogDetail | null
}>()
defineEmits<{ close: [] }>()

const { t } = useI18n()

const isEmpty = computed(() => {
  const d = props.data
  if (!d) return true
  return !d.request_url && !d.request_headers && !d.request_body && !d.response_headers &&
    !d.response_body && !d.body_truncated && !d.response_truncated && !d.budget_skipped
})

const prettyJSON = (text: string) => {
  try {
    return JSON.stringify(JSON.parse(text), null, 2)
  } catch {
    return text
  }
}

const formatBytes = (bytes: number) => {
  if (!bytes) return '0 B'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

const copyText = async (text: string) => {
  try {
    await navigator.clipboard.writeText(text)
  } catch (error) {
    console.error('复制失败:', error)
  }
}
</script>

<style scoped>
.capture-detail-modal {
  display: flex;
  flex-direction: column;
  gap: 14px;
  max-height: 70vh;
  overflow-y: auto;
}

.capture-detail-hint {
  color: var(--mac-text-secondary);
  font-size: 0.85rem;
}

.capture-detail-error {
  color: #ef4444;
}

.capture-detail-warning {
  margin: 0;
  padding: 8px 12px;
  border-radius: 8px;
  background: rgba(239, 68, 68, 0.1);
  color: #ef4444;
  font-size: 0.8rem;
  font-weight: 500;
}

.capture-pre--url {
  white-space: pre-wrap;
  word-break: break-all;
}

.capture-section__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 6px;
}

.capture-section__header h4 {
  margin: 0;
  font-size: 0.9rem;
}

.capture-copy-btn {
  border: 1px solid var(--mac-border);
  background: transparent;
  border-radius: 6px;
  padding: 2px 10px;
  font-size: 0.78rem;
  cursor: pointer;
  color: var(--mac-text-secondary);
}

.capture-copy-btn:hover {
  color: var(--mac-text);
}

.capture-pre {
  margin: 0;
  padding: 12px;
  border-radius: 8px;
  background: #f6f8fa;
  font-size: 0.78rem;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 320px;
  overflow-y: auto;
}

html.dark .capture-pre {
  background: #161b22;
}
</style>
