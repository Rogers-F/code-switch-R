<template>
  <div class="page-layout" :class="props.className">
    <!-- 固定的sticky header -->
    <header class="page-header" :class="{ 'page-header--sticky': props.sticky }">
      <div class="page-header-content">
        <!-- 左侧：标题区域 -->
        <div class="page-title-section">
          <button
            v-if="showBackButton"
            class="back-button"
            :aria-label="$t('common.actions.back')"
            @click="goBack"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path d="M15 18l-6-6 6-6" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
            </svg>
          </button>
          <div class="page-title-text">
            <p v-if="eyebrow" class="page-eyebrow">{{ eyebrow }}</p>
            <h1 v-if="title" class="page-title">{{ title }}</h1>
          </div>
        </div>

        <!-- 右侧：操作按钮 -->
        <div class="page-actions">
          <slot name="actions" />
        </div>
      </div>

      <!-- 面包屑（可选） -->
      <div v-if="$slots.breadcrumbs" class="page-breadcrumbs">
        <slot name="breadcrumbs" />
      </div>
    </header>

    <!-- 页面内容区域 -->
    <main class="page-content">
      <slot />
    </main>
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'

// 定义组件名称，使其可以被其他组件正确导入
defineOptions({
  name: 'PageLayout'
})

interface PageLayoutProps {
  eyebrow?: string      // 页面副标题（如 "PROMPTS"）
  title?: string        // 页面主标题
  sticky?: boolean      // header是否sticky，默认true
  showBackButton?: boolean  // 是否显示返回按钮，默认false
  className?: string    // 额外CSS类
}

const props = withDefaults(defineProps<PageLayoutProps>(), {
  sticky: true,
  showBackButton: false
})

const router = useRouter()

const goBack = () => {
  router.go(-1)
}
</script>

<style scoped>
/* 组件样式已在 style.css 中定义 */
</style>
