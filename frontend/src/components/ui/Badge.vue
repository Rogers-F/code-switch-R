<template>
  <span :class="badgeClass">
    <slot />
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'

interface Props {
  variant?: 'default' | 'success' | 'warning' | 'error' | 'info'
  size?: 'sm' | 'default'
}

const props = withDefaults(defineProps<Props>(), {
  variant: 'default',
  size: 'default'
})

const badgeClass = computed(() => {
  const baseClass = 'ui-badge'

  const variantClasses = {
    default: 'ui-badge-default',
    success: 'ui-badge-success',
    warning: 'ui-badge-warning',
    error: 'ui-badge-error',
    info: 'ui-badge-info'
  }

  const sizeClasses = {
    sm: 'ui-badge-sm',
    default: 'ui-badge-md'
  }

  return [baseClass, variantClasses[props.variant], sizeClasses[props.size]]
    .filter(Boolean)
    .join(' ')
})
</script>
