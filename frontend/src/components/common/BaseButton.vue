<template>
  <button :type="type" :class="['btn', variantClass, sizeClass]" v-bind="$attrs">
    <slot />
  </button>
</template>

<script setup lang="ts">
import { computed, useAttrs } from 'vue'

const props = withDefaults(
  defineProps<{
    variant?: 'primary' | 'outline' | 'danger'
    size?: 'sm' | 'md' | 'lg'
    type?: 'button' | 'submit' | 'reset'
  }>(),
  {
    variant: 'primary',
    size: 'md',
    type: 'button',
  },
)

useAttrs()

const variantClass = computed(() => `btn-${props.variant}`)
const sizeClass = computed(() => (props.size === 'md' ? '' : `btn-${props.size}`))
</script>
