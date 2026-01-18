<template>
  <TransitionRoot as="template" :show="open">
    <Dialog as="div" class="modal-backdrop" :open="open" @close="$emit('close')">
      <div class="modal-overlay" aria-hidden="true"></div>
      <div class="modal-wrapper">
        <TransitionChild
          as="template"
          enter="ease-out duration-200"
          enter-from="opacity-0 translate-y-4"
          enter-to="opacity-100 translate-y-0"
          leave="ease-in duration-150"
          leave-from="opacity-100 translate-y-0"
          leave-to="opacity-0 translate-y-4"
        >
          <DialogPanel :class="['modal', variantClass]">
            <header class="modal-header">
              <DialogTitle class="modal-title">{{ title }}</DialogTitle>
              <button
                class="ghost-icon"
                type="button"
                aria-label="Close"
                @click="$emit('close')"
              >
                <svg viewBox="0 0 24 24" aria-hidden="true">
                  <path
                    d="M6 6l12 12M6 18L18 6"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                    stroke-linecap="round"
                  />
                </svg>
              </button>
            </header>
            <div class="modal-body modal-scrollable">
              <slot />
            </div>
          </DialogPanel>
        </TransitionChild>
      </div>
    </Dialog>
  </TransitionRoot>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Dialog, DialogPanel, DialogTitle, TransitionChild, TransitionRoot } from '@headlessui/vue'

type Variant = 'default' | 'confirm'

const props = withDefaults(
  defineProps<{
    open: boolean
    title: string
    variant?: Variant
  }>(),
  { variant: 'default' },
)

defineEmits<{ (e: 'close'): void }>()

const variantClass = computed(() => (props.variant === 'confirm' ? 'confirm-modal' : ''))
</script>
