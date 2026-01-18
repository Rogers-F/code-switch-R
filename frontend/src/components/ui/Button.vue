<template>
  <BaseButton :type="type" :variant="variant" :size="size" :disabled="disabled" v-bind="$attrs">
    <slot />
  </BaseButton>
</template>

<script setup lang="ts">
import BaseButton from '../common/BaseButton.vue'

interface Props {
  variant?: 'default' | 'destructive' | 'outline' | 'secondary' | 'ghost' | 'link'
  size?: 'default' | 'sm' | 'lg' | 'icon'
  disabled?: boolean
  type?: 'button' | 'submit' | 'reset'
}

const props = withDefaults(defineProps<Props>(), {
  variant: 'default',
  size: 'default',
  disabled: false,
  type: 'button'
})

const { type, variant, size, disabled } = props
</script>
