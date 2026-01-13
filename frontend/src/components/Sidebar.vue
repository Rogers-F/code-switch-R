<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Browser } from '@wailsio/runtime'
import { fetchCurrentVersion } from '../services/version'
import { getUpdateState, type UpdateState } from '../services/update'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()

// 动态版本号（从后端获取）
const appVersion = ref('...')
const updateState = ref<UpdateState | null>(null)
let updateTimer: number | undefined
const releasePageUrl = 'https://github.com/SimonUTD/code-switch-R/releases'

const normalizeVersion = (value: string) => value.replace(/^v/i, '').trim()

const compareVersions = (current: string, remote: string) => {
  const curParts = normalizeVersion(current).split('.').map((part) => parseInt(part, 10) || 0)
  const remoteParts = normalizeVersion(remote).split('.').map((part) => parseInt(part, 10) || 0)
  const maxLen = Math.max(curParts.length, remoteParts.length)
  for (let i = 0; i < maxLen; i++) {
    const cur = curParts[i] ?? 0
    const rem = remoteParts[i] ?? 0
    if (cur === rem) continue
    return cur < rem ? -1 : 1
  }
  return 0
}

const latestKnownVersion = computed(() => updateState.value?.latest_known_version?.trim() ?? '')
const hasNewVersion = computed(() => {
  if (!latestKnownVersion.value) return false
  if (!appVersion.value || appVersion.value === '...') return false
  if (appVersion.value === 'v?.?.?') return false
  return compareVersions(appVersion.value, latestKnownVersion.value) < 0
})

const openReleases = () => {
  Browser.OpenURL(releasePageUrl).catch(() => {
    console.error('failed to open release page')
  })
}

const refreshUpdateState = async () => {
  try {
    updateState.value = await getUpdateState()
  } catch (error) {
    console.error('failed to load update state', error)
  }
}

onMounted(async () => {
  try {
    appVersion.value = await fetchCurrentVersion()
  } catch {
    appVersion.value = 'v?.?.?'
  }

  await refreshUpdateState()
  updateTimer = window.setInterval(() => {
    void refreshUpdateState()
  }, 30_000)
})

onUnmounted(() => {
  if (updateTimer) {
    window.clearInterval(updateTimer)
    updateTimer = undefined
  }
})

// 侧边栏收起状态
const SIDEBAR_COLLAPSED_KEY = 'sidebar-collapsed'
const SIDEBAR_GROUP_COLLAPSED_KEY = 'sidebar-group-collapsed'
const VISITED_PAGES_KEY = 'visited-pages'
const isCollapsed = ref(false)
const visitedPages = ref<Set<string>>(new Set())
const collapsedGroups = ref<Set<string>>(new Set())

onMounted(() => {
  // 加载侧边栏状态
  const saved = localStorage.getItem(SIDEBAR_COLLAPSED_KEY)
  if (saved !== null) {
    isCollapsed.value = saved === 'true'
  }
  // 加载已访问页面
  const visitedJson = localStorage.getItem(VISITED_PAGES_KEY)
  if (visitedJson) {
    try {
      visitedPages.value = new Set(JSON.parse(visitedJson))
    } catch {
      visitedPages.value = new Set()
    }
  }
  // 加载折叠的分组
  const groupsJson = localStorage.getItem(SIDEBAR_GROUP_COLLAPSED_KEY)
  if (groupsJson) {
    try {
      collapsedGroups.value = new Set(JSON.parse(groupsJson))
    } catch {
      collapsedGroups.value = new Set()
    }
  }
  // 标记当前页面为已访问
  markAsVisited(route.path)
})

// 监听路由变化，标记为已访问
watch(() => route.path, (newPath) => {
  markAsVisited(newPath)
})

function markAsVisited(path: string) {
  if (!visitedPages.value.has(path)) {
    visitedPages.value.add(path)
    localStorage.setItem(VISITED_PAGES_KEY, JSON.stringify([...visitedPages.value]))
  }
}

// 判断是否显示 NEW 徽章（仅在未访问时显示）
function shouldShowNew(item: NavItem): boolean {
  return item.isNew === true && !visitedPages.value.has(item.path)
}

const toggleCollapse = () => {
  isCollapsed.value = !isCollapsed.value
  localStorage.setItem(SIDEBAR_COLLAPSED_KEY, String(isCollapsed.value))
}

const toggleGroupCollapse = (groupId: string) => {
  if (collapsedGroups.value.has(groupId)) {
    collapsedGroups.value.delete(groupId)
  } else {
    collapsedGroups.value.add(groupId)
  }
  localStorage.setItem(SIDEBAR_GROUP_COLLAPSED_KEY, JSON.stringify([...collapsedGroups.value]))
}

const isGroupCollapsed = (groupId: string) => {
  return collapsedGroups.value.has(groupId)
}

interface NavItem {
  path: string
  icon: string
  labelKey: string
  isNew?: boolean
}

interface NavGroup {
  id: string
  labelKey: string
  icon?: string
  collapsible: boolean
  items: NavItem[]
}

const navGroups: NavGroup[] = [
  {
    id: 'dashboard',
    labelKey: 'sidebar.dashboard',
    icon: 'grid',
    collapsible: false,
    items: [
      { path: '/dashboard', icon: 'grid', labelKey: 'sidebar.dashboard' }
    ]
  },
  {
    id: 'providers',
    labelKey: 'sidebar.providers',
    icon: 'layers',
    collapsible: true,
    items: [
      { path: '/providers', icon: 'layers', labelKey: 'sidebar.providers', isNew: true }
    ]
  },
  {
    id: 'rules',
    labelKey: 'sidebar.rules',
    icon: 'star',
    collapsible: true,
    items: [
      { path: '/rules', icon: 'star', labelKey: 'sidebar.rules', isNew: true }
    ]
  },
  {
    id: 'logs',
    labelKey: 'sidebar.logs',
    icon: 'bar-chart',
    collapsible: true,
    items: [
      { path: '/logs', icon: 'bar-chart', labelKey: 'sidebar.logs' },
      { path: '/logs/terminal', icon: 'terminal', labelKey: 'sidebar.terminal_logs', isNew: true }
    ]
  },
  {
    id: 'settings',
    labelKey: 'sidebar.settings',
    icon: 'settings',
    collapsible: true,
    items: [
      { path: '/settings', icon: 'settings', labelKey: 'sidebar.settings' },
      { path: '/console', icon: 'terminal', labelKey: 'sidebar.console' },
      { path: '/mcp', icon: 'plug', labelKey: 'sidebar.mcp' },
      { path: '/skill', icon: 'tool', labelKey: 'sidebar.skill' },
      { path: '/prompts', icon: 'file-text', labelKey: 'sidebar.prompts', isNew: true },
      { path: '/env', icon: 'search', labelKey: 'sidebar.env', isNew: true },
      { path: '/speedtest', icon: 'zap', labelKey: 'sidebar.speedtest', isNew: true },
      { path: '/availability', icon: 'activity', labelKey: 'sidebar.availability', isNew: true },
      { path: '/mitm-poc', icon: 'shield', labelKey: 'MITM PoC', isNew: true }
    ]
  }
]

const currentPath = computed(() => route.path)

const navigate = (path: string) => {
  router.push(path)
}
</script>

<template>
  <nav class="mac-sidebar" :class="{ collapsed: isCollapsed }">
    <div class="sidebar-header">
      <span class="sidebar-title" v-if="!isCollapsed">Simon Switch</span>
      <button class="collapse-btn" @click="toggleCollapse" :title="isCollapsed ? 'Expand' : 'Collapse'">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <polyline v-if="isCollapsed" points="9 18 15 12 9 6"></polyline>
          <polyline v-else points="15 18 9 12 15 6"></polyline>
        </svg>
      </button>
    </div>

    <div class="nav-list">
      <div v-for="group in navGroups" :key="group.id" class="nav-group">
        <!-- Group Header (for collapsible groups) -->
        <button
          v-if="group.collapsible && group.items.length > 1 && !isCollapsed"
          class="group-header"
          @click="toggleGroupCollapse(group.id)"
        >
          <span class="group-label">{{ t(group.labelKey) }}</span>
          <svg class="chevron-icon" :class="{ collapsed: isGroupCollapsed(group.id) }" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <polyline points="6 9 12 15 18 9"></polyline>
          </svg>
        </button>

        <!-- Group Items -->
        <div v-if="!group.collapsible || group.items.length === 1 || !isGroupCollapsed(group.id)" class="group-items">
          <button
            v-for="item in group.items"
            :key="item.path"
            class="nav-item"
            :class="{ active: currentPath === item.path }"
            :title="isCollapsed ? t(item.labelKey) : ''"
            @click="navigate(item.path)"
          >
            <!-- Home -->
            <svg v-if="item.icon === 'home'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"></path>
              <polyline points="9 22 9 12 15 12 15 22"></polyline>
            </svg>

            <!-- Grid -->
            <svg v-else-if="item.icon === 'grid'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="3" y="3" width="7" height="7" rx="1"></rect>
              <rect x="14" y="3" width="7" height="7" rx="1"></rect>
              <rect x="3" y="14" width="7" height="7" rx="1"></rect>
              <rect x="14" y="14" width="7" height="7" rx="1"></rect>
            </svg>

            <!-- Layers (Providers) -->
            <svg v-else-if="item.icon === 'layers'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M12 2L2 7l10 5 10-5-10-5z"></path>
              <path d="M2 17l10 5 10-5"></path>
              <path d="M2 12l10 5 10-5"></path>
            </svg>

            <!-- Star (Rules) -->
            <svg v-else-if="item.icon === 'star'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"></polygon>
            </svg>

            <!-- File Text -->
            <svg v-else-if="item.icon === 'file-text'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
              <polyline points="14 2 14 8 20 8"></polyline>
              <line x1="16" y1="13" x2="8" y2="13"></line>
              <line x1="16" y1="17" x2="8" y2="17"></line>
              <polyline points="10 9 9 9 8 9"></polyline>
            </svg>

            <!-- Plug -->
            <svg v-else-if="item.icon === 'plug'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M12 22v-5"></path>
              <path d="M9 8V2"></path>
              <path d="M15 8V2"></path>
              <path d="M18 8v5a6 6 0 0 1-12 0V8h12z"></path>
            </svg>

            <!-- Tool -->
            <svg v-else-if="item.icon === 'tool'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"></path>
            </svg>

            <!-- Activity -->
            <svg v-else-if="item.icon === 'activity'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <polyline points="22 12 18 12 15 21 9 3 6 12 2 12"></polyline>
            </svg>

            <!-- Zap -->
            <svg v-else-if="item.icon === 'zap'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"></polygon>
            </svg>

            <!-- Search -->
            <svg v-else-if="item.icon === 'search'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="11" cy="11" r="8"></circle>
              <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
            </svg>

            <!-- Bar Chart -->
            <svg v-else-if="item.icon === 'bar-chart'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <line x1="12" y1="20" x2="12" y2="10"></line>
              <line x1="18" y1="20" x2="18" y2="4"></line>
              <line x1="6" y1="20" x2="6" y2="16"></line>
            </svg>

            <!-- Terminal -->
            <svg v-else-if="item.icon === 'terminal'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <polyline points="4 17 10 11 4 5"></polyline>
              <line x1="12" y1="19" x2="20" y2="19"></line>
            </svg>

            <!-- Shield -->
            <svg v-else-if="item.icon === 'shield'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"></path>
            </svg>

            <!-- Settings -->
            <svg v-else-if="item.icon === 'settings'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="12" cy="12" r="3"></circle>
              <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"></path>
            </svg>

            <span class="nav-label" v-if="!isCollapsed">{{ t(item.labelKey) }}</span>
            <span v-if="shouldShowNew(item) && !isCollapsed" class="new-badge">NEW</span>
          </button>
        </div>
      </div>
    </div>

    <div class="sidebar-footer" v-if="!isCollapsed">
      <button
        v-if="hasNewVersion"
        type="button"
        class="version version-link"
        :title="t('sidebar.updateOpenReleases')"
        @click="openReleases"
      >
        {{ t('sidebar.updateFound', { version: latestKnownVersion }) }}
      </button>
      <span v-else class="version">{{ appVersion }}</span>
    </div>
  </nav>
</template>

<style scoped>
.mac-sidebar {
  width: 200px;
  min-width: 200px;
  background: var(--mac-surface);
  border-right: 1px solid var(--mac-border);
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
  transition: width 0.2s ease, min-width 0.2s ease;
}

.mac-sidebar.collapsed {
  width: 48px;
  min-width: 48px;
}

.sidebar-header {
  /* macOS 红绿灯按钮区域约 52px 高，添加额外 padding */
  padding: 52px 16px 16px;
  border-bottom: 1px solid var(--mac-border);
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  align-items: center;
  justify-items: center;
  gap: 8px;
  /* 拖拽区域 */
  -webkit-app-region: drag;
}

.sidebar-header * {
  /* 按钮等元素需要可点击 */
  -webkit-app-region: no-drag;
}

.mac-sidebar.collapsed .sidebar-header {
  padding: 52px 0 16px;
  grid-template-columns: 1fr;
  justify-items: center;
}

.sidebar-title {
  font-size: 1.1rem;
  font-weight: 700;
  color: var(--mac-text);
  letter-spacing: -0.02em;
  white-space: nowrap;
  overflow: hidden;
  grid-column: 2;
  justify-self: center;
}

.collapse-btn {
  width: 28px;
  height: 28px;
  border: none;
  background: transparent;
  border-radius: 6px;
  color: var(--mac-text-secondary);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s ease;
  flex-shrink: 0;
  grid-column: 3;
  justify-self: end;
}

.mac-sidebar.collapsed .collapse-btn {
  grid-column: 1;
  justify-self: center;
}

.collapse-btn:hover {
  background: rgba(15, 23, 42, 0.06);
  color: var(--mac-text);
}

html.dark .collapse-btn:hover {
  background: rgba(255, 255, 255, 0.08);
}

.collapse-btn svg {
  width: 16px;
  height: 16px;
}

.nav-list {
  flex: 1;
  padding: 12px 8px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  overflow-y: auto;
  scrollbar-width: none; /* Firefox 隐藏滚动条但保留滚动 */
  -ms-overflow-style: none; /* IE/Edge Legacy 隐藏滚动条 */
}

.nav-list::-webkit-scrollbar {
  display: none; /* WebKit 隐藏滚动条 */
}

.mac-sidebar.collapsed .nav-list {
  padding: 12px 0;
  align-items: center;
}

.nav-group {
  margin-bottom: 4px;
}

.group-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 12px;
  border: none;
  background: transparent;
  color: var(--mac-text-secondary);
  font-size: 0.75rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  cursor: pointer;
  transition: all 0.15s ease;
  width: 100%;
  text-align: left;
}

.group-header:hover {
  color: var(--mac-text);
}

.group-label {
  flex: 1;
}

.chevron-icon {
  width: 14px;
  height: 14px;
  flex-shrink: 0;
  transition: transform 0.2s ease;
}

.chevron-icon.collapsed {
  transform: rotate(-90deg);
}

.group-items {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border-radius: 8px;
  border: none;
  background: transparent;
  color: var(--mac-text-secondary);
  font-size: 0.9rem;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s ease;
  /* 横向留出缓冲，避免被父级 overflow 裁切圆角 */
  box-sizing: border-box;
  width: calc(100% - 8px);
  margin: 0 4px;
  text-align: left;
}

.mac-sidebar.collapsed .nav-item {
  /* 收起态固定宽度，确保图标居中 */
  width: 36px;
  margin: 0 auto;
  padding: 10px 0;
  justify-content: center;
}

.nav-item:hover {
  background: rgba(15, 23, 42, 0.06);
  color: var(--mac-text);
}

html.dark .nav-item:hover {
  background: rgba(255, 255, 255, 0.08);
}

.nav-item.active {
  background: var(--mac-accent);
  color: #fff;
}

.nav-item.active:hover {
  background: var(--mac-accent);
  color: #fff;
}

.nav-icon {
  width: 18px;
  height: 18px;
  flex-shrink: 0;
}

.nav-label {
  flex: 1;
}

.new-badge {
  font-size: 0.6rem;
  font-weight: 700;
  padding: 2px 5px;
  border-radius: 4px;
  background: rgba(16, 185, 129, 0.15);
  color: #10b981;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.nav-item.active .new-badge {
  background: rgba(255, 255, 255, 0.2);
  color: #fff;
}

.sidebar-footer {
  padding: 12px 16px;
  border-top: 1px solid var(--mac-border);
}

.version {
  font-size: 0.75rem;
  color: var(--mac-text-secondary);
  opacity: 0.6;
}

.version-link {
  border: none;
  background: transparent;
  padding: 0;
  font: inherit;
  text-align: left;
  cursor: pointer;
  color: var(--mac-accent);
  opacity: 0.9;
}

.version-link:hover {
  opacity: 1;
  text-decoration: underline;
}
</style>
