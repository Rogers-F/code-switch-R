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
        <div v-if="!data.request_headers && !data.request_body" class="capture-detail-hint">
          {{ t('components.logs.captureDetail.empty') }}
        </div>
        <template v-else>
          <section v-if="data.request_headers" class="capture-section">
            <div class="capture-section__header">
              <h4>{{ t('components.logs.captureDetail.headers') }}</h4>
              <button
                class="capture-copy-btn"
                @click="copyText(prettyJSON(data.request_headers))"
              >{{ t('components.logs.captureDetail.copy') }}</button>
            </div>
            <pre class="capture-pre">{{ prettyJSON(data.request_headers) }}</pre>
          </section>
          <section v-if="data.request_body" class="capture-section">
            <div class="capture-section__header">
              <h4>{{ t('components.logs.captureDetail.body') }}</h4>
              <button
                class="capture-copy-btn"
                @click="copyText(data.request_body)"
              >{{ t('components.logs.captureDetail.copy') }}</button>
            </div>
            <p v-if="data.body_truncated" class="capture-detail-hint">
              {{ t('components.logs.captureDetail.truncated', { bytes: data.body_bytes }) }}
            </p>
            <pre class="capture-pre">{{ data.request_body }}</pre>
          </section>
        </template>
      </template>
    </div>
  </BaseModal>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import BaseModal from './BaseModal.vue'
import type { RequestLogDetail } from '../../services/logs'

defineProps<{
  open: boolean
  loading: boolean
  error: string
  data: RequestLogDetail | null
}>()
defineEmits<{ close: [] }>()

const { t } = useI18n()

const prettyJSON = (text: string) => {
  try {
    return JSON.stringify(JSON.parse(text), null, 2)
  } catch {
    return text
  }
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
