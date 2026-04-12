<template>
  <div class="field-list">
    <div class="field-list-label">{{ label }}</div>

    <!-- 已添加的标签 -->
    <div v-if="items.length > 0" class="field-tags">
      <div v-for="(item, index) in items" :key="index" class="field-tag">
        <span class="field-name">{{ item }}</span>
        <button
          type="button"
          class="tag-remove"
          @click="removeItem(index)"
        >
          <svg viewBox="0 0 12 12" width="10" height="10" aria-hidden="true">
            <path
              d="M3 3l6 6M9 3l-6 6"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
            />
          </svg>
        </button>
      </div>
    </div>
    <div v-else class="default-hint">{{ defaultHint }}</div>

    <!-- 添加输入框 -->
    <div class="field-input-row">
      <BaseInput
        v-model="newItem"
        type="text"
        :placeholder="placeholder"
        @keydown.enter.prevent="addItem"
      />
      <BaseButton
        type="button"
        variant="outline"
        @click="addItem"
      >
        {{ $t('components.provider.sanitizeConfig.add') }}
      </BaseButton>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import BaseInput from './BaseInput.vue'
import BaseButton from './BaseButton.vue'

interface Props {
  label: string
  items: string[]
  placeholder: string
  defaultHint: string
}

interface Emits {
  (e: 'update', value: string[]): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const newItem = ref('')

const addItem = () => {
  const trimmed = newItem.value.trim()
  if (!trimmed) return
  if (props.items.includes(trimmed)) {
    newItem.value = ''
    return
  }
  emit('update', [...props.items, trimmed])
  newItem.value = ''
}

const removeItem = (index: number) => {
  const updated = [...props.items]
  updated.splice(index, 1)
  emit('update', updated)
}
</script>

<style scoped>
.field-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.field-list-label {
  font-size: 0.8125rem;
  font-weight: 500;
  color: var(--foreground);
}

.field-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding: 8px;
  background-color: var(--background);
  border-radius: 6px;
  min-height: 36px;
}

.field-tag {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 3px 6px 3px 8px;
  background-color: var(--background-secondary);
  border: 1px solid var(--border);
  border-radius: 4px;
  font-size: 0.75rem;
  line-height: 1.4;
}

.field-tag:hover {
  background-color: var(--background-hover);
}

.field-name {
  color: var(--foreground);
  font-family: 'SF Mono', 'Menlo', 'Monaco', 'Courier New', monospace;
}

.tag-remove {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 1px;
  border: none;
  background: none;
  color: var(--foreground-muted);
  cursor: pointer;
  border-radius: 2px;
  transition: all 0.2s;
}

.tag-remove:hover {
  color: var(--error);
  background-color: var(--error-bg);
}

.default-hint {
  font-size: 0.75rem;
  color: var(--foreground-muted);
  font-style: italic;
  padding: 6px 8px;
  background-color: var(--background);
  border-radius: 6px;
}

.field-input-row {
  display: flex;
  gap: 6px;
  align-items: center;
}

.field-input-row :deep(input) {
  flex: 1;
  font-family: 'SF Mono', 'Menlo', 'Monaco', 'Courier New', monospace;
  font-size: 0.8125rem;
}
</style>
