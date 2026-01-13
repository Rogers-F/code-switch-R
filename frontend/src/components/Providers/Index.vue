<template>
  <PageLayout :title="t('sidebar.providers')" :sticky="true">
    <div class="providers-page">
      <div class="page-header">
        <div>
          <h2 class="page-title">{{ t('sidebar.providers') }}</h2>
          <p class="page-subtitle">{{ t('providers.subtitle') }}</p>
        </div>
      </div>

      <div class="providers-grid">
        <Card v-for="provider in providers" :key="provider.id" class="provider-card" variant="outline">
          <div class="provider-header">
            <div class="provider-icon" :class="`provider-icon--${provider.platform}`">
              {{ provider.icon }}
            </div>
            <div class="provider-info">
              <h3 class="provider-name">{{ provider.name }}</h3>
              <p class="provider-platform">{{ provider.platform }}</p>
            </div>
            <Badge :variant="provider.enabled ? 'success' : 'default'">
              {{ provider.enabled ? t('providers.status.enabled', 'Enabled') : t('providers.status.disabled', 'Disabled') }}
            </Badge>
          </div>

          <Separator class="my-3" />

          <div class="provider-details">
            <div class="detail-row">
              <span class="detail-label">{{ t('providers.field.endpoint', 'Endpoint') }}</span>
              <span class="detail-value">{{ provider.endpoint }}</span>
            </div>
            <div class="detail-row">
              <span class="detail-label">{{ t('providers.field.model', 'Model') }}</span>
              <span class="detail-value">{{ provider.model || t('providers.default', 'Default') }}</span>
            </div>
            <div class="detail-row">
              <span class="detail-label">{{ t('providers.field.priority', 'Priority') }}</span>
              <span class="detail-value">Level {{ provider.priority }}</span>
            </div>
          </div>

          <div class="provider-actions">
            <Button variant="outline" size="sm" class="flex-1">
              {{ t('providers.actions.edit', 'Edit') }}
            </Button>
            <Button variant="ghost" size="sm">
              {{ t('providers.actions.test', 'Test') }}
            </Button>
          </div>
        </Card>
      </div>

      <div v-if="providers.length === 0" class="empty-state">
        <svg class="empty-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M12 2L2 7l10 5 10-5-10-5z"></path>
          <path d="M2 17l10 5 10-5"></path>
          <path d="M2 12l10 5 10-5"></path>
        </svg>
        <h3>{{ t('providers.empty.title', 'No Providers') }}</h3>
        <p>{{ t('providers.empty.desc', 'Add your first AI provider to get started') }}</p>
      </div>
    </div>
  </PageLayout>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import PageLayout from '../common/PageLayout.vue'
import Card from '../ui/Card.vue'
import Badge from '../ui/Badge.vue'
import Button from '../ui/Button.vue'
import Separator from '../ui/Separator.vue'

const { t } = useI18n()

interface Provider {
  id: string
  name: string
  platform: string
  icon: string
  endpoint: string
  model: string
  priority: number
  enabled: boolean
}

const providers = ref<Provider[]>([
  {
    id: '1',
    name: 'Claude (Anthropic)',
    platform: 'claude',
    icon: '🤖',
    endpoint: 'https://api.anthropic.com',
    model: 'claude-sonnet-4',
    priority: 1,
    enabled: true,
  },
  {
    id: '2',
    name: 'Codex CLI',
    platform: 'codex',
    icon: '⚡',
    endpoint: 'Local MCP',
    model: 'gpt-5-codex',
    priority: 2,
    enabled: true,
  },
  {
    id: '3',
    name: 'Gemini Pro',
    platform: 'gemini',
    icon: '💎',
    endpoint: 'https://generativelanguage.googleapis.com',
    model: 'gemini-2.0-flash-exp',
    priority: 3,
    enabled: false,
  },
])
</script>

<style scoped>
.providers-page {
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

.providers-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 1rem;
}

.provider-card {
  padding: 1.25rem;
  transition: border-color 0.2s ease, box-shadow 0.2s ease;
}

.provider-card:hover {
  border-color: color-mix(in srgb, var(--color-primary) 30%, var(--color-border));
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.05);
}

.provider-header {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.provider-icon {
  width: 48px;
  height: 48px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 1.5rem;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
}

.provider-icon--claude {
  background: linear-gradient(135deg, #d97706 0%, #ea580c 100%);
  border-color: #ea580c;
}

.provider-icon--codex {
  background: linear-gradient(135deg, #7c3aed 0%, #6366f1 100%);
  border-color: #6366f1;
}

.provider-icon--gemini {
  background: linear-gradient(135deg, #06b6d4 0%, #3b82f6 100%);
  border-color: #3b82f6;
}

.provider-info {
  flex: 1;
  min-width: 0;
}

.provider-name {
  font-size: 1rem;
  font-weight: 600;
  color: var(--color-text);
  margin: 0 0 0.25rem 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.provider-platform {
  font-size: 0.75rem;
  color: var(--color-text-secondary);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  margin: 0;
}

.my-3 {
  margin: 0.75rem 0;
}

.provider-details {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  margin-bottom: 1rem;
}

.detail-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.5rem;
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
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: right;
}

.provider-actions {
  display: flex;
  gap: 0.5rem;
}

.flex-1 {
  flex: 1;
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
  margin: 0;
}
</style>
