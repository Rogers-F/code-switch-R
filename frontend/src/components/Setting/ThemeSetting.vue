<template>
  <select class="mac-select" v-model="themevalue" @change="themeChange">
    <option value="systemdefault">{{ $t('components.themesetting.select.opt_system') }}</option>
    <option value="light">{{ $t('components.themesetting.select.opt_light') }}</option>
    <option value="dark">{{ $t('components.themesetting.select.opt_dark') }}</option>
  </select>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { setTheme, getCurrentTheme, ThemeMode } from '../../utils/ThemeManager'

const themevalue = ref<ThemeMode>('systemdefault')

const themeChange = () => {
  setTheme(themevalue.value)
}

onMounted(() => {
  themevalue.value = getCurrentTheme()
})
</script>
