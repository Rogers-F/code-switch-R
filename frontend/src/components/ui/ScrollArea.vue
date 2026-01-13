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
  background: rgba(156, 163, 175, 0.3);
  border-radius: 4px;
}

.scroll-area-viewport::-webkit-scrollbar-thumb:hover {
  background: rgba(156, 163, 175, 0.5);
}

:root.dark .scroll-area-viewport::-webkit-scrollbar-thumb {
  background: rgba(107, 114, 128, 0.3);
}

:root.dark .scroll-area-viewport::-webkit-scrollbar-thumb:hover {
  background: rgba(107, 114, 128, 0.5);
}
</style>
