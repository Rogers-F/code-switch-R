<template>
  <button :type="type" :class="['btn', variantClass, sizeClass]" v-bind="$attrs">
    <slot />
  </button>
</template>

<script setup lang="ts">
import { computed, useAttrs } from 'vue'

type Variant =
  | 'default'
  | 'primary'
  | 'secondary'
  | 'outline'
  | 'ghost'
  | 'link'
  | 'destructive'
  | 'danger'

type Size = 'default' | 'sm' | 'md' | 'lg' | 'icon'

const props = withDefaults(
  defineProps<{
    variant?: Variant
    size?: Size
    type?: 'button' | 'submit' | 'reset'
  }>(),
  {
    variant: 'primary',
    size: 'md',
    type: 'button',
  },
)

useAttrs()

const normalizedVariant = computed((): Exclude<Variant, 'default' | 'destructive'> => {
  switch (props.variant) {
    case 'default':
      return 'primary'
    case 'destructive':
      return 'danger'
    default:
      return props.variant
  }
})

const normalizedSize = computed((): Exclude<Size, 'default'> => {
  if (props.size === 'default') return 'md'
  return props.size
})

const variantClass = computed(() => `btn-${normalizedVariant.value}`)
const sizeClass = computed(() => (normalizedSize.value === 'md' ? '' : `btn-${normalizedSize.value}`))
</script>
