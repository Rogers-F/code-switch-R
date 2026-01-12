<template>
  <PageLayout :title="t('sidebar.dashboard')" :sticky="true">
    <!-- 1) 热力图（从首页迁移） -->
    <section class="dashboard-section">
      <div class="dashboard-section__header">
        <h2 class="section__title">{{ t('dashboard.sections.heatmap.title') }}</h2>
      </div>

      <div
        ref="heatmapContainerRef"
        class="contrib-wall"
        :aria-label="t('components.main.heatmap.ariaLabel')"
      >
        <div class="contrib-legend">
          <span>{{ t('components.main.heatmap.legendLow') }}</span>
          <span v-for="level in 5" :key="level" :class="['legend-box', intensityClass(level - 1)]" />
          <span>{{ t('components.main.heatmap.legendHigh') }}</span>
        </div>

        <div class="contrib-grid">
          <div v-for="(week, weekIndex) in usageHeatmap" :key="weekIndex" class="contrib-column">
            <div
              v-for="(day, dayIndex) in week"
              :key="dayIndex"
              class="contrib-cell"
              :class="intensityClass(day.intensity)"
              @mouseenter="showUsageTooltip(day, $event)"
              @mousemove="showUsageTooltip(day, $event)"
              @mouseleave="hideUsageTooltip"
            />
          </div>
        </div>

        <div
          v-if="usageTooltip.visible"
          ref="tooltipRef"
          class="contrib-tooltip"
          :class="usageTooltip.placement"
          :style="{ left: `${usageTooltip.left}px`, top: `${usageTooltip.top}px` }"
        >
          <p class="tooltip-heading">{{ formattedTooltipLabel }}</p>
          <ul class="tooltip-metrics">
            <li v-for="metric in usageTooltipMetrics" :key="metric.key">
              <span class="metric-label">{{ metric.label }}</span>
              <span class="metric-value">{{ metric.value }}</span>
            </li>
          </ul>
        </div>
      </div>
    </section>

    <!-- 2) 使用统计（从日志迁移：4 统计 + 折线图） -->
    <section class="dashboard-section">
      <div class="dashboard-section__header">
        <h2 class="section__title">{{ t('dashboard.sections.usage.title') }}</h2>
      </div>

      <div class="dashboard-stats">
        <article
          v-for="card in usageStatsCards"
          :key="card.key"
          :class="['dashboard-stat-card', { 'dashboard-stat-card--clickable': card.key === 'cost' }]"
          @click="card.key === 'cost' && openCostDetailModal()"
        >
          <div class="dashboard-stat-card__label">{{ card.label }}</div>
          <div class="dashboard-stat-card__value">{{ card.value }}</div>
          <div class="dashboard-stat-card__hint">{{ card.hint }}</div>
        </article>
      </div>

      <div class="dashboard-chart mac-panel">
        <Line :data="chartData" :options="chartOptions" />
      </div>

      <BaseModal
        :open="costDetailModal.open"
        :title="t('components.logs.costDetail.title')"
        @close="closeCostDetailModal"
      >
        <div v-if="costDetailModal.loading" class="empty-state">{{ t('components.logs.loading') }}</div>
        <div v-else-if="costDetailModal.data.length === 0" class="empty-state">
          {{ t('components.logs.costDetail.empty') }}
        </div>
        <div v-else class="cost-detail-list">
          <div v-for="item in costDetailModal.data" :key="item.provider" class="cost-detail-row">
            <span class="cost-detail-provider">{{ item.provider }}</span>
            <span class="cost-detail-cost">{{ formatCurrency(item.cost_total) }}</span>
          </div>
        </div>
      </BaseModal>
    </section>

    <!-- 3) 监控情况（从可用性迁移：上方 4 统计） -->
    <section class="dashboard-section">
      <div class="dashboard-section__header">
        <h2 class="section__title">{{ t('dashboard.sections.monitor.title') }}</h2>
      </div>

      <div class="dashboard-monitor-stats">
        <div class="monitor-stat monitor-ok">
          <div class="monitor-stat__value">{{ monitorStats.operational }}</div>
          <div class="monitor-stat__label">{{ t('availability.stats.operational') }}</div>
        </div>
        <div class="monitor-stat monitor-warn">
          <div class="monitor-stat__value">{{ monitorStats.degraded }}</div>
          <div class="monitor-stat__label">{{ t('availability.stats.degraded') }}</div>
        </div>
        <div class="monitor-stat monitor-bad">
          <div class="monitor-stat__value">{{ monitorStats.failed }}</div>
          <div class="monitor-stat__label">{{ t('availability.stats.failed') }}</div>
        </div>
        <div class="monitor-stat monitor-off">
          <div class="monitor-stat__value">{{ monitorStats.disabled }}</div>
          <div class="monitor-stat__label">{{ t('availability.stats.disabled') }}</div>
        </div>
      </div>
    </section>
  </PageLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import PageLayout from '../common/PageLayout.vue'
import BaseModal from '../common/BaseModal.vue'
import {
  buildUsageHeatmapMatrix,
  generateFallbackUsageHeatmap,
  DEFAULT_HEATMAP_DAYS,
  calculateHeatmapDayRange,
  type UsageHeatmapWeek,
  type UsageHeatmapDay,
} from '../../data/usageHeatmap'
import {
  fetchHeatmapStats,
  fetchLogStats,
  fetchProviderDailyStats,
  type LogStats,
  type ProviderDailyStat,
} from '../../services/logs'
import {
  Chart,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
} from 'chart.js'
import type { ChartOptions } from 'chart.js'
import { Line } from 'vue-chartjs'
import {
  getLatestResults,
  HealthStatus,
  type ProviderTimeline,
} from '../../services/healthcheck'

Chart.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend)

const { t, locale } = useI18n()

const REFRESH_INTERVAL_MS = 30_000
let refreshTimer: number | undefined
const refreshing = ref(false)

// ===== 1) 热力图 =====
const HEATMAP_DAYS = DEFAULT_HEATMAP_DAYS
const usageHeatmap = ref<UsageHeatmapWeek[]>(generateFallbackUsageHeatmap(HEATMAP_DAYS))
const heatmapContainerRef = ref<HTMLElement | null>(null)
const tooltipRef = ref<HTMLElement | null>(null)

const intensityClass = (value: number) => `gh-level-${value}`

type TooltipPlacement = 'above' | 'below'

const usageTooltip = reactive({
  visible: false,
  label: '',
  dateKey: '',
  left: 0,
  top: 0,
  placement: 'above' as TooltipPlacement,
  requests: 0,
  inputTokens: 0,
  outputTokens: 0,
  reasoningTokens: 0,
  cost: 0,
})

const formatMetric = (value: number) => value.toLocaleString()

const tooltipDateFormatter = computed(() =>
  new Intl.DateTimeFormat(locale.value || 'en', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
)

const currencyFormatter = computed(() =>
  new Intl.NumberFormat(locale.value || 'en', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
)

const formattedTooltipLabel = computed(() => {
  if (!usageTooltip.dateKey) return usageTooltip.label
  const date = new Date(usageTooltip.dateKey)
  if (Number.isNaN(date.getTime())) {
    return usageTooltip.label
  }
  return tooltipDateFormatter.value.format(date)
})

const formattedTooltipAmount = computed(() =>
  currencyFormatter.value.format(Math.max(usageTooltip.cost, 0))
)

const usageTooltipMetrics = computed(() => [
  {
    key: 'cost',
    label: t('components.main.heatmap.metrics.cost'),
    value: formattedTooltipAmount.value,
  },
  {
    key: 'requests',
    label: t('components.main.heatmap.metrics.requests'),
    value: formatMetric(usageTooltip.requests),
  },
  {
    key: 'inputTokens',
    label: t('components.main.heatmap.metrics.inputTokens'),
    value: formatMetric(usageTooltip.inputTokens),
  },
  {
    key: 'outputTokens',
    label: t('components.main.heatmap.metrics.outputTokens'),
    value: formatMetric(usageTooltip.outputTokens),
  },
  {
    key: 'reasoningTokens',
    label: t('components.main.heatmap.metrics.reasoningTokens'),
    value: formatMetric(usageTooltip.reasoningTokens),
  },
])

const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value))

const TOOLTIP_DEFAULT_WIDTH = 220
const TOOLTIP_DEFAULT_HEIGHT = 120
const TOOLTIP_VERTICAL_OFFSET = 12
const TOOLTIP_HORIZONTAL_MARGIN = 20
const TOOLTIP_VERTICAL_MARGIN = 24

const getTooltipSize = () => {
  const rect = tooltipRef.value?.getBoundingClientRect()
  return {
    width: rect?.width ?? TOOLTIP_DEFAULT_WIDTH,
    height: rect?.height ?? TOOLTIP_DEFAULT_HEIGHT,
  }
}

const viewportSize = () => {
  if (typeof window !== 'undefined') {
    return { width: window.innerWidth, height: window.innerHeight }
  }
  if (typeof document !== 'undefined' && document.documentElement) {
    return {
      width: document.documentElement.clientWidth,
      height: document.documentElement.clientHeight,
    }
  }
  return {
    width: heatmapContainerRef.value?.clientWidth ?? 0,
    height: heatmapContainerRef.value?.clientHeight ?? 0,
  }
}

const showUsageTooltip = (day: UsageHeatmapDay, event: MouseEvent) => {
  const target = event.currentTarget as HTMLElement | null
  const cellRect = target?.getBoundingClientRect()
  if (!cellRect) return

  usageTooltip.label = day.label
  usageTooltip.dateKey = day.dateKey
  usageTooltip.requests = day.requests
  usageTooltip.inputTokens = day.inputTokens
  usageTooltip.outputTokens = day.outputTokens
  usageTooltip.reasoningTokens = day.reasoningTokens
  usageTooltip.cost = day.cost

  const { width: tooltipWidth, height: tooltipHeight } = getTooltipSize()
  const { width: viewportWidth, height: viewportHeight } = viewportSize()

  const centerX = cellRect.left + cellRect.width / 2
  const halfWidth = tooltipWidth / 2
  const minLeft = TOOLTIP_HORIZONTAL_MARGIN + halfWidth
  const maxLeft = viewportWidth > 0 ? viewportWidth - halfWidth - TOOLTIP_HORIZONTAL_MARGIN : centerX
  usageTooltip.left = clamp(centerX, minLeft, maxLeft)

  const anchorTop = cellRect.top
  const anchorBottom = cellRect.bottom
  const canShowAbove = anchorTop - tooltipHeight - TOOLTIP_VERTICAL_OFFSET >= TOOLTIP_VERTICAL_MARGIN
  const viewportBottomLimit =
    viewportHeight > 0 ? viewportHeight - tooltipHeight - TOOLTIP_VERTICAL_MARGIN : anchorBottom
  const shouldPlaceBelow = !canShowAbove
  usageTooltip.placement = shouldPlaceBelow ? 'below' : 'above'
  const desiredTop = shouldPlaceBelow
    ? anchorBottom + TOOLTIP_VERTICAL_OFFSET
    : anchorTop - tooltipHeight - TOOLTIP_VERTICAL_OFFSET
  usageTooltip.top = clamp(desiredTop, TOOLTIP_VERTICAL_MARGIN, viewportBottomLimit)
  usageTooltip.visible = true
}

const hideUsageTooltip = () => {
  usageTooltip.visible = false
}

const loadUsageHeatmap = async () => {
  const rangeDays = calculateHeatmapDayRange(HEATMAP_DAYS)
  const stats = await fetchHeatmapStats(rangeDays)
  usageHeatmap.value = buildUsageHeatmapMatrix(stats, HEATMAP_DAYS)
}

// ===== 2) 使用统计（日志 StatsSince） =====
const usageStats = ref<LogStats | null>(null)
const statsSeries = computed(() => usageStats.value?.series ?? [])

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

const padHour = (num: number) => num.toString().padStart(2, '0')

const formatSeriesLabel = (value?: string) => {
  if (!value) return ''
  const parsed = parseLogDate(value)
  if (parsed) {
    return `${padHour(parsed.getHours())}:00`
  }
  const match = value.match(/(\d{2}):(\d{2})/)
  if (match) {
    return `${match[1]}:${match[2]}`
  }
  return value
}

const chartData = computed(() => {
  const series = statsSeries.value
  return {
    labels: series.map((item) => formatSeriesLabel(item.day)),
    datasets: [
      {
        label: t('components.logs.tokenLabels.cost'),
        data: series.map((item) => Number(((item.total_cost ?? 0)).toFixed(4))),
        borderColor: '#f97316',
        backgroundColor: 'rgba(249, 115, 22, 0.2)',
        tension: 0.3,
        fill: false,
        yAxisID: 'yCost',
      },
      {
        label: t('components.logs.tokenLabels.input'),
        data: series.map((item) => item.input_tokens ?? 0),
        borderColor: '#34d399',
        backgroundColor: 'rgba(52, 211, 153, 0.25)',
        tension: 0.35,
        fill: true,
      },
      {
        label: t('components.logs.tokenLabels.output'),
        data: series.map((item) => item.output_tokens ?? 0),
        borderColor: '#60a5fa',
        backgroundColor: 'rgba(96, 165, 250, 0.2)',
        tension: 0.35,
        fill: true,
      },
      {
        label: t('components.logs.tokenLabels.reasoning'),
        data: series.map((item) => item.reasoning_tokens ?? 0),
        borderColor: '#f472b6',
        backgroundColor: 'rgba(244, 114, 182, 0.2)',
        tension: 0.35,
        fill: true,
      },
      {
        label: t('components.logs.tokenLabels.cacheWrite'),
        data: series.map((item) => item.cache_create_tokens ?? 0),
        borderColor: '#fbbf24',
        backgroundColor: 'rgba(251, 191, 36, 0.2)',
        tension: 0.35,
        fill: false,
      },
      {
        label: t('components.logs.tokenLabels.cacheRead'),
        data: series.map((item) => item.cache_read_tokens ?? 0),
        borderColor: '#a78bfa',
        backgroundColor: 'rgba(167, 139, 250, 0.2)',
        tension: 0.35,
        fill: false,
      },
    ],
  }
})

const chartOptions: ChartOptions<'line'> = {
  responsive: true,
  maintainAspectRatio: false,
  plugins: {
    legend: {
      position: 'bottom',
      labels: { color: '#64748b', usePointStyle: true },
    },
    tooltip: {
      backgroundColor: 'rgba(15, 23, 42, 0.92)',
      titleColor: '#fff',
      bodyColor: '#e2e8f0',
      borderColor: 'rgba(148, 163, 184, 0.35)',
      borderWidth: 1,
    },
  },
  interaction: { mode: 'index', intersect: false },
  scales: {
    x: { ticks: { color: '#94a3b8' }, grid: { color: 'rgba(148, 163, 184, 0.1)' } },
    y: {
      position: 'left',
      beginAtZero: true,
      ticks: { color: '#94a3b8' },
      grid: { color: 'rgba(148, 163, 184, 0.2)' },
    },
    yCost: {
      position: 'right',
      beginAtZero: true,
      grid: { drawOnChartArea: false },
      ticks: {
        color: '#475569',
        callback: (value: string | number) => {
          const numeric = typeof value === 'number' ? value : Number(value)
          if (Number.isNaN(numeric)) return '$0'
          if (numeric >= 1) return `$${numeric.toFixed(2)}`
          return `$${numeric.toFixed(4)}`
        },
      },
    },
  },
}

const formatNumber = (value?: number) => {
  if (value === undefined || value === null) return '—'
  return value.toLocaleString()
}

const formatCurrency = (value?: number) => {
  if (value === undefined || value === null || Number.isNaN(value)) {
    return '$0.0000'
  }
  if (value >= 1) {
    return `$${value.toFixed(2)}`
  }
  if (value >= 0.01) {
    return `$${value.toFixed(3)}`
  }
  return `$${value.toFixed(4)}`
}

const startOfTodayLocal = () => {
  const now = new Date()
  now.setHours(0, 0, 0, 0)
  return now
}

const summaryDateLabel = computed(() => {
  const firstBucket = statsSeries.value.find((item) => item.day)
  const parsed = parseLogDate(firstBucket?.day ?? '')
  const date = parsed ?? startOfTodayLocal()
  return `${date.getFullYear()}-${padHour(date.getMonth() + 1)}-${padHour(date.getDate())}`
})

const usageStatsCards = computed(() => {
  const data = usageStats.value
  const summaryDate = summaryDateLabel.value
  const totalTokens =
    (data?.input_tokens ?? 0) + (data?.output_tokens ?? 0) + (data?.reasoning_tokens ?? 0)
  return [
    {
      key: 'requests',
      label: t('components.logs.summary.total'),
      hint: t('components.logs.summary.requests'),
      value: data ? formatNumber(data.total_requests) : '—',
    },
    {
      key: 'tokens',
      label: t('components.logs.summary.tokens'),
      hint: t('components.logs.summary.tokenHint'),
      value: data ? formatNumber(totalTokens) : '—',
    },
    {
      key: 'cacheReads',
      label: t('components.logs.summary.cache'),
      hint: t('components.logs.summary.cacheHint'),
      value: data ? formatNumber(data.cache_read_tokens) : '—',
    },
    {
      key: 'cost',
      label: t('components.logs.tokenLabels.cost'),
      hint: summaryDate ? t('components.logs.summary.todayScope', { date: summaryDate }) : '',
      value: formatCurrency(data?.cost_total ?? 0),
    },
  ]
})

const loadUsageStats = async () => {
  usageStats.value = await fetchLogStats('')
}

const costDetailModal = reactive<{
  open: boolean
  loading: boolean
  data: ProviderDailyStat[]
}>({
  open: false,
  loading: false,
  data: [],
})

const openCostDetailModal = async () => {
  costDetailModal.open = true
  costDetailModal.loading = true
  costDetailModal.data = []
  try {
    const stats = await fetchProviderDailyStats('')
    costDetailModal.data = (stats ?? [])
      .filter((item) => item.cost_total > 0)
      .sort((a, b) => b.cost_total - a.cost_total)
  } catch (error) {
    console.error('failed to load provider daily stats', error)
  } finally {
    costDetailModal.loading = false
  }
}

const closeCostDetailModal = () => {
  costDetailModal.open = false
}

// ===== 3) 监控统计（可用性） =====
const monitorTimelines = ref<Record<string, ProviderTimeline[]>>({})

const monitorStats = computed(() => {
  const stats = {
    operational: 0,
    degraded: 0,
    failed: 0,
    disabled: 0,
    total: 0,
  }

  for (const platform of Object.keys(monitorTimelines.value)) {
    for (const timeline of monitorTimelines.value[platform] || []) {
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

const loadMonitorStats = async () => {
  monitorTimelines.value = await getLatestResults()
}

// ===== 统一刷新 =====
const refreshAll = async () => {
  if (refreshing.value) return
  refreshing.value = true
  try {
    await Promise.all([loadUsageHeatmap(), loadUsageStats(), loadMonitorStats()])
  } catch (error) {
    console.error('failed to refresh dashboard', error)
  } finally {
    refreshing.value = false
  }
}

onMounted(async () => {
  await refreshAll()
  refreshTimer = window.setInterval(() => {
    void refreshAll()
  }, REFRESH_INTERVAL_MS)
})

onUnmounted(() => {
  if (refreshTimer) {
    window.clearInterval(refreshTimer)
    refreshTimer = undefined
  }
})
</script>

<style scoped>
.dashboard-section__header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
}

.dashboard-stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
  gap: 12px;
}

.dashboard-stat-card {
  border: 1px solid var(--mac-border);
  background: var(--mac-surface);
  border-radius: 16px;
  padding: 16px 18px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  transition: border-color 0.2s ease, box-shadow 0.2s ease;
}

.dashboard-stat-card--clickable {
  cursor: pointer;
}

.dashboard-stat-card--clickable:hover {
  border-color: color-mix(in srgb, var(--mac-accent) 30%, var(--mac-border));
  box-shadow: 0 8px 22px rgba(0, 0, 0, 0.08);
}

.dashboard-stat-card__label {
  font-size: 0.8rem;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--mac-text-secondary);
}

.dashboard-stat-card__value {
  font-size: 1.25rem;
  font-weight: 800;
  color: var(--mac-text);
}

.dashboard-stat-card__hint {
  font-size: 0.85rem;
  color: var(--mac-text-secondary);
  line-height: 1.2;
}

.dashboard-chart {
  height: 280px;
  padding: 16px 18px;
  margin-top: var(--page-padding-y);
}

.cost-detail-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.cost-detail-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 12px;
  border-radius: 12px;
  border: 1px solid var(--mac-border);
  background: var(--mac-surface);
}

.cost-detail-provider {
  font-weight: 600;
  color: var(--mac-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cost-detail-cost {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  color: var(--mac-text-secondary);
}

.dashboard-monitor-stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
  gap: 12px;
}

.monitor-stat {
  border-radius: 16px;
  border: 1px solid var(--mac-border);
  background: var(--mac-surface);
  padding: 16px 18px;
}

.monitor-stat__value {
  font-size: 1.35rem;
  font-weight: 800;
  color: var(--mac-text);
}

.monitor-stat__label {
  margin-top: 4px;
  font-size: 0.85rem;
  color: var(--mac-text-secondary);
}

.monitor-ok .monitor-stat__value {
  color: #10b981;
}

.monitor-warn .monitor-stat__value {
  color: #f59e0b;
}

.monitor-bad .monitor-stat__value {
  color: #ef4444;
}

.monitor-off .monitor-stat__value {
  color: #64748b;
}
</style>
