<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getLatestResults,
  runAllChecks,
  runSingleCheck,
  setAvailabilityMonitorEnabled,
  isPollingRunning,
  saveAvailabilityConfig,
  ProviderTimeline,
  HealthStatus,
  formatStatus,
  getStatusColor,
} from '../../services/healthcheck'

const { t } = useI18n()

// 状态
const loading = ref(true)
const refreshing = ref(false)
const timelines = ref<Record<string, ProviderTimeline[]>>({})
const pollingRunning = ref(false)
const lastUpdated = ref<Date | null>(null)
const nextRefreshIn = ref(0)

// 配置编辑弹窗状态
const showConfigModal = ref(false)
const savingConfig = ref(false)
const activeProvider = ref<(ProviderTimeline & { platform: string }) | null>(null)
const configForm = ref({
  testModel: '',
  testEndpoint: '',
  timeout: 15000,
})

// 刷新定时器
let refreshTimer: ReturnType<typeof setInterval> | null = null
let countdownTimer: ReturnType<typeof setInterval> | null = null

// 计算属性：状态统计
const statusStats = computed(() => {
  const stats = {
    operational: 0,
    degraded: 0,
    failed: 0,
    disabled: 0,
    total: 0,
  }

  for (const platform of Object.keys(timelines.value)) {
    for (const timeline of timelines.value[platform] || []) {
      stats.total++
      if (!timeline.availabilityMonitorEnabled) {
        stats.disabled++
      } else if (timeline.latest) {
        switch (timeline.latest.status) {
          case HealthStatus.OPERATIONAL:
            stats.operational++
            break
          case HealthStatus.DEGRADED:
            stats.degraded++
            break
          case HealthStatus.FAILED:
          case HealthStatus.VALIDATION_ERROR:
            stats.failed++
            break
        }
      } else {
        stats.disabled++
      }
    }
  }

  return stats
})

// 计算属性：所有平台列表（过滤掉空平台）
const platforms = computed(() =>
  Object.keys(timelines.value).filter((platform) => (timelines.value[platform]?.length || 0) > 0)
)

// 加载数据
async function loadData() {
  try {
    timelines.value = await getLatestResults()
    pollingRunning.value = await isPollingRunning()
    lastUpdated.value = new Date()
  } catch (error) {
    console.error('Failed to load availability data:', error)
  } finally {
    loading.value = false
  }
}

// 刷新全部
async function refreshAll() {
  if (refreshing.value) return
  refreshing.value = true
  try {
    await runAllChecks()
    await loadData()
  } catch (error) {
    console.error('Failed to refresh:', error)
  } finally {
    refreshing.value = false
  }
}

// 检测单个 Provider
async function checkSingle(platform: string, providerId: number) {
  try {
    await runSingleCheck(platform, providerId)
    await loadData()
  } catch (error) {
    console.error('Failed to check provider:', error)
  }
}

// 切换监控开关
async function toggleMonitor(platform: string, providerId: number, enabled: boolean) {
  try {
    await setAvailabilityMonitorEnabled(platform, providerId, enabled)
    await loadData() // 刷新当前页面

    // 通知主页面刷新供应商列表
    window.dispatchEvent(new CustomEvent('providers-updated', {
      detail: { platform, providerId, enabled }
    }))
  } catch (error) {
    console.error('Failed to toggle monitor:', error)
  }
}

// 启用监控并打开配置编辑
async function enableMonitoringAndEdit(platform: string, timeline: ProviderTimeline) {
  try {
    await toggleMonitor(platform, timeline.providerId, true)
    // 等待状态更新后打开配置弹窗
    editConfig(platform, { ...timeline, availabilityMonitorEnabled: true })
  } catch (error) {
    console.error('Failed to enable monitoring and edit:', error)
  }
}

// 格式化时间
function formatTime(dateStr: string): string {
  if (!dateStr) return '-'
  const date = new Date(dateStr)
  return date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

// 格式化上次更新时间
function formatLastUpdated(): string {
  if (!lastUpdated.value) return '-'
  return lastUpdated.value.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

// 启动刷新定时器
function startRefreshTimer() {
  // 每 60 秒刷新一次
  const refreshInterval = 60000
  nextRefreshIn.value = 60

  refreshTimer = setInterval(() => {
    loadData()
    nextRefreshIn.value = 60
  }, refreshInterval)

  countdownTimer = setInterval(() => {
    if (nextRefreshIn.value > 0) {
      nextRefreshIn.value--
    }
  }, 1000)
}

// 停止定时器
function stopTimers() {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
  if (countdownTimer) {
    clearInterval(countdownTimer)
    countdownTimer = null
  }
}

// 打开配置编辑弹窗
function editConfig(platform: string, timeline: ProviderTimeline) {
  activeProvider.value = { ...timeline, platform }
  const cfg = timeline.availabilityConfig || {}
  configForm.value = {
    testModel: cfg.testModel || '',
    testEndpoint: cfg.testEndpoint || '',
    timeout: cfg.timeout || 15000,
  }
  showConfigModal.value = true
}

// 关闭配置编辑弹窗
function closeConfigModal() {
  showConfigModal.value = false
  activeProvider.value = null
}

// 保存配置
async function saveConfig() {
  if (!activeProvider.value) return
  savingConfig.value = true
  try {
    await saveAvailabilityConfig(activeProvider.value.platform, activeProvider.value.providerId, {
      testModel: configForm.value.testModel,
      testEndpoint: configForm.value.testEndpoint,
      timeout: Number(configForm.value.timeout) || 15000,
    })
    showConfigModal.value = false
    await loadData()
  } catch (error) {
    console.error('Failed to save availability config:', error)
  } finally {
    savingConfig.value = false
  }
}

// 显示配置值（为空时标注默认）
function displayConfigValue(value: string | number | undefined, label: string) {
  if (value === undefined || value === null || value === '' || value === 0) {
    return `${label}（${t('availability.default')}）`
  }
  return String(value)
}

onMounted(async () => {
  await loadData()
  startRefreshTimer()

  // 监听主页面的 Provider 更新事件
  const handleProvidersUpdated = () => {
    void loadData()
  }
  window.addEventListener('providers-updated', handleProvidersUpdated)

  // 清理监听器
  onUnmounted(() => {
    window.removeEventListener('providers-updated', handleProvidersUpdated)
    stopTimers()
  })
})

onUnmounted(() => {
  stopTimers()
})
</script>

<template>
  <div class="main-shell">
    <header class="app-page-header">
      <div class="app-page-title-group">
        <h1 class="app-page-title">{{ t('availability.title') }}</h1>
        <p class="app-page-subtitle">{{ t('availability.subtitle') }}</p>
      </div>
      <div class="app-page-actions">
        <button
          @click="refreshAll"
          :disabled="refreshing"
          class="primary-btn"
        >
          <span v-if="refreshing">🔄 {{ t('availability.refreshing') }}</span>
          <span v-else>⚡ {{ t('availability.refreshAll') }}</span>
        </button>
      </div>
    </header>

    <div class="app-page-container availability-page">

    <!-- 状态概览 -->
    <div class="grid grid-cols-4 gap-4 mb-6">
      <div class="stat-card bg-emerald-500/10 border border-emerald-500/20 rounded-xl p-4">
        <div class="text-3xl font-bold text-emerald-600 dark:text-emerald-400">{{ statusStats.operational }}</div>
        <div class="text-sm text-emerald-700 dark:text-emerald-300">{{ t('availability.stats.operational') }}</div>
      </div>
      <div class="stat-card bg-amber-500/10 border border-amber-500/20 rounded-xl p-4">
        <div class="text-3xl font-bold text-amber-600 dark:text-amber-400">{{ statusStats.degraded }}</div>
        <div class="text-sm text-amber-700 dark:text-amber-300">{{ t('availability.stats.degraded') }}</div>
      </div>
      <div class="stat-card bg-rose-500/10 border border-rose-500/20 rounded-xl p-4">
        <div class="text-3xl font-bold text-rose-600 dark:text-rose-400">{{ statusStats.failed }}</div>
        <div class="text-sm text-rose-700 dark:text-rose-300">{{ t('availability.stats.failed') }}</div>
      </div>
      <div class="stat-card bg-[var(--mac-surface-strong)] border border-[var(--mac-border)] rounded-xl p-4">
        <div class="text-3xl font-bold text-[var(--mac-text)] opacity-80">{{ statusStats.disabled }}</div>
        <div class="text-sm text-[var(--mac-text-secondary)]">{{ t('availability.stats.disabled') }}</div>
      </div>
    </div>

    <!-- 刷新状态 -->
    <div class="flex items-center justify-between text-sm text-[var(--mac-text-secondary)] mb-4">
      <span>{{ t('availability.lastUpdate') }}: {{ formatLastUpdated() }}</span>
      <span>{{ t('availability.nextRefresh') }}: {{ nextRefreshIn }}s</span>
    </div>

    <!-- 加载状态 -->
    <div v-if="loading" class="flex items-center justify-center py-12">
      <div class="animate-spin rounded-full h-8 w-8 border-b-2 border-[var(--mac-accent)]"></div>
    </div>

    <!-- Provider 列表 -->
    <div v-else class="space-y-6">
      <!-- 动态遍历所有平台 -->
      <div v-for="platform in platforms" :key="platform">
        <div v-if="timelines[platform]?.length">
          <h2 class="text-lg font-semibold text-[var(--mac-text)] mb-3 capitalize">
            {{ platform }} {{ t('availability.providers') }}
          </h2>
          <div class="space-y-3">
            <div
              v-for="timeline in timelines[platform]"
              :key="timeline.providerId"
              class="provider-card bg-[var(--mac-surface)] border border-[var(--mac-border)] rounded-xl p-4"
            >
              <div class="flex items-center justify-between">
                <!-- 左侧：开关 + 名称 + 状态 -->
                <div class="flex items-center gap-4">
                  <!-- 开关 -->
                  <label class="mac-switch">
                    <input
                      type="checkbox"
                      :checked="timeline.availabilityMonitorEnabled"
                      @change="toggleMonitor(platform, timeline.providerId, !timeline.availabilityMonitorEnabled)"
                    />
                    <span></span>
                  </label>

                  <!-- 名称 -->
                  <span class="font-medium text-[var(--mac-text)]">{{ timeline.providerName }}</span>

                  <!-- 状态指示 -->
                  <span v-if="timeline.availabilityMonitorEnabled && timeline.latest" :class="getStatusColor(timeline.latest.status)">
                    {{ formatStatus(timeline.latest.status) }}
                  </span>
                  <span v-else class="text-gray-400">{{ t('availability.notMonitored') }}</span>
                </div>

                <!-- 右侧：延迟 + 可用率 + 按钮 -->
                <div class="flex items-center gap-4">
                  <!-- 延迟 -->
                  <span v-if="timeline.latest?.latencyMs" class="text-sm text-[var(--mac-text-secondary)]">
                    {{ timeline.latest.latencyMs }}ms
                  </span>

                  <!-- 可用率 -->
                  <span v-if="timeline.uptime > 0" class="text-sm text-[var(--mac-text-secondary)]">
                    {{ timeline.uptime.toFixed(1) }}%
                  </span>

                  <!-- 检测按钮 (Secondary - Gemini 设计方案 C) -->
                  <button
                    v-if="timeline.availabilityMonitorEnabled"
                    @click="checkSingle(platform, timeline.providerId)"
                    class="action-btn text-xs py-1 px-2 flex items-center gap-1"
                  >
                    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                    </svg>
                    {{ t('availability.check') }}
                  </button>

                  <!-- 启用监控按钮 (监控关闭时显示 - 高对比度吸引注意力) -->
                  <button
                    v-if="!timeline.availabilityMonitorEnabled"
                    @click="enableMonitoringAndEdit(platform, timeline)"
                    class="primary-btn text-sm py-1.5 px-3"
                  >
                    {{ t('availability.enableMonitoring') }}
                  </button>

                  <!-- 编辑配置按钮 (监控开启时显示 - 相对低调但清晰可见) -->
                  <button
                    v-else
                    @click="editConfig(platform, timeline)"
                    class="action-btn text-sm py-1.5 px-3 flex items-center gap-2"
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z" />
                    </svg>
                    {{ t('availability.editConfig') }}
                  </button>
                </div>
              </div>

              <!-- 当前生效配置 -->
              <div v-if="timeline.availabilityMonitorEnabled" class="mt-3 text-sm text-[var(--mac-text-secondary)] space-y-1">
                <div>{{ t('availability.currentModel') }}：{{ displayConfigValue(timeline.availabilityConfig?.testModel, t('availability.defaultModel')) }}</div>
                <div>{{ t('availability.currentEndpoint') }}：{{ displayConfigValue(timeline.availabilityConfig?.testEndpoint, t('availability.defaultEndpoint')) }}</div>
                <div>{{ t('availability.currentTimeout') }}：{{ displayConfigValue(timeline.availabilityConfig?.timeout, '15000ms') }}</div>
              </div>

              <!-- 时间线（如果有历史记录） -->
              <div v-if="timeline.items?.length > 0" class="mt-3 flex gap-1">
                <div
                  v-for="(item, idx) in timeline.items.slice(0, 20)"
                  :key="idx"
                  :title="`${formatTime(item.checkedAt)} - ${formatStatus(item.status)} (${item.latencyMs}ms)`"
                  class="w-3 h-3 rounded-sm"
                  :class="{
                    'bg-green-500': item.status === HealthStatus.OPERATIONAL,
                    'bg-yellow-500': item.status === HealthStatus.DEGRADED,
                    'bg-red-500': item.status === HealthStatus.FAILED || item.status === HealthStatus.VALIDATION_ERROR,
                  }"
                ></div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- 无数据提示 -->
      <div v-if="platforms.length === 0" class="text-center py-12 text-[var(--mac-text-secondary)]">
        {{ t('availability.noProviders') }}
      </div>
    </div>

    <!-- 配置编辑弹窗 -->
    <div v-if="showConfigModal" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
      <div class="bg-[var(--mac-surface)] border border-[var(--mac-border)] rounded-2xl shadow-xl w-full max-w-lg p-6">
        <div class="flex items-center justify-between mb-4">
          <div>
            <h3 class="text-xl font-semibold text-[var(--mac-text)]">
              {{ t('availability.configTitle') }}
            </h3>
            <p class="text-sm text-[var(--mac-text-secondary)]">
              {{ activeProvider?.providerName }} ({{ activeProvider?.platform }})
            </p>
          </div>
          <button class="text-[var(--mac-text-secondary)] hover:text-[var(--mac-text)]" @click="closeConfigModal">✕</button>
        </div>

        <div class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-[var(--mac-text)] mb-1">{{ t('availability.field.testModel') }}</label>
            <input
              v-model="configForm.testModel"
              type="text"
              class="w-full rounded-lg border border-[var(--mac-border)] bg-[var(--mac-surface-strong)] px-3 py-2 focus:outline-none focus:ring-2 focus:ring-[var(--mac-accent)]"
              :placeholder="t('availability.placeholder.testModel')"
            />
          </div>

          <div>
            <label class="block text-sm font-medium text-[var(--mac-text)] mb-1">{{ t('availability.field.testEndpoint') }}</label>
            <input
              v-model="configForm.testEndpoint"
              type="text"
              class="w-full rounded-lg border border-[var(--mac-border)] bg-[var(--mac-surface-strong)] px-3 py-2 focus:outline-none focus:ring-2 focus:ring-[var(--mac-accent)]"
              :placeholder="t('availability.placeholder.testEndpoint')"
            />
          </div>

          <div>
            <label class="block text-sm font-medium text-[var(--mac-text)] mb-1">{{ t('availability.field.timeout') }}</label>
            <input
              v-model.number="configForm.timeout"
              type="number"
              min="1000"
              class="w-full rounded-lg border border-[var(--mac-border)] bg-[var(--mac-surface-strong)] px-3 py-2 focus:outline-none focus:ring-2 focus:ring-[var(--mac-accent)]"
              :placeholder="t('availability.placeholder.timeout')"
            />
            <p class="mt-1 text-xs text-[var(--mac-text-secondary)]">{{ t('availability.hint.timeout') }}</p>
          </div>
        </div>

        <div class="mt-6 flex justify-end gap-3">
          <button
            class="action-btn px-4 py-2 rounded-lg text-sm"
            @click="closeConfigModal"
          >
            {{ t('common.cancel') }}
          </button>
          <button
            class="primary-btn px-4 py-2 rounded-lg text-sm"
            :disabled="savingConfig"
            @click="saveConfig"
          >
            <span v-if="savingConfig">{{ t('common.saving') }}</span>
            <span v-else>{{ t('common.save') }}</span>
          </button>
        </div>
      </div>
    </div>
    </div>
  </div>
</template>

<style scoped>
.availability-page {
}

.provider-card {
  transition: box-shadow 0.2s;
}

.provider-card:hover {
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
}
</style>
