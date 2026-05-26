<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { setTheme, getCurrentTheme, ThemeMode } from '../../utils/ThemeManager'

const themevalue = ref<ThemeMode>('systemdefault')

const themeChange = () => {
  setTheme(themevalue.value)
  window.dispatchEvent(new CustomEvent('theme-changed', { detail: themevalue.value }))
}

const handleThemeEvent = (e: Event) => {
  const customEvent = e as CustomEvent
  if (customEvent.detail) {
    themevalue.value = customEvent.detail
  } else {
    themevalue.value = getCurrentTheme()
  }
}

onMounted(() => {
  themevalue.value = getCurrentTheme()
  window.addEventListener('theme-changed', handleThemeEvent)
})

onUnmounted(() => {
  window.removeEventListener('theme-changed', handleThemeEvent)
})
</script>

<template>
  <select class="mac-select" v-model="themevalue" @change="themeChange">
    <option value="light">{{ $t('components.themesetting.select.opt_light') }}</option>
    <option value="dark">{{ $t('components.themesetting.select.opt_dark') }}</option>
    <option value="systemdefault">{{ $t('components.themesetting.select.opt_system') }}</option>
  </select>
</template>
