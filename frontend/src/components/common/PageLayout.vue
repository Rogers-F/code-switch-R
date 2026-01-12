<template>
  <div class="page-layout" :class="props.className">
    <!-- 固定的sticky header -->
    <header class="page-header" :class="{ 'page-header--sticky': props.sticky }">
      <div class="page-header-content">
        <!-- 左侧：标题区域 -->
        <div class="page-title-section">
          <div class="page-title-text">
            <h1 class="page-title">{{ title }}</h1>
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
// 定义组件名称，使其可以被其他组件正确导入
defineOptions({
  name: 'PageLayout'
})

interface PageLayoutProps {
  title: string         // 页面主标题
  sticky?: boolean      // header是否sticky，默认true
  className?: string    // 额外CSS类
}

const props = withDefaults(defineProps<PageLayoutProps>(), {
  sticky: true
})
</script>

<style scoped>
/* 组件样式已在 style.css 中定义 */
</style>
