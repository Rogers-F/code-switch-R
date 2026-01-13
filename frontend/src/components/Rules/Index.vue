<template>
  <PageLayout :title="t('sidebar.rules')" :sticky="true">
    <div class="rules-page">
      <!-- Header -->
      <div class="page-header">
        <div>
          <h2 class="page-title">{{ t('sidebar.rules') }}</h2>
          <p class="page-subtitle">{{ t('rules.subtitle') }}</p>
        </div>
        <Button @click="showCreateDialog = true" class="create-button">
          <svg class="button-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="12" y1="5" x2="12" y2="19"></line>
            <line x1="5" y1="12" x2="19" y2="12"></line>
          </svg>
          {{ t('rules.actions.create') }}
        </Button>
      </div>

      <!-- Rules List -->
      <div v-if="rules.length > 0" class="rules-list">
        <Card v-for="rule in rules" :key="rule.id" class="rule-card" variant="outline">
          <div class="rule-header">
            <div class="rule-info">
              <div class="rule-title-row">
                <h3 class="rule-name">{{ rule.name }}</h3>
                <Badge :variant="rule.enabled ? 'success' : 'default'">
                  {{ rule.enabled ? t('rules.status.enabled') : t('rules.status.disabled') }}
                </Badge>
              </div>
              <p class="rule-route">{{ rule.sourceHost }} → {{ rule.targetProvider }}</p>
            </div>
            <div class="rule-actions">
              <Button variant="ghost" size="sm" @click="toggleRule(rule)">
                {{ rule.enabled ? t('rules.actions.disable') : t('rules.actions.enable') }}
              </Button>
              <Button variant="ghost" size="sm" @click="editRule(rule)">
                {{ t('rules.actions.edit') }}
              </Button>
              <Button variant="ghost" size="sm" @click="deleteRule(rule)">
                {{ t('rules.actions.delete') }}
              </Button>
            </div>
          </div>

          <Separator class="my-3" />

          <div class="rule-details">
            <div class="detail-row">
              <span class="detail-label">{{ t('rules.fields.priority') }}</span>
              <span class="detail-value">{{ rule.priority }}</span>
            </div>
            <div v-if="rule.pathRewrite" class="detail-row">
              <span class="detail-label">{{ t('rules.fields.pathRewrite') }}</span>
              <span class="detail-value">{{ rule.pathRewrite }}</span>
            </div>
            <div v-if="rule.modelMappings && rule.modelMappings.length > 0" class="detail-row">
              <span class="detail-label">{{ t('rules.fields.modelMappings') }}</span>
              <span class="detail-value">{{ rule.modelMappings.length }} {{ t('rules.fields.mappings') }}</span>
            </div>
          </div>
        </Card>
      </div>

      <!-- Empty State -->
      <div v-else class="empty-state">
        <svg class="empty-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"></polygon>
        </svg>
        <h3>{{ t('rules.empty.title') }}</h3>
        <p>{{ t('rules.empty.desc') }}</p>
        <Button @click="showCreateDialog = true" class="empty-action">
          {{ t('rules.actions.create') }}
        </Button>
      </div>

      <!-- Create/Edit Dialog -->
      <div v-if="showCreateDialog || editingRule" class="dialog-overlay" @click.self="closeDialog">
        <div class="dialog">
          <div class="dialog-header">
            <h3 class="dialog-title">
              {{ editingRule ? t('rules.dialog.edit') : t('rules.dialog.create') }}
            </h3>
            <button class="dialog-close" @click="closeDialog">×</button>
          </div>

          <div class="dialog-body">
            <div class="form-group">
              <label class="form-label">{{ t('rules.fields.name') }}</label>
              <input v-model="formData.name" type="text" class="form-input" :placeholder="t('rules.placeholders.name')" />
            </div>

            <div class="form-group">
              <label class="form-label">{{ t('rules.fields.sourceHost') }}</label>
              <input v-model="formData.sourceHost" type="text" class="form-input" :placeholder="t('rules.placeholders.sourceHost')" />
            </div>

            <div class="form-group">
              <label class="form-label">{{ t('rules.fields.targetProvider') }}</label>
              <input v-model="formData.targetProvider" type="text" class="form-input" :placeholder="t('rules.placeholders.targetProvider')" />
            </div>

            <div class="form-group">
              <label class="form-label">{{ t('rules.fields.priority') }}</label>
              <input v-model.number="formData.priority" type="number" class="form-input" min="0" max="100" />
            </div>

            <div class="form-group">
              <label class="form-label">{{ t('rules.fields.pathRewrite') }} ({{ t('rules.optional') }})</label>
              <input v-model="formData.pathRewrite" type="text" class="form-input" :placeholder="t('rules.placeholders.pathRewrite')" />
            </div>

            <div class="form-group">
              <label class="form-label-row">
                <input v-model="formData.enabled" type="checkbox" class="form-checkbox" />
                <span>{{ t('rules.fields.enabled') }}</span>
              </label>
            </div>
          </div>

          <div class="dialog-footer">
            <Button variant="ghost" @click="closeDialog">{{ t('rules.actions.cancel') }}</Button>
            <Button @click="saveRule">{{ t('rules.actions.save') }}</Button>
          </div>
        </div>
      </div>
    </div>
  </PageLayout>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import PageLayout from '../common/PageLayout.vue'
import Card from '../ui/Card.vue'
import Badge from '../ui/Badge.vue'
import Button from '../ui/Button.vue'
import Separator from '../ui/Separator.vue'
// @ts-ignore
import { ListRules, CreateRule, UpdateRule, DeleteRule, ToggleRuleEnabled } from '../../../bindings/codeswitch/services/ruleservice'

const { t } = useI18n()

interface ModelMapping {
  sourceModel: string
  targetModel: string
}

interface Rule {
  id: string
  name: string
  enabled: boolean
  sourceHost: string
  targetProvider: string
  modelMappings: ModelMapping[]
  pathRewrite: string
  priority: number
  createdAt: string
  updatedAt: string
}

const rules = ref<Rule[]>([])
const showCreateDialog = ref(false)
const editingRule = ref<Rule | null>(null)
const formData = ref({
  name: '',
  sourceHost: '',
  targetProvider: '',
  priority: 0,
  pathRewrite: '',
  enabled: true,
  modelMappings: [] as ModelMapping[]
})

// Load rules
const loadRules = async () => {
  try {
    const result = await ListRules()
    rules.value = (result || []).filter((r: any) => r !== null) as Rule[]
  } catch (error) {
    console.error('Failed to load rules:', error)
  }
}

// Toggle rule
const toggleRule = async (rule: Rule) => {
  try {
    await ToggleRuleEnabled(rule.id)
    await loadRules()
  } catch (error) {
    console.error('Failed to toggle rule:', error)
  }
}

// Edit rule
const editRule = (rule: Rule) => {
  editingRule.value = rule
  formData.value = {
    name: rule.name,
    sourceHost: rule.sourceHost,
    targetProvider: rule.targetProvider,
    priority: rule.priority,
    pathRewrite: rule.pathRewrite || '',
    enabled: rule.enabled,
    modelMappings: rule.modelMappings || []
  }
}

// Delete rule
const deleteRule = async (rule: Rule) => {
  if (!confirm(t('rules.confirm.delete', { name: rule.name }))) {
    return
  }

  try {
    await DeleteRule(rule.id)
    await loadRules()
  } catch (error) {
    console.error('Failed to delete rule:', error)
  }
}

// Save rule
const saveRule = async () => {
  try {
    const ruleData = {
      ...formData.value,
      id: editingRule.value?.id || '',
      createdAt: editingRule.value?.createdAt || new Date().toISOString(),
      updatedAt: new Date().toISOString()
    }

    if (editingRule.value) {
      await UpdateRule(ruleData)
    } else {
      await CreateRule(ruleData)
    }

    await loadRules()
    closeDialog()
  } catch (error) {
    console.error('Failed to save rule:', error)
  }
}

// Close dialog
const closeDialog = () => {
  showCreateDialog.value = false
  editingRule.value = null
  formData.value = {
    name: '',
    sourceHost: '',
    targetProvider: '',
    priority: 0,
    pathRewrite: '',
    enabled: true,
    modelMappings: []
  }
}

onMounted(() => {
  loadRules()
})
</script>

<style scoped>
.rules-page {
  padding: 1.5rem;
  max-width: 1400px;
  margin: 0 auto;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 2rem;
}

.page-title {
  font-size: 1.5rem;
  font-weight: 700;
  color: var(--color-text);
  margin-bottom: 0.25rem;
}

.page-subtitle {
  font-size: 0.875rem;
  color: var(--color-text-secondary);
  margin: 0;
}

.create-button {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.button-icon {
  width: 16px;
  height: 16px;
}

.rules-list {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.rule-card {
  padding: 1.25rem;
  transition: border-color 0.2s ease;
}

.rule-card:hover {
  border-color: color-mix(in srgb, var(--color-primary) 30%, var(--color-border));
}

.rule-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 1rem;
}

.rule-info {
  flex: 1;
  min-width: 0;
}

.rule-title-row {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 0.5rem;
}

.rule-name {
  font-size: 1rem;
  font-weight: 600;
  color: var(--color-text);
  margin: 0;
}

.rule-route {
  font-size: 0.875rem;
  color: var(--color-text-secondary);
  font-family: ui-monospace, monospace;
  margin: 0;
}

.rule-actions {
  display: flex;
  gap: 0.5rem;
}

.my-3 {
  margin: 0.75rem 0;
}

.rule-details {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 0.75rem;
}

.detail-row {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}

.detail-label {
  font-size: 0.75rem;
  color: var(--color-text-secondary);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.detail-value {
  font-size: 0.875rem;
  color: var(--color-text);
  font-family: ui-monospace, monospace;
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 4rem 2rem;
  text-align: center;
  color: var(--color-text-secondary);
}

.empty-icon {
  width: 64px;
  height: 64px;
  margin-bottom: 1rem;
  opacity: 0.4;
}

.empty-state h3 {
  font-size: 1.25rem;
  font-weight: 600;
  color: var(--color-text);
  margin-bottom: 0.5rem;
}

.empty-state p {
  font-size: 0.875rem;
  max-width: 400px;
  margin-bottom: 1.5rem;
}

.empty-action {
  margin-top: 0.5rem;
}

/* Dialog */
.dialog-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  padding: 1rem;
}

.dialog {
  background: var(--color-surface);
  border-radius: 12px;
  width: 100%;
  max-width: 600px;
  max-height: 90vh;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.3);
}

.dialog-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 1.5rem;
  border-bottom: 1px solid var(--color-border);
}

.dialog-title {
  font-size: 1.25rem;
  font-weight: 600;
  color: var(--color-text);
  margin: 0;
}

.dialog-close {
  width: 32px;
  height: 32px;
  border-radius: 6px;
  border: none;
  background: transparent;
  color: var(--color-text-secondary);
  font-size: 1.5rem;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s;
}

.dialog-close:hover {
  background: var(--color-surface-hover);
  color: var(--color-text);
}

.dialog-body {
  padding: 1.5rem;
  overflow-y: auto;
}

.form-group {
  margin-bottom: 1.25rem;
}

.form-label {
  display: block;
  font-size: 0.875rem;
  font-weight: 500;
  color: var(--color-text);
  margin-bottom: 0.5rem;
}

.form-label-row {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  cursor: pointer;
}

.form-input {
  width: 100%;
  padding: 0.625rem 0.875rem;
  border: 1px solid var(--color-border);
  border-radius: 6px;
  background: var(--color-background);
  color: var(--color-text);
  font-size: 0.875rem;
  transition: all 0.2s;
}

.form-input:focus {
  outline: none;
  border-color: var(--color-primary);
  box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.1);
}

.form-checkbox {
  width: 18px;
  height: 18px;
  cursor: pointer;
}

.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 0.75rem;
  padding: 1.5rem;
  border-top: 1px solid var(--color-border);
}
</style>
