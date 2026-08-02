<template>
  <div class="main-shell capture-shell">
    <header class="app-page-header">
      <div class="app-page-title-group">
        <h1 class="app-page-title">{{ t('components.capture.title') }}</h1>
        <p class="app-page-subtitle">{{ t('components.capture.subtitle') }}</p>
      </div>
      <div class="app-page-actions">
        <div class="record-toggle" :title="t('components.capture.recordHint')">
          <label class="mac-switch">
            <input
              type="checkbox"
              v-model="recording"
              :disabled="toggling"
              @change="toggleRecording"
            />
            <span></span>
          </label>
          <span class="record-label" :class="{ on: recording }">
            {{ recording ? t('components.capture.recording') : t('components.capture.recordOff') }}
          </span>
        </div>
        <button class="secondary-btn danger" :disabled="clearing" @click="clearAll">
          {{ t('components.capture.clearAll') }}
        </button>
      </div>
    </header>

    <div class="app-page-container capture-page">
      <div class="capture-layout">
        <!-- 左侧：会话列表 -->
        <aside class="session-sidebar">
          <div class="session-sidebar__header">
            <h3>{{ t('components.capture.sessions') }}</h3>
          </div>
          <div class="session-list">
            <p v-if="sessions.length === 0" class="session-empty">
              {{ t('components.capture.noSessions') }}
            </p>
            <div
              v-for="session in sessions"
              :key="sessionKey(session)"
              :class="['session-item', { selected: isSelected(session) }]"
              @click="selectSession(session)"
            >
              <div class="session-item__main">
                <span class="session-item__name">
                  {{ sessionTitle(session) }}
                  <span v-if="session.active" class="session-badge recording">● {{ t('components.capture.live') }}</span>
                  <span v-else-if="session.interrupted" class="session-badge interrupted">{{ t('components.capture.interrupted') }}</span>
                </span>
                <span class="session-item__time">{{ sessionTimeRange(session) }}</span>
              </div>
              <div class="session-item__side">
                <span class="session-item__count">{{ session.request_count }}</span>
                <button
                  class="session-delete-btn"
                  :title="t('components.capture.deleteSession')"
                  @click.stop="deleteSession(session)"
                >×</button>
              </div>
            </div>
          </div>
        </aside>

        <!-- 右侧：选中会话的请求列表 -->
        <section class="session-detail">
          <div v-if="!selected" class="session-detail__empty">
            {{ t('components.capture.selectHint') }}
          </div>
          <template v-else>
            <div class="session-detail__toolbar">
              <span class="session-detail__title">{{ sessionTitle(selected) }}</span>
              <div class="session-detail__actions">
                <button class="secondary-btn" :disabled="exporting" @click="exportSelected">
                  {{ exporting ? t('components.capture.exporting') : t('components.capture.export') }}
                </button>
              </div>
            </div>
            <p class="session-detail__hint">{{ t('components.capture.sensitiveHint') }}</p>
            <div class="session-table-wrapper">
              <table class="session-table">
                <thead>
                  <tr>
                    <th>{{ t('components.capture.table.time') }}</th>
                    <th>{{ t('components.capture.table.platform') }}</th>
                    <th>{{ t('components.capture.table.provider') }}</th>
                    <th>{{ t('components.capture.table.model') }}</th>
                    <th>{{ t('components.capture.table.status') }}</th>
                    <th>{{ t('components.capture.table.size') }}</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="row in rows" :key="row.id">
                    <td>{{ formatTime(row.created_at) }}</td>
                    <td>{{ row.platform || '—' }}</td>
                    <td>{{ row.provider || '—' }}</td>
                    <td class="model-cell">{{ row.model || '—' }}</td>
                    <td :class="['code', httpCodeClass(row.http_code)]">{{ row.http_code || '—' }}</td>
                    <td>{{ formatBytes(row.body_bytes) }}</td>
                    <td>
                      <button class="capture-view-btn" @click="openDetail(row.id)">
                        {{ t('components.logs.captureDetail.view') }}
                      </button>
                    </td>
                  </tr>
                  <tr v-if="rows.length === 0 && !loadingRows">
                    <td colspan="7" class="empty-row">{{ t('components.capture.noRequests') }}</td>
                  </tr>
                </tbody>
              </table>
              <div class="session-table-footer">
                <button
                  v-if="mayHaveMore"
                  class="secondary-btn"
                  :disabled="loadingRows"
                  @click="loadMore"
                >{{ t('components.capture.loadMore') }}</button>
              </div>
            </div>
          </template>
        </section>
      </div>

      <CaptureDetailModal
        :open="detailModal.open"
        :loading="detailModal.loading"
        :error="detailModal.error"
        :data="detailModal.data"
        @close="closeDetail"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Call } from '@wailsio/runtime'
import CaptureDetailModal from '../common/CaptureDetailModal.vue'
import { fetchRequestLogDetail, type RequestLogDetail } from '../../services/logs'

interface CaptureSession {
  id: number
  started_at: string
  ended_at: string
  interrupted: boolean
  legacy: boolean
  active: boolean
  request_count: number
}

interface CaptureRow {
  id: number
  created_at: string
  platform: string
  provider: string
  model: string
  http_code: number
  is_stream: boolean
  duration_sec: number
  body_bytes: number
  body_truncated: boolean
}

const { t } = useI18n()
const svc = 'codeswitch/services.ProviderRelayService.'

const recording = ref(false)
const toggling = ref(false)
const clearing = ref(false)
const exporting = ref(false)
const sessions = ref<CaptureSession[]>([])
const selected = ref<CaptureSession | null>(null)
const rows = ref<CaptureRow[]>([])
const loadingRows = ref(false)
const mayHaveMore = ref(false)
const PAGE = 200
// rowsEpoch：每次切换会话/重置递增；异步 RPC 返回时若纪元已变则丢弃结果，
// 防止把上一个会话的行渲染到当前会话（内容可能敏感，不能张冠李戴）
let rowsEpoch = 0

let sessionTimer: number | null = null
let liveTimer: number | null = null

const sessionKey = (s: CaptureSession) => (s.legacy ? 'legacy' : s.id)
const isSelected = (s: CaptureSession) =>
  !!selected.value && selected.value.id === s.id && selected.value.legacy === s.legacy

const sessionTitle = (s: CaptureSession) =>
  s.legacy ? t('components.capture.legacySession') : t('components.capture.sessionName', { id: s.id })

const formatTime = (value: string) => {
  if (!value) return '—'
  // created_at 为 UTC 文本，补 Z 后按本地展示
  const date = new Date(value.includes('T') || value.endsWith('Z') ? value : value.replace(' ', 'T') + 'Z')
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

const sessionTimeRange = (s: CaptureSession) => {
  if (s.legacy) return t('components.capture.legacyHint')
  const start = formatTime(s.started_at)
  if (s.active) return start
  const end = s.ended_at ? formatTime(s.ended_at) : ''
  return end ? `${start} ~ ${end}` : start
}

const httpCodeClass = (code: number) => {
  if (code >= 200 && code < 300) return 'ok'
  if (code >= 400) return 'err'
  return ''
}

const formatBytes = (bytes: number) => {
  if (!bytes) return '—'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

const loadRecordingState = async () => {
  try {
    recording.value = (await Call.ByName(svc + 'GetRequestCapture')) as boolean
  } catch (error) {
    console.error('读取录制状态失败:', error)
  }
}

const toggleRecording = async () => {
  toggling.value = true
  try {
    await Call.ByName(svc + 'SetRequestCapture', recording.value)
  } catch (error) {
    console.error('切换录制失败:', error)
    alert(t('components.capture.toggleFailed') + (error as Error).message)
  } finally {
    await loadRecordingState()
    await refreshSessions()
    toggling.value = false
  }
}

const refreshSessions = async () => {
  try {
    const list = (await Call.ByName(svc + 'ListCaptureSessions')) as CaptureSession[] | null
    sessions.value = list ?? []
    // 维持选中项引用最新数据；被删掉则回退到第一个
    if (selected.value) {
      const match = sessions.value.find(s => isSelected(s))
      selected.value = match ?? sessions.value[0] ?? null
      if (!match) resetRows()
    } else if (sessions.value.length > 0) {
      selected.value = sessions.value[0]
      resetRows()
    }
    syncLiveTimer()
  } catch (error) {
    console.error('加载会话列表失败:', error)
  }
}

const fetchRows = async (sessionID: number, sinceID: number, beforeID: number): Promise<CaptureRow[]> => {
  const result = (await Call.ByName(svc + 'GetCaptureSessionLogs', sessionID, sinceID, beforeID, PAGE)) as
    | CaptureRow[]
    | null
  return result ?? []
}

const resetRows = async () => {
  // 纪元先行递增：即使随后发现没有选中会话而提前返回，
  // 也要作废所有在途请求（例如删除最后一个会话后清空列表的场景）
  const epoch = ++rowsEpoch
  rows.value = []
  mayHaveMore.value = false
  if (!selected.value) return
  loadingRows.value = true
  try {
    const initial = await fetchRows(selected.value.id, 0, 0)
    if (epoch !== rowsEpoch) return // 会话已切换/删除，丢弃过期结果
    rows.value = initial
    mayHaveMore.value = initial.length >= PAGE
  } catch (error) {
    console.error('加载会话请求失败:', error)
  } finally {
    if (epoch === rowsEpoch) loadingRows.value = false
  }
}

const loadMore = async () => {
  if (!selected.value || rows.value.length === 0) return
  const epoch = rowsEpoch
  loadingRows.value = true
  try {
    const oldest = rows.value[rows.value.length - 1].id
    const older = await fetchRows(selected.value.id, 0, oldest)
    if (epoch !== rowsEpoch) return
    rows.value.push(...older)
    mayHaveMore.value = older.length >= PAGE
  } catch (error) {
    console.error('加载更多失败:', error)
  } finally {
    if (epoch === rowsEpoch) loadingRows.value = false
  }
}

// 录制中的会话增量拉取：只取新行，头插保持新行在前。
// epoch 校验丢弃跨会话的过期响应；in-flight 防重入避免重复插入同批行
let livePolling = false
const pollLive = async () => {
  const current = selected.value
  if (!current || !current.active || livePolling) return
  livePolling = true
  const epoch = rowsEpoch
  try {
    const newest = rows.value[0]?.id ?? 0
    if (newest === 0) {
      // 尚无任何行：增量游标无从谈起，走初始加载（sinceID=0 是降序初始语义，
      // 直接 unshift 会把顺序装反）
      await resetRows()
      return
    }
    const fresh = await fetchRows(current.id, newest, 0)
    if (epoch !== rowsEpoch) return
    if (fresh.length > 0) {
      rows.value.unshift(...fresh.reverse())
    }
  } catch (error) {
    console.error('增量拉取失败:', error)
  } finally {
    livePolling = false
  }
}

const syncLiveTimer = () => {
  const needLive = !!selected.value?.active
  if (needLive && liveTimer === null) {
    liveTimer = window.setInterval(pollLive, 2000)
  } else if (!needLive && liveTimer !== null) {
    clearInterval(liveTimer)
    liveTimer = null
  }
}

const selectSession = (s: CaptureSession) => {
  selected.value = s
  resetRows()
  syncLiveTimer()
}

const deleteSession = async (s: CaptureSession) => {
  if (!confirm(t('components.capture.deleteConfirm', { name: sessionTitle(s) }))) return
  try {
    await Call.ByName(svc + 'DeleteCaptureSession', s.id)
    await refreshSessions()
    if (isSelected(s)) resetRows()
  } catch (error) {
    console.error('删除会话失败:', error)
    alert(t('components.capture.deleteFailed') + (error as Error).message)
  }
}

const clearAll = async () => {
  if (!confirm(t('components.capture.clearConfirm'))) return
  clearing.value = true
  try {
    const affected = (await Call.ByName(svc + 'ClearCapturedRequests')) as number
    alert(t('components.capture.clearDone', { count: affected }))
    selected.value = null
    rows.value = []
    rowsEpoch++ // 作废在途的行请求，防止清空后被旧响应回填
    await refreshSessions()
  } catch (error) {
    console.error('清空失败:', error)
    alert(t('components.capture.clearFailed') + (error as Error).message)
  } finally {
    clearing.value = false
  }
}

const exportSelected = async () => {
  if (!selected.value) return
  exporting.value = true
  try {
    const result = (await Call.ByName(svc + 'ExportCaptureSessionWithDialog', selected.value.id)) as {
      path: string
      count: number
      canceled: boolean
    }
    if (!result.canceled) {
      alert(t('components.capture.exportDone', { count: result.count, path: result.path }))
    }
  } catch (error) {
    console.error('导出失败:', error)
    alert(t('components.capture.exportFailed') + (error as Error).message)
  } finally {
    exporting.value = false
  }
}

// 详情弹窗
const detailModal = reactive<{
  open: boolean
  loading: boolean
  error: string
  data: RequestLogDetail | null
}>({ open: false, loading: false, error: '', data: null })
// 详情请求序号：快速连开两个详情时，先发慢返的旧响应不得覆盖新弹窗
let detailSeq = 0

const openDetail = async (id: number) => {
  const seq = ++detailSeq
  detailModal.open = true
  detailModal.loading = true
  detailModal.error = ''
  detailModal.data = null
  try {
    const data = await fetchRequestLogDetail(id)
    if (seq !== detailSeq) return
    detailModal.data = data
  } catch (error) {
    if (seq !== detailSeq) return
    detailModal.error = String((error as Error)?.message ?? error)
  } finally {
    if (seq === detailSeq) detailModal.loading = false
  }
}

const closeDetail = () => {
  detailModal.open = false
  detailModal.data = null
  detailModal.error = ''
}

onMounted(async () => {
  await loadRecordingState()
  await refreshSessions()
  sessionTimer = window.setInterval(refreshSessions, 3000)
})

onUnmounted(() => {
  if (sessionTimer !== null) clearInterval(sessionTimer)
  if (liveTimer !== null) clearInterval(liveTimer)
})
</script>

<style scoped>
.capture-shell {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

.capture-page {
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.record-toggle {
  display: flex;
  align-items: center;
  gap: 8px;
}

.record-label {
  font-size: 0.88rem;
  color: var(--mac-text-secondary);
}

.record-label.on {
  color: #ef4444;
  font-weight: 600;
}

.secondary-btn.danger:hover {
  color: #ef4444;
  border-color: #ef4444;
}

.capture-layout {
  flex: 1;
  min-height: 0;
  display: flex;
  gap: 16px;
}

.session-sidebar {
  width: 264px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  background: var(--mac-surface);
  border: 1px solid var(--mac-border);
  border-radius: 12px;
  overflow: hidden;
}

.session-sidebar__header {
  padding: 12px 14px;
  border-bottom: 1px solid var(--mac-border);
}

.session-sidebar__header h3 {
  margin: 0;
  font-size: 0.95rem;
}

.session-list {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.session-empty {
  color: var(--mac-text-secondary);
  font-size: 0.85rem;
  text-align: center;
  padding: 24px 8px;
}

.session-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 10px 12px;
  border-radius: 8px;
  cursor: pointer;
  border: 1px solid transparent;
}

.session-item:hover {
  background: var(--mac-hover, rgba(0, 0, 0, 0.04));
}

.session-item.selected {
  border-color: var(--mac-accent);
  background: color-mix(in srgb, var(--mac-accent) 8%, transparent);
}

.session-item__main {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.session-item__name {
  font-size: 0.88rem;
  font-weight: 600;
  display: flex;
  align-items: center;
  gap: 6px;
}

.session-item__time {
  font-size: 0.75rem;
  color: var(--mac-text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.session-badge {
  font-size: 0.7rem;
  padding: 0 6px;
  border-radius: 999px;
  font-weight: 500;
}

.session-badge.recording {
  color: #ef4444;
}

.session-badge.interrupted {
  color: #f59e0b;
  border: 1px solid #f59e0b;
}

.session-item__side {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}

.session-item__count {
  font-size: 0.78rem;
  color: var(--mac-text-secondary);
  background: var(--mac-hover, rgba(0, 0, 0, 0.05));
  border-radius: 999px;
  padding: 1px 8px;
}

.session-delete-btn {
  border: none;
  background: transparent;
  color: var(--mac-text-secondary);
  font-size: 1rem;
  cursor: pointer;
  padding: 0 4px;
  border-radius: 4px;
  opacity: 0;
}

.session-item:hover .session-delete-btn {
  opacity: 1;
}

.session-delete-btn:hover {
  color: #ef4444;
}

.session-detail {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  background: var(--mac-surface);
  border: 1px solid var(--mac-border);
  border-radius: 12px;
  padding: 14px;
}

.session-detail__empty {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--mac-text-secondary);
}

.session-detail__toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.session-detail__title {
  font-weight: 600;
}

.session-detail__hint {
  margin: 6px 0 10px;
  font-size: 0.75rem;
  color: var(--mac-text-secondary);
}

.session-table-wrapper {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}

.session-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.82rem;
}

.session-table th {
  text-align: left;
  padding: 6px 8px;
  color: var(--mac-text-secondary);
  font-weight: 500;
  border-bottom: 1px solid var(--mac-border);
  position: sticky;
  top: 0;
  background: var(--mac-surface);
}

.session-table td {
  padding: 7px 8px;
  border-bottom: 1px solid var(--mac-border);
}

.model-cell {
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.code.ok {
  color: #10b981;
}

.code.err {
  color: #ef4444;
}

.empty-row {
  text-align: center;
  color: var(--mac-text-secondary);
  padding: 24px 0 !important;
}

.session-table-footer {
  display: flex;
  justify-content: center;
  padding: 10px 0;
}

.capture-view-btn {
  border: 1px solid var(--mac-border);
  background: transparent;
  border-radius: 6px;
  padding: 2px 10px;
  font-size: 0.78rem;
  cursor: pointer;
  color: var(--mac-text-secondary);
}

.capture-view-btn:hover {
  color: var(--mac-text);
  border-color: var(--mac-accent);
}
</style>
