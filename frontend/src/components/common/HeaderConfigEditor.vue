<template>
  <div class="header-config-editor">
    <!-- 折叠面板标题 -->
    <button
      type="button"
      class="collapse-header"
      @click="expanded = !expanded"
    >
      <svg
        class="chevron"
        :class="{ expanded }"
        viewBox="0 0 16 16"
        width="14"
        height="14"
        aria-hidden="true"
      >
        <path
          d="M6 4l4 4-4 4"
          fill="none"
          stroke="currentColor"
          stroke-width="1.5"
          stroke-linecap="round"
          stroke-linejoin="round"
        />
      </svg>
      <span class="collapse-title">{{ $t('components.provider.headerConfig.title') }}</span>
      <span v-if="hasConfig" class="config-badge">{{ configCount }}</span>
    </button>

    <div v-if="expanded" class="collapse-content">
      <!-- Extra Headers (不存在才添加) -->
      <div class="header-section">
        <div class="section-header">
          <label class="section-label">
            <span>{{ $t('components.provider.headerConfig.extraHeaders.label') }}</span>
            <button
              type="button"
              class="help-icon"
              :data-tooltip="$t('components.provider.headerConfig.extraHeaders.tooltip')"
            >
              <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
                <path
                  d="M8 1a7 7 0 100 14A7 7 0 008 1zm0 13A6 6 0 118 2a6 6 0 010 12zm0-9.5a.75.75 0 01.75.75v4a.75.75 0 01-1.5 0v-4A.75.75 0 018 4.5zm0 7.5a1 1 0 100-2 1 1 0 000 2z"
                  fill="currentColor"
                />
              </svg>
            </button>
          </label>
        </div>
        
        <div v-if="extraHeadersList.length > 0" class="header-list">
          <div
            v-for="(header, index) in extraHeadersList"
            :key="`extra-${index}`"
            class="header-row"
          >
            <code class="header-key">{{ header.key }}</code>
            <span class="header-separator">:</span>
            <code class="header-value">{{ header.value }}</code>
            <button
              type="button"
              class="header-remove"
              :aria-label="$t('components.provider.headerConfig.remove')"
              @click="removeExtraHeader(index)"
            >
              <svg viewBox="0 0 12 12" width="10" height="10" aria-hidden="true">
                <path d="M3 3l6 6M9 3l-6 6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
              </svg>
            </button>
          </div>
        </div>
        
        <div class="header-input-row">
          <BaseInput
            v-model="newExtraKey"
            type="text"
            :placeholder="$t('components.provider.headerConfig.keyPlaceholder')"
            @keydown.enter.prevent="focusExtraValue"
          />
          <span class="input-separator">:</span>
          <BaseInput
            ref="extraValueRef"
            v-model="newExtraValue"
            type="text"
            :placeholder="$t('components.provider.headerConfig.valuePlaceholder')"
            @keydown.enter.prevent="addExtraHeader"
          />
          <BaseButton type="button" variant="outline" @click="addExtraHeader">
            {{ $t('components.provider.headerConfig.add') }}
          </BaseButton>
        </div>
      </div>

      <!-- Override Headers (强制覆盖) -->
      <div class="header-section">
        <div class="section-header">
          <label class="section-label">
            <span>{{ $t('components.provider.headerConfig.overrideHeaders.label') }}</span>
            <button
              type="button"
              class="help-icon"
              :data-tooltip="$t('components.provider.headerConfig.overrideHeaders.tooltip')"
            >
              <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
                <path
                  d="M8 1a7 7 0 100 14A7 7 0 008 1zm0 13A6 6 0 118 2a6 6 0 010 12zm0-9.5a.75.75 0 01.75.75v4a.75.75 0 01-1.5 0v-4A.75.75 0 018 4.5zm0 7.5a1 1 0 100-2 1 1 0 000 2z"
                  fill="currentColor"
                />
              </svg>
            </button>
          </label>
        </div>
        
        <div v-if="overrideHeadersList.length > 0" class="header-list">
          <div
            v-for="(header, index) in overrideHeadersList"
            :key="`override-${index}`"
            class="header-row"
          >
            <code class="header-key">{{ header.key }}</code>
            <span class="header-separator">:</span>
            <code class="header-value">{{ header.value }}</code>
            <button
              type="button"
              class="header-remove"
              :aria-label="$t('components.provider.headerConfig.remove')"
              @click="removeOverrideHeader(index)"
            >
              <svg viewBox="0 0 12 12" width="10" height="10" aria-hidden="true">
                <path d="M3 3l6 6M9 3l-6 6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
              </svg>
            </button>
          </div>
        </div>
        
        <div class="header-input-row">
          <BaseInput
            v-model="newOverrideKey"
            type="text"
            :placeholder="$t('components.provider.headerConfig.keyPlaceholder')"
            @keydown.enter.prevent="focusOverrideValue"
          />
          <span class="input-separator">:</span>
          <BaseInput
            ref="overrideValueRef"
            v-model="newOverrideValue"
            type="text"
            :placeholder="$t('components.provider.headerConfig.valuePlaceholder')"
            @keydown.enter.prevent="addOverrideHeader"
          />
          <BaseButton type="button" variant="outline" @click="addOverrideHeader">
            {{ $t('components.provider.headerConfig.add') }}
          </BaseButton>
        </div>
        
        <!-- 认证头警告 -->
        <p class="auth-warning">
          ⚠️ {{ $t('components.provider.headerConfig.overrideHeaders.authWarning') }}
        </p>
      </div>

      <!-- Strip Headers (需要移除) -->
      <div class="header-section">
        <div class="section-header">
          <label class="section-label">
            <span>{{ $t('components.provider.headerConfig.stripHeaders.label') }}</span>
            <button
              type="button"
              class="help-icon"
              :data-tooltip="$t('components.provider.headerConfig.stripHeaders.tooltip')"
            >
              <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
                <path
                  d="M8 1a7 7 0 100 14A7 7 0 008 1zm0 13A6 6 0 118 2a6 6 0 010 12zm0-9.5a.75.75 0 01.75.75v4a.75.75 0 01-1.5 0v-4A.75.75 0 018 4.5zm0 7.5a1 1 0 100-2 1 1 0 000 2z"
                  fill="currentColor"
                />
              </svg>
            </button>
          </label>
        </div>
        
        <div v-if="stripHeaders.length > 0" class="header-list strip-list">
          <div
            v-for="(header, index) in stripHeaders"
            :key="`strip-${index}`"
            class="header-row strip-row"
          >
            <code class="header-key">{{ header }}</code>
            <button
              type="button"
              class="header-remove"
              :aria-label="$t('components.provider.headerConfig.remove')"
              @click="removeStripHeader(index)"
            >
              <svg viewBox="0 0 12 12" width="10" height="10" aria-hidden="true">
                <path d="M3 3l6 6M9 3l-6 6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
              </svg>
            </button>
          </div>
        </div>
        
        <div class="header-input-row">
          <BaseInput
            v-model="newStripHeader"
            type="text"
            :placeholder="$t('components.provider.headerConfig.stripHeaders.placeholder')"
            @keydown.enter.prevent="addStripHeader"
          />
          <BaseButton type="button" variant="outline" @click="addStripHeader">
            {{ $t('components.provider.headerConfig.add') }}
          </BaseButton>
        </div>
      </div>

      <!-- 帮助说明 -->
      <div class="help-text">
        <p class="help-example">
          <strong>{{ $t('components.provider.headerConfig.help.title') }}</strong>
        </p>
        <ul class="help-list">
          <li>
            <strong>{{ $t('components.provider.headerConfig.extraHeaders.label') }}:</strong>
            {{ $t('components.provider.headerConfig.help.extra') }}
          </li>
          <li>
            <strong>{{ $t('components.provider.headerConfig.overrideHeaders.label') }}:</strong>
            {{ $t('components.provider.headerConfig.help.override') }}
          </li>
          <li>
            <strong>{{ $t('components.provider.headerConfig.stripHeaders.label') }}:</strong>
            {{ $t('components.provider.headerConfig.help.strip') }}
          </li>
        </ul>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import BaseInput from './BaseInput.vue'
import BaseButton from './BaseButton.vue'

interface Props {
  extraHeaders?: Record<string, string>
  overrideHeaders?: Record<string, string>
  stripHeaders?: string[]
}

interface Emits {
  (e: 'update:extraHeaders', value: Record<string, string>): void
  (e: 'update:overrideHeaders', value: Record<string, string>): void
  (e: 'update:stripHeaders', value: string[]): void
}

const props = withDefaults(defineProps<Props>(), {
  extraHeaders: () => ({}),
  overrideHeaders: () => ({}),
  stripHeaders: () => [],
})

const emit = defineEmits<Emits>()

// 展开/折叠状态
const expanded = ref(false)

// 禁止配置的认证头
const forbiddenAuthHeaders = ['authorization', 'x-api-key', 'x-goog-api-key']

// 计算列表
const extraHeadersList = computed(() => {
  if (!props.extraHeaders) return []
  return Object.entries(props.extraHeaders).map(([key, value]) => ({ key, value }))
})

const overrideHeadersList = computed(() => {
  if (!props.overrideHeaders) return []
  return Object.entries(props.overrideHeaders).map(([key, value]) => ({ key, value }))
})

// 是否有配置
const hasConfig = computed(() => {
  return extraHeadersList.value.length > 0 || 
         overrideHeadersList.value.length > 0 || 
         (props.stripHeaders?.length ?? 0) > 0
})

const configCount = computed(() => {
  return extraHeadersList.value.length + 
         overrideHeadersList.value.length + 
         (props.stripHeaders?.length ?? 0)
})

// 输入状态
const newExtraKey = ref('')
const newExtraValue = ref('')
const newOverrideKey = ref('')
const newOverrideValue = ref('')
const newStripHeader = ref('')

// 输入框引用
const extraValueRef = ref<InstanceType<typeof BaseInput> | null>(null)
const overrideValueRef = ref<InstanceType<typeof BaseInput> | null>(null)

// 聚焦到 value 输入框
const focusExtraValue = () => {
  if (extraValueRef.value) {
    const inputElement = (extraValueRef.value as any).$el?.querySelector('input')
    if (inputElement) inputElement.focus()
  }
}

const focusOverrideValue = () => {
  if (overrideValueRef.value) {
    const inputElement = (overrideValueRef.value as any).$el?.querySelector('input')
    if (inputElement) inputElement.focus()
  }
}

// Extra Headers 操作
const addExtraHeader = () => {
  const key = newExtraKey.value.trim()
  const value = newExtraValue.value.trim()
  if (!key || !value) return
  
  const updated = { ...props.extraHeaders }
  updated[key] = value
  emit('update:extraHeaders', updated)
  
  newExtraKey.value = ''
  newExtraValue.value = ''
}

const removeExtraHeader = (index: number) => {
  const header = extraHeadersList.value[index]
  if (!header) return
  
  const updated = { ...props.extraHeaders }
  delete updated[header.key]
  emit('update:extraHeaders', updated)
}

// Override Headers 操作
const addOverrideHeader = () => {
  const key = newOverrideKey.value.trim()
  const value = newOverrideValue.value.trim()
  if (!key || !value) return
  
  // 检查是否为禁止的认证头
  if (forbiddenAuthHeaders.includes(key.toLowerCase())) {
    // 可以在这里添加提示，但不阻止添加（后端会覆盖）
    console.warn(`Warning: ${key} will be overridden by the authentication system`)
  }
  
  const updated = { ...props.overrideHeaders }
  updated[key] = value
  emit('update:overrideHeaders', updated)
  
  newOverrideKey.value = ''
  newOverrideValue.value = ''
}

const removeOverrideHeader = (index: number) => {
  const header = overrideHeadersList.value[index]
  if (!header) return
  
  const updated = { ...props.overrideHeaders }
  delete updated[header.key]
  emit('update:overrideHeaders', updated)
}

// Strip Headers 操作
const addStripHeader = () => {
  const header = newStripHeader.value.trim()
  if (!header) return
  
  // 避免重复
  if (props.stripHeaders?.includes(header)) return
  
  emit('update:stripHeaders', [...(props.stripHeaders ?? []), header])
  newStripHeader.value = ''
}

const removeStripHeader = (index: number) => {
  const updated = [...(props.stripHeaders ?? [])]
  updated.splice(index, 1)
  emit('update:stripHeaders', updated)
}
</script>

<style scoped>
.header-config-editor {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--border);
  border-radius: 8px;
  overflow: hidden;
}

.collapse-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 14px;
  background: var(--background-secondary);
  border: none;
  cursor: pointer;
  font-size: 0.875rem;
  font-weight: 500;
  color: var(--foreground);
  text-align: left;
  transition: background-color 0.2s;
}

.collapse-header:hover {
  background: var(--background-hover);
}

.chevron {
  flex-shrink: 0;
  color: var(--foreground-muted);
  transition: transform 0.2s;
}

.chevron.expanded {
  transform: rotate(90deg);
}

.collapse-title {
  flex: 1;
}

.config-badge {
  padding: 2px 8px;
  background: var(--accent-primary);
  color: white;
  border-radius: 10px;
  font-size: 0.75rem;
  font-weight: 600;
}

.collapse-content {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 14px;
  background: var(--background);
  border-top: 1px solid var(--border);
}

.header-section {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.section-label {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 500;
  font-size: 0.8125rem;
  color: var(--foreground);
}

.help-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 2px;
  border: none;
  background: none;
  color: var(--foreground-muted);
  cursor: help;
  border-radius: 4px;
  transition: all 0.2s;
}

.help-icon:hover {
  color: var(--foreground);
  background-color: var(--background-hover);
}

.header-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 10px;
  background-color: var(--background-secondary);
  border-radius: 6px;
}

.header-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  background-color: var(--background);
  border: 1px solid var(--border);
  border-radius: 4px;
  transition: all 0.2s;
}

.header-row:hover {
  background-color: var(--background-hover);
}

.header-key {
  padding: 2px 6px;
  background-color: var(--background-secondary);
  border: 1px solid var(--border);
  border-radius: 3px;
  font-family: 'SF Mono', 'Menlo', 'Monaco', 'Courier New', monospace;
  font-size: 0.75rem;
  color: var(--accent-primary);
  word-break: break-all;
}

.header-separator {
  color: var(--foreground-muted);
  font-weight: 500;
}

.header-value {
  flex: 1;
  padding: 2px 6px;
  background-color: var(--background-secondary);
  border: 1px solid var(--border);
  border-radius: 3px;
  font-family: 'SF Mono', 'Menlo', 'Monaco', 'Courier New', monospace;
  font-size: 0.75rem;
  color: var(--foreground);
  word-break: break-all;
}

.strip-row .header-key {
  flex: 1;
}

.header-remove {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 4px;
  border: none;
  background: none;
  color: var(--foreground-muted);
  cursor: pointer;
  border-radius: 3px;
  flex-shrink: 0;
  transition: all 0.2s;
}

.header-remove:hover {
  color: var(--error);
  background-color: var(--error-bg);
}

.header-input-row {
  display: flex;
  gap: 8px;
  align-items: center;
}

.header-input-row :deep(input) {
  flex: 1;
  font-family: 'SF Mono', 'Menlo', 'Monaco', 'Courier New', monospace;
  font-size: 0.8125rem;
}

.input-separator {
  color: var(--foreground-muted);
  font-weight: 500;
  flex-shrink: 0;
}

.auth-warning {
  margin: 4px 0 0 0;
  padding: 8px 10px;
  background-color: rgba(251, 191, 36, 0.1);
  border: 1px solid rgba(251, 191, 36, 0.3);
  border-radius: 4px;
  font-size: 0.75rem;
  color: #d97706;
}

.help-text {
  padding: 12px;
  background-color: var(--background-secondary);
  border-radius: 6px;
  font-size: 0.8125rem;
  color: var(--foreground-muted);
}

.help-example {
  margin-bottom: 8px;
  color: var(--foreground);
}

.help-list {
  margin: 0;
  padding-left: 20px;
  list-style: disc;
}

.help-list li {
  margin-bottom: 6px;
  line-height: 1.5;
}

.help-list strong {
  color: var(--foreground);
}
</style>
