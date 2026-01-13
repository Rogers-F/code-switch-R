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
  const baseClass = 'rounded-lg transition-colors'

  const variantClasses = {
    default: 'bg-white dark:bg-gray-900 shadow-sm',
    outline: 'border border-gray-200 dark:border-gray-800'
  }

  return [baseClass, variantClasses[props.variant]].join(' ')
})
</script>
