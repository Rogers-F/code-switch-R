<template>
  <PageLayout
    :title="t('sidebar.request_logs')"
    :sticky="true"
  >
    <template #actions>
      <button
        type="button"
        class="ghost-icon"
        :class="{ rotating: loading }"
        :data-tooltip="t('components.logs.refresh')"
        :aria-label="t('components.logs.refresh')"
        :disabled="loading"
        @click="manualRefresh"
      >
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path
            d="M20.5 8a8.5 8.5 0 10-2.38 7.41"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
          <path
            d="M20.5 4v4h-4"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </button>
    </template>

    <form class="logs-filter-row" @submit.prevent="applyFilters">
      <div class="filter-fields">
        <label class="filter-field">
          <span>{{ t('components.logs.filters.platform') }}</span>
          <select v-model="filters.platform" class="mac-select">
            <option value="">{{ t('components.logs.filters.allPlatforms') }}</option>
            <option value="claude">Claude</option>
            <option value="codex">Codex</option>
            <option value="gemini">Gemini</option>
          </select>
        </label>
        <label class="filter-field">
          <span>{{ t('components.logs.filters.provider') }}</span>
          <select v-model="filters.provider" class="mac-select">
            <option value="">{{ t('components.logs.filters.allProviders') }}</option>
            <option v-for="provider in providerOptions" :key="provider" :value="provider">
              {{ provider }}
            </option>
          </select>
        </label>
        <label class="filter-field record-mode-field">
          <span>{{ t('components.logs.recording.label') }}</span>
          <select v-model="recordMode" class="mac-select" @change="updateRecordMode(recordMode)">
            <option value="off">{{ t('components.logs.recording.off') }}</option>
            <option value="fail_only">{{ t('components.logs.recording.failedOnly') }}</option>
            <option value="all">{{ t('components.logs.recording.all') }}</option>
          </select>
        </label>
      </div>
      <div class="filter-actions">
        <BaseButton type="submit" :disabled="loading">
          {{ t('components.logs.query') }}
        </BaseButton>
      </div>
    </form>

    <section class="logs-table-wrapper">
      <table class="logs-table">
        <thead>
          <tr>
            <th class="col-time">{{ t('components.logs.table.time') }}</th>
            <th class="col-platform">{{ t('components.logs.table.platform') }}</th>
            <th class="col-provider">{{ t('components.logs.table.provider') }}</th>
            <th class="col-model">{{ t('components.logs.table.model') }}</th>
            <th class="col-http">{{ t('components.logs.table.httpCode') }}</th>
            <th class="col-stream">{{ t('components.logs.table.stream') }}</th>
            <th class="col-duration">{{ t('components.logs.table.duration') }}</th>
            <th class="col-tokens">{{ t('components.logs.table.tokens') }}</th>
            <th class="col-detail">{{ t('components.logs.detail.viewDetail') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in pagedLogs" :key="item.id" :class="{ 'row-clickable': item.request_detail_id }" @click="item.request_detail_id && openDetailDrawer(item.request_detail_id)">
            <td>{{ formatTime(item.created_at) }}</td>
            <td>{{ item.platform || '—' }}</td>
            <td>{{ item.provider || '—' }}</td>
            <td>{{ item.model || '—' }}</td>
            <td :class="['code', httpCodeClass(item.http_code)]">{{ item.http_code }}</td>
            <td><span :class="['stream-tag', item.is_stream ? 'on' : 'off']">{{ formatStream(item.is_stream) }}</span></td>
            <td><span :class="['duration-tag', durationColor(item.duration_sec)]">{{ formatDuration(item.duration_sec) }}</span></td>
            <td class="token-cell">
              <div>
                <span class="token-label">{{ t('components.logs.tokenLabels.input') }}</span>
                <span class="token-value">{{ formatNumber(item.input_tokens) }}</span>
              </div>
              <div>
                <span class="token-label">{{ t('components.logs.tokenLabels.output') }}</span>
                <span class="token-value">{{ formatNumber(item.output_tokens) }}</span>
              </div>
              <div>
                <span class="token-label">{{ t('components.logs.tokenLabels.reasoning') }}</span>
                <span class="token-value">{{ formatNumber(item.reasoning_tokens) }}</span>
              </div>
              <div>
                <span class="token-label">{{ t('components.logs.tokenLabels.cacheWrite') }}</span>
                <span class="token-value">{{ formatNumber(item.cache_create_tokens) }}</span>
              </div>
              <div>
                <span class="token-label">{{ t('components.logs.tokenLabels.cacheRead') }}</span>
                <span class="token-value">{{ formatNumber(item.cache_read_tokens) }}</span>
              </div>
            </td>
            <td class="detail-cell">
              <button
                v-if="item.request_detail_id"
                class="detail-btn"
                @click.stop="openDetailDrawer(item.request_detail_id)"
                :title="t('components.logs.detail.viewDetail')"
              >
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
                  <polyline points="14 2 14 8 20 8"/>
                  <line x1="16" y1="13" x2="8" y2="13"/>
                  <line x1="16" y1="17" x2="8" y2="17"/>
                  <polyline points="10 9 9 9 8 9"/>
                </svg>
              </button>
              <span v-else class="no-detail">{{ t('components.logs.detail.noDetail') }}</span>
            </td>
          </tr>
          <tr v-if="!pagedLogs.length && !loading">
            <td colspan="9" class="empty">{{ t('components.logs.empty') }}</td>
          </tr>
        </tbody>
      </table>
      <p v-if="loading" class="empty">{{ t('components.logs.loading') }}</p>
    </section>

    <div class="logs-pagination">
      <span>{{ page }} / {{ totalPages }}</span>
      <div class="pagination-actions">
        <BaseButton variant="outline" size="sm" :disabled="page === 1 || loading" @click="prevPage">
          ‹
        </BaseButton>
        <BaseButton variant="outline" size="sm" :disabled="page >= totalPages || loading" @click="nextPage">
          ›
        </BaseButton>
      </div>
    </div>

    <!-- 请求详情抽屉 -->
    <RequestDetailDrawer
      :open="detailDrawer.open"
      :sequenceId="detailDrawer.sequenceId"
      @close="closeDetailDrawer"
    />
  </PageLayout>
</template>

<script setup lang="ts">
import { computed, reactive, ref, onMounted, watch, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseButton from '../common/BaseButton.vue'
import PageLayout from '../common/PageLayout.vue'
import RequestDetailDrawer from './RequestDetailDrawer.vue'
import {
  fetchRequestLogs,
  fetchLogProviders,
  type RequestLog,
  type LogPlatform,
} from '../../services/logs'
import {
  setRecordMode,
  getRecordMode,
  type RecordMode,
} from '../../services/requestDetail'

const { t } = useI18n()

const logs = ref<RequestLog[]>([])
const loading = ref(false)
const filters = reactive<{ platform: LogPlatform | ''; provider: string }>({ platform: '', provider: '' })
const page = ref(1)
const PAGE_SIZE = 15
const providerOptions = ref<string[]>([])

// 请求详情抽屉状态
const detailDrawer = reactive<{
  open: boolean
  sequenceId: number | null
}>({
  open: false,
  sequenceId: null,
})

// 详情记录模式
const recordMode = ref<RecordMode>('fail_only')

// 打开请求详情抽屉
const openDetailDrawer = (sequenceId: number | null | undefined) => {
  if (!sequenceId) return
  detailDrawer.sequenceId = sequenceId
  detailDrawer.open = true
}

// 关闭请求详情抽屉
const closeDetailDrawer = () => {
  detailDrawer.open = false
}

// 更新记录模式
const updateRecordMode = async (mode: RecordMode) => {
  try {
    await setRecordMode(mode)
    recordMode.value = mode
  } catch (error) {
    console.error('Failed to set record mode:', error)
  }
}

// 加载记录模式
const loadRecordMode = async () => {
  try {
    const mode = await getRecordMode()
    recordMode.value = mode
  } catch (error) {
    console.error('Failed to get record mode:', error)
  }
}

const parseLogDate = (value?: string) => {
  if (!value) return null
  const normalize = value.replace(' ', 'T')
  const attempts = [value, `${normalize}`, `${normalize}Z`]
  for (const candidate of attempts) {
    const parsed = new Date(candidate)
    if (!Number.isNaN(parsed.getTime())) {
      return parsed
    }
  }
  const match = value.match(/^(\d{4}-\d{2}-\d{2}) (\d{2}:\d{2}:\d{2}) ([+-]\d{4}) UTC$/)
  if (match) {
    const [, day, time, zone] = match
    const zoneFormatted = `${zone.slice(0, 3)}:${zone.slice(3)}`
    const parsed = new Date(`${day}T${time}${zoneFormatted}`)
    if (!Number.isNaN(parsed.getTime())) {
      return parsed
    }
  }
  return null
}

const REFRESH_INTERVAL = 30
const countdown = ref(REFRESH_INTERVAL)
let timer: number | undefined

const resetTimer = () => {
  countdown.value = REFRESH_INTERVAL
}

const startCountdown = () => {
  stopCountdown()
  timer = window.setInterval(() => {
    if (countdown.value <= 1) {
      countdown.value = REFRESH_INTERVAL
      void loadDashboard()
    } else {
      countdown.value -= 1
    }
  }, 1000)
}

const stopCountdown = () => {
  if (timer) {
    clearInterval(timer)
    timer = undefined
  }
}

const loadLogs = async () => {
  loading.value = true
  try {
    const data = await fetchRequestLogs({
      platform: filters.platform,
      provider: filters.provider,
      limit: 200,
    })
    logs.value = data ?? []
    page.value = Math.min(page.value, totalPages.value)
  } catch (error) {
    console.error('failed to load request logs', error)
  } finally {
    loading.value = false
  }
}

const loadDashboard = async () => {
  await loadLogs()
}

const pagedLogs = computed(() => {
  const start = (page.value - 1) * PAGE_SIZE
  return logs.value.slice(start, start + PAGE_SIZE)
})

const totalPages = computed(() => Math.max(1, Math.ceil(logs.value.length / PAGE_SIZE)))

const applyFilters = async () => {
  page.value = 1
  await loadDashboard()
  resetTimer()
}

const refreshLogs = () => {
  void loadDashboard()
}

const manualRefresh = () => {
  resetTimer()
  void loadDashboard()
}

const nextPage = () => {
  if (page.value < totalPages.value) {
    page.value += 1
  }
}

const prevPage = () => {
  if (page.value > 1) {
    page.value -= 1
  }
}

const padHour = (num: number) => num.toString().padStart(2, '0')

const formatTime = (value?: string) => {
  const date = parseLogDate(value)
  if (!date) return value || '—'
  return `${date.getFullYear()}-${padHour(date.getMonth() + 1)}-${padHour(date.getDate())} ${padHour(date.getHours())}:${padHour(date.getMinutes())}:${padHour(date.getSeconds())}`
}

const formatStream = (value?: boolean | number) => {
  const isOn = value === true || value === 1
  return isOn ? t('components.logs.streamOn') : t('components.logs.streamOff')
}

const formatDuration = (value?: number) => {
  if (!value || Number.isNaN(value)) return '—'
  return `${value.toFixed(2)}s`
}

const httpCodeClass = (code: number) => {
  if (code >= 500) return 'http-server-error'
  if (code >= 400) return 'http-client-error'
  if (code >= 300) return 'http-redirect'
  if (code >= 200) return 'http-success'
  return 'http-info'
}

const durationColor = (value?: number) => {
  if (!value || Number.isNaN(value)) return 'neutral'
  if (value < 2) return 'fast'
  if (value < 5) return 'medium'
  return 'slow'
}

const formatNumber = (value?: number) => {
  if (value === undefined || value === null) return '—'
  return value.toLocaleString()
}

const loadProviderOptions = async () => {
  try {
    const list = await fetchLogProviders(filters.platform)
    providerOptions.value = list ?? []
    if (filters.provider && !providerOptions.value.includes(filters.provider)) {
      filters.provider = ''
    }
  } catch (error) {
    console.error('failed to load provider options', error)
  }
}

watch(
  () => filters.platform,
  async () => {
    await loadProviderOptions()
  },
)

onMounted(async () => {
  await Promise.all([loadDashboard(), loadProviderOptions(), loadRecordMode()])
  startCountdown()
})

onUnmounted(() => {
  stopCountdown()
})
</script>

<style scoped>
.logs-header-actions {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.logs-refresh-label {
  font-size: 0.85rem;
  color: var(--mac-text-secondary);
}

.logs-summary {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
  gap: 1rem;
  margin-bottom: 0.75rem;
}

.summary-meta {
  grid-column: 1 / -1;
  font-size: 0.85rem;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: #64748b;
}

.summary-card {
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 16px;
  padding: 1rem 1.25rem;
  background: radial-gradient(circle at top, rgba(148, 163, 184, 0.1), rgba(15, 23, 42, 0));
  backdrop-filter: blur(6px);
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}

.summary-card__label {
  font-size: 0.85rem;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: #475569;
}

.summary-card__value {
  font-size: 1.85rem;
  font-weight: 600;
  color: #0f172a;
}

.summary-card__hint {
  font-size: 0.85rem;
  color: #94a3b8;
}

html.dark .summary-card {
  border-color: rgba(255, 255, 255, 0.12);
  background: radial-gradient(circle at top, rgba(148, 163, 184, 0.2), rgba(15, 23, 42, 0.35));
}

html.dark .summary-card__label {
  color: rgba(248, 250, 252, 0.75);
}

html.dark .summary-card__value {
  color: rgba(248, 250, 252, 0.95);
}

html.dark .summary-card__hint {
  color: rgba(186, 194, 210, 0.8);
}

@media (max-width: 768px) {
  .logs-summary {
    grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  }
}

/* 可点击卡片 */
.summary-card--clickable {
  cursor: pointer;
  transition: transform 0.15s ease, box-shadow 0.15s ease;
}
.summary-card--clickable:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(249, 115, 22, 0.15);
}
.summary-card--clickable:active {
  transform: translateY(0);
}
html.dark .summary-card--clickable:hover {
  box-shadow: 0 4px 12px rgba(249, 115, 22, 0.25);
}

/* 弹窗内容 */
.cost-detail-modal {
  min-height: 120px;
}
.cost-detail-loading,
.cost-detail-empty {
  text-align: center;
  color: #64748b;
  padding: 2rem 0;
}
html.dark .cost-detail-loading,
html.dark .cost-detail-empty {
  color: #94a3b8;
}
.cost-detail-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}
.cost-detail-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.75rem 1rem;
  background: rgba(148, 163, 184, 0.08);
  border-radius: 8px;
  transition: background 0.15s ease;
}
.cost-detail-item:hover {
  background: rgba(148, 163, 184, 0.12);
}
html.dark .cost-detail-item {
  background: rgba(148, 163, 184, 0.12);
}
html.dark .cost-detail-item:hover {
  background: rgba(148, 163, 184, 0.18);
}
.cost-detail-item__name {
  font-weight: 500;
  color: #1e293b;
}
html.dark .cost-detail-item__name {
  color: #f1f5f9;
}
.cost-detail-item__value {
  font-weight: 600;
  color: #f97316;
  font-variant-numeric: tabular-nums;
}

/* 记录模式选择器 */
.record-mode-field {
  min-width: 140px;
}

/* 详情列 */
.col-detail {
  width: 60px;
  text-align: center;
}

.detail-cell {
  text-align: center;
}

.detail-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: none;
  background: rgba(10, 132, 255, 0.1);
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.15s ease;
  color: #0a84ff;
}

.detail-btn:hover {
  background: rgba(10, 132, 255, 0.2);
  transform: scale(1.05);
}

.detail-btn:active {
  transform: scale(0.98);
}

html.dark .detail-btn {
  background: rgba(10, 132, 255, 0.15);
}

html.dark .detail-btn:hover {
  background: rgba(10, 132, 255, 0.25);
}

.no-detail {
  color: #94a3b8;
  font-size: 0.85rem;
}

/* 可点击行 */
.row-clickable {
  cursor: pointer;
  transition: background 0.15s ease;
}

.row-clickable:hover {
  background: rgba(10, 132, 255, 0.06);
}

html.dark .row-clickable:hover {
  background: rgba(10, 132, 255, 0.12);
}
</style>
