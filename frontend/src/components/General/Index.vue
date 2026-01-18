<template>
  <PageLayout :title="$t('sidebar.settings')">
    <section>
      <h2 class="mac-section-title">{{ $t('components.general.title.application') }}</h2>
      <div class="mac-panel">
        <ListItem :label="$t('components.general.label.autoStart')">
          <label class="mac-switch">
            <input type="checkbox" :disabled="settingsLoading || saveBusy" v-model="autoStartEnabled"
              @change="persistAppSettings" />
            <span></span>
          </label>
        </ListItem>
        <ListItem :label="$t('components.general.label.switchNotify')">
          <div class="toggle-with-hint">
            <label class="mac-switch">
              <input type="checkbox" :disabled="settingsLoading || saveBusy" v-model="switchNotifyEnabled"
                @change="persistAppSettings" />
              <span></span>
            </label>
            <span class="hint-text">{{ $t('components.general.label.switchNotifyHint') }}</span>
          </div>
        </ListItem>
        <ListItem :label="$t('components.general.label.roundRobin')">
          <div class="toggle-with-hint">
            <label class="mac-switch">
              <input type="checkbox" :disabled="settingsLoading || saveBusy" v-model="roundRobinEnabled"
                @change="persistAppSettings" />
              <span></span>
            </label>
            <span class="hint-text">{{ $t('components.general.label.roundRobinHint') }}</span>
          </div>
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

    <section>
      <h2 class="mac-section-title">{{ $t('components.general.title.proxy') }}</h2>
      <div class="mac-panel">
        <ListItem :label="$t('components.general.label.proxyAddress')"
          :subLabel="$t('components.general.label.proxyAddressHint')">
          <input type="text" v-model="proxyAddress"
            :placeholder="$t('components.general.label.proxyAddressPlaceholder')"
            :disabled="settingsLoading || saveBusy" class="mac-input import-path-input" @change="persistAppSettings" />
        </ListItem>
        <ListItem :label="$t('components.general.label.proxyType')"
          :subLabel="$t('components.general.label.proxyTypeHint')">
          <select v-model="proxyType" :disabled="settingsLoading || saveBusy" class="mac-select"
            @change="persistAppSettings">
            <option value="http">{{ $t('components.general.label.proxyTypeHttp') }}</option>
            <option value="socks5">{{ $t('components.general.label.proxyTypeSocks5') }}</option>
          </select>
        </ListItem>
        <ListItem :label="$t('components.general.label.proxyClaude')">
          <div class="toggle-with-hint">
            <label class="mac-switch">
              <input type="checkbox" :disabled="settingsLoading || saveBusy" v-model="proxyClaude"
                @change="persistAppSettings" />
              <span></span>
            </label>
            <span class="hint-text">{{ $t('components.general.label.proxyChannelHint') }}</span>
          </div>
        </ListItem>
        <ListItem :label="$t('components.general.label.proxyCodex')">
          <div class="toggle-with-hint">
            <label class="mac-switch">
              <input type="checkbox" :disabled="settingsLoading || saveBusy" v-model="proxyCodex"
                @change="persistAppSettings" />
              <span></span>
            </label>
            <span class="hint-text">{{ $t('components.general.label.proxyChannelHint') }}</span>
          </div>
        </ListItem>
        <ListItem :label="$t('components.general.label.proxyGemini')">
          <div class="toggle-with-hint">
            <label class="mac-switch">
              <input type="checkbox" :disabled="settingsLoading || saveBusy" v-model="proxyGemini"
                @change="persistAppSettings" />
              <span></span>
            </label>
            <span class="hint-text">{{ $t('components.general.label.proxyChannelHint') }}</span>
          </div>
        </ListItem>
        <ListItem :label="$t('components.general.label.proxyCustom')">
          <div class="toggle-with-hint">
            <label class="mac-switch">
              <input type="checkbox" :disabled="settingsLoading || saveBusy" v-model="proxyCustom"
                @change="persistAppSettings" />
              <span></span>
            </label>
            <span class="hint-text">{{ $t('components.general.label.proxyChannelHint') }}</span>
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
              <input type="checkbox" :disabled="settingsLoading || saveBusy" v-model="autoConnectivityTestEnabled"
                @change="persistAppSettings" />
              <span></span>
            </label>
            <span class="hint-text">{{ $t('components.general.label.autoConnectivityTestHint') }}</span>
          </div>
        </ListItem>
      </div>
    </section>

    <section>
      <h2 class="mac-section-title">{{ $t('components.general.title.blacklist') }}</h2>
      <div class="mac-panel">
        <ListItem :label="$t('components.general.label.enableBlacklist')">
          <div class="toggle-with-hint">
            <label class="mac-switch">
              <input type="checkbox" :disabled="blacklistLoading || blacklistSaving" v-model="blacklistEnabled"
                @change="toggleBlacklist" />
              <span></span>
            </label>
            <span class="hint-text">{{ $t('components.general.label.enableBlacklistHint') }}</span>
          </div>
        </ListItem>
        <ListItem :label="$t('components.general.label.enableLevelBlacklist')">
          <div class="toggle-with-hint">
            <label class="mac-switch">
              <input type="checkbox" :disabled="blacklistLoading || blacklistSaving" v-model="levelBlacklistEnabled"
                @change="toggleLevelBlacklist" />
              <span></span>
            </label>
            <span class="hint-text">{{ $t('components.general.label.enableLevelBlacklistHint') }}</span>
          </div>
        </ListItem>
        <ListItem :label="$t('components.general.label.blacklistThreshold')">
          <select v-model.number="blacklistThreshold" :disabled="blacklistLoading || blacklistSaving"
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
          <select v-model.number="blacklistDuration" :disabled="blacklistLoading || blacklistSaving" class="mac-select">
            <option :value="5">5 {{ $t('components.general.label.minutes') }}</option>
            <option :value="15">15 {{ $t('components.general.label.minutes') }}</option>
            <option :value="30">30 {{ $t('components.general.label.minutes') }}</option>
            <option :value="60">60 {{ $t('components.general.label.minutes') }}</option>
          </select>
        </ListItem>
        <ListItem :label="$t('components.general.label.saveBlacklist')">
          <BaseButton size="sm" type="button" :disabled="blacklistLoading || blacklistSaving"
            @click="saveBlacklistSettings">
            {{ blacklistSaving ? $t('components.general.label.saving') : $t('components.general.label.save') }}
          </BaseButton>
        </ListItem>
      </div>
    </section>

    <section>
      <h2 class="mac-section-title">{{ $t('components.general.title.configBackup') }}</h2>
      <div class="mac-panel">
        <ListItem :label="$t('components.general.backup.exportPath')">
          <input type="text" v-model="backupExportPath"
            :placeholder="$t('components.general.backup.exportPathPlaceholder')" class="mac-input import-path-input" />
        </ListItem>
        <ListItem :label="$t('components.general.backup.includeSecrets')">
          <div class="toggle-with-hint">
            <label class="mac-switch">
              <input type="checkbox" v-model="backupIncludeSecrets" :disabled="exportingBackup || importingBackup" />
              <span></span>
            </label>
            <span class="hint-text">{{ $t('components.general.backup.includeSecretsHint') }}</span>
          </div>
        </ListItem>
        <ListItem :label="$t('components.general.backup.includeDatabase')">
          <div class="toggle-with-hint">
            <label class="mac-switch">
              <input type="checkbox" v-model="backupIncludeDatabase" :disabled="exportingBackup || importingBackup" />
              <span></span>
            </label>
            <span class="hint-text">{{ $t('components.general.backup.includeDatabaseHint') }}</span>
          </div>
        </ListItem>
        <ListItem :label="$t('components.general.backup.exportAction')">
          <BaseButton variant="outline" size="sm" type="button" :disabled="exportingBackup || !backupExportPath.trim()"
            @click="handleExportBackup">
            {{ exportingBackup ? $t('components.general.backup.exporting') : $t('components.general.backup.exportBtn')
            }}
          </BaseButton>
        </ListItem>
      </div>

      <div class="mac-panel">
        <ListItem :label="$t('components.general.backup.importPath')">
          <input type="text" v-model="backupImportPath"
            :placeholder="$t('components.general.backup.importPathPlaceholder')" class="mac-input import-path-input" />
        </ListItem>
        <ListItem :label="$t('components.general.backup.preserveSecrets')">
          <div class="toggle-with-hint">
            <label class="mac-switch">
              <input type="checkbox" v-model="backupPreserveSecrets" :disabled="exportingBackup || importingBackup" />
              <span></span>
            </label>
            <span class="hint-text">{{ $t('components.general.backup.preserveSecretsHint') }}</span>
          </div>
        </ListItem>
        <ListItem :label="$t('components.general.backup.importDatabase')">
          <div class="toggle-with-hint">
            <label class="mac-switch">
              <input type="checkbox" v-model="backupImportDatabase" :disabled="exportingBackup || importingBackup" />
              <span></span>
            </label>
            <span class="hint-text">{{ $t('components.general.backup.importDatabaseHint') }}</span>
          </div>
        </ListItem>
        <ListItem :label="$t('components.general.backup.importAction')">
          <BaseButton variant="outline" size="sm" type="button" :disabled="importingBackup || !backupImportPath.trim()"
            @click="handleImportBackup">
            {{ importingBackup ? $t('components.general.backup.importing') : $t('components.general.backup.importBtn')
            }}
          </BaseButton>
        </ListItem>
      </div>
    </section>

    <section>
      <h2 class="mac-section-title">{{ $t('components.general.title.dataImport') }}</h2>
      <div class="mac-panel">
        <ListItem :label="$t('components.general.import.configPath')">
          <input type="text" v-model="importPath" :placeholder="$t('components.general.import.pathPlaceholder')"
            class="mac-input import-path-input" />
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
          <BaseButton variant="outline" size="sm" type="button" :disabled="importing || !importPath.trim()"
            @click="handleImport">
            {{ importing ? $t('components.general.import.importing') : $t('components.general.import.importBtn') }}
          </BaseButton>
        </ListItem>
      </div>
    </section>

    <section>
      <h2 class="mac-section-title">{{ $t('components.general.title.update') }}</h2>
      <div class="mac-panel">
        <ListItem :label="$t('components.general.label.autoUpdate')">
          <label class="mac-switch">
            <input type="checkbox" :disabled="settingsLoading || saveBusy" v-model="autoUpdateEnabled"
              @change="persistAppSettings" />
            <span></span>
          </label>
        </ListItem>

        <ListItem :label="$t('components.general.label.lastCheck')">
          <span class="info-text">{{ formatLastCheckTime(updateState?.last_check_time) }}</span>
          <Badge
            v-if="updateState && updateState.consecutive_failures > 0"
            variant="warning"
            size="sm"
          >
            {{ $t('components.general.update.checkFailed', { count: updateState.consecutive_failures }) }}
          </Badge>
        </ListItem>

        <ListItem :label="$t('components.general.label.currentVersion')">
          <span class="version-text">{{ appVersion }}</span>
        </ListItem>

        <ListItem v-if="updateState?.latest_known_version && updateState.latest_known_version !== appVersion"
          :label="$t('components.general.label.latestVersion')">
          <span class="version-text highlight">
            {{ updateState.latest_known_version }}
          </span>
          <Badge variant="info" size="sm">NEW</Badge>
        </ListItem>

        <ListItem :label="$t('components.general.label.checkNow')">
          <BaseButton variant="outline" size="sm" type="button" :disabled="checking" @click="checkUpdateManually">
            {{ checking ? $t('components.general.update.checking') : $t('components.general.update.checkNow') }}
          </BaseButton>
        </ListItem>

        <ListItem v-if="updateState?.update_ready" :label="$t('components.general.label.manualUpdate')">
          <BaseButton size="sm" type="button" @click="installAndRestart">
            {{ $t('components.general.update.installAndRestart') }}
          </BaseButton>
        </ListItem>
      </div>
    </section>
  </PageLayout>
</template>

<script setup lang="ts">
  import { ref, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { Call } from '@wailsio/runtime'
import PageLayout from '../common/PageLayout.vue'
import BaseButton from '../common/BaseButton.vue'
import ListItem from '../Setting/ListRow.vue'
import LanguageSwitcher from '../Setting/LanguageSwitcher.vue'
import ThemeSetting from '../Setting/ThemeSetting.vue'
import Badge from '../ui/Badge.vue'
import { fetchAppSettings, saveAppSettings, type AppSettings } from '../../services/appSettings'
import { checkUpdate, downloadUpdate, restartApp, getUpdateState, setAutoCheckEnabled, type UpdateState } from '../../services/update'
import { fetchCurrentVersion } from '../../services/version'
import { getBlacklistSettings, updateBlacklistSettings, getLevelBlacklistEnabled, setLevelBlacklistEnabled, getBlacklistEnabled, setBlacklistEnabled, type BlacklistSettings } from '../../services/settings'
import { fetchConfigImportStatus, importFromPath, type ConfigImportStatus } from '../../services/configImport'
import { getDefaultExportPath, exportConfig as exportAppConfig, importConfig as importAppConfig } from '../../services/configBackup'
import { useI18n } from 'vue-i18n'
import { extractErrorMessage } from '../../utils/error'
import { showToast } from '../../utils/toast'

const { t } = useI18n()

const router = useRouter()
// 从 localStorage 读取缓存值作为初始值，避免加载时的视觉闪烁
const getCachedValue = (key: string, defaultValue: boolean): boolean => {
  const cached = localStorage.getItem(`app-settings-${key}`)
  return cached !== null ? cached === 'true' : defaultValue
}
const autoStartEnabled = ref(getCachedValue('autoStart', false))
const autoUpdateEnabled = ref(getCachedValue('autoUpdate', true))
const autoConnectivityTestEnabled = ref(getCachedValue('autoConnectivityTest', false))
const switchNotifyEnabled = ref(getCachedValue('switchNotify', true)) // 切换通知开关
const roundRobinEnabled = ref(getCachedValue('roundRobin', false))    // 同 Level 轮询开关

// 出站代理配置
const proxyAddress = ref('')
const proxyType = ref('http')
const proxyClaude = ref(getCachedValue('proxyClaude', false))
const proxyCodex = ref(getCachedValue('proxyCodex', false))
const proxyGemini = ref(getCachedValue('proxyGemini', false))
const proxyCustom = ref(getCachedValue('proxyCustom', false))

const settingsLoading = ref(true)
const saveBusy = ref(false)

// 更新相关状态
const updateState = ref<UpdateState | null>(null)
const checking = ref(false)
const downloading = ref(false)
const appVersion = ref('')

// 拉黑配置相关状态
const blacklistEnabled = ref(true)  // 拉黑功能总开关
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

// 本应用配置导出/导入
const backupExportPath = ref('')
const backupIncludeSecrets = ref(false)
const backupIncludeDatabase = ref(false)
const exportingBackup = ref(false)

const backupImportPath = ref('')
const backupImportDatabase = ref(false)
const backupPreserveSecrets = ref(true)
const importingBackup = ref(false)

const goBack = () => {
  router.push('/')
}

const loadAppSettings = async () => {
  settingsLoading.value = true
  try {
    const data = await fetchAppSettings()
    autoStartEnabled.value = data?.auto_start ?? false
    autoUpdateEnabled.value = data?.auto_update ?? true
    autoConnectivityTestEnabled.value = data?.auto_connectivity_test ?? false
    switchNotifyEnabled.value = data?.enable_switch_notify ?? true
    roundRobinEnabled.value = data?.enable_round_robin ?? false

    proxyAddress.value = data?.proxy_address ?? ''
    proxyType.value = (data?.proxy_type ?? 'http') || 'http'
    proxyClaude.value = Boolean(data?.proxy_claude)
    proxyCodex.value = Boolean(data?.proxy_codex)
    proxyGemini.value = Boolean(data?.proxy_gemini)
    proxyCustom.value = Boolean(data?.proxy_custom)

    // 缓存到 localStorage，下次打开时直接显示正确状态
    localStorage.setItem('app-settings-autoStart', String(autoStartEnabled.value))
    localStorage.setItem('app-settings-autoUpdate', String(autoUpdateEnabled.value))
    localStorage.setItem('app-settings-autoConnectivityTest', String(autoConnectivityTestEnabled.value))
    localStorage.setItem('app-settings-switchNotify', String(switchNotifyEnabled.value))
    localStorage.setItem('app-settings-roundRobin', String(roundRobinEnabled.value))

    localStorage.setItem('app-settings-proxyClaude', String(proxyClaude.value))
    localStorage.setItem('app-settings-proxyCodex', String(proxyCodex.value))
    localStorage.setItem('app-settings-proxyGemini', String(proxyGemini.value))
    localStorage.setItem('app-settings-proxyCustom', String(proxyCustom.value))
  } catch (error) {
    console.error('failed to load app settings', error)
    autoStartEnabled.value = false
    autoUpdateEnabled.value = true
    autoConnectivityTestEnabled.value = false
    switchNotifyEnabled.value = true
    roundRobinEnabled.value = false

    proxyAddress.value = ''
    proxyType.value = 'http'
    proxyClaude.value = false
    proxyCodex.value = false
    proxyGemini.value = false
    proxyCustom.value = false
  } finally {
    settingsLoading.value = false
  }
}

const persistAppSettings = async () => {
  if (settingsLoading.value || saveBusy.value) return
  saveBusy.value = true
  try {
    const payload: AppSettings = {
      auto_start: autoStartEnabled.value,
      auto_update: autoUpdateEnabled.value,
      auto_connectivity_test: autoConnectivityTestEnabled.value,
      enable_switch_notify: switchNotifyEnabled.value,
      enable_round_robin: roundRobinEnabled.value,

      proxy_address: proxyAddress.value.trim(),
      proxy_type: proxyType.value,
      proxy_claude: proxyClaude.value,
      proxy_codex: proxyCodex.value,
      proxy_gemini: proxyGemini.value,
      proxy_custom: proxyCustom.value,
    }
    await saveAppSettings(payload)

    // 同步自动更新设置到 UpdateService
    await setAutoCheckEnabled(autoUpdateEnabled.value)

    // 同步自动可用性监控设置到 HealthCheckService（复用旧字段名）
    await Call.ByName(
      'codeswitch/services.HealthCheckService.SetAutoAvailabilityPolling',
      autoConnectivityTestEnabled.value
    )

    // 更新缓存
    localStorage.setItem('app-settings-autoStart', String(autoStartEnabled.value))
    localStorage.setItem('app-settings-autoUpdate', String(autoUpdateEnabled.value))
    localStorage.setItem('app-settings-autoConnectivityTest', String(autoConnectivityTestEnabled.value))
    localStorage.setItem('app-settings-switchNotify', String(switchNotifyEnabled.value))
    localStorage.setItem('app-settings-roundRobin', String(roundRobinEnabled.value))

    localStorage.setItem('app-settings-proxyClaude', String(proxyClaude.value))
    localStorage.setItem('app-settings-proxyCodex', String(proxyCodex.value))
    localStorage.setItem('app-settings-proxyGemini', String(proxyGemini.value))
    localStorage.setItem('app-settings-proxyCustom', String(proxyCustom.value))

    window.dispatchEvent(new CustomEvent('app-settings-updated'))
  } catch (error) {
    console.error('failed to save app settings', error)
  } finally {
    saveBusy.value = false
  }
}

const loadUpdateState = async () => {
  try {
    updateState.value = await getUpdateState()
  } catch (error) {
    console.error('failed to load update state', error)
  }
}

const checkUpdateManually = async () => {
  checking.value = true
  try {
    const info = await checkUpdate()
    await loadUpdateState()

    if (!info.available) {
      showToast('已是最新版本', 'success')
    } else {
      // 发现新版本，提示用户并开始下载
      const confirmed = confirm(`发现新版本 ${info.version}，是否立即下载？`)
      if (confirmed) {
        downloading.value = true
        checking.value = false
        try {
          await downloadUpdate()
          await loadUpdateState()

          // 下载完成，提示重启
          const restart = confirm('新版本已下载完成，是否立即重启应用？')
          if (restart) {
            await restartApp()
          }
        } catch (downloadError) {
          console.error('download failed', downloadError)
          showToast('下载失败，请稍后重试', 'error')
        } finally {
          downloading.value = false
        }
      }
    }
  } catch (error) {
    console.error('check update failed', error)
    showToast('检查更新失败，请检查网络连接', 'error')
  } finally {
    checking.value = false
  }
}

const downloadAndInstall = async () => {
  downloading.value = true
  try {
    await downloadUpdate()
    await loadUpdateState()

    // 弹窗确认重启
    const confirmed = confirm('新版本已下载完成，是否立即重启应用？')
    if (confirmed) {
      await restartApp()
    }
  } catch (error) {
    console.error('download failed', error)
    showToast('下载失败，请稍后重试', 'error')
  } finally {
    downloading.value = false
  }
}

// 当更新已下载完成时，直接安装并重启（无需再次下载）
const installAndRestart = async () => {
  const confirmed = confirm('是否立即安装更新并重启应用？')
  if (confirmed) {
    try {
      await restartApp()
    } catch (error) {
      console.error('restart failed', error)
      showToast('重启失败，请手动重启应用', 'error')
    }
  }
}

const formatLastCheckTime = (timeStr?: string) => {
  if (!timeStr) return '从未检查'

  const checkTime = new Date(timeStr)
  const now = new Date()
  const diffMs = now.getTime() - checkTime.getTime()
  const diffHours = Math.floor(diffMs / (1000 * 60 * 60))

  if (diffHours < 1) {
    return '刚刚'
  } else if (diffHours < 24) {
    return `${diffHours} 小时前`
  } else {
    const diffDays = Math.floor(diffHours / 24)
    return `${diffDays} 天前`
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
    blacklistEnabled.value = true
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
    showToast('拉黑配置已保存', 'success')
  } catch (error) {
    console.error('failed to save blacklist settings', error)
    showToast('保存失败：' + (error as Error).message, 'error')
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
    showToast('切换失败：' + (error as Error).message, 'error')
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
    showToast('切换失败：' + (error as Error).message, 'error')
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
      showToast(t('components.general.import.fileNotFound'), 'error')
      return
    }
    const imported = result.imported_providers + result.imported_mcp
    if (imported > 0) {
      showToast(t('components.general.import.success', {
        providers: result.imported_providers,
        mcp: result.imported_mcp
      }), 'success')
    } else {
      showToast(t('components.general.import.nothingToImport'), 'warning')
    }
  } catch (error) {
    console.error('import failed', error)
    showToast(t('components.general.import.failed') + ': ' + (error as Error).message, 'error')
  } finally {
    importing.value = false
  }
}

const loadBackupDefaults = async () => {
  try {
    backupExportPath.value = await getDefaultExportPath()
  } catch (error) {
    console.error('failed to load default export path', error)
  }
}

const handleExportBackup = async () => {
  if (exportingBackup.value || !backupExportPath.value.trim()) return
  exportingBackup.value = true
  try {
    const result = await exportAppConfig(backupExportPath.value.trim(), {
      include_secrets: backupIncludeSecrets.value,
      include_database: backupIncludeDatabase.value
    })
    showToast(t('components.general.backup.exportSuccess', { count: result.file_count, path: result.path }), 'success')
  } catch (error) {
    console.error('export config failed', error)
    showToast(t('components.general.backup.exportFailed') + ': ' + extractErrorMessage(error), 'error')
  } finally {
    exportingBackup.value = false
  }
}

const handleImportBackup = async () => {
  if (importingBackup.value || !backupImportPath.value.trim()) return
  const confirmed = confirm(t('components.general.backup.importConfirm'))
  if (!confirmed) return

  importingBackup.value = true
  try {
    const result = await importAppConfig(backupImportPath.value.trim(), {
      import_database: backupImportDatabase.value,
      preserve_existing_secrets: backupPreserveSecrets.value
    })

    showToast(t('components.general.backup.importSuccess', {
      imported: result.imported_files,
      skipped: result.skipped_files,
      backups: result.backups_created
    }), 'success')

    if (result.warnings && result.warnings.length > 0) {
      const sampleWarnings = result.warnings.slice(0, 3).join('\n') + (result.warnings.length > 3 ? '\n…' : '')
      showToast(t('components.general.backup.importWarnings', { warnings: sampleWarnings }), 'warning')
    }

    // 刷新当前页面展示的配置
    await loadAppSettings()
    await loadBlacklistSettings()
    await loadImportStatus()
  } catch (error) {
    console.error('import config failed', error)
    showToast(t('components.general.backup.importFailed') + ': ' + extractErrorMessage(error), 'error')
  } finally {
    importingBackup.value = false
  }
}

let updateStateTimer: number | undefined

onMounted(async () => {
  await loadAppSettings()

  // 加载当前版本号
  try {
    appVersion.value = await fetchCurrentVersion()
  } catch (error) {
    console.error('failed to load app version', error)
  }

  // 加载更新状态
  await loadUpdateState()
  updateStateTimer = window.setInterval(() => {
    void loadUpdateState()
  }, 30_000)

  // 加载拉黑配置
  await loadBlacklistSettings()

  // 加载导入状态
  await loadImportStatus()

  // 初始化导出默认路径
  await loadBackupDefaults()
})

onUnmounted(() => {
  if (updateStateTimer) {
    window.clearInterval(updateStateTimer)
    updateStateTimer = undefined
  }
})
</script>

<style scoped>
.toggle-with-hint {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 4px;
}

.mac-panel+.mac-panel {
  margin-top: 12px;
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
</style>
