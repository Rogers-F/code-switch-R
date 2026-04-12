<template>
  <div class="sanitize-config-editor">
    <div class="editor-header" @click="expanded = !expanded">
      <span class="editor-label">{{ $t('components.provider.sanitizeConfig.label') }}</span>
      <svg
        class="chevron"
        :class="{ open: expanded }"
        viewBox="0 0 20 20"
        width="16"
        height="16"
        aria-hidden="true"
      >
        <path
          d="M6 8l4 4 4-4"
          stroke="currentColor"
          stroke-width="1.5"
          stroke-linecap="round"
          stroke-linejoin="round"
          fill="none"
        />
      </svg>
    </div>

    <div v-if="expanded" class="config-sections">
      <!-- 要移除的请求体字段 -->
      <SanitizeFieldList
        :label="$t('components.provider.sanitizeConfig.blockedBodyFields')"
        :items="config.blockedBodyFields || []"
        :placeholder="$t('components.provider.sanitizeConfig.placeholder')"
        :default-hint="$t('components.provider.sanitizeConfig.defaultHint')"
        @update="updateField('blockedBodyFields', $event)"
      />

      <!-- 要移除的请求头 -->
      <SanitizeFieldList
        :label="$t('components.provider.sanitizeConfig.blockedHeaders')"
        :items="config.blockedHeaders || []"
        :placeholder="$t('components.provider.sanitizeConfig.placeholderHeader')"
        :default-hint="$t('components.provider.sanitizeConfig.defaultHint')"
        @update="updateField('blockedHeaders', $event)"
      />

      <!-- 要移除的 anthropic-beta 值 -->
      <SanitizeFieldList
        :label="$t('components.provider.sanitizeConfig.blockedBetaValues')"
        :items="config.blockedBetaValues || []"
        :placeholder="$t('components.provider.sanitizeConfig.placeholderBeta')"
        :default-hint="$t('components.provider.sanitizeConfig.defaultHint')"
        @update="updateField('blockedBetaValues', $event)"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import SanitizeFieldList from './SanitizeFieldList.vue'

interface SanitizeConfig {
  blockedBodyFields?: string[]
  blockedHeaders?: string[]
  blockedBetaValues?: string[]
}

interface Props {
  modelValue?: SanitizeConfig
}

interface Emits {
  (e: 'update:modelValue', value: SanitizeConfig): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const expanded = ref(false)

const config = computed(() => props.modelValue || {})

const updateField = (field: keyof SanitizeConfig, value: string[]) => {
  const updated = { ...config.value, [field]: value.length > 0 ? value : undefined }
  const hasContent = Object.values(updated).some(v => v && (v as string[]).length > 0)
  emit('update:modelValue', hasContent ? updated : {})
}
</script>

<style scoped>
.sanitize-config-editor {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.editor-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  cursor: pointer;
  padding: 8px 0;
  user-select: none;
}

.editor-label {
  font-weight: 500;
  font-size: 0.875rem;
  color: var(--foreground);
}

.chevron {
  color: var(--foreground-muted);
  transition: transform 0.2s ease;
}

.chevron.open {
  transform: rotate(180deg);
}

.config-sections {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 12px;
  background-color: var(--background-secondary);
  border-radius: 8px;
}
</style>
