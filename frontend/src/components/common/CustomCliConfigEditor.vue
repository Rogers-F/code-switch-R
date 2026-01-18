<template>
  <div class="custom-cli-config-editor">
    <div class="config-header" @click="toggleExpanded">
      <div class="config-header-left">
        <svg
          class="expand-icon"
          :class="{ expanded }"
          viewBox="0 0 20 20"
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
        <span class="config-title">{{ t('components.cliConfig.title') }}</span>
        <span class="config-tool-badge">{{ toolName }}</span>
      </div>
      <div class="config-header-right" @click.stop>
        <button
          v-if="expanded && hasChanges"
          class="config-action-btn save-btn"
          type="button"
          :disabled="saving"
          :title="t('components.cliConfig.previewApply')"
          @click="handleSaveAll"
        >
          <span v-if="saving" class="btn-spinner"></span>
          <svg v-else viewBox="0 0 20 20" aria-hidden="true">
            <path
              d="M16.7 5.3a1 1 0 010 1.4l-8 8a1 1 0 01-1.4 0l-4-4a1 1 0 111.4-1.4L8 12.6l7.3-7.3a1 1 0 011.4 0z"
              fill="currentColor"
            />
          </svg>
        </button>
        <button
          v-if="expanded"
          class="config-action-btn"
          type="button"
          :title="t('components.cliConfig.previewReset')"
          @click="handleReloadAll"
        >
          <svg viewBox="0 0 20 20" aria-hidden="true">
            <path
              d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
              stroke-linejoin="round"
              fill="none"
            />
          </svg>
        </button>
      </div>
    </div>

    <div v-if="expanded" class="config-content">
      <div v-if="loading" class="config-loading">
        {{ t('components.cliConfig.loading') }}
      </div>

      <template v-else-if="configFiles.length > 0">
        <!-- 配置文件选项卡 -->
        <div v-if="configFiles.length > 1" class="config-tabs">
          <button
            v-for="file in configFiles"
            :key="file.id"
            class="config-tab"
            :class="{ active: activeFileId === file.id, primary: file.isPrimary }"
            @click="activeFileId = file.id"
          >
            <span class="tab-label">{{ file.label || file.path }}</span>
            <span class="tab-format">{{ file.format.toUpperCase() }}</span>
            <span v-if="file.isPrimary" class="tab-primary" aria-hidden="true">
              <svg viewBox="0 0 24 24">
                <path
                  d="M12 17.3l-5.3 2.8 1-5.9-4.3-4.2 6-.9L12 3.7l2.7 5.4 6 .9-4.3 4.2 1 5.9L12 17.3z"
                  fill="currentColor"
                />
              </svg>
            </span>
            <span v-if="fileChanges[file.id]" class="tab-changed">●</span>
          </button>
        </div>

        <!-- 当前配置文件编辑器 -->
        <div v-for="file in configFiles" :key="file.id" v-show="activeFileId === file.id" class="config-file-editor">
          <div class="file-meta">
            <span class="file-path">{{ file.path }}</span>
            <span class="file-format-badge">{{ file.format.toUpperCase() }}</span>
            <span v-if="fileErrors[file.id]" class="file-error">{{ fileErrors[file.id] }}</span>
          </div>

          <!-- 锁定字段提示 -->
          <div v-if="lockedFields.length > 0 && file.isPrimary" class="locked-fields-hint">
            <span class="lock-icon" aria-hidden="true">
              <svg viewBox="0 0 24 24">
                <path
                  d="M7 11V8a5 5 0 0110 0v3"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.8"
                  stroke-linecap="round"
                />
                <path
                  d="M7 11h10a2 2 0 012 2v6a2 2 0 01-2 2H7a2 2 0 01-2-2v-6a2 2 0 012-2z"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.8"
                  stroke-linejoin="round"
                />
              </svg>
            </span>
            <span>{{ t('components.cliConfig.lockedFields') }}: {{ lockedFields.join(', ') }}</span>
          </div>

          <!-- 配置内容编辑器 -->
          <textarea
            v-model="fileContents[file.id]"
            class="config-textarea"
            :class="{ 'has-error': fileErrors[file.id] }"
            rows="15"
            spellcheck="false"
            @input="markFileChanged(file.id)"
          />

          <!-- 单文件操作按钮 -->
          <div class="file-actions">
            <button
              type="button"
              class="file-action-btn primary"
              :disabled="saving || !fileChanges[file.id]"
              @click="handleSaveFile(file.id)"
            >
              {{ t('components.cliConfig.previewApply') }}
            </button>
            <button
              type="button"
              class="file-action-btn"
              :disabled="loading"
              @click="handleReloadFile(file.id)"
            >
              {{ t('components.cliConfig.previewReset') }}
            </button>
          </div>
        </div>
      </template>

      <div v-else class="config-empty">
        {{ t('components.main.customCli.noTools') }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getCustomCliConfigContent,
  saveCustomCliConfigContent,
  getCustomCliLockedFields,
  type ConfigFile,
} from '../../services/customCliService'
import { showToast } from '../../utils/toast'
import { extractErrorMessage } from '../../utils/error'

const props = defineProps<{
  toolId: string
  toolName: string
  configFiles: ConfigFile[]
}>()

const emit = defineEmits<{
  (e: 'saved'): void
}>()

const { t } = useI18n()

const expanded = ref(false)
const loading = ref(false)
const saving = ref(false)
const activeFileId = ref<string>('')
const fileContents = ref<Record<string, string>>({})
const originalContents = ref<Record<string, string>>({})
const fileChanges = ref<Record<string, boolean>>({})
const fileErrors = ref<Record<string, string>>({})
const lockedFields = ref<string[]>([])

// 是否有未保存的更改
const hasChanges = computed(() => {
  return Object.values(fileChanges.value).some(v => v)
})

const toggleExpanded = () => {
  expanded.value = !expanded.value
  if (expanded.value && Object.keys(fileContents.value).length === 0) {
    loadAllConfigs()
  }
}

const loadAllConfigs = async () => {
  if (!props.toolId || props.configFiles.length === 0) return

  loading.value = true
  try {
    // 加载锁定字段
    lockedFields.value = await getCustomCliLockedFields(props.toolId)

    // 加载所有配置文件内容
    for (const file of props.configFiles) {
      try {
        const content = await getCustomCliConfigContent(props.toolId, file.id)
        fileContents.value[file.id] = content
        originalContents.value[file.id] = content
        fileChanges.value[file.id] = false
        delete fileErrors.value[file.id]
      } catch (err) {
        console.error(`Failed to load config file ${file.id}:`, err)
        fileContents.value[file.id] = ''
        originalContents.value[file.id] = ''
        fileErrors.value[file.id] = t('components.cliConfig.loadError')
      }
    }

    // 默认选中第一个（主）配置文件
    const primary = props.configFiles.find(f => f.isPrimary)
    activeFileId.value = primary?.id || props.configFiles[0]?.id || ''
  } catch (err) {
    console.error('Failed to load configs:', err)
    showToast(t('components.cliConfig.loadError'), 'error')
  } finally {
    loading.value = false
  }
}

const markFileChanged = (fileId: string) => {
  fileChanges.value[fileId] = fileContents.value[fileId] !== originalContents.value[fileId]
  // 清除错误（用户正在编辑）
  delete fileErrors.value[fileId]
}

const validateContent = (content: string, format: string): boolean => {
  if (!content.trim()) return true // 空内容允许

  try {
    if (format === 'json') {
      JSON.parse(content)
    } else if (format === 'toml') {
      // 简单的 TOML 验证：检查基本语法
      const lines = content.split('\n')
      for (const line of lines) {
        const trimmed = line.trim()
        if (!trimmed || trimmed.startsWith('#') || trimmed.startsWith('[')) continue
        // 检查是否有 = 号
        if (trimmed.includes('=')) continue
        // 允许空行和注释
      }
    }
    // ENV 格式比较宽松，不做严格验证
    return true
  } catch {
    return false
  }
}

const handleSaveFile = async (fileId: string) => {
  const file = props.configFiles.find(f => f.id === fileId)
  if (!file) return

  const content = fileContents.value[fileId] || ''

  // 验证内容格式
  if (!validateContent(content, file.format)) {
    fileErrors.value[fileId] = t('components.cliConfig.previewParseError')
    showToast(t('components.cliConfig.previewParseError'), 'error')
    return
  }

  saving.value = true
  try {
    await saveCustomCliConfigContent(props.toolId, fileId, content)
    originalContents.value[fileId] = content
    fileChanges.value[fileId] = false
    delete fileErrors.value[fileId]
    showToast(t('components.cliConfig.previewApplySuccess'), 'success')
    emit('saved')
  } catch (err) {
    console.error(`Failed to save config file ${fileId}:`, err)
    fileErrors.value[fileId] = extractErrorMessage(err)
    showToast(t('components.cliConfig.loadError'), 'error')
  } finally {
    saving.value = false
  }
}

const handleReloadFile = async (fileId: string) => {
  loading.value = true
  try {
    const content = await getCustomCliConfigContent(props.toolId, fileId)
    fileContents.value[fileId] = content
    originalContents.value[fileId] = content
    fileChanges.value[fileId] = false
    delete fileErrors.value[fileId]
  } catch (err) {
    console.error(`Failed to reload config file ${fileId}:`, err)
    fileErrors.value[fileId] = t('components.cliConfig.loadError')
  } finally {
    loading.value = false
  }
}

const handleSaveAll = async () => {
  saving.value = true
  let successCount = 0
  let failCount = 0
  const filesToSave = props.configFiles.filter(f => fileChanges.value[f.id])

  for (const file of filesToSave) {
    const content = fileContents.value[file.id] || ''

    // 验证
    if (!validateContent(content, file.format)) {
      fileErrors.value[file.id] = t('components.cliConfig.previewParseError')
      failCount++
      continue
    }

    try {
      await saveCustomCliConfigContent(props.toolId, file.id, content)
      originalContents.value[file.id] = content
      fileChanges.value[file.id] = false
      delete fileErrors.value[file.id]
      successCount++
    } catch (err) {
      console.error(`Failed to save config file ${file.id}:`, err)
      fileErrors.value[file.id] = extractErrorMessage(err)
      failCount++
    }
  }

  saving.value = false

  // 根据结果显示不同的提示
  if (filesToSave.length === 0) {
    // 没有需要保存的文件
    return
  } else if (failCount === 0) {
    // 全部成功
    showToast(t('components.cliConfig.previewApplySuccess'), 'success')
    emit('saved')
  } else if (successCount === 0) {
    // 全部失败
    showToast(t('components.cliConfig.saveAllFailed'), 'error')
  } else {
    // 部分成功
    showToast(t('components.cliConfig.savePartialSuccess', { success: successCount, fail: failCount }), 'warning')
    emit('saved')
  }
}

const handleReloadAll = async () => {
  await loadAllConfigs()
}

// 监听 toolId 变化，重新加载
watch(() => props.toolId, () => {
  if (expanded.value) {
    loadAllConfigs()
  } else {
    // 重置状态
    fileContents.value = {}
    originalContents.value = {}
    fileChanges.value = {}
    fileErrors.value = {}
    lockedFields.value = []
  }
})

// 监听 configFiles 变化
watch(() => props.configFiles, () => {
  if (expanded.value && props.configFiles.length > 0) {
    loadAllConfigs()
  }
}, { deep: true })

onMounted(() => {
  // 设置默认选中的文件
  if (props.configFiles.length > 0) {
    const primary = props.configFiles.find(f => f.isPrimary)
    activeFileId.value = primary?.id || props.configFiles[0]?.id || ''
  }
})
</script>

<style scoped>
.custom-cli-config-editor {
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  overflow: hidden;
  margin-top: 16px;
}

.config-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  background: var(--mac-surface);
  cursor: pointer;
  user-select: none;
  transition: background 0.2s;
}

.config-header:hover {
  background: var(--mac-surface-strong);
}

.config-header-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.expand-icon {
  width: 16px;
  height: 16px;
  transition: transform 0.2s;
  opacity: 0.6;
}

.expand-icon.expanded {
  transform: rotate(180deg);
}

.config-title {
  font-size: 14px;
  font-weight: 500;
  color: var(--mac-text);
}

.config-tool-badge {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 4px;
  background: var(--mac-accent);
  color: white;
  font-weight: 500;
}

.config-header-right {
  display: flex;
  gap: 8px;
}

.config-action-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 6px;
  background: transparent;
  cursor: pointer;
  transition: background 0.2s;
}

.config-action-btn:hover:not(:disabled) {
  background: var(--mac-surface-strong);
}

.config-action-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.config-action-btn svg {
  width: 16px;
  height: 16px;
  color: var(--mac-text-secondary);
}

.config-action-btn.save-btn svg {
  color: var(--mac-accent);
}

.btn-spinner {
  width: 14px;
  height: 14px;
  border: 2px solid var(--mac-border);
  border-top-color: var(--mac-accent);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.config-content {
  padding: 16px;
  border-top: 1px solid var(--mac-border);
  background: var(--mac-surface);
}

.config-loading,
.config-empty {
  text-align: center;
  padding: 24px;
  color: var(--mac-text-secondary);
  font-size: 14px;
}

/* 配置文件选项卡 */
.config-tabs {
  display: flex;
  gap: 4px;
  margin-bottom: 16px;
  overflow-x: auto;
  padding-bottom: 4px;
}

.config-tab {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  border: 1px solid var(--mac-border);
  border-radius: 6px;
  background: var(--mac-bg);
  color: var(--mac-text-secondary);
  font-size: 13px;
  cursor: pointer;
  transition: all 0.2s;
  white-space: nowrap;
}

.config-tab:hover {
  background: var(--mac-surface-strong);
  color: var(--mac-text);
}

.config-tab.active {
  background: var(--mac-accent);
  color: white;
  border-color: var(--mac-accent);
}

.config-tab.primary {
  font-weight: 500;
}

.tab-label {
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
}

.tab-format {
  font-size: 10px;
  padding: 2px 4px;
  border-radius: 3px;
  background: rgba(0, 0, 0, 0.1);
}

.config-tab.active .tab-format {
  background: rgba(255, 255, 255, 0.2);
}

.tab-primary {
  display: inline-flex;
  width: 12px;
  height: 12px;
  align-items: center;
  justify-content: center;
  color: var(--primary);
}

.tab-primary svg {
  width: 12px;
  height: 12px;
  display: block;
}

.tab-changed {
  color: #ef4444;
  font-size: 16px;
  line-height: 1;
}

.config-tab.active .tab-changed {
  color: #fecaca;
}

/* 配置文件编辑器 */
.config-file-editor {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.file-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.file-path {
  font-size: 12px;
  color: var(--mac-text-secondary);
  font-family: monospace;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}

.file-format-badge {
  font-size: 10px;
  font-weight: 600;
  padding: 2px 6px;
  border-radius: 4px;
  background: var(--mac-accent);
  color: white;
  flex-shrink: 0;
}

.file-error {
  font-size: 12px;
  color: var(--mac-error, #ef4444);
  flex-basis: 100%;
  margin-top: 4px;
}

.locked-fields-hint {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  background: rgba(245, 158, 11, 0.1);
  border-left: 3px solid #f59e0b;
  border-radius: 4px;
  font-size: 12px;
  color: var(--mac-text-secondary);
}

.lock-icon {
  display: inline-flex;
  width: 14px;
  height: 14px;
  align-items: center;
  justify-content: center;
  color: #f59e0b;
}

.lock-icon svg {
  width: 14px;
  height: 14px;
  display: block;
}

.config-textarea {
  width: 100%;
  min-height: 300px;
  padding: 12px;
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  font-size: 12px;
  line-height: 1.6;
  font-family: 'SF Mono', 'Monaco', 'Inconsolata', 'Fira Code', monospace;
  background: var(--mac-bg);
  color: var(--mac-text);
  resize: vertical;
  transition: border-color 0.2s;
}

.config-textarea:focus {
  outline: none;
  border-color: var(--mac-accent);
}

.config-textarea.has-error {
  border-color: var(--mac-error, #ef4444);
}

.file-actions {
  display: flex;
  gap: 8px;
}

.file-action-btn {
  padding: 8px 16px;
  border: 1px solid var(--mac-border);
  border-radius: 6px;
  background: var(--mac-surface);
  color: var(--mac-text);
  font-size: 13px;
  cursor: pointer;
  transition: all 0.2s;
}

.file-action-btn:hover:not(:disabled) {
  background: var(--mac-surface-strong);
}

.file-action-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.file-action-btn.primary {
  background: var(--mac-accent);
  border-color: var(--mac-accent);
  color: white;
}

.file-action-btn.primary:hover:not(:disabled) {
  filter: brightness(1.1);
}

/* 深色模式适配 */
:global(.dark) .config-textarea {
  background: var(--mac-surface-strong);
}

:global(.dark) .tab-format {
  background: rgba(255, 255, 255, 0.1);
}

:global(.dark) .locked-fields-hint {
  background: rgba(245, 158, 11, 0.15);
}
</style>
