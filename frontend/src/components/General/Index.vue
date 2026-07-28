<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Call } from '@wailsio/runtime'
import { GetSyncStatus, SyncNow, RestoreBuiltinPricing } from '../../../bindings/codeswitch/services/modelsyncservice'
import type { ModelSyncStatus } from '../../../bindings/codeswitch/services/models'
import ListItem from '../Setting/ListRow.vue'
import LanguageSwitcher from '../Setting/LanguageSwitcher.vue'
import ThemeSetting from '../Setting/ThemeSetting.vue'
import NetworkWslSettings from '../Setting/NetworkWslSettings.vue'
import { fetchAppSettings, saveAppSettings, type AppSettings } from '../../services/appSettings'
import { getBlacklistSettings, updateBlacklistSettings, getLevelBlacklistEnabled, setLevelBlacklistEnabled, getBlacklistEnabled, setBlacklistEnabled, type BlacklistSettings } from '../../services/settings'
import { fetchConfigImportStatus, importFromPath, type ConfigImportStatus } from '../../services/configImport'
import { useI18n } from 'vue-i18n'
import { extractErrorMessage } from '../../utils/error'

const { t } = useI18n()

const router = useRouter()
// 从 localStorage 读取缓存值作为初始值，避免加载时的视觉闪烁
const getCachedValue = (key: string, defaultValue: boolean): boolean => {
  const cached = localStorage.getItem(`app-settings-${key}`)
  return cached !== null ? cached === 'true' : defaultValue
}
const getCachedNumber = (key: string, defaultValue: number): number => {
  const cached = localStorage.getItem(`app-settings-${key}`)
  if (cached === null) return defaultValue
  const parsed = Number(cached)
  return Number.isFinite(parsed) ? parsed : defaultValue
}
const getCachedString = (key: string, defaultValue: string): string => {
  const cached = localStorage.getItem(`app-settings-${key}`)
  return cached !== null ? cached : defaultValue
}
const heatmapEnabled = ref(getCachedValue('heatmap', true))
const homeTitleVisible = ref(getCachedValue('homeTitle', true))
const autoStartEnabled = ref(getCachedValue('autoStart', false))
const autoConnectivityTestEnabled = ref(getCachedValue('autoConnectivityTest', true))
const switchNotifyEnabled = ref(getCachedValue('switchNotify', true)) // 切换通知开关
const roundRobinEnabled = ref(getCachedValue('roundRobin', false))    // 同 Level 轮询开关
const autoUpdateEnabled = ref(getCachedValue('autoUpdate', true))     // 自动更新开关
const autoSyncModelsEnabled = ref(getCachedValue('autoSyncModels', true)) // 模型价格自动同步开关
const modelSyncStatus = ref<ModelSyncStatus | null>(null)
const modelSyncBusy = ref(false)
const budgetTotal = ref(getCachedNumber('budgetTotal', 0))
const budgetUsedAdjustment = ref(getCachedNumber('budgetUsedAdjustment', 0))
const budgetForecastMethod = ref(getCachedString('budgetForecastMethod', 'cycle'))
const budgetCycleEnabled = ref(getCachedValue('budgetCycleEnabled', false))
const budgetCycleMode = ref(getCachedString('budgetCycleMode', 'daily'))
const budgetRefreshTime = ref(getCachedString('budgetRefreshTime', '00:00'))
const budgetRefreshDay = ref(getCachedNumber('budgetRefreshDay', 1))
const budgetShowCountdown = ref(getCachedValue('budgetShowCountdown', false))
const budgetShowForecast = ref(getCachedValue('budgetShowForecast', false))
const budgetTotalCodex = ref(getCachedNumber('budgetTotalCodex', 0))
const budgetUsedAdjustmentCodex = ref(getCachedNumber('budgetUsedAdjustmentCodex', 0))
const budgetForecastMethodCodex = ref(getCachedString('budgetForecastMethodCodex', 'cycle'))
const budgetCycleEnabledCodex = ref(getCachedValue('budgetCycleEnabledCodex', false))
const budgetCycleModeCodex = ref(getCachedString('budgetCycleModeCodex', 'daily'))
const budgetRefreshTimeCodex = ref(getCachedString('budgetRefreshTimeCodex', '00:00'))
const budgetRefreshDayCodex = ref(getCachedNumber('budgetRefreshDayCodex', 1))
const budgetShowCountdownCodex = ref(getCachedValue('budgetShowCountdownCodex', false))
const budgetShowForecastCodex = ref(getCachedValue('budgetShowForecastCodex', false))
const settingsLoading = ref(true)
const settingsLoadFailed = ref(false)  // 设置加载失败标记：为 true 时禁止整对象保存，防止用默认值覆盖后端配置
const saveBusy = ref(false)

// 拉黑配置相关状态
const blacklistEnabled = ref(false)  // 拉黑功能总开关
const blacklistThreshold = ref(3)
const blacklistDuration = ref(30)
const levelBlacklistEnabled = ref(false)
const blacklistLoading = ref(false)
const blacklistSaving = ref(false)

// cc-switch 导入相关状态
const importStatus = ref<ConfigImportStatus | null>(null)
const importPath = ref('')
const importing = ref(false)
const importLoading = ref(true)

const goBack = () => {
  router.push('/')
}

const normalizeBudgetForecastMethod = (value: string) => {
  const trimmed = value?.trim()
  if (trimmed === 'cycle' || trimmed === '10m' || trimmed === '1h' || trimmed === 'yesterday' || trimmed === 'last24h') {
    return trimmed
  }
  return 'cycle'
}

const loadAppSettings = async () => {
  settingsLoading.value = true
  try {
    const data = await fetchAppSettings()
    heatmapEnabled.value = data?.show_heatmap ?? true
    homeTitleVisible.value = data?.show_home_title ?? true
    budgetTotal.value = Number(data?.budget_total ?? 0)
    budgetUsedAdjustment.value = Number(data?.budget_used_adjustment ?? 0)
    budgetForecastMethod.value = normalizeBudgetForecastMethod(data?.budget_forecast_method ?? 'cycle')
    budgetCycleEnabled.value = data?.budget_cycle_enabled ?? false
    budgetCycleMode.value = data?.budget_cycle_mode === 'weekly' ? 'weekly' : 'daily'
    budgetRefreshTime.value = data?.budget_refresh_time || '00:00'
    budgetRefreshDay.value = Number.isFinite(data?.budget_refresh_day) ? data?.budget_refresh_day : 1
    budgetShowCountdown.value = data?.budget_show_countdown ?? false
    budgetShowForecast.value = data?.budget_show_forecast ?? false
    budgetTotalCodex.value = Number(data?.budget_total_codex ?? 0)
    budgetUsedAdjustmentCodex.value = Number(data?.budget_used_adjustment_codex ?? 0)
    budgetForecastMethodCodex.value = normalizeBudgetForecastMethod(data?.budget_forecast_method_codex ?? 'cycle')
    budgetCycleEnabledCodex.value = data?.budget_cycle_enabled_codex ?? false
    budgetCycleModeCodex.value = data?.budget_cycle_mode_codex === 'weekly' ? 'weekly' : 'daily'
    budgetRefreshTimeCodex.value = data?.budget_refresh_time_codex || '00:00'
    budgetRefreshDayCodex.value = Number.isFinite(data?.budget_refresh_day_codex) ? data?.budget_refresh_day_codex : 1
    budgetShowCountdownCodex.value = data?.budget_show_countdown_codex ?? false
    budgetShowForecastCodex.value = data?.budget_show_forecast_codex ?? false
    autoStartEnabled.value = data?.auto_start ?? false
    autoConnectivityTestEnabled.value = data?.auto_connectivity_test ?? false
    switchNotifyEnabled.value = data?.enable_switch_notify ?? true
    roundRobinEnabled.value = data?.enable_round_robin ?? false
    autoUpdateEnabled.value = data?.auto_update ?? true
    autoSyncModelsEnabled.value = data?.auto_sync_models ?? true

    // 缓存到 localStorage，下次打开时直接显示正确状态
    localStorage.setItem('app-settings-heatmap', String(heatmapEnabled.value))
    localStorage.setItem('app-settings-homeTitle', String(homeTitleVisible.value))
    localStorage.setItem('app-settings-budgetTotal', String(budgetTotal.value))
    localStorage.setItem('app-settings-budgetUsedAdjustment', String(budgetUsedAdjustment.value))
    localStorage.setItem('app-settings-budgetForecastMethod', budgetForecastMethod.value)
    localStorage.setItem('app-settings-budgetCycleEnabled', String(budgetCycleEnabled.value))
    localStorage.setItem('app-settings-budgetCycleMode', budgetCycleMode.value)
    localStorage.setItem('app-settings-budgetRefreshTime', budgetRefreshTime.value)
    localStorage.setItem('app-settings-budgetRefreshDay', String(budgetRefreshDay.value))
    localStorage.setItem('app-settings-budgetShowCountdown', String(budgetShowCountdown.value))
    localStorage.setItem('app-settings-budgetShowForecast', String(budgetShowForecast.value))
    localStorage.setItem('app-settings-budgetTotalCodex', String(budgetTotalCodex.value))
    localStorage.setItem('app-settings-budgetUsedAdjustmentCodex', String(budgetUsedAdjustmentCodex.value))
    localStorage.setItem('app-settings-budgetForecastMethodCodex', budgetForecastMethodCodex.value)
    localStorage.setItem('app-settings-budgetCycleEnabledCodex', String(budgetCycleEnabledCodex.value))
    localStorage.setItem('app-settings-budgetCycleModeCodex', budgetCycleModeCodex.value)
    localStorage.setItem('app-settings-budgetRefreshTimeCodex', budgetRefreshTimeCodex.value)
    localStorage.setItem('app-settings-budgetRefreshDayCodex', String(budgetRefreshDayCodex.value))
    localStorage.setItem('app-settings-budgetShowCountdownCodex', String(budgetShowCountdownCodex.value))
    localStorage.setItem('app-settings-budgetShowForecastCodex', String(budgetShowForecastCodex.value))
    localStorage.setItem('app-settings-autoStart', String(autoStartEnabled.value))
    localStorage.setItem('app-settings-autoConnectivityTest', String(autoConnectivityTestEnabled.value))
    localStorage.setItem('app-settings-switchNotify', String(switchNotifyEnabled.value))
    localStorage.setItem('app-settings-roundRobin', String(roundRobinEnabled.value))
    localStorage.setItem('app-settings-autoUpdate', String(autoUpdateEnabled.value))
    localStorage.setItem('app-settings-autoSyncModels', String(autoSyncModelsEnabled.value))
    settingsLoadFailed.value = false
  } catch (error) {
    console.error('failed to load app settings', error)
    // 加载失败时保留当前值（初始值来自 localStorage 缓存，是最接近真实的状态），
    // 不重置为硬编码默认值；同时置失败标记，禁止整对象保存覆盖后端配置
    settingsLoadFailed.value = true
  } finally {
    settingsLoading.value = false
  }
  void refreshModelSyncStatus()
}

// —— 模型数据同步 ——

const refreshModelSyncStatus = async () => {
  try {
    modelSyncStatus.value = await GetSyncStatus()
  } catch (error) {
    console.error('failed to load model sync status', error)
  }
}

const syncModelDataNow = async () => {
  if (modelSyncBusy.value) return
  modelSyncBusy.value = true
  try {
    modelSyncStatus.value = await SyncNow()
  } catch (error) {
    console.error('model data sync failed', error)
    alert(t('components.general.label.modelSyncFailed') + extractErrorMessage(error))
  } finally {
    modelSyncBusy.value = false
  }
}

const restoreBuiltinModelData = async () => {
  // 与常规设置保存互斥:恢复动作在服务端会改写 AppSettings(关闭自动同步),
  // 若与保存并发会用旧快照覆盖用户刚保存的设置
  if (modelSyncBusy.value || saveBusy.value || settingsLoading.value) return
  if (!window.confirm(t('components.general.label.modelRestoreConfirm'))) return
  modelSyncBusy.value = true
  saveBusy.value = true
  try {
    modelSyncStatus.value = await RestoreBuiltinPricing()
    // 恢复内置会同时关闭自动同步(可再手动开启)
    autoSyncModelsEnabled.value = false
    localStorage.setItem('app-settings-autoSyncModels', 'false')
  } catch (error) {
    console.error('restore builtin model data failed', error)
    alert(t('components.general.label.modelRestoreFailed') + extractErrorMessage(error))
  } finally {
    saveBusy.value = false
    modelSyncBusy.value = false
    if (pendingSave.value) {
      pendingSave.value = false
      void persistAppSettings()
    }
  }
}

const modelSyncStatusText = computed(() => {
  const status = modelSyncStatus.value
  if (!status) return '—'
  const last = status.lastSuccess ? new Date(status.lastSuccess as unknown as string) : null
  const never = !last || Number.isNaN(last.getTime()) || last.getFullYear() < 2000
  const timeText = never ? t('components.general.label.modelSyncNever') : last!.toLocaleString()
  return t('components.general.label.modelSyncSummary', {
    time: timeText,
    count: status.pricing?.totalModels ?? 0,
  })
})

const modelSyncDefaultsText = computed(() => {
  const models = modelSyncStatus.value?.defaultModels
  if (!models) return '—'
  return `Codex: ${models.codexDefault} · Gemini: ${models.geminiDefault} · Claude(探测): ${models.claudeProbe}`
})

const pendingSave = ref(false)

const persistAppSettings = async () => {
  if (settingsLoading.value) return
  if (settingsLoadFailed.value) {
    // 设置尚未成功加载，此时整对象保存会把缓存/默认值写回后端，静默清空真实配置
    alert(t('components.general.label.settingsLoadFailed'))
    void loadAppSettings()
    return
  }
  if (saveBusy.value) {
    // 保存进行中:排队一次尾随保存,避免连续切换时后一次改动被静默丢弃
    pendingSave.value = true
    return
  }
  saveBusy.value = true
  try {
    const normalizedBudgetTotal = Number.isFinite(budgetTotal.value) ? Math.max(0, budgetTotal.value) : 0
    budgetTotal.value = normalizedBudgetTotal
    const normalizedBudgetUsedAdjustment = Number.isFinite(budgetUsedAdjustment.value)
      ? budgetUsedAdjustment.value
      : 0
    budgetUsedAdjustment.value = normalizedBudgetUsedAdjustment
    const normalizedBudgetForecastMethod = normalizeBudgetForecastMethod(budgetForecastMethod.value)
    budgetForecastMethod.value = normalizedBudgetForecastMethod
    const normalizedBudgetTotalCodex = Number.isFinite(budgetTotalCodex.value)
      ? Math.max(0, budgetTotalCodex.value)
      : 0
    budgetTotalCodex.value = normalizedBudgetTotalCodex
    const normalizedBudgetUsedAdjustmentCodex = Number.isFinite(budgetUsedAdjustmentCodex.value)
      ? budgetUsedAdjustmentCodex.value
      : 0
    budgetUsedAdjustmentCodex.value = normalizedBudgetUsedAdjustmentCodex
    const normalizedBudgetForecastMethodCodex = normalizeBudgetForecastMethod(budgetForecastMethodCodex.value)
    budgetForecastMethodCodex.value = normalizedBudgetForecastMethodCodex
    const normalizedBudgetRefreshDay = Number.isFinite(budgetRefreshDay.value)
      ? Math.min(Math.max(Math.floor(budgetRefreshDay.value), 0), 6)
      : 1
    budgetRefreshDay.value = normalizedBudgetRefreshDay
    const normalizedBudgetCycleMode = budgetCycleMode.value === 'weekly' ? 'weekly' : 'daily'
    budgetCycleMode.value = normalizedBudgetCycleMode
    const normalizedBudgetRefreshDayCodex = Number.isFinite(budgetRefreshDayCodex.value)
      ? Math.min(Math.max(Math.floor(budgetRefreshDayCodex.value), 0), 6)
      : 1
    budgetRefreshDayCodex.value = normalizedBudgetRefreshDayCodex
    const normalizedBudgetCycleModeCodex = budgetCycleModeCodex.value === 'weekly' ? 'weekly' : 'daily'
    budgetCycleModeCodex.value = normalizedBudgetCycleModeCodex
    const payload: AppSettings = {
      show_heatmap: heatmapEnabled.value,
      show_home_title: homeTitleVisible.value,
      budget_total: normalizedBudgetTotal,
      budget_used_adjustment: normalizedBudgetUsedAdjustment,
      budget_forecast_method: normalizedBudgetForecastMethod,
      budget_cycle_enabled: budgetCycleEnabled.value,
      budget_cycle_mode: normalizedBudgetCycleMode,
      budget_refresh_time: budgetRefreshTime.value || '00:00',
      budget_refresh_day: normalizedBudgetRefreshDay,
      budget_show_countdown: budgetShowCountdown.value,
      budget_show_forecast: budgetShowForecast.value,
      budget_total_codex: normalizedBudgetTotalCodex,
      budget_used_adjustment_codex: normalizedBudgetUsedAdjustmentCodex,
      budget_forecast_method_codex: normalizedBudgetForecastMethodCodex,
      budget_cycle_enabled_codex: budgetCycleEnabledCodex.value,
      budget_cycle_mode_codex: normalizedBudgetCycleModeCodex,
      budget_refresh_time_codex: budgetRefreshTimeCodex.value || '00:00',
      budget_refresh_day_codex: normalizedBudgetRefreshDayCodex,
      budget_show_countdown_codex: budgetShowCountdownCodex.value,
      budget_show_forecast_codex: budgetShowForecastCodex.value,
      auto_start: autoStartEnabled.value,
      auto_connectivity_test: autoConnectivityTestEnabled.value,
      enable_switch_notify: switchNotifyEnabled.value,
      enable_round_robin: roundRobinEnabled.value,
      auto_update: autoUpdateEnabled.value,
      auto_sync_models: autoSyncModelsEnabled.value,
    }
    await saveAppSettings(payload)

    // 同步自动可用性监控设置到 HealthCheckService（复用旧字段名）
    await Call.ByName(
      'codeswitch/services.HealthCheckService.SetAutoAvailabilityPolling',
      autoConnectivityTestEnabled.value
    )

    // 更新缓存
    localStorage.setItem('app-settings-heatmap', String(heatmapEnabled.value))
    localStorage.setItem('app-settings-homeTitle', String(homeTitleVisible.value))
    localStorage.setItem('app-settings-budgetTotal', String(budgetTotal.value))
    localStorage.setItem('app-settings-budgetUsedAdjustment', String(budgetUsedAdjustment.value))
    localStorage.setItem('app-settings-budgetForecastMethod', budgetForecastMethod.value)
    localStorage.setItem('app-settings-budgetCycleEnabled', String(budgetCycleEnabled.value))
    localStorage.setItem('app-settings-budgetCycleMode', budgetCycleMode.value)
    localStorage.setItem('app-settings-budgetRefreshTime', budgetRefreshTime.value)
    localStorage.setItem('app-settings-budgetRefreshDay', String(budgetRefreshDay.value))
    localStorage.setItem('app-settings-budgetShowCountdown', String(budgetShowCountdown.value))
    localStorage.setItem('app-settings-budgetShowForecast', String(budgetShowForecast.value))
    localStorage.setItem('app-settings-budgetTotalCodex', String(budgetTotalCodex.value))
    localStorage.setItem('app-settings-budgetUsedAdjustmentCodex', String(budgetUsedAdjustmentCodex.value))
    localStorage.setItem('app-settings-budgetForecastMethodCodex', budgetForecastMethodCodex.value)
    localStorage.setItem('app-settings-budgetCycleEnabledCodex', String(budgetCycleEnabledCodex.value))
    localStorage.setItem('app-settings-budgetCycleModeCodex', budgetCycleModeCodex.value)
    localStorage.setItem('app-settings-budgetRefreshTimeCodex', budgetRefreshTimeCodex.value)
    localStorage.setItem('app-settings-budgetRefreshDayCodex', String(budgetRefreshDayCodex.value))
    localStorage.setItem('app-settings-budgetShowCountdownCodex', String(budgetShowCountdownCodex.value))
    localStorage.setItem('app-settings-budgetShowForecastCodex', String(budgetShowForecastCodex.value))
    localStorage.setItem('app-settings-autoStart', String(autoStartEnabled.value))
    localStorage.setItem('app-settings-autoConnectivityTest', String(autoConnectivityTestEnabled.value))
    localStorage.setItem('app-settings-switchNotify', String(switchNotifyEnabled.value))
    localStorage.setItem('app-settings-roundRobin', String(roundRobinEnabled.value))
    localStorage.setItem('app-settings-autoUpdate', String(autoUpdateEnabled.value))
    localStorage.setItem('app-settings-autoSyncModels', String(autoSyncModelsEnabled.value))

    window.dispatchEvent(new CustomEvent('app-settings-updated'))
  } catch (error) {
    console.error('failed to save app settings', error)
    alert(t('components.general.label.settingsSaveFailed') + extractErrorMessage(error))
  } finally {
    saveBusy.value = false
    if (pendingSave.value) {
      pendingSave.value = false
      void persistAppSettings()
    }
  }
}

// 加载拉黑配置
const loadBlacklistSettings = async () => {
  blacklistLoading.value = true
  try {
    const settings = await getBlacklistSettings()
    blacklistThreshold.value = settings.failureThreshold
    blacklistDuration.value = settings.durationMinutes

    // 加载拉黑功能总开关
    const enabled = await getBlacklistEnabled()
    blacklistEnabled.value = enabled

    // 加载等级拉黑开关状态
    const levelEnabled = await getLevelBlacklistEnabled()
    levelBlacklistEnabled.value = levelEnabled
  } catch (error) {
    console.error('failed to load blacklist settings', error)
    // 使用默认值
    blacklistEnabled.value = false
    blacklistThreshold.value = 3
    blacklistDuration.value = 30
    levelBlacklistEnabled.value = false
  } finally {
    blacklistLoading.value = false
  }
}

// 保存拉黑配置
const saveBlacklistSettings = async () => {
  if (blacklistLoading.value || blacklistSaving.value) return
  blacklistSaving.value = true
  try {
    await updateBlacklistSettings(blacklistThreshold.value, blacklistDuration.value)
    alert('拉黑配置已保存')
  } catch (error) {
    console.error('failed to save blacklist settings', error)
    alert('保存失败：' + (error as Error).message)
  } finally {
    blacklistSaving.value = false
  }
}

// 切换拉黑功能总开关
const toggleBlacklist = async () => {
  if (blacklistLoading.value || blacklistSaving.value) return
  blacklistSaving.value = true
  try {
    await setBlacklistEnabled(blacklistEnabled.value)
  } catch (error) {
    console.error('failed to toggle blacklist', error)
    // 回滚状态
    blacklistEnabled.value = !blacklistEnabled.value
    alert('切换失败：' + (error as Error).message)
  } finally {
    blacklistSaving.value = false
  }
}

// 切换等级拉黑开关
const toggleLevelBlacklist = async () => {
  if (blacklistLoading.value || blacklistSaving.value) return
  blacklistSaving.value = true
  try {
    await setLevelBlacklistEnabled(levelBlacklistEnabled.value)
  } catch (error) {
    console.error('failed to toggle level blacklist', error)
    // 回滚状态
    levelBlacklistEnabled.value = !levelBlacklistEnabled.value
    alert('切换失败：' + (error as Error).message)
  } finally {
    blacklistSaving.value = false
  }
}

// 加载 cc-switch 导入状态
const loadImportStatus = async () => {
  importLoading.value = true
  try {
    importStatus.value = await fetchConfigImportStatus()
    // 设置默认路径
    if (importStatus.value?.config_path) {
      importPath.value = importStatus.value.config_path
    }
  } catch (error) {
    console.error('failed to load import status', error)
    importStatus.value = null
  } finally {
    importLoading.value = false
  }
}

// 执行导入
const handleImport = async () => {
  if (importing.value || !importPath.value.trim()) return
  importing.value = true
  try {
    const result = await importFromPath(importPath.value.trim())
    // 无论结果如何，都更新状态
    importStatus.value = result.status
    if (result.status.config_path) {
      importPath.value = result.status.config_path
    }
    if (!result.status.config_exists) {
      alert(t('components.general.import.fileNotFound'))
      return
    }
    const imported = result.imported_providers + result.imported_mcp
    if (imported > 0) {
      alert(t('components.general.import.success', {
        providers: result.imported_providers,
        mcp: result.imported_mcp
      }))
    } else {
      alert(t('components.general.import.nothingToImport'))
    }
  } catch (error) {
    console.error('import failed', error)
    alert(t('components.general.import.failed') + ': ' + (error as Error).message)
  } finally {
    importing.value = false
  }
}

onMounted(async () => {
  await loadAppSettings()

  // 加载拉黑配置
  await loadBlacklistSettings()

  // 加载导入状态
  await loadImportStatus()
})
</script>

<template>
  <div class="main-shell general-shell">
    <div class="global-actions">
      <p class="global-eyebrow">{{ $t('components.general.title.application') }}</p>
      <button class="ghost-icon" :aria-label="$t('components.general.buttons.back')" @click="goBack">
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path
            d="M15 18l-6-6 6-6"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </button>
    </div>

    <div class="general-page">
      <section>
        <h2 class="mac-section-title">{{ $t('components.general.title.application') }}</h2>
        <div class="mac-panel">
          <ListItem :label="$t('components.general.label.heatmap')">
            <label class="mac-switch">
              <input
                type="checkbox"
                :disabled="settingsLoading || saveBusy"
                v-model="heatmapEnabled"
                @change="persistAppSettings"
              />
              <span></span>
            </label>
          </ListItem>
          <ListItem :label="$t('components.general.label.homeTitle')">
            <label class="mac-switch">
              <input
                type="checkbox"
                :disabled="settingsLoading || saveBusy"
                v-model="homeTitleVisible"
                @change="persistAppSettings"
              />
              <span></span>
            </label>
          </ListItem>
          <ListItem :label="$t('components.general.label.autoStart')">
            <label class="mac-switch">
              <input
                type="checkbox"
                :disabled="settingsLoading || saveBusy"
                v-model="autoStartEnabled"
                @change="persistAppSettings"
              />
              <span></span>
            </label>
          </ListItem>
          <ListItem :label="$t('components.general.label.switchNotify')">
            <div class="toggle-with-hint">
              <label class="mac-switch">
                <input
                  type="checkbox"
                  :disabled="settingsLoading || saveBusy"
                  v-model="switchNotifyEnabled"
                  @change="persistAppSettings"
                />
                <span></span>
              </label>
              <span class="hint-text">{{ $t('components.general.label.switchNotifyHint') }}</span>
            </div>
          </ListItem>
          <ListItem :label="$t('components.general.label.roundRobin')">
            <div class="toggle-with-hint">
              <label class="mac-switch">
                <input
                  type="checkbox"
                  :disabled="settingsLoading || saveBusy"
                  v-model="roundRobinEnabled"
                  @change="persistAppSettings"
                />
                <span></span>
              </label>
              <span class="hint-text">{{ $t('components.general.label.roundRobinHint') }}</span>
            </div>
          </ListItem>
        </div>
      </section>

      <section>
        <h2 class="mac-section-title">{{ $t('components.general.title.trayPanel') }}</h2>
        <div class="mac-panel">
          <p class="panel-title">{{ $t('components.general.label.trayPanelClaude') }}</p>
          <ListItem :label="$t('components.general.label.budgetTotal')">
            <div class="toggle-with-hint">
              <div class="budget-input">
                <input
                  type="number"
                  inputmode="decimal"
                  min="0"
                  step="0.01"
                  :disabled="settingsLoading || saveBusy"
                  v-model.number="budgetTotal"
                  @change="persistAppSettings"
                  class="mac-input budget-input-field"
                />
                <span class="budget-unit">USD</span>
              </div>
              <span class="hint-text">{{ $t('components.general.label.budgetTotalHint') }}</span>
            </div>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetUsedAdjustment')">
            <div class="toggle-with-hint">
              <div class="budget-input">
                <input
                  type="number"
                  inputmode="decimal"
                  step="0.01"
                  :disabled="settingsLoading || saveBusy"
                  v-model.number="budgetUsedAdjustment"
                  @change="persistAppSettings"
                  class="mac-input budget-input-field"
                />
                <span class="budget-unit">USD</span>
              </div>
              <span class="hint-text">{{ $t('components.general.label.budgetUsedAdjustmentHint') }}</span>
            </div>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetCycle')">
            <div class="toggle-with-hint">
              <label class="mac-switch">
                <input
                  type="checkbox"
                  :disabled="settingsLoading || saveBusy"
                  v-model="budgetCycleEnabled"
                  @change="persistAppSettings"
                />
                <span></span>
              </label>
              <span class="hint-text">{{ $t('components.general.label.budgetCycleHint') }}</span>
            </div>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetCycleMode')">
            <select
              v-model="budgetCycleMode"
              :disabled="settingsLoading || saveBusy || !budgetCycleEnabled"
              class="mac-select budget-select"
              @change="persistAppSettings">
              <option value="daily">{{ $t('components.general.label.budgetCycleModeDaily') }}</option>
              <option value="weekly">{{ $t('components.general.label.budgetCycleModeWeekly') }}</option>
            </select>
          </ListItem>
          <ListItem
            v-if="budgetCycleMode === 'weekly'"
            :label="$t('components.general.label.budgetRefreshDay')">
            <select
              v-model.number="budgetRefreshDay"
              :disabled="settingsLoading || saveBusy || !budgetCycleEnabled"
              class="mac-select budget-select"
              @change="persistAppSettings">
              <option :value="1">{{ $t('components.general.label.weekdayMon') }}</option>
              <option :value="2">{{ $t('components.general.label.weekdayTue') }}</option>
              <option :value="3">{{ $t('components.general.label.weekdayWed') }}</option>
              <option :value="4">{{ $t('components.general.label.weekdayThu') }}</option>
              <option :value="5">{{ $t('components.general.label.weekdayFri') }}</option>
              <option :value="6">{{ $t('components.general.label.weekdaySat') }}</option>
              <option :value="0">{{ $t('components.general.label.weekdaySun') }}</option>
            </select>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetRefreshTime')">
            <input
              type="time"
              :disabled="settingsLoading || saveBusy || !budgetCycleEnabled"
              v-model="budgetRefreshTime"
              @change="persistAppSettings"
              class="mac-input budget-time-input"
            />
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetShowCountdown')">
            <label class="mac-switch">
              <input
                type="checkbox"
                :disabled="settingsLoading || saveBusy"
                v-model="budgetShowCountdown"
                @change="persistAppSettings"
              />
              <span></span>
            </label>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetShowForecast')">
            <label class="mac-switch">
              <input
                type="checkbox"
                :disabled="settingsLoading || saveBusy"
                v-model="budgetShowForecast"
                @change="persistAppSettings"
              />
              <span></span>
            </label>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetForecastMethod')">
            <div class="toggle-with-hint">
              <select
                v-model="budgetForecastMethod"
                :disabled="settingsLoading || saveBusy || !budgetShowForecast"
                class="mac-select budget-select"
                @change="persistAppSettings">
                <option value="cycle">{{ $t('components.general.label.budgetForecastMethodCycle') }}</option>
                <option value="10m">{{ $t('components.general.label.budgetForecastMethod10m') }}</option>
                <option value="1h">{{ $t('components.general.label.budgetForecastMethod1h') }}</option>
                <option value="yesterday">{{ $t('components.general.label.budgetForecastMethodYesterday') }}</option>
                <option value="last24h">{{ $t('components.general.label.budgetForecastMethod24h') }}</option>
              </select>
              <span class="hint-text">{{ $t('components.general.label.budgetForecastMethodHint') }}</span>
            </div>
          </ListItem>
        </div>
        <div class="mac-panel">
          <p class="panel-title">{{ $t('components.general.label.trayPanelCodex') }}</p>
          <ListItem :label="$t('components.general.label.budgetTotal')">
            <div class="toggle-with-hint">
              <div class="budget-input">
                <input
                  type="number"
                  inputmode="decimal"
                  min="0"
                  step="0.01"
                  :disabled="settingsLoading || saveBusy"
                  v-model.number="budgetTotalCodex"
                  @change="persistAppSettings"
                  class="mac-input budget-input-field"
                />
                <span class="budget-unit">USD</span>
              </div>
              <span class="hint-text">{{ $t('components.general.label.budgetTotalHint') }}</span>
            </div>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetUsedAdjustment')">
            <div class="toggle-with-hint">
              <div class="budget-input">
                <input
                  type="number"
                  inputmode="decimal"
                  step="0.01"
                  :disabled="settingsLoading || saveBusy"
                  v-model.number="budgetUsedAdjustmentCodex"
                  @change="persistAppSettings"
                  class="mac-input budget-input-field"
                />
                <span class="budget-unit">USD</span>
              </div>
              <span class="hint-text">{{ $t('components.general.label.budgetUsedAdjustmentHint') }}</span>
            </div>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetCycle')">
            <div class="toggle-with-hint">
              <label class="mac-switch">
                <input
                  type="checkbox"
                  :disabled="settingsLoading || saveBusy"
                  v-model="budgetCycleEnabledCodex"
                  @change="persistAppSettings"
                />
                <span></span>
              </label>
              <span class="hint-text">{{ $t('components.general.label.budgetCycleHint') }}</span>
            </div>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetCycleMode')">
            <select
              v-model="budgetCycleModeCodex"
              :disabled="settingsLoading || saveBusy || !budgetCycleEnabledCodex"
              class="mac-select budget-select"
              @change="persistAppSettings">
              <option value="daily">{{ $t('components.general.label.budgetCycleModeDaily') }}</option>
              <option value="weekly">{{ $t('components.general.label.budgetCycleModeWeekly') }}</option>
            </select>
          </ListItem>
          <ListItem
            v-if="budgetCycleModeCodex === 'weekly'"
            :label="$t('components.general.label.budgetRefreshDay')">
            <select
              v-model.number="budgetRefreshDayCodex"
              :disabled="settingsLoading || saveBusy || !budgetCycleEnabledCodex"
              class="mac-select budget-select"
              @change="persistAppSettings">
              <option :value="1">{{ $t('components.general.label.weekdayMon') }}</option>
              <option :value="2">{{ $t('components.general.label.weekdayTue') }}</option>
              <option :value="3">{{ $t('components.general.label.weekdayWed') }}</option>
              <option :value="4">{{ $t('components.general.label.weekdayThu') }}</option>
              <option :value="5">{{ $t('components.general.label.weekdayFri') }}</option>
              <option :value="6">{{ $t('components.general.label.weekdaySat') }}</option>
              <option :value="0">{{ $t('components.general.label.weekdaySun') }}</option>
            </select>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetRefreshTime')">
            <input
              type="time"
              :disabled="settingsLoading || saveBusy || !budgetCycleEnabledCodex"
              v-model="budgetRefreshTimeCodex"
              @change="persistAppSettings"
              class="mac-input budget-time-input"
            />
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetShowCountdown')">
            <label class="mac-switch">
              <input
                type="checkbox"
                :disabled="settingsLoading || saveBusy"
                v-model="budgetShowCountdownCodex"
                @change="persistAppSettings"
              />
              <span></span>
            </label>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetShowForecast')">
            <label class="mac-switch">
              <input
                type="checkbox"
                :disabled="settingsLoading || saveBusy"
                v-model="budgetShowForecastCodex"
                @change="persistAppSettings"
              />
              <span></span>
            </label>
          </ListItem>
          <ListItem :label="$t('components.general.label.budgetForecastMethod')">
            <div class="toggle-with-hint">
              <select
                v-model="budgetForecastMethodCodex"
                :disabled="settingsLoading || saveBusy || !budgetShowForecastCodex"
                class="mac-select budget-select"
                @change="persistAppSettings">
                <option value="cycle">{{ $t('components.general.label.budgetForecastMethodCycle') }}</option>
                <option value="10m">{{ $t('components.general.label.budgetForecastMethod10m') }}</option>
                <option value="1h">{{ $t('components.general.label.budgetForecastMethod1h') }}</option>
                <option value="yesterday">{{ $t('components.general.label.budgetForecastMethodYesterday') }}</option>
                <option value="last24h">{{ $t('components.general.label.budgetForecastMethod24h') }}</option>
              </select>
              <span class="hint-text">{{ $t('components.general.label.budgetForecastMethodHint') }}</span>
            </div>
          </ListItem>
        </div>
      </section>

      <section>
        <h2 class="mac-section-title">{{ $t('components.general.title.connectivity') }}</h2>
        <div class="mac-panel">
          <ListItem :label="$t('components.general.label.autoConnectivityTest')">
            <div class="toggle-with-hint">
              <label class="mac-switch">
                <input
                  type="checkbox"
                  :disabled="settingsLoading || saveBusy"
                  v-model="autoConnectivityTestEnabled"
                  @change="persistAppSettings"
                />
                <span></span>
              </label>
              <span class="hint-text">{{ $t('components.general.label.autoConnectivityTestHint') }}</span>
            </div>
          </ListItem>
        </div>
      </section>

      <section>
        <h2 class="mac-section-title">{{ $t('components.general.title.modelData') }}</h2>
        <div class="mac-panel">
          <ListItem :label="$t('components.general.label.autoSyncModels')">
            <div class="toggle-with-hint">
              <label class="mac-switch">
                <input
                  type="checkbox"
                  :disabled="settingsLoading || saveBusy || modelSyncBusy"
                  v-model="autoSyncModelsEnabled"
                  @change="persistAppSettings"
                />
                <span></span>
              </label>
              <span class="hint-text">{{ $t('components.general.label.autoSyncModelsHint') }}</span>
            </div>
          </ListItem>
          <ListItem :label="$t('components.general.label.modelSyncStatus')">
            <span class="info-text">{{ modelSyncStatusText }}</span>
          </ListItem>
          <ListItem :label="$t('components.general.label.modelSyncDefaults')">
            <span class="info-text">{{ modelSyncDefaultsText }}</span>
          </ListItem>
          <ListItem :label="$t('components.general.label.modelSyncActions')">
            <div class="toggle-with-hint">
              <button
                @click="syncModelDataNow"
                :disabled="settingsLoading || modelSyncBusy"
                class="primary-btn">
                {{ modelSyncBusy ? $t('components.general.label.modelSyncRunning') : $t('components.general.label.modelSyncNow') }}
              </button>
              <button
                @click="restoreBuiltinModelData"
                :disabled="settingsLoading || saveBusy || modelSyncBusy"
                class="action-btn">
                {{ $t('components.general.label.modelRestoreBuiltin') }}
              </button>
            </div>
          </ListItem>
        </div>
      </section>

      <!-- Network & WSL Settings -->
      <NetworkWslSettings />

      <section>
        <h2 class="mac-section-title">{{ $t('components.general.title.blacklist') }}</h2>
        <div class="mac-panel">
          <ListItem :label="$t('components.general.label.enableBlacklist')">
            <div class="toggle-with-hint">
              <label class="mac-switch">
                <input
                  type="checkbox"
                  :disabled="blacklistLoading || blacklistSaving"
                  v-model="blacklistEnabled"
                  @change="toggleBlacklist"
                />
                <span></span>
              </label>
              <span class="hint-text">{{ $t('components.general.label.enableBlacklistHint') }}</span>
            </div>
          </ListItem>
          <ListItem :label="$t('components.general.label.enableLevelBlacklist')">
            <div class="toggle-with-hint">
              <label class="mac-switch">
                <input
                  type="checkbox"
                  :disabled="blacklistLoading || blacklistSaving"
                  v-model="levelBlacklistEnabled"
                  @change="toggleLevelBlacklist"
                />
                <span></span>
              </label>
              <span class="hint-text">{{ $t('components.general.label.enableLevelBlacklistHint') }}</span>
            </div>
          </ListItem>
          <ListItem :label="$t('components.general.label.blacklistThreshold')">
            <select
              v-model.number="blacklistThreshold"
              :disabled="blacklistLoading || blacklistSaving"
              class="mac-select">
              <option :value="1">1 {{ $t('components.general.label.times') }}</option>
              <option :value="2">2 {{ $t('components.general.label.times') }}</option>
              <option :value="3">3 {{ $t('components.general.label.times') }}</option>
              <option :value="4">4 {{ $t('components.general.label.times') }}</option>
              <option :value="5">5 {{ $t('components.general.label.times') }}</option>
              <option :value="6">6 {{ $t('components.general.label.times') }}</option>
              <option :value="7">7 {{ $t('components.general.label.times') }}</option>
              <option :value="8">8 {{ $t('components.general.label.times') }}</option>
              <option :value="9">9 {{ $t('components.general.label.times') }}</option>
            </select>
          </ListItem>
          <ListItem :label="$t('components.general.label.blacklistDuration')">
            <select
              v-model.number="blacklistDuration"
              :disabled="blacklistLoading || blacklistSaving"
              class="mac-select">
              <option :value="5">5 {{ $t('components.general.label.minutes') }}</option>
              <option :value="15">15 {{ $t('components.general.label.minutes') }}</option>
              <option :value="30">30 {{ $t('components.general.label.minutes') }}</option>
              <option :value="60">60 {{ $t('components.general.label.minutes') }}</option>
            </select>
          </ListItem>
          <ListItem :label="$t('components.general.label.saveBlacklist')">
            <button
              @click="saveBlacklistSettings"
              :disabled="blacklistLoading || blacklistSaving"
              class="primary-btn">
              {{ blacklistSaving ? $t('components.general.label.saving') : $t('components.general.label.save') }}
            </button>
          </ListItem>
        </div>
      </section>

      <section>
        <h2 class="mac-section-title">{{ $t('components.general.title.dataImport') }}</h2>
        <div class="mac-panel">
          <ListItem :label="$t('components.general.import.configPath')">
            <input
              type="text"
              v-model="importPath"
              :placeholder="$t('components.general.import.pathPlaceholder')"
              class="mac-input import-path-input"
            />
          </ListItem>
          <ListItem :label="$t('components.general.import.status')">
            <span class="info-text" v-if="importLoading">
              {{ $t('components.general.import.loading') }}
            </span>
            <span class="info-text" v-else-if="importStatus?.config_exists">
              {{ $t('components.general.import.configFound') }}
              <span v-if="importStatus.pending_provider_count > 0 || importStatus.pending_mcp_count > 0">
                ({{ $t('components.general.import.pendingCount', {
                  providers: importStatus.pending_provider_count,
                  mcp: importStatus.pending_mcp_count
                }) }})
              </span>
            </span>
            <span class="info-text warning" v-else-if="importStatus">
              {{ $t('components.general.import.configNotFound') }}
            </span>
          </ListItem>
          <ListItem :label="$t('components.general.import.action')">
            <button
              @click="handleImport"
              :disabled="importing || !importPath.trim()"
              class="action-btn">
              {{ importing ? $t('components.general.import.importing') : $t('components.general.import.importBtn') }}
            </button>
          </ListItem>
        </div>
      </section>

      <section>
        <h2 class="mac-section-title">{{ $t('components.general.title.exterior') }}</h2>
        <div class="mac-panel">
          <ListItem :label="$t('components.general.label.language')">
            <LanguageSwitcher />
          </ListItem>
          <ListItem :label="$t('components.general.label.theme')">
            <ThemeSetting />
          </ListItem>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.mac-input {
  padding: 6px 12px;
  border: 1px solid var(--mac-border);
  border-radius: 6px;
  background: var(--mac-surface);
  color: var(--mac-text);
  font-size: 13px;
  font-family: monospace;
  min-width: 160px;
  transition: border-color 0.2s;
}

.mac-input:focus {
  outline: none;
  border-color: var(--mac-accent);
}

.panel-title {
  margin: 0;
  padding: 12px 18px 6px;
  font-size: 12px;
  font-weight: 600;
  color: var(--mac-text-secondary);
  letter-spacing: 0.02em;
  border-bottom: 1px solid var(--mac-divider);
}

.mac-panel + .mac-panel {
  margin-top: 12px;
}

.toggle-with-hint {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 4px;
}

.hint-text {
  font-size: 11px;
  color: var(--mac-text-secondary);
  line-height: 1.4;
  max-width: 320px;
  text-align: right;
  white-space: nowrap;
}

:global(.dark) .hint-text {
  color: rgba(255, 255, 255, 0.5);
}

.budget-input {
  display: flex;
  align-items: center;
  gap: 8px;
}

.budget-input-field {
  width: 140px;
}

.budget-time-input {
  width: 140px;
}

.budget-select {
  width: 160px;
}

.budget-unit {
  font-size: 12px;
  color: var(--mac-text-secondary);
}

.import-path-input {
  width: 280px;
  font-size: 12px;
}

.info-text.warning {
  color: var(--mac-text-warning, #e67e22);
}

:global(.dark) .info-text.warning {
  color: #f39c12;
}

:global(.dark) .mac-input {
  background: var(--mac-surface-strong);
}
</style>
