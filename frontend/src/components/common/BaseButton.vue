<template>
  <button :type="type" :class="['btn', variantClass]" v-bind="$attrs">
    <slot />
  </button>
</template>

<script setup lang="ts">
import { computed, useAttrs } from 'vue'

const props = withDefaults(
  defineProps<{
    variant?: 'primary' | 'outline' | 'danger' | 'ghost'
    type?: 'button' | 'submit' | 'reset'
  }>(),
  {
    variant: 'primary',
    type: 'button',
  },
)

useAttrs()

const variantClass = computed(() => `btn-${props.variant}`)
</script>
