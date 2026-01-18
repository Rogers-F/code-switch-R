<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getUpdateState, restartApp, type UpdateState } from '../../services/update'
import BaseButton from './BaseButton.vue'

const { t } = useI18n()

const visible = ref(false)
const updateState = ref<UpdateState | null>(null)
const isRestarting = ref(false)
let pollInterval: ReturnType<typeof setInterval> | null = null

const version = computed(() => {
  return updateState.value?.latest_known_version || ''
})

async function checkUpdateReady() {
  try {
    const state = await getUpdateState()
    updateState.value = state

    // 当更新准备好时显示通知
    if (state.update_ready && state.latest_known_version) {
      visible.value = true
    }
  } catch (err) {
    console.error('[UpdateNotification] Failed to get update state:', err)
  }
}

async function installNow() {
  if (isRestarting.value) return

  isRestarting.value = true
  try {
    await restartApp()
  } catch (err) {
    console.error('[UpdateNotification] Failed to restart app:', err)
    isRestarting.value = false
  }
}

function dismiss() {
  visible.value = false
}

onMounted(() => {
  // 初始检查
  checkUpdateReady()

  // 每 30 秒轮询一次更新状态
  pollInterval = setInterval(checkUpdateReady, 30000)
})

onUnmounted(() => {
  if (pollInterval) {
    clearInterval(pollInterval)
    pollInterval = null
  }
})
</script>

<template>
  <Teleport to="body">
    <Transition
      enter-active-class="transition duration-300 ease-out"
      enter-from-class="translate-y-full opacity-0"
      enter-to-class="translate-y-0 opacity-100"
      leave-active-class="transition duration-200 ease-in"
      leave-from-class="translate-y-0 opacity-100"
      leave-to-class="translate-y-full opacity-0"
    >
      <div
        v-if="visible"
        class="update-toast-wrap"
        role="status"
        aria-live="polite"
      >
        <div class="update-toast">
          <div class="update-toast-icon" aria-hidden="true">
            <svg viewBox="0 0 24 24" fill="none">
              <path
                d="M12 2l1.2 3.7c.2.6.7 1 1.3 1.2L18 8l-3.5 1.1c-.6.2-1.1.7-1.3 1.3L12 14l-1.2-3.6c-.2-.6-.7-1.1-1.3-1.3L6 8l3.5-1.1c.6-.2 1.1-.7 1.3-1.3L12 2z"
                stroke="currentColor"
                stroke-width="1.6"
                stroke-linejoin="round"
              />
              <path
                d="M5 14l.8 2.3c.2.6.6 1 1.2 1.2L9 18l-2 0.6c-.6.2-1 .6-1.2 1.2L5 22l-.6-2.2c-.2-.6-.6-1-1.2-1.2L1 18l2.2-.7c.6-.2 1-.6 1.2-1.2L5 14z"
                stroke="currentColor"
                stroke-width="1.6"
                stroke-linejoin="round"
                opacity="0.75"
              />
            </svg>
          </div>

          <div class="update-toast-body">
            <p class="update-toast-title">
              {{ t('update.newVersionReady') }}
            </p>
            <p class="update-toast-subtitle">
              {{ version }}
            </p>
          </div>

          <div class="update-toast-actions">
            <BaseButton
              size="sm"
              type="button"
              :disabled="isRestarting"
              @click="installNow"
            >
              <span v-if="isRestarting" class="update-toast-spinner" aria-hidden="true"></span>
              <span>{{ isRestarting ? t('update.installing') : t('update.installNow') }}</span>
            </BaseButton>
            <BaseButton
              size="sm"
              variant="outline"
              type="button"
              :disabled="isRestarting"
              @click="dismiss"
            >
              {{ t('update.later') }}
            </BaseButton>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
