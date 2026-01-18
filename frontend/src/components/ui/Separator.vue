<template>
  <div class="separator" :class="orientationClass" role="separator" />
</template>

<script setup lang="ts">
import { computed } from 'vue'

interface Props {
  orientation?: 'horizontal' | 'vertical'
}

const props = withDefaults(defineProps<Props>(), {
  orientation: 'horizontal'
})

const orientationClass = computed(() => {
  return props.orientation === 'horizontal'
    ? 'separator-horizontal'
    : 'separator-vertical'
})
</script>

<style scoped>
.separator {
  background-color: var(--mac-divider);
  flex-shrink: 0;
}

.separator-horizontal {
  height: 1px;
  width: 100%;
}

.separator-vertical {
  width: 1px;
  height: 100%;
}
</style>
