<script setup lang="ts">
import { computed } from 'vue'
import { ThemeMode } from '../../utils/ThemeManager'
import { useTheme } from '../../composables/useTheme'

const { mode, setMode } = useTheme()

// select 双向绑定共享主题状态：侧边栏切换时此处同步刷新
const themevalue = computed<ThemeMode>({
  get: () => mode.value,
  set: (v) => setMode(v),
})
</script>

<template>
  <select class="mac-select" v-model="themevalue">
    <option value="light">{{ $t('components.themesetting.select.opt_light') }}</option>
    <option value="dark">{{ $t('components.themesetting.select.opt_dark') }}</option>
    <option value="systemdefault">{{ $t('components.themesetting.select.opt_system') }}</option>
  </select>
</template>
