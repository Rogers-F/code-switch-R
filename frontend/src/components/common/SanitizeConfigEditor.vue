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
      <!-- Anthropic 请求体字段白名单 -->
      <SanitizeFieldList
        :label="$t('components.provider.sanitizeConfig.allowedBodyFields')"
        :items="config.allowedBodyFields || []"
        :placeholder="$t('components.provider.sanitizeConfig.placeholder')"
        :default-hint="$t('components.provider.sanitizeConfig.defaultHint')"
        @update="updateField('allowedBodyFields', $event)"
      />

      <!-- OpenAI Chat 请求体字段白名单 -->
      <SanitizeFieldList
        :label="$t('components.provider.sanitizeConfig.allowedBodyFieldsChat')"
        :items="config.allowedBodyFieldsChat || []"
        :placeholder="$t('components.provider.sanitizeConfig.placeholder')"
        :default-hint="$t('components.provider.sanitizeConfig.defaultHint')"
        @update="updateField('allowedBodyFieldsChat', $event)"
      />

      <!-- 请求头白名单 -->
      <SanitizeFieldList
        :label="$t('components.provider.sanitizeConfig.allowedHeaders')"
        :items="config.allowedHeaders || []"
        :placeholder="$t('components.provider.sanitizeConfig.placeholderHeader')"
        :default-hint="$t('components.provider.sanitizeConfig.defaultHint')"
        @update="updateField('allowedHeaders', $event)"
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
  allowedBodyFields?: string[]
  allowedBodyFieldsChat?: string[]
  allowedHeaders?: string[]
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
  // 清理空配置：如果所有字段都为空，返回空对象
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
