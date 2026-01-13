<template>
  <PageLayout
    title="MITM PoC (P0 Testing)"
    :sticky="true"
  >
    <div class="mitm-poc-container">
      <!-- Status Card -->
      <div class="controls-section">
        <h3>MITM Service Control</h3>

        <div class="status-row">
          <span class="label">Status:</span>
          <span class="status-badge" :class="status.running ? 'running' : 'stopped'">
            <span class="status-indicator"></span>
            {{ status.running ? 'Running' : 'Stopped' }}
          </span>
        </div>

        <div class="status-row">
          <span class="label">Port:</span>
          <span class="value">{{ status.port || 8443 }}</span>
        </div>

        <div class="status-row">
          <span class="label">Target:</span>
          <span class="value">{{ status.target || 'api.anthropic.com:443' }}</span>
        </div>

        <div class="control-buttons">
          <button
            v-if="!status.running"
            @click="startMITM"
            :disabled="loading"
            class="btn btn-primary"
          >
            {{ loading ? 'Starting...' : 'Start MITM' }}
          </button>
          <button
            v-else
            @click="stopMITM"
            class="btn btn-danger"
          >
            Stop MITM
          </button>
          <button
            @click="loadCACertPath"
            class="btn btn-secondary"
          >
            Show CA Cert Path
          </button>
        </div>

        <div v-if="caCertPath" class="info-row">
          <span class="label">CA Certificate:</span>
          <code class="cert-path">{{ caCertPath }}</code>
        </div>
      </div>

      <!-- Logs Section -->
      <div class="logs-section">
        <div class="logs-header">
          <h3>Recent Logs</h3>
          <div class="auto-scroll-toggle">
            <span>Auto Refresh</span>
            <label class="mac-switch">
              <input type="checkbox" v-model="autoRefresh" />
              <span></span>
            </label>
          </div>
        </div>

        <div class="logs-container">
          <div v-if="logs.length === 0" class="empty-state">
            <p>No MITM logs yet. Start the server and make some requests.</p>
          </div>

          <div v-else>
            <div v-for="(log, index) in logs" :key="index" class="log-entry">
              <span class="log-timestamp">{{ formatTimestamp(log.timestamp) }}</span>
              <span class="log-method">{{ log.method }}</span>
              <span class="log-domain">{{ log.domain }}</span>
              <span class="log-path">{{ log.path }}</span>
              <span class="log-status" :class="getStatusClass(log.statusCode)">{{ log.statusCode }}</span>
              <span class="log-latency">{{ log.latency }}ms</span>
              <span v-if="log.error" class="log-error">{{ log.error }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </PageLayout>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { Call } from '@wailsio/runtime'
import PageLayout from '../common/PageLayout.vue'

interface MITMStatus {
  running: boolean
  port: number
  target: string
}

interface MITMLogEntry {
  timestamp: string
  domain: string
  method: string
  path: string
  target: string
  statusCode: number
  latency: number
  error?: string
}

const status = ref<MITMStatus>({ running: false, port: 8443, target: '' })
const logs = ref<MITMLogEntry[]>([])
const loading = ref(false)
const caCertPath = ref('')
const autoRefresh = ref(true)
let refreshInterval: number | null = null

const loadStatus = async () => {
  try {
    const result = await Call.ByName('codeswitch/services.MITMService.GetMITMStatus')
    status.value = result as MITMStatus
  } catch (error) {
    console.error('Failed to load MITM status:', error)
  }
}

const loadCACertPath = async () => {
  try {
    const path = await Call.ByName('codeswitch/services.MITMService.GetMITMCACertPath')
    caCertPath.value = path as string
    alert('CA Certificate path: ' + path)
  } catch (error) {
    console.error('Failed to get CA cert path:', error)
    alert('Failed to get CA cert path')
  }
}

const startMITM = async () => {
  try {
    loading.value = true
    await Call.ByName('codeswitch/services.MITMService.StartMITM')
    await loadStatus()
    alert('MITM service started successfully on port ' + status.value.port)
  } catch (error) {
    console.error('Failed to start MITM:', error)
    alert('Failed to start MITM service: ' + error)
  } finally {
    loading.value = false
  }
}

const stopMITM = async () => {
  try {
    loading.value = true
    await Call.ByName('codeswitch/services.MITMService.StopMITM')
    await loadStatus()
    alert('MITM service stopped')
  } catch (error) {
    console.error('Failed to stop MITM:', error)
    alert('Failed to stop MITM service: ' + error)
  } finally {
    loading.value = false
  }
}

const loadLogs = async () => {
  if (!autoRefresh.value || !status.value.running) {
    return
  }

  try {
    const result = await Call.ByName('codeswitch/services.MITMService.GetMITMLogs')
    const newLogs = result as MITMLogEntry[]
    if (newLogs && newLogs.length > 0) {
      logs.value.push(...newLogs)
      // Keep only last 100 logs
      if (logs.value.length > 100) {
        logs.value = logs.value.slice(-100)
      }
    }
  } catch (error) {
    console.error('Failed to load logs:', error)
  }
}

const formatTimestamp = (timestamp: string) => {
  if (!timestamp) return ''
  return new Date(timestamp).toLocaleTimeString()
}

const getStatusClass = (statusCode: number) => {
  if (statusCode >= 200 && statusCode < 300) return 'success'
  if (statusCode >= 400) return 'error'
  return ''
}

onMounted(async () => {
  await loadStatus()

  refreshInterval = window.setInterval(async () => {
    await loadStatus()
    await loadLogs()
  }, 1000)
})

onUnmounted(() => {
  if (refreshInterval !== null) {
    clearInterval(refreshInterval)
  }
})
</script>

<style scoped>
.mitm-poc-container {
  padding: 1.5rem;
  max-width: 1200px;
  margin: 0 auto;
}

.controls-section,
.logs-section {
  background: var(--color-background-soft);
  border-radius: 0.75rem;
  padding: 1.5rem;
  margin-bottom: 1.5rem;
}

h3 {
  font-size: 1rem;
  font-weight: 600;
  margin-bottom: 1rem;
}

.status-row,
.info-row {
  display: flex;
  align-items: center;
  gap: 1rem;
  margin-bottom: 1rem;
}

.label {
  font-weight: 500;
  color: var(--color-text-secondary);
  min-width: 80px;
}

.value {
  font-family: 'SF Mono', monospace;
  font-size: 0.875rem;
}

.status-badge {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.375rem 0.75rem;
  border-radius: 0.5rem;
  font-size: 0.875rem;
  font-weight: 500;
}

.status-badge.running {
  background: rgba(34, 197, 94, 0.1);
  color: #22c55e;
}

.status-badge.stopped {
  background: rgba(239, 68, 68, 0.1);
  color: #ef4444;
}

.status-indicator {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: currentColor;
}

.control-buttons {
  display: flex;
  gap: 0.75rem;
  margin-top: 1.5rem;
}

.btn {
  padding: 0.625rem 1.25rem;
  border: none;
  border-radius: 0.5rem;
  font-size: 0.875rem;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
}

.btn-primary {
  background: var(--color-brand);
  color: white;
}

.btn-primary:hover:not(:disabled) {
  opacity: 0.9;
}

.btn-danger {
  background: #ef4444;
  color: white;
}

.btn-danger:hover {
  opacity: 0.9;
}

.btn-secondary {
  background: var(--color-background-mute);
  color: var(--color-text);
}

.btn-secondary:hover {
  background: var(--color-border);
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.cert-path {
  font-family: 'SF Mono', monospace;
  font-size: 0.8125rem;
  background: var(--color-background-mute);
  padding: 0.25rem 0.5rem;
  border-radius: 0.25rem;
}

.logs-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 1rem;
}

.auto-scroll-toggle {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.875rem;
}

.logs-container {
  background: var(--color-background-mute);
  border-radius: 0.5rem;
  padding: 1rem;
  max-height: 400px;
  overflow-y: auto;
  font-family: 'SF Mono', monospace;
  font-size: 0.8125rem;
}

.log-entry {
  padding: 0.5rem;
  border-bottom: 1px solid var(--color-border);
  display: grid;
  grid-template-columns: 100px 80px 150px 1fr 60px 80px;
  gap: 0.75rem;
  align-items: center;
}

.log-entry:last-child {
  border-bottom: none;
}

.log-timestamp {
  color: var(--color-text-secondary);
}

.log-method {
  font-weight: 600;
  color: var(--color-brand);
}

.log-status {
  font-weight: 500;
  text-align: right;
}

.log-status.success {
  color: #22c55e;
}

.log-status.error {
  color: #ef4444;
}

.log-domain {
  color: var(--color-text);
}

.log-path {
  color: var(--color-text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.log-latency {
  color: var(--color-text-secondary);
  text-align: right;
}

.log-error {
  grid-column: 1 / -1;
  color: #ef4444;
  font-size: 0.75rem;
  padding-top: 0.25rem;
}

.empty-state {
  text-align: center;
  padding: 2rem;
  color: var(--color-text-secondary);
}

/* Mac-style switch */
.mac-switch {
  position: relative;
  display: inline-block;
  width: 44px;
  height: 24px;
}

.mac-switch input {
  opacity: 0;
  width: 0;
  height: 0;
}

.mac-switch span {
  position: absolute;
  cursor: pointer;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background-color: #ccc;
  transition: 0.3s;
  border-radius: 24px;
}

.mac-switch span:before {
  position: absolute;
  content: "";
  height: 18px;
  width: 18px;
  left: 3px;
  bottom: 3px;
  background-color: white;
  transition: 0.3s;
  border-radius: 50%;
}

.mac-switch input:checked + span {
  background-color: var(--color-brand);
}

.mac-switch input:checked + span:before {
  transform: translateX(20px);
}
</style>
