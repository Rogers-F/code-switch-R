<template>
  <div class="scroll-area-wrapper" :style="{ height: height }">
    <div class="scroll-area-viewport" ref="viewportRef">
      <slot />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'

interface Props {
  height?: string
}

withDefaults(defineProps<Props>(), {
  height: '100%'
})

const viewportRef = ref<HTMLElement>()

defineExpose({
  scrollToTop: () => {
    if (viewportRef.value) {
      viewportRef.value.scrollTop = 0
    }
  },
  scrollToBottom: () => {
    if (viewportRef.value) {
      viewportRef.value.scrollTop = viewportRef.value.scrollHeight
    }
  }
})
</script>

<style scoped>
.scroll-area-wrapper {
  position: relative;
  overflow: hidden;
}

.scroll-area-viewport {
  height: 100%;
  overflow-y: auto;
  overflow-x: hidden;
}

.scroll-area-viewport::-webkit-scrollbar {
  width: 8px;
}

.scroll-area-viewport::-webkit-scrollbar-track {
  background: transparent;
}

.scroll-area-viewport::-webkit-scrollbar-thumb {
  background: color-mix(in srgb, var(--mac-text-secondary) 35%, transparent);
  border-radius: 4px;
}

.scroll-area-viewport::-webkit-scrollbar-thumb:hover {
  background: color-mix(in srgb, var(--mac-text-secondary) 55%, transparent);
}
</style>
