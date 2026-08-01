import { ref, computed } from 'vue'
import { setTheme, getCurrentTheme, ThemeMode } from '../utils/ThemeManager'

// 模块级单例：侧边栏与设置页共享同一份主题状态，ThemeManager 是唯一的持久化与 DOM 写入源
const mode = ref<ThemeMode>(getCurrentTheme())
const systemDark = ref(window.matchMedia('(prefers-color-scheme: dark)').matches)

window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', (e) => {
  systemDark.value = e.matches
})

export function useTheme() {
  // 实际生效的明暗结果（systemdefault 时跟随系统）
  const isDark = computed(() => {
    if (mode.value === 'systemdefault') return systemDark.value
    return mode.value === 'dark'
  })

  const setMode = (next: ThemeMode) => {
    mode.value = next
    setTheme(next)
  }

  return { mode, isDark, setMode }
}
