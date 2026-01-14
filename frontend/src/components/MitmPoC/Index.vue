<template>
  <PageLayout :title="t('dashboard.mitm.controls')" :sticky="true">
    <template #actions>
      <button
        type="button"
        class="ghost-icon"
        :class="{ rotating: refreshing }"
        :data-tooltip="t('components.mitm.refresh', '刷新')"
        :aria-label="t('components.mitm.refresh', '刷新')"
        :disabled="refreshing"
        @click="refreshSystemStatus"
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

    <p class="page-lead">
      {{ t('components.mitm.lead', '启动/停止 MITM、安装根证书，并根据已启用的规则写入 Hosts 以便拦截指定域名。') }}
    </p>

    <section class="dashboard-section">
      <div class="system-status-grid">
        <Card class="status-card" variant="outline">
          <div class="status-card__header">
            <div class="status-indicator" :class="{ active: systemStatus.mitm }"></div>
            <h3 class="status-card__title">{{ t('dashboard.system.mitm', 'MITM Proxy') }}</h3>
          </div>
          <div class="status-card__content">
            <Badge :variant="systemStatus.mitm ? 'success' : 'default'">
              {{ systemStatus.mitm ? t('dashboard.status.running', 'Running') : t('dashboard.status.stopped', 'Stopped') }}
            </Badge>
            <p class="status-card__desc">{{ t('dashboard.system.mitmDesc', { port: mitmPortText }, `Port ${mitmPortText}`) }}</p>
          </div>
        </Card>

        <Card class="status-card" variant="outline">
          <div class="status-card__header">
            <div class="status-indicator" :class="{ active: systemStatus.rootCA }"></div>
            <h3 class="status-card__title">{{ t('dashboard.system.rootCA', 'Root CA') }}</h3>
          </div>
          <div class="status-card__content">
            <Badge :variant="systemStatus.rootCA ? 'success' : 'warning'">
              {{ systemStatus.rootCA ? t('dashboard.status.installed', 'Installed') : t('dashboard.status.notInstalled', 'Not Installed') }}
            </Badge>
            <p class="status-card__desc">{{ t('dashboard.system.rootCADesc', 'HTTPS Interception') }}</p>
          </div>
        </Card>

        <Card class="status-card" variant="outline">
          <div class="status-card__header">
            <div class="status-indicator" :class="{ active: systemStatus.hosts }"></div>
            <h3 class="status-card__title">{{ t('dashboard.system.hosts', 'Hosts File') }}</h3>
          </div>
          <div class="status-card__content">
            <Badge :variant="systemStatus.hosts ? 'success' : 'default'">
              {{ systemStatus.hosts ? t('dashboard.status.configured', 'Configured') : t('dashboard.status.notConfigured', 'Not Configured') }}
            </Badge>
            <p class="status-card__desc">{{ t('dashboard.system.hostsDesc', 'DNS Overrides') }}</p>
          </div>
        </Card>
      </div>

      <div class="mitm-controls">
        <div class="controls-grid">
          <Button
            :variant="systemStatus.mitm ? 'destructive' : 'default'"
            :disabled="mitmLoading"
            @click="toggleMITM"
          >
            {{ systemStatus.mitm ? t('dashboard.mitm.stop') : t('dashboard.mitm.start') }}
          </Button>

          <Button
            :variant="systemStatus.rootCA ? 'destructive' : 'default'"
            :disabled="caLoading"
            @click="toggleRootCA"
          >
            {{ systemStatus.rootCA ? t('dashboard.mitm.uninstallCA') : t('dashboard.mitm.installCA') }}
          </Button>

          <Button
            :variant="systemStatus.hosts ? 'destructive' : 'default'"
            :disabled="hostsLoading"
            @click="toggleHosts"
          >
            {{ systemStatus.hosts ? t('dashboard.mitm.cleanupHosts') : t('dashboard.mitm.applyHosts') }}
          </Button>

          <Button variant="outline" @click="showCACertPath">
            {{ t('dashboard.mitm.exportCA') }}
          </Button>

          <Button variant="outline" @click="openMitmLogs">
            {{ t('components.mitm.openLogs', '打开转发日志') }}
          </Button>
        </div>

        <div v-if="mitmTarget" class="mitm-meta">
          <span class="meta-label">Target</span>
          <span class="meta-value">{{ mitmTarget }}</span>
        </div>

        <div v-if="caCertPath" class="mitm-meta">
          <span class="meta-label">CA</span>
          <code class="meta-value">{{ caCertPath }}</code>
        </div>
      </div>
    </section>
  </PageLayout>
</template>

<script setup lang="ts">
import { computed, reactive, ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import PageLayout from '../common/PageLayout.vue'
import Card from '../ui/Card.vue'
import Badge from '../ui/Badge.vue'
import Button from '../ui/Button.vue'

// @ts-ignore
import {
  Start as StartMITM,
  Stop as StopMITM,
  GetMITMStatus,
  GetCACertPath
} from '../../../bindings/codeswitch/services/mitmservice'
// @ts-ignore
import {
  Install as InstallCertificate,
  Uninstall as UninstallCertificate,
  CheckInstalled as CheckCertificateInstalled
} from '../../../bindings/codeswitch/services/systemtrustservice'
// @ts-ignore
import {
  Apply as ApplyHostsEntries,
  Cleanup as CleanupHostsEntries,
  GetManagedDomains
} from '../../../bindings/codeswitch/services/hostsservice'
// @ts-ignore
import { ListEnabled as ListEnabledRules } from '../../../bindings/codeswitch/services/ruleservice'

const router = useRouter()
const { t } = useI18n()

const systemStatus = reactive({
  mitm: false,
  rootCA: false,
  hosts: false,
})

const mitmPort = ref<number | null>(null)
const mitmTarget = ref('')
const caCertPath = ref('')

const refreshing = ref(false)
const mitmLoading = ref(false)
const caLoading = ref(false)
const hostsLoading = ref(false)

const mitmPortText = computed(() => (mitmPort.value ? `:${mitmPort.value}` : ':443'))

const refreshSystemStatus = async () => {
  if (refreshing.value) return
  refreshing.value = true
  try {
    const mitmStatus: any = await GetMITMStatus()
    systemStatus.mitm = mitmStatus?.running || false
    mitmPort.value = typeof mitmStatus?.port === 'number' ? mitmStatus.port : null
    mitmTarget.value = String(mitmStatus?.target || '')

    const caInstalled = await CheckCertificateInstalled('Code-Switch MITM CA')
    systemStatus.rootCA = caInstalled

    const managedDomains = await GetManagedDomains()
    systemStatus.hosts = managedDomains && managedDomains.length > 0
  } catch (error) {
    console.error('Failed to refresh MITM status:', error)
  } finally {
    refreshing.value = false
  }
}

const toggleMITM = async () => {
  if (mitmLoading.value) return
  mitmLoading.value = true
  try {
    if (systemStatus.mitm) {
      await StopMITM()
    } else {
      await StartMITM()
    }
    await refreshSystemStatus()
  } catch (error) {
    console.error('Failed to toggle MITM:', error)
    const message = String((error as any)?.message || error || '')
    if (message.includes('administrator privileges required')) {
      alert(t('components.mitm.errors.adminPort', 'Listening on port 443 requires administrator privileges. Restart the app with elevated rights and try again.'))
      return
    }
    alert(t('dashboard.mitm.error.toggle', 'Failed to toggle MITM proxy. Check console for details.'))
  } finally {
    mitmLoading.value = false
  }
}

const toggleRootCA = async () => {
  if (caLoading.value) return
  caLoading.value = true
  try {
    if (systemStatus.rootCA) {
      await UninstallCertificate('Code-Switch MITM CA')
    } else {
      const certPath = await GetCACertPath()
      if (!certPath) {
        throw new Error('CA certificate path not available')
      }
      await InstallCertificate(certPath)
    }
    await refreshSystemStatus()
  } catch (error) {
    console.error('Failed to toggle Root CA:', error)
    alert(t('dashboard.mitm.error.ca', 'Failed to install/uninstall Root CA. Administrator privileges may be required.'))
  } finally {
    caLoading.value = false
  }
}

const toggleHosts = async () => {
  if (hostsLoading.value) return
  hostsLoading.value = true
  try {
    if (systemStatus.hosts) {
      await CleanupHostsEntries()
    } else {
      const rules = await ListEnabledRules()
      if (!rules || rules.length === 0) {
        alert(t('dashboard.mitm.error.noRules', 'No enabled rules found. Please create and enable rules first.'))
        return
      }

      const domains = rules.map((rule: any) => rule.sourceHost).filter(Boolean)
      if (domains.length === 0) {
        alert(t('dashboard.mitm.error.noDomains', 'No valid domains found in enabled rules.'))
        return
      }

      await ApplyHostsEntries(domains, true, true)
    }
    await refreshSystemStatus()
  } catch (error) {
    console.error('Failed to toggle Hosts:', error)
    alert(t('dashboard.mitm.error.hosts', 'Failed to apply/cleanup hosts entries. Administrator privileges may be required.'))
  } finally {
    hostsLoading.value = false
  }
}

const showCACertPath = async () => {
  try {
    const certPath = await GetCACertPath()
    if (!certPath) {
      alert(t('dashboard.mitm.error.noCert', 'CA certificate not found. Please start MITM proxy first.'))
      return
    }
    caCertPath.value = certPath
    alert(t('dashboard.mitm.info.certPath', { path: certPath }, `Certificate path: ${certPath}`))
  } catch (error) {
    console.error('Failed to get CA certificate path:', error)
    alert(t('dashboard.mitm.error.openCert', 'Failed to open CA certificate.'))
  }
}

const openMitmLogs = () => {
  router.push('/logs/terminal')
}

onMounted(() => {
  refreshSystemStatus()
})
</script>

<style scoped>
.system-status-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 1rem;
  margin-bottom: 1.5rem;
}

.status-card {
  padding: 1.25rem;
}

.status-card__header {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 1rem;
}

.status-indicator {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: var(--color-border);
}

.status-indicator.active {
  background: var(--color-primary);
}

.status-card__title {
  font-size: 1rem;
  font-weight: 600;
  color: var(--color-text);
}

.status-card__content {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.status-card__desc {
  margin: 0;
  font-size: 0.875rem;
  color: var(--color-text-secondary);
}

.mitm-controls {
  padding-top: 0.5rem;
}

.controls-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem;
  align-items: center;
}

.mitm-meta {
  display: flex;
  gap: 0.5rem;
  margin-top: 0.75rem;
  color: var(--color-text-secondary);
  font-size: 0.875rem;
  align-items: baseline;
}

.meta-label {
  min-width: 40px;
  font-weight: 600;
  color: var(--color-text);
}

.meta-value {
  word-break: break-all;
}
</style>
