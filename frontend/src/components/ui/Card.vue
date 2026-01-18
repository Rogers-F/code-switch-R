<template>
  <div :class="cardClass">
    <slot />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

interface Props {
  variant?: 'default' | 'outline'
}

const props = withDefaults(defineProps<Props>(), {
  variant: 'default'
})

const cardClass = computed(() => {
  const variantClasses = {
    default: 'mac-panel',
    outline: 'mac-panel mac-panel-outline'
  }

  return variantClasses[props.variant]
})
</script>
