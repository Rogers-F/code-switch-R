<template>
  <PageLayout :title="t('sidebar.providers')" :sticky="true">
    <div class="providers-page">
      <div class="page-header">
        <h2 class="page-title">{{ t('sidebar.providers') }}</h2>
        <p class="page-subtitle">{{ t('providers.subtitle', 'Manage upstream AI providers (Claude, Codex, Gemini)') }}</p>
      </div>

      <div class="placeholder-content">
        <svg class="placeholder-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M12 2L2 7l10 5 10-5-10-5z"></path>
          <path d="M2 17l10 5 10-5"></path>
          <path d="M2 12l10 5 10-5"></path>
        </svg>
        <h3>{{ t('providers.coming_soon', 'Providers Management') }}</h3>
        <p>{{ t('providers.placeholder', 'Unified provider management interface coming soon') }}</p>
      </div>
    </div>
  </PageLayout>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import PageLayout from '../common/PageLayout.vue'

const { t } = useI18n()
</script>

<style scoped>
.providers-page {
  padding: 1.5rem;
  max-width: 1400px;
  margin: 0 auto;
}

.page-header {
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
}

.placeholder-content {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 4rem 2rem;
  text-align: center;
  color: var(--color-text-secondary);
}

.placeholder-icon {
  width: 64px;
  height: 64px;
  margin-bottom: 1rem;
  opacity: 0.4;
}

.placeholder-content h3 {
  font-size: 1.25rem;
  font-weight: 600;
  color: var(--color-text);
  margin-bottom: 0.5rem;
}

.placeholder-content p {
  font-size: 0.875rem;
  max-width: 400px;
}
</style>
