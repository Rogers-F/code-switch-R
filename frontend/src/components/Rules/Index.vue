<template>
  <PageLayout :title="t('sidebar.rules')" :sticky="true">
    <div class="rules-page">
      <div class="page-header">
        <h2 class="page-title">{{ t('sidebar.rules') }}</h2>
        <p class="page-subtitle">{{ t('rules.subtitle', 'Configure MITM domain routing rules and traffic policies') }}</p>
      </div>

      <div class="placeholder-content">
        <svg class="placeholder-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"></polygon>
        </svg>
        <h3>{{ t('rules.coming_soon', 'Routing Rules') }}</h3>
        <p>{{ t('rules.placeholder', 'Domain routing and traffic policy management linked with P2/P3 MITM capabilities') }}</p>
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
.rules-page {
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
