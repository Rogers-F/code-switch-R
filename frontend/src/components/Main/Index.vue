<template>
  <div class="main-shell">
    <div class="global-actions">
      <p class="global-eyebrow">{{ t('components.main.hero.eyebrow') }}</p>
      <button
        class="ghost-icon github-icon"
        :data-tooltip="getGithubTooltip()"
        @click="handleGithubClick"
      >
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path
            d="M9 19c-4.5 1.5-4.5-2.5-6-3m12 5v-3.87a3.37 3.37 0 00-.94-2.61c3.14-.35 6.44-1.54 6.44-7A5.44 5.44 0 0018 3.77 5.07 5.07 0 0017.91 1S16.73.65 14 2.48a13.38 13.38 0 00-5 0C6.27.65 5.09 1 5.09 1A5.07 5.07 0 005 3.77a5.44 5.44 0 00-1.5 3.76c0 5.42 3.3 6.61 6.44 7A3.37 3.37 0 009 18.13V22"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </button>
      <button
        class="ghost-icon"
        :data-tooltip="t('components.main.controls.theme')"
        @click="toggleTheme"
      >
        <svg v-if="themeIcon === 'sun'" viewBox="0 0 24 24" aria-hidden="true">
          <circle cx="12" cy="12" r="4" stroke="currentColor" stroke-width="1.5" fill="none" />
          <path
            d="M12 3v2m0 14v2m9-9h-2M5 12H3m14.95 6.95-1.41-1.41M7.46 7.46 6.05 6.05m12.9 0-1.41 1.41M7.46 16.54l-1.41 1.41"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
          />
        </svg>
        <svg v-else viewBox="0 0 24 24" aria-hidden="true">
          <path
            d="M21 12.79A9 9 0 1111.21 3a7 7 0 109.79 9.79z"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </button>
      <button
        v-if="showImportButton"
        class="ghost-icon"
        :data-tooltip="importButtonTooltip"
        :disabled="importBusy"
        @click="handleImportClick"
      >
        <svg viewBox="0 0 24 24" aria-hidden="true" :class="{ rotating: importBusy }">
          <path
            d="M12 4v9"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
            fill="none"
          />
          <path
            d="M8.5 10.5l3.5 3.5 3.5-3.5"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
            fill="none"
          />
          <path
            d="M5 19h14"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
            fill="none"
          />
        </svg>
      </button>
      <button
        class="ghost-icon"
        :data-tooltip="t('components.main.controls.settings')"
        @click="goToSettings"
      >
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path
            d="M12 15a3 3 0 100-6 3 3 0 000 6z"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
            fill="none"
          />
          <path
            d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 01-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09a1.65 1.65 0 00-1-1.51 1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09a1.65 1.65 0 001.51-1 1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06a1.65 1.65 0 001.82.33H9a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06a1.65 1.65 0 00-.33 1.82V9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
            fill="none"
          />
        </svg>
      </button>
    </div>
    <div class="contrib-page">
      <!-- 首次使用提示横幅 -->
      <div v-if="showFirstRunPrompt" class="first-run-banner">
        <div class="banner-content">
          <span class="banner-icon">💡</span>
          <span class="banner-text">{{ t('components.main.firstRun.message') }}</span>
        </div>
        <div class="banner-actions">
          <button class="banner-btn primary" @click="goToImportSettings">
            {{ t('components.main.firstRun.goToSettings') }}
          </button>
          <button class="banner-btn" @click="dismissFirstRunPrompt">
            {{ t('components.main.firstRun.dismiss') }}
          </button>
        </div>
      </div>
      <section class="contrib-hero">
        <h1 v-if="showHomeTitle">{{ t('components.main.hero.title') }}</h1>
        <!-- <p class="lead">
          {{ t('components.main.hero.lead') }}
        </p> -->
      </section>

      <section
        v-if="showHeatmap"
        ref="heatmapContainerRef"
        class="contrib-wall"
        :aria-label="t('components.main.heatmap.ariaLabel')"
      >
        <div class="contrib-legend">
          <span>{{ t('components.main.heatmap.legendLow') }}</span>
          <span v-for="level in 5" :key="level" :class="['legend-box', intensityClass(level - 1)]" />
          <span>{{ t('components.main.heatmap.legendHigh') }}</span>
        </div>

        <div class="contrib-grid">
          <div
            v-for="(week, weekIndex) in usageHeatmap"
            :key="weekIndex"
            class="contrib-column"
          >
            <div
              v-for="(day, dayIndex) in week"
              :key="dayIndex"
              class="contrib-cell"
              :class="intensityClass(day.intensity)"
              @mouseenter="showUsageTooltip(day, $event)"
              @mousemove="showUsageTooltip(day, $event)"
              @mouseleave="hideUsageTooltip"
            />
          </div>
        </div>
        <div
          v-if="usageTooltip.visible"
          ref="tooltipRef"
          class="contrib-tooltip"
          :class="usageTooltip.placement"
          :style="{ left: `${usageTooltip.left}px`, top: `${usageTooltip.top}px` }"
        >
          <p class="tooltip-heading">{{ formattedTooltipLabel }}</p>
          <ul class="tooltip-metrics">
            <li v-for="metric in usageTooltipMetrics" :key="metric.key">
              <span class="metric-label">{{ metric.label }}</span>
              <span class="metric-value">{{ metric.value }}</span>
            </li>
          </ul>
        </div>
      </section>

      <section class="automation-section">
      <div class="section-header">
        <div class="tab-group" role="tablist" :aria-label="t('components.main.tabs.ariaLabel')">
          <button
            v-for="(tab, idx) in tabs"
            :key="tab.id"
            class="tab-pill"
            :class="{ active: selectedIndex === idx }"
            role="tab"
            :aria-selected="selectedIndex === idx"
            type="button"
            @click="onTabChange(idx)"
          >
            {{ tab.label }}
          </button>
        </div>
        <div class="section-controls">
          <div class="relay-toggle" :aria-label="currentProxyLabel">
            <div class="relay-switch">
              <label class="mac-switch sm">
                <input
                  type="checkbox"
                  :checked="activeProxyState"
                  :disabled="activeProxyBusy"
                  @change="onProxyToggle"
                />
                <span></span>
              </label>
              <span class="relay-tooltip-content">{{ currentProxyLabel }} · {{ t('components.main.relayToggle.tooltip') }}</span>
            </div>
          </div>
          <button
            class="ghost-icon"
            :data-tooltip="t('components.main.tabs.addCard')"
            @click="openCreateModal"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M12 5v14M5 12h14"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-linecap="round"
                stroke-linejoin="round"
                fill="none"
              />
            </svg>
          </button>
          <button
            class="ghost-icon"
            :class="{ 'rotating': refreshing }"
            :data-tooltip="t('components.main.tabs.refresh')"
            @click="refreshAllData"
            :disabled="refreshing"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M21.5 2v6h-6M2.5 22v-6h6M2 11.5a10 10 0 0118.8-4.3M22 12.5a10 10 0 01-18.8 4.2"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-linecap="round"
                stroke-linejoin="round"
                fill="none"
              />
            </svg>
          </button>
        </div>
      </div>

      <!-- 'others' Tab: CLI 工具选择器 -->
      <div v-if="activeTab === 'others'" class="cli-tool-selector">
        <div class="tool-selector-row">
          <select
            v-model="selectedToolId"
            class="tool-select"
            @change="onToolSelect"
          >
            <option v-if="customCliTools.length === 0" value="" disabled>
              {{ t('components.main.customCli.noTools') }}
            </option>
            <option
              v-for="tool in customCliTools"
              :key="tool.id"
              :value="tool.id"
            >
              {{ tool.name }}
            </option>
          </select>
          <button
            class="ghost-icon add-tool-btn"
            :data-tooltip="t('components.main.customCli.addTool')"
            @click="openCliToolModal"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M12 5v14M5 12h14"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-linecap="round"
                stroke-linejoin="round"
                fill="none"
              />
            </svg>
          </button>
          <button
            v-if="selectedToolId"
            class="ghost-icon"
            :data-tooltip="t('components.main.form.editTitle')"
            @click="editCurrentCliTool"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M11.983 2.25a1.125 1.125 0 011.077.81l.563 2.101a7.482 7.482 0 012.326 1.343l2.08-.621a1.125 1.125 0 011.356.651l1.313 3.207a1.125 1.125 0 01-.442 1.339l-1.86 1.205a7.418 7.418 0 010 2.686l1.86 1.205a1.125 1.125 0 01.442 1.339l-1.313 3.207a1.125 1.125 0 01-1.356.651l-2.08-.621a7.482 7.482 0 01-2.326 1.343l-.563 2.101a1.125 1.125 0 01-1.077.81h-2.634a1.125 1.125 0 01-1.077-.81l-.563-2.101a7.482 7.482 0 01-2.326-1.343l-2.08.621a1.125 1.125 0 01-1.356-.651l-1.313-3.207a1.125 1.125 0 01.442-1.339l1.86-1.205a7.418 7.418 0 010-2.686l-1.86-1.205a1.125 1.125 0 01-.442-1.339l1.313-3.207a1.125 1.125 0 011.356-.651l2.08.621a7.482 7.482 0 012.326-1.343l.563-2.101a1.125 1.125 0 011.077-.81h2.634z"
                fill="none"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
              <path d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
            </svg>
          </button>
          <button
            v-if="selectedToolId"
            class="ghost-icon"
            :data-tooltip="t('components.main.form.actions.delete')"
            @click="deleteCurrentCliTool"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M9 3h6m-7 4h8m-6 0v11m4-11v11M5 7h14l-.867 12.138A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.862L5 7z"
                fill="none"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
            </svg>
          </button>
        </div>
        <p v-if="customCliTools.length === 0" class="no-tools-hint">
          {{ t('components.main.customCli.noTools') }} - {{ t('components.main.customCli.addTool') }}
        </p>
      </div>

      <div class="automation-list" @dragover.prevent>
        <article
          v-for="card in activeCards"
          :key="card.id"
          :ref="el => { if (card.name === highlightedProvider) scrollToCard(el as HTMLElement) }"
          :class="[
            'automation-card',
            { dragging: draggingId === card.id },
            { 'is-last-used': isLastUsedProvider(card.name) },
            { 'is-highlighted': highlightedProvider === card.name }
          ]"
          draggable="true"
          @dragstart="onDragStart(card.id)"
          @dragend="onDragEnd"
          @drop="onDrop(card.id)"
        >
          <!-- 正在使用标签 -->
          <span v-if="isLastUsedProvider(card.name)" class="last-used-badge">
            ✓ {{ t('components.main.providers.lastUsed') }}
          </span>
          <div class="card-leading">
            <div class="card-icon" :style="{ backgroundColor: card.tint, color: card.accent }">
              <span
                v-if="!iconSvg(card.icon)"
                class="icon-fallback"
              >
                {{ vendorInitials(card.name) }}
              </span>
              <span
                v-else
                class="icon-svg"
                v-html="iconSvg(card.icon)"
                aria-hidden="true"
              ></span>
            </div>
            <div class="card-text">
              <div class="card-title-row">
                <p class="card-title">{{ card.name }}</p>
                <!-- 当前使用徽章 -->
                <span
                  v-if="isDirectApplied(card) && !activeProxyState"
                  class="current-use-badge"
                >
                  {{ t('components.main.directApply.currentBadge') }}
                </span>
                <!-- 连通性状态指示器 -->
                <span
                  v-if="card.availabilityMonitorEnabled"
                  class="connectivity-dot"
                  :class="getConnectivityIndicatorClass(card.id)"
                  :title="getConnectivityTooltip(card.id)"
                ></span>
                <span v-if="card.level" class="level-badge scheduling-level" :class="`level-${card.level}`">
                  L{{ card.level }}
                </span>
                <!-- 黑名单等级徽章（始终显示，包括 L0） -->
                <span
                  v-if="getProviderBlacklistStatus(card.name)"
                  :class="[
                    'blacklist-level-badge',
                    `bl-level-${getProviderBlacklistStatus(card.name)!.blacklistLevel}`,
                    { dark: resolvedTheme === 'dark' }
                  ]"
                  :title="t('components.main.blacklist.levelTitle', { level: getProviderBlacklistStatus(card.name)!.blacklistLevel })"
                >
                  BL{{ getProviderBlacklistStatus(card.name)!.blacklistLevel }}
                </span>
                <button
                  v-if="card.officialSite"
                  class="card-site"
                  type="button"
                  @click.stop="openOfficialSite(card.officialSite)"
                >
                  {{ formatOfficialSite(card.officialSite) }}
                </button>
              </div>
              <!-- <p class="card-subtitle">{{ card.apiUrl }}</p> -->
              <p
                v-for="stats in [providerStatDisplay(card.name)]"
                :key="`metrics-${card.id}`"
                class="card-metrics"
              >
                <template v-if="stats.state !== 'ready'">
                  {{ stats.message }}
                </template>
                <template v-else>
                  <span
                    v-if="stats.successRateLabel"
                    class="card-success-rate"
                    :class="stats.successRateClass"
                  >
                    {{ stats.successRateLabel }}
                  </span>
                  <span class="card-metric-separator" aria-hidden="true">·</span>
                  <span >{{ stats.requests }}</span>
                  <span class="card-metric-separator" aria-hidden="true">·</span>
                  <span>{{ stats.tokens }}</span>
                  <span class="card-metric-separator" aria-hidden="true">·</span>
                  <span>{{ stats.cost }}</span>
                </template>
              </p>
              <!-- 黑名单横幅 -->
              <div
                v-if="getProviderBlacklistStatus(card.name)?.isBlacklisted"
                :class="['blacklist-banner', { dark: resolvedTheme === 'dark' }]"
              >
                <div class="blacklist-info">
                  <span class="blacklist-icon">⛔</span>
                  <!-- 等级徽章（L1-L5，黑色/红色） -->
                  <span
                    v-if="getProviderBlacklistStatus(card.name)!.blacklistLevel > 0"
                    :class="['level-badge', `level-${getProviderBlacklistStatus(card.name)!.blacklistLevel}`, { dark: resolvedTheme === 'dark' }]"
                  >
                    L{{ getProviderBlacklistStatus(card.name)!.blacklistLevel }}
                  </span>
                  <span class="blacklist-text">
                    {{ t('components.main.blacklist.blocked') }} |
                    {{ t('components.main.blacklist.remaining') }}:
                    {{ formatBlacklistCountdown(getProviderBlacklistStatus(card.name)!.remainingSeconds) }}
                  </span>
                </div>
                <div class="blacklist-actions">
                  <button
                    class="unblock-btn primary"
                    type="button"
                    @click.stop="handleUnblockAndReset(card.name)"
                    :title="t('components.main.blacklist.unblockAndResetHint')"
                  >
                    {{ t('components.main.blacklist.unblockAndReset') }}
                  </button>
                  <button
                    class="unblock-btn secondary"
                    type="button"
                    @click.stop="handleResetLevel(card.name)"
                    :title="t('components.main.blacklist.resetLevelHint')"
                  >
                    {{ t('components.main.blacklist.resetLevel') }}
                  </button>
                </div>
              </div>
              <!-- 等级徽章（未拉黑但有等级） -->
              <div
                v-else-if="getProviderBlacklistStatus(card.name) && getProviderBlacklistStatus(card.name)!.blacklistLevel > 0"
                class="level-badge-standalone"
              >
                <span
                  :class="['level-badge', `level-${getProviderBlacklistStatus(card.name)!.blacklistLevel}`, { dark: resolvedTheme === 'dark' }]"
                >
                  L{{ getProviderBlacklistStatus(card.name)!.blacklistLevel }}
                </span>
                <span class="level-hint">{{ t('components.main.blacklist.levelHint') }}</span>
                <button
                  class="reset-level-mini"
                  type="button"
                  @click.stop="handleResetLevel(card.name)"
                  :title="t('components.main.blacklist.resetLevelHint')"
                >
                  ✕
                </button>
              </div>
            </div>
          </div>
          <div class="card-actions">
            <label class="mac-switch sm">
              <input type="checkbox" v-model="card.enabled" @change="persistProviders(activeTab)" />
              <span></span>
            </label>
            <!-- 直连应用按钮 -->
            <button
              v-if="activeTab !== 'others'"
              class="ghost-icon direct-apply-btn"
              :class="{ 'is-active': isDirectApplied(card) && !activeProxyState }"
              :disabled="activeProxyState"
              :data-tooltip="activeProxyState ? t('components.main.directApply.proxyEnabled') : (isDirectApplied(card) ? t('components.main.directApply.inUse') : t('components.main.directApply.title'))"
              @click.stop="!isDirectApplied(card) && handleDirectApply(card)"
            >
              <span v-if="isDirectApplied(card) && !activeProxyState" class="apply-text">{{ t('components.main.directApply.inUse') }}</span>
              <svg v-else viewBox="0 0 24 24" aria-hidden="true" class="lightning-icon">
                <path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/>
              </svg>
            </button>
            <button class="ghost-icon" :data-tooltip="t('components.main.form.editTitle')" @click="configure(card)">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path
                  d="M11.983 2.25a1.125 1.125 0 011.077.81l.563 2.101a7.482 7.482 0 012.326 1.343l2.08-.621a1.125 1.125 0 011.356.651l1.313 3.207a1.125 1.125 0 01-.442 1.339l-1.86 1.205a7.418 7.418 0 010 2.686l1.86 1.205a1.125 1.125 0 01.442 1.339l-1.313 3.207a1.125 1.125 0 01-1.356.651l-2.08-.621a7.482 7.482 0 01-2.326 1.343l-.563 2.101a1.125 1.125 0 01-1.077.81h-2.634a1.125 1.125 0 01-1.077-.81l-.563-2.101a7.482 7.482 0 01-2.326-1.343l-2.08.621a1.125 1.125 0 01-1.356-.651l-1.313-3.207a1.125 1.125 0 01.442-1.339l1.86-1.205a7.418 7.418 0 010-2.686l-1.86-1.205a1.125 1.125 0 01-.442-1.339l1.313-3.207a1.125 1.125 0 011.356-.651l2.08.621a7.482 7.482 0 012.326-1.343l.563-2.101a1.125 1.125 0 011.077-.81h2.634z"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.5"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
                <path d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
              </svg>
            </button>
            <button class="ghost-icon" :data-tooltip="t('components.main.controls.duplicate')" @click="handleDuplicate(card)">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path
                  d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.5"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
              </svg>
            </button>
            <button class="ghost-icon" :data-tooltip="t('components.main.form.actions.delete')" @click="requestRemove(card)">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path
                  d="M9 3h6m-7 4h8m-6 0v11m4-11v11M5 7h14l-.867 12.138A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.862L5 7z"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.5"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
              </svg>
            </button>
          </div>
        </article>
      </div>

      <!-- 自定义 CLI 工具配置文件编辑器 -->
      <CustomCliConfigEditor
        v-if="activeTab === 'others' && selectedToolId && selectedCustomCliTool"
        :tool-id="selectedToolId"
        :tool-name="selectedCustomCliTool.name"
        :config-files="selectedCustomCliTool.configFiles"
        @saved="onConfigFileSaved"
      />
      </section>

      <BaseModal
      :open="modalState.open"
      :title="modalState.editingId ? t('components.main.form.editTitle') : t('components.main.form.createTitle')"
      @close="closeModal"
    >
      <form class="vendor-form" @submit.prevent="submitModal">
                <label class="form-field">
                  <span>{{ t('components.main.form.labels.name') }}</span>
                  <BaseInput
                    v-model="modalState.form.name"
                    type="text"
                    :placeholder="t('components.main.form.placeholders.name')"
                    required
                    :disabled="Boolean(modalState.editingId)"
                  />
                </label>

                <label class="form-field">
                  <span class="label-row">
                    {{ t('components.main.form.labels.apiUrl') }}
                    <span v-if="modalState.errors.apiUrl" class="field-error">
                      {{ modalState.errors.apiUrl }}
                    </span>
                  </span>
                  <BaseInput
                    v-model="modalState.form.apiUrl"
                    type="text"
                    :placeholder="t('components.main.form.placeholders.apiUrl')"
                    required
                    :class="{ 'has-error': !!modalState.errors.apiUrl }"
                  />
                </label>

                <label class="form-field">
                  <span>{{ t('components.main.form.labels.officialSite') }}</span>
                  <BaseInput
                    v-model="modalState.form.officialSite"
                    type="text"
                    :placeholder="t('components.main.form.placeholders.officialSite')"
                  />
                </label>

                <label class="form-field">
                  <span>{{ t('components.main.form.labels.apiKey') }}</span>
                  <BaseInput
                    v-model="modalState.form.apiKey"
                    type="text"
                    :placeholder="t('components.main.form.placeholders.apiKey')"
                  />
                </label>

                <!-- API 端点（可选）-->
                <label class="form-field">
                  <span>{{ t('components.main.form.labels.apiEndpoint') }}</span>
                  <BaseInput
                    v-model="modalState.form.apiEndpoint"
                    type="text"
                    :placeholder="t('components.main.form.placeholders.apiEndpoint')"
                  />
                  <span class="field-hint">{{ t('components.main.form.hints.apiEndpoint') }}</span>
                </label>

                <!-- 上游协议类型 -->
                <div class="form-field">
                  <span>{{ t('components.main.form.labels.upstreamProtocol') }}</span>
                  <Listbox v-model="modalState.form.upstreamProtocol" v-slot="{ open }">
                    <div class="level-select">
                      <ListboxButton class="level-select-button">
                        <span class="level-label">
                          {{ upstreamProtocolOptions.find((item) => item.value === modalState.form.upstreamProtocol)?.label || modalState.form.upstreamProtocol }}
                        </span>
                        <svg viewBox="0 0 20 20" aria-hidden="true">
                          <path d="M6 8l4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none" />
                        </svg>
                      </ListboxButton>
                      <ListboxOptions v-if="open" class="level-select-options">
                        <ListboxOption
                          v-for="option in upstreamProtocolOptions"
                          :key="option.value"
                          :value="option.value"
                          v-slot="{ active, selected }"
                        >
                          <div :class="['level-option', { active, selected }]">
                            <span class="level-name">{{ option.label }}</span>
                            <span class="level-desc">{{ option.desc }}</span>
                          </div>
                        </ListboxOption>
                      </ListboxOptions>
                    </div>
                  </Listbox>
                  <span class="field-hint">{{ t('components.main.form.hints.upstreamProtocol') }}</span>
                </div>

                <!-- 认证方式 -->
                <div class="form-field">
                  <span>{{ t('components.main.form.labels.connectivityAuthType') }}</span>
                  <Listbox v-model="selectedAuthType" v-slot="{ open }">
                    <div class="level-select">
                      <ListboxButton class="level-select-button">
                        <span class="level-label">
                          {{ authTypeOptions.find((item) => item.value === selectedAuthType)?.label || selectedAuthType }}
                        </span>
                        <svg viewBox="0 0 20 20" aria-hidden="true">
                          <path d="M6 8l4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none" />
                        </svg>
                      </ListboxButton>
                      <ListboxOptions v-if="open" class="level-select-options">
                        <ListboxOption
                          v-for="option in authTypeOptions"
                          :key="option.value"
                          :value="option.value"
                          v-slot="{ active, selected }"
                        >
                          <div :class="['level-option', { active, selected }]">
                            <span class="level-name">{{ option.label }}</span>
                          </div>
                        </ListboxOption>
                      </ListboxOptions>
                    </div>
                  </Listbox>
                  <BaseInput
                    v-model="customAuthHeader"
                    type="text"
                    :placeholder="t('components.main.form.placeholders.customAuthHeader')"
                    class="mt-2"
                  />
                  <span class="field-hint">{{ t('components.main.form.hints.connectivityAuthType') }}</span>
                </div>

                <div class="form-field">
                  <span>{{ t('components.main.form.labels.icon') }}</span>
                  <Listbox v-model="modalState.form.icon" v-slot="{ open }" class="w-full">
                    <div class="icon-select">
                      <ListboxButton class="icon-select-button">
                        <span class="icon-preview" v-html="iconSvg(modalState.form.icon)" aria-hidden="true"></span>
                        <span class="icon-select-label">{{ modalState.form.icon }}</span>
                        <svg viewBox="0 0 20 20" aria-hidden="true">
                          <path d="M6 8l4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none" />
                        </svg>
                      </ListboxButton>
                      <ListboxOptions v-if="open" class="icon-select-options">
                        <div class="icon-search-wrapper">
                          <input
                            v-model="iconSearchQuery"
                            type="text"
                            class="icon-search-input"
                            :placeholder="t('components.main.form.placeholders.searchIcon')"
                            @click.stop
                            @keydown.stop
                          />
                        </div>
                        <ListboxOption
                          v-for="iconName in filteredIconOptions"
                          :key="iconName"
                          :value="iconName"
                          v-slot="{ active, selected }"
                        >
                          <div :class="['icon-option', { active, selected }]">
                            <span class="icon-preview" v-html="iconSvg(iconName)" aria-hidden="true"></span>
                            <span class="icon-name">{{ iconName }}</span>
                          </div>
                        </ListboxOption>
                        <div v-if="filteredIconOptions.length === 0" class="icon-no-results">
                          {{ t('components.main.form.noIconResults') }}
                        </div>
                      </ListboxOptions>
                    </div>
                  </Listbox>
                </div>

                <div class="form-field">
                  <span>{{ t('components.main.form.labels.level') }}</span>
                  <Listbox v-model="modalState.form.level" v-slot="{ open }">
                    <div class="level-select">
                      <ListboxButton class="level-select-button">
                        <span class="level-badge" :class="`level-${modalState.form.level || 1}`">
                          L{{ modalState.form.level || 1 }}
                        </span>
                        <span class="level-label">
                          Level {{ modalState.form.level || 1 }} - {{ getLevelDescription(modalState.form.level || 1) }}
                        </span>
                        <svg viewBox="0 0 20 20" aria-hidden="true">
                          <path d="M6 8l4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none" />
                        </svg>
                      </ListboxButton>
                      <ListboxOptions v-if="open" class="level-select-options">
                        <ListboxOption
                          v-for="lvl in 10"
                          :key="lvl"
                          :value="lvl"
                          v-slot="{ active, selected }"
                        >
                          <div :class="['level-option', { active, selected }]">
                            <span class="level-badge" :class="`level-${lvl}`">L{{ lvl }}</span>
                            <span class="level-name">Level {{ lvl }} - {{ getLevelDescription(lvl) }}</span>
                          </div>
                        </ListboxOption>
                      </ListboxOptions>
                    </div>
                  </Listbox>
                  <span class="field-hint">{{ t('components.main.form.hints.level') }}</span>
                </div>

                <div class="form-field">
                  <ModelWhitelistEditor v-model="modalState.form.supportedModels" />
                </div>

                <div class="form-field">
                  <ModelMappingEditor v-model="modalState.form.modelMapping" />
                </div>

                <div class="form-field">
                  <CLIConfigEditor
                    :platform="activeTab as CLIPlatform"
                    v-model="modalState.form.cliConfig"
                    :provider-config="{
                      apiKey: modalState.form.apiKey,
                      baseUrl: modalState.form.apiUrl
                    }"
                  />
                </div>

                <div class="form-field switch-field">
                  <span>{{ t('components.main.form.labels.enabled') }}</span>
                  <div class="switch-inline">
                    <label class="mac-switch">
                      <input type="checkbox" v-model="modalState.form.enabled" />
                      <span></span>
                    </label>
                    <span class="switch-text">
                      {{ modalState.form.enabled ? t('components.main.form.switch.on') : t('components.main.form.switch.off') }}
                    </span>
                  </div>
                </div>

                <!-- 可用性监控配置 -->
                <div class="form-field switch-field">
                  <span>{{ t('components.main.form.labels.availabilityMonitor') }}</span>
                  <div class="switch-inline">
                    <label class="mac-switch">
                      <input type="checkbox" v-model="modalState.form.availabilityMonitorEnabled" />
                      <span></span>
                    </label>
                    <span class="switch-text">
                      {{ modalState.form.availabilityMonitorEnabled ? t('components.main.form.switch.on') : t('components.main.form.switch.off') }}
                    </span>
                  </div>
                  <span class="field-hint">{{ t('components.main.form.hints.availabilityMonitor') }}</span>
                </div>

                <!-- 连通性自动拉黑 -->
                <div v-if="modalState.form.availabilityMonitorEnabled" class="form-field switch-field">
                  <span>{{ t('components.main.form.labels.connectivityAutoBlacklist') }}</span>
                  <div class="switch-inline">
                    <label class="mac-switch">
                      <input type="checkbox" v-model="modalState.form.connectivityAutoBlacklist" />
                      <span></span>
                    </label>
                    <span class="switch-text">
                      {{ modalState.form.connectivityAutoBlacklist ? t('components.main.form.switch.on') : t('components.main.form.switch.off') }}
                    </span>
                  </div>
                  <span class="field-hint">{{ t('components.main.form.hints.connectivityAutoBlacklist') }}</span>
                </div>

                <!-- 高级配置提示 -->
                <div v-if="modalState.form.availabilityMonitorEnabled" class="form-field">
                  <span class="field-hint" style="color: #6b7280;">
                    💡 {{ t('components.main.form.hints.availabilityAdvancedConfig') }}
                  </span>
                </div>

                <footer class="form-actions">
                  <BaseButton variant="outline" type="button" @click="closeModal">
                    {{ t('components.main.form.actions.cancel') }}
                  </BaseButton>
                  <BaseButton type="submit">
                    {{ t('components.main.form.actions.save') }}
                  </BaseButton>
                  <!-- 保存并应用：仅在编辑模式、非代理模式、非 others 平台时显示 -->
                  <BaseButton
                    v-if="modalState.editingId && modalState.tabId !== 'others' && !activeProxyState"
                    type="button"
                    variant="primary"
                    @click="submitAndApplyModal"
                  >
                    {{ t('components.main.form.actions.saveAndApply') }}
                  </BaseButton>
                </footer>
      </form>
      </BaseModal>
      <BaseModal
      :open="confirmState.open"
      :title="t('components.main.form.confirmDeleteTitle')"
      variant="confirm"
      @close="closeConfirm"
    >
      <div class="confirm-body">
        <p>
          {{ t('components.main.form.confirmDeleteMessage', { name: confirmState.card?.name ?? '' }) }}
        </p>
      </div>
      <footer class="form-actions confirm-actions">
        <BaseButton variant="outline" type="button" @click="closeConfirm">
          {{ t('components.main.form.actions.cancel') }}
        </BaseButton>
        <BaseButton variant="danger" type="button" @click="confirmRemove">
          {{ t('components.main.form.actions.delete') }}
        </BaseButton>
      </footer>
      </BaseModal>

      <!-- CLI 工具配置模态框 -->
      <BaseModal
        :open="cliToolModalState.open"
        :title="cliToolModalState.editingId ? t('components.main.customCli.editTitle') : t('components.main.customCli.createTitle')"
        @close="closeCliToolModal"
      >
        <form class="vendor-form cli-tool-form" @submit.prevent="submitCliToolModal">
          <label class="form-field">
            <span>{{ t('components.main.customCli.toolName') }}</span>
            <BaseInput
              v-model="cliToolModalState.form.name"
              type="text"
              :placeholder="t('components.main.customCli.toolNamePlaceholder')"
              required
            />
          </label>

          <!-- 配置文件列表 -->
          <div class="form-field">
            <div class="field-header">
              <span>{{ t('components.main.customCli.configFiles') }}</span>
              <button type="button" class="add-btn" @click="addConfigFile">
                <svg viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" fill="none" />
                </svg>
              </button>
            </div>
            <div class="config-files-list">
              <div
                v-for="(cf, idx) in cliToolModalState.form.configFiles"
                :key="cf.id"
                class="config-file-item"
              >
                <div class="config-file-row">
                  <BaseInput
                    v-model="cf.label"
                    class="config-label-input"
                    :placeholder="t('components.main.customCli.labelPlaceholder')"
                  />
                  <select v-model="cf.format" class="config-format-select">
                    <option value="json">JSON</option>
                    <option value="toml">TOML</option>
                    <option value="env">ENV</option>
                  </select>
                  <label class="primary-checkbox">
                    <input type="checkbox" v-model="cf.isPrimary" />
                    <span>{{ t('components.main.customCli.primary') }}</span>
                  </label>
                  <button
                    type="button"
                    class="remove-btn"
                    :disabled="cliToolModalState.form.configFiles.length <= 1"
                    @click="removeConfigFile(idx)"
                  >
                    <svg viewBox="0 0 24 24" aria-hidden="true">
                      <path d="M6 18L18 6M6 6l12 12" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" fill="none" />
                    </svg>
                  </button>
                </div>
                <BaseInput
                  v-model="cf.path"
                  class="config-path-input"
                  :placeholder="t('components.main.customCli.pathPlaceholder')"
                />
              </div>
            </div>
          </div>

          <!-- 代理注入配置 -->
          <div class="form-field">
            <div class="field-header">
              <span>{{ t('components.main.customCli.proxySettings') }}</span>
              <button type="button" class="add-btn" @click="addProxyInjection">
                <svg viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" fill="none" />
                </svg>
              </button>
            </div>
            <div class="proxy-injection-list">
              <div
                v-for="(pi, idx) in cliToolModalState.form.proxyInjection"
                :key="idx"
                class="proxy-injection-item"
              >
                <div class="proxy-injection-row">
                  <select v-model="pi.targetFileId" class="target-file-select">
                    <option value="">{{ t('components.main.customCli.selectConfigFile') }}</option>
                    <option
                      v-for="cf in cliToolModalState.form.configFiles"
                      :key="cf.id"
                      :value="cf.id"
                    >
                      {{ cf.label || cf.path || t('components.main.customCli.unnamed') }}
                    </option>
                  </select>
                  <button
                    type="button"
                    class="remove-btn"
                    :disabled="cliToolModalState.form.proxyInjection.length <= 1"
                    @click="removeProxyInjection(idx)"
                  >
                    <svg viewBox="0 0 24 24" aria-hidden="true">
                      <path d="M6 18L18 6M6 6l12 12" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" fill="none" />
                    </svg>
                  </button>
                </div>
                <div class="proxy-fields-row">
                  <BaseInput
                    v-model="pi.baseUrlField"
                    class="proxy-field-input"
                    :placeholder="t('components.main.customCli.baseUrlFieldPlaceholder')"
                  />
                  <BaseInput
                    v-model="pi.authTokenField"
                    class="proxy-field-input"
                    :placeholder="t('components.main.customCli.authTokenFieldPlaceholder')"
                  />
                </div>
              </div>
            </div>
            <p class="field-hint">{{ t('components.main.customCli.proxyHint') }}</p>
          </div>

          <footer class="form-actions">
            <BaseButton variant="outline" type="button" @click="closeCliToolModal">
              {{ t('components.main.form.actions.cancel') }}
            </BaseButton>
            <BaseButton type="submit">
              {{ t('components.main.form.actions.save') }}
            </BaseButton>
          </footer>
        </form>
      </BaseModal>

      <!-- CLI 工具删除确认框 -->
      <BaseModal
        :open="cliToolConfirmState.open"
        :title="t('components.main.customCli.deleteTitle')"
        variant="confirm"
        @close="closeCliToolConfirm"
      >
        <div class="confirm-body">
          <p>{{ t('components.main.customCli.deleteMessage', { name: cliToolConfirmState.tool?.name ?? '' }) }}</p>
        </div>
        <footer class="form-actions confirm-actions">
          <BaseButton variant="outline" type="button" @click="closeCliToolConfirm">
            {{ t('components.main.form.actions.cancel') }}
          </BaseButton>
          <BaseButton variant="danger" type="button" @click="confirmDeleteCliTool">
            {{ t('components.main.form.actions.delete') }}
          </BaseButton>
        </footer>
      </BaseModal>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Listbox, ListboxButton, ListboxOptions, ListboxOption } from '@headlessui/vue'
import { Browser, Call, Events } from '@wailsio/runtime'
import { type UsageHeatmapDay } from '../../data/usageHeatmap'
import { useAdaptiveHeatmap } from '../../composables/useAdaptiveHeatmap'
import { automationCardGroups, createAutomationCards, type AutomationCard } from '../../data/cards'
import lobeIcons from '../../icons/lobeIconMap'
import BaseButton from '../common/BaseButton.vue'
import BaseModal from '../common/BaseModal.vue'
import BaseInput from '../common/BaseInput.vue'
import ModelWhitelistEditor from '../common/ModelWhitelistEditor.vue'
import ModelMappingEditor from '../common/ModelMappingEditor.vue'
import CLIConfigEditor from '../common/CLIConfigEditor.vue'
import CustomCliConfigEditor from '../common/CustomCliConfigEditor.vue'
import { LoadProviders, SaveProviders, DuplicateProvider } from '../../../bindings/codeswitch/services/providerservice'
import { GetProviders as GetGeminiProviders, UpdateProvider as UpdateGeminiProvider, AddProvider as AddGeminiProvider, DeleteProvider as DeleteGeminiProvider, ReorderProviders as ReorderGeminiProviders } from '../../../bindings/codeswitch/services/geminiservice'
import { GeminiProvider } from '../../../bindings/codeswitch/services/models'
import { fetchProxyStatus, enableProxy, disableProxy } from '../../services/claudeSettings'
import { fetchGeminiProxyStatus, enableGeminiProxy, disableGeminiProxy } from '../../services/geminiSettings'
import { fetchProviderDailyStats, type ProviderDailyStat } from '../../services/logs'
import { fetchCurrentVersion } from '../../services/version'
import { fetchAppSettings, type AppSettings } from '../../services/appSettings'
import { getCurrentTheme, setTheme, type ThemeMode } from '../../utils/ThemeManager'
import { useRouter } from 'vue-router'
import { fetchConfigImportStatus, importFromCcSwitch, isFirstRun, markFirstRunDone, type ConfigImportStatus } from '../../services/configImport'
import { showToast } from '../../utils/toast'
import { extractErrorMessage } from '../../utils/error'
import { getBlacklistStatus, manualUnblock, type BlacklistStatus } from '../../services/blacklist'
import { saveCLIConfig, type CLIPlatform } from '../../services/cliConfig'
import {
  listCustomCliTools,
  createCustomCliTool,
  updateCustomCliTool,
  deleteCustomCliTool,
  getCustomCliProxyStatus,
  enableCustomCliProxy,
  disableCustomCliProxy,
  type CustomCliTool,
  type ConfigFile,
  type ProxyInjection,
} from '../../services/customCliService'
import {
  getConnectivityResults,
  StatusAvailable,
  StatusDegraded,
  StatusUnavailable,
  StatusMissing,
  getStatusColorClass,
  type ConnectivityResult,
} from '../../services/connectivity'
import {
  getLatestResults,
  HealthStatus,
  type ProviderTimeline,
} from '../../services/healthcheck'

const { t, locale } = useI18n()
const router = useRouter()
const themeMode = ref<ThemeMode>(getCurrentTheme())
const resolvedTheme = computed(() => {
  if (themeMode.value === 'systemdefault') {
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  }
  return themeMode.value
})
const themeIcon = computed(() => (resolvedTheme.value === 'dark' ? 'moon' : 'sun'))
const releasePageUrl = 'https://github.com/Rogers-F/code-switch-R/releases'
const releaseApiUrl = 'https://api.github.com/repos/Rogers-F/code-switch-R/releases/latest'

const heatmapContainerRef = ref<HTMLElement | null>(null)
// 使用自适应热力图 composable
const {
  displayData: usageHeatmap,
  init: initHeatmap,
  cleanup: cleanupHeatmap,
  reload: reloadHeatmap,
} = useAdaptiveHeatmap(heatmapContainerRef)
const tooltipRef = ref<HTMLElement | null>(null)
const proxyStates = reactive<Record<ProviderTab, boolean>>({
  claude: false,
  codex: false,
  gemini: false,
  others: false,
})
const proxyBusy = reactive<Record<ProviderTab, boolean>>({
  claude: false,
  codex: false,
  gemini: false,
  others: false,
})

// 直连应用状态
const directAppliedIds = reactive<Record<ProviderTab, string | number | null>>({
  claude: null,
  codex: null,
  gemini: null,
  others: null,
})

const refreshDirectAppliedStatus = async (tab: ProviderTab = activeTab.value) => {
  if (tab === 'others') return

  try {
    let id: string | number | null = null
    if (tab === 'claude') {
      id = await Call.ByName('codeswitch/services.ClaudeSettingsService.GetDirectAppliedProviderID')
    } else if (tab === 'codex') {
      id = await Call.ByName('codeswitch/services.CodexSettingsService.GetDirectAppliedProviderID')
    } else if (tab === 'gemini') {
      id = await Call.ByName('codeswitch/services.GeminiService.GetDirectAppliedProviderID')
    }
    directAppliedIds[tab] = id
  } catch (error) {
    console.error(`Failed to get direct applied status for ${tab}`, error)
  }
}

const handleDirectApply = async (card: AutomationCard) => {
  if (activeProxyState.value) return
  const tab = activeTab.value
  try {
    if (tab === 'claude') {
      await Call.ByName('codeswitch/services.ClaudeSettingsService.ApplySingleProvider', card.id)
    } else if (tab === 'codex') {
      await Call.ByName('codeswitch/services.CodexSettingsService.ApplySingleProvider', card.id)
    } else if (tab === 'gemini') {
      // Gemini 使用字符串 ID，需要从 cache 中找到原始 provider
      const index = cards.gemini.findIndex(c => c.id === card.id)
      if (index === -1 || !geminiProvidersCache.value[index]) return
      const realId = geminiProvidersCache.value[index].id
      await Call.ByName('codeswitch/services.GeminiService.ApplySingleProvider', realId)
    }
    await refreshDirectAppliedStatus(tab)
    showToast(t('components.main.directApply.success', { name: card.name }), 'success')
  } catch (error) {
    console.error('Direct apply failed', error)
    showToast(t('components.main.directApply.failed'), 'error')
  }
}

const isDirectApplied = (card: AutomationCard) => {
  const appliedId = directAppliedIds[activeTab.value]
  if (appliedId === null) return false

  if (activeTab.value === 'gemini') {
    const index = cards.gemini.findIndex(c => c.id === card.id)
    if (index === -1 || !geminiProvidersCache.value[index]) return false
    return geminiProvidersCache.value[index].id === appliedId
  }
  return card.id === appliedId
}

const providerStatsMap = reactive<Record<ProviderTab, Record<string, ProviderDailyStat>>>({
  claude: {},
  codex: {},
  gemini: {},
  others: {},
})
const providerStatsLoading = reactive<Record<ProviderTab, boolean>>({
  claude: false,
  codex: false,
  gemini: false,
  others: false,
})
const providerStatsLoaded = reactive<Record<ProviderTab, boolean>>({
  claude: false,
  codex: false,
  gemini: false,
  others: false,
})
let providerStatsTimer: number | undefined
const showHeatmap = ref(true)
const showHomeTitle = ref(true)
const mcpIcon = lobeIcons['mcp'] ?? ''
const appVersion = ref('')
const importStatus = ref<ConfigImportStatus | null>(null)
const importBusy = ref(false)
const showFirstRunPrompt = ref(false)

// 自定义 CLI 工具状态
const customCliTools = ref<CustomCliTool[]>([])
const selectedToolId = ref<string | null>(null)
const customCliProxyStates = reactive<Record<string, boolean>>({})  // toolId -> enabled

// 当前选中的 CLI 工具（计算属性）
const selectedCustomCliTool = computed(() => {
  if (!selectedToolId.value) return null
  return customCliTools.value.find(t => t.id === selectedToolId.value) || null
})

// 配置文件保存成功后的回调
const onConfigFileSaved = () => {
  // 配置文件保存成功，可以在这里添加额外逻辑（如刷新状态）
  console.log('[CustomCliConfigEditor] Config file saved')
}

// 黑名单状态
const blacklistStatusMap = reactive<Record<ProviderTab, Record<string, BlacklistStatus>>>({
  claude: {},
  codex: {},
  gemini: {},
  others: {},
})
let blacklistTimer: number | undefined

// 连通性状态（已废弃，保留用于兼容）
const connectivityResultsMap = reactive<Record<ProviderTab, Record<number, ConnectivityResult>>>({
  claude: {},
  codex: {},
  gemini: {},
  others: {},
})

// 可用性监控状态（新）
const availabilityResultsMap = reactive<Record<ProviderTab, Record<number, ProviderTimeline>>>({
  claude: {},
  codex: {},
  gemini: {},
  others: {},
})

// 最后使用的供应商（用于高亮显示）
// @author sm
interface LastUsedProvider {
  platform: string
  provider_name: string
  updated_at: number
}
const lastUsedProviders = reactive<Record<string, LastUsedProvider | null>>({
  claude: null,
  codex: null,
  gemini: null,
  others: null,
})
// 高亮闪烁的供应商名称
const highlightedProvider = ref<string | null>(null)
let highlightTimer: number | undefined

const showImportButton = computed(() => {
  const status = importStatus.value
  if (!status) return false
  return status.config_exists && (status.pending_providers || status.pending_mcp)
})

const importButtonTooltip = computed(() => {
  if (!showImportButton.value) {
    return t('components.main.controls.import')
  }
  const status = importStatus.value
  if (!status) {
    return t('components.main.controls.import')
  }
  return t('components.main.importConfig.tooltip', {
    providers: status.pending_provider_count,
    servers: status.pending_mcp_count,
  })
})

const intensityClass = (value: number) => `gh-level-${value}`

type TooltipPlacement = 'above' | 'below'

const usageTooltip = reactive({
  visible: false,
  label: '',
  dateKey: '',
  left: 0,
  top: 0,
  placement: 'above' as TooltipPlacement,
  requests: 0,
  inputTokens: 0,
  outputTokens: 0,
  reasoningTokens: 0,
  cost: 0,
})

const formatMetric = (value: number) => value.toLocaleString()

/**
 * 格式化 token 数值，支持 k/M/B 单位换算
 * @author sm
 */
const formatTokenNumber = (value: number) => {
  if (value >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(2)}B`
  }
  if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(2)}M`
  }
  if (value >= 1_000) {
    return `${(value / 1_000).toFixed(2)}k`
  }
  return value.toLocaleString()
}

const tooltipDateFormatter = computed(() =>
  new Intl.DateTimeFormat(locale.value || 'en', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
)

const currencyFormatter = computed(() =>
  new Intl.NumberFormat(locale.value || 'en', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
)

const formattedTooltipLabel = computed(() => {
  if (!usageTooltip.dateKey) return usageTooltip.label
  const date = new Date(usageTooltip.dateKey)
  if (Number.isNaN(date.getTime())) {
    return usageTooltip.label
  }
  return tooltipDateFormatter.value.format(date)
})

const formattedTooltipAmount = computed(() =>
  currencyFormatter.value.format(Math.max(usageTooltip.cost, 0))
)

const usageTooltipMetrics = computed(() => [
  {
    key: 'cost',
    label: t('components.main.heatmap.metrics.cost'),
    value: formattedTooltipAmount.value,
  },
  {
    key: 'requests',
    label: t('components.main.heatmap.metrics.requests'),
    value: formatMetric(usageTooltip.requests),
  },
  {
    key: 'inputTokens',
    label: t('components.main.heatmap.metrics.inputTokens'),
    value: formatTokenNumber(usageTooltip.inputTokens),
  },
  {
    key: 'outputTokens',
    label: t('components.main.heatmap.metrics.outputTokens'),
    value: formatTokenNumber(usageTooltip.outputTokens),
  },
  {
    key: 'reasoningTokens',
    label: t('components.main.heatmap.metrics.reasoningTokens'),
    value: formatTokenNumber(usageTooltip.reasoningTokens),
  },
])

const clamp = (value: number, min: number, max: number) => {
  if (max <= min) return min
  return Math.min(Math.max(value, min), max)
}

const TOOLTIP_DEFAULT_WIDTH = 220
const TOOLTIP_DEFAULT_HEIGHT = 120
const TOOLTIP_VERTICAL_OFFSET = 12
const TOOLTIP_HORIZONTAL_MARGIN = 20
const TOOLTIP_VERTICAL_MARGIN = 24

const getTooltipSize = () => {
  const rect = tooltipRef.value?.getBoundingClientRect()
  return {
    width: rect?.width ?? TOOLTIP_DEFAULT_WIDTH,
    height: rect?.height ?? TOOLTIP_DEFAULT_HEIGHT,
  }
}

const viewportSize = () => {
  if (typeof window !== 'undefined') {
    return { width: window.innerWidth, height: window.innerHeight }
  }
  if (typeof document !== 'undefined' && document.documentElement) {
    return {
      width: document.documentElement.clientWidth,
      height: document.documentElement.clientHeight,
    }
  }
  return {
    width: heatmapContainerRef.value?.clientWidth ?? 0,
    height: heatmapContainerRef.value?.clientHeight ?? 0,
  }
}

const showUsageTooltip = (day: UsageHeatmapDay, event: MouseEvent) => {
  const target = event.currentTarget as HTMLElement | null
  const cellRect = target?.getBoundingClientRect()
  if (!cellRect) return
  usageTooltip.label = day.label
  usageTooltip.dateKey = day.dateKey
  usageTooltip.requests = day.requests
  usageTooltip.inputTokens = day.inputTokens
  usageTooltip.outputTokens = day.outputTokens
  usageTooltip.reasoningTokens = day.reasoningTokens
  usageTooltip.cost = day.cost
  const { width: tooltipWidth, height: tooltipHeight } = getTooltipSize()
  const { width: viewportWidth, height: viewportHeight } = viewportSize()
  const centerX = cellRect.left + cellRect.width / 2
  const halfWidth = tooltipWidth / 2
  const minLeft = TOOLTIP_HORIZONTAL_MARGIN + halfWidth
  const maxLeft = viewportWidth > 0 ? viewportWidth - halfWidth - TOOLTIP_HORIZONTAL_MARGIN : centerX
  usageTooltip.left = clamp(centerX, minLeft, maxLeft)

  const anchorTop = cellRect.top
  const anchorBottom = cellRect.bottom
  const canShowAbove = anchorTop - tooltipHeight - TOOLTIP_VERTICAL_OFFSET >= TOOLTIP_VERTICAL_MARGIN
  const viewportBottomLimit = viewportHeight > 0 ? viewportHeight - tooltipHeight - TOOLTIP_VERTICAL_MARGIN : anchorBottom
  const shouldPlaceBelow = !canShowAbove
  usageTooltip.placement = shouldPlaceBelow ? 'below' : 'above'
  const desiredTop = shouldPlaceBelow
    ? anchorBottom + TOOLTIP_VERTICAL_OFFSET
    : anchorTop - tooltipHeight - TOOLTIP_VERTICAL_OFFSET
  usageTooltip.top = clamp(desiredTop, TOOLTIP_VERTICAL_MARGIN, viewportBottomLimit)
  usageTooltip.visible = true
}

const hideUsageTooltip = () => {
  usageTooltip.visible = false
}

const loadAppSettings = async () => {
  try {
    const data: AppSettings = await fetchAppSettings()
    showHeatmap.value = data?.show_heatmap ?? true
    showHomeTitle.value = data?.show_home_title ?? true
  } catch (error) {
    console.error('failed to load app settings', error)
    showHeatmap.value = true
    showHomeTitle.value = true
    // 加载应用设置失败时提示用户
    showToast(t('components.main.errors.loadAppSettingsFailed'), 'warning')
  }
}

const loadAppVersion = async () => {
  try {
    const version = await fetchCurrentVersion()
    appVersion.value = version || ''
  } catch (error) {
    console.error('failed to load app version', error)
  }
}

const handleAppSettingsUpdated = () => {
  void loadAppSettings()
}

const normalizeProviderKey = (value: string) => value?.trim().toLowerCase() ?? ''

const normalizeVersion = (value: string) => value.replace(/^v/i, '').trim()

const compareVersions = (current: string, remote: string) => {
  const curParts = normalizeVersion(current).split('.').map((part) => parseInt(part, 10) || 0)
  const remoteParts = normalizeVersion(remote).split('.').map((part) => parseInt(part, 10) || 0)
  const maxLen = Math.max(curParts.length, remoteParts.length)
  for (let i = 0; i < maxLen; i++) {
    const cur = curParts[i] ?? 0
    const rem = remoteParts[i] ?? 0
    if (cur === rem) continue
    return cur < rem ? -1 : 1
  }
  return 0
}


const tabs = [
  { id: 'claude', label: 'Claude Code' },
  { id: 'codex', label: 'Codex' },
  { id: 'gemini', label: 'Gemini' },
  { id: 'others', label: '其他' },
] as const
type ProviderTab = (typeof tabs)[number]['id']
const providerTabIds = tabs.map((tab) => tab.id) as ProviderTab[]

const cards = reactive<Record<ProviderTab, AutomationCard[]>>({
  claude: createAutomationCards(automationCardGroups.claude),
  codex: createAutomationCards(automationCardGroups.codex),
  gemini: [],
  others: [],
})
const draggingId = ref<number | null>(null)

// Gemini Provider 到 AutomationCard 的转换
const geminiToCard = (provider: GeminiProvider, index: number): AutomationCard => ({
  id: 300 + index, // Gemini 使用 300+ 的 ID 范围
  name: provider.name,
  apiUrl: provider.baseUrl || '',
  apiKey: provider.apiKey || '',
  officialSite: provider.websiteUrl || '',
  icon: 'gemini',
  tint: 'rgba(251, 146, 60, 0.18)',
  accent: '#fb923c',
  enabled: provider.enabled,
  level: provider.level || 1,
  // 可用性监控配置（Gemini 暂不支持，使用默认值）
  availabilityMonitorEnabled: false,
  connectivityAutoBlacklist: false,
  availabilityConfig: undefined,
})

// AutomationCard 到 Gemini Provider 的转换
const cardToGemini = (card: AutomationCard, original: GeminiProvider): GeminiProvider => new GeminiProvider({
  ...original,
  name: card.name,
  baseUrl: card.apiUrl,
  apiKey: card.apiKey,
  websiteUrl: card.officialSite,
  enabled: card.enabled,
  level: card.level || 1,
  // 注意：Gemini 不支持可用性监控配置，这些字段不会保存
})

const serializeProviders = (providers: AutomationCard[]) =>
  providers.map((provider) => ({
    ...provider,
    // 确保可用性配置正确序列化
    availabilityMonitorEnabled: !!provider.availabilityMonitorEnabled,
    connectivityAutoBlacklist: !!provider.connectivityAutoBlacklist,
    availabilityConfig: provider.availabilityConfig
      ? {
          testModel: provider.availabilityConfig.testModel || '',
          testEndpoint: provider.availabilityConfig.testEndpoint || '',
          timeout: provider.availabilityConfig.timeout || 15000,
        }
      : undefined,
    // 清除旧连通性字段（避免再次写入配置文件）
    connectivityCheck: false,
    connectivityTestModel: '',
    connectivityTestEndpoint: '',
    // 保留认证方式配置（已从废弃字段升级为活跃字段）
    connectivityAuthType: provider.connectivityAuthType || '',
  }))

// 生成 custom CLI 工具的 provider kind（后端需要 "custom:{toolId}" 格式）
const getCustomProviderKind = (toolId: string): string => `custom:${toolId}`

// 存储 Gemini 原始数据，用于转换回去
const geminiProvidersCache = ref<GeminiProvider[]>([])

const persistProviders = async (tabId: ProviderTab): Promise<{ ok: boolean; error?: string }> => {
  try {
    if (tabId === 'others') {
      // 'others' Tab 需要使用 "custom:{toolId}" 格式
      if (!selectedToolId.value) {
        showToast(t('components.main.customCli.selectToolFirst'), 'error')
        return { ok: false, error: t('components.main.customCli.selectToolFirst') }
      }
      await SaveProviders(getCustomProviderKind(selectedToolId.value), serializeProviders(cards.others))
    } else if (tabId === 'gemini') {
      // Gemini 使用独立的保存逻辑
      // 1. 收集当前卡片的 name 集合
      const currentNames = new Set(cards.gemini.map(c => c.name))

      // 2. 删除不在当前卡片中的 provider
      for (const cached of geminiProvidersCache.value) {
        if (!currentNames.has(cached.name)) {
          await DeleteGeminiProvider(cached.id)
        }
      }

      // 3. 添加或更新 provider
      for (const card of cards.gemini) {
        const original = geminiProvidersCache.value.find(p => p.name === card.name)

        if (original) {
          // 已存在的 provider，更新
          await UpdateGeminiProvider(cardToGemini(card, original))
        } else {
          // 新添加的 provider，调用 AddProvider
          const newProvider = new GeminiProvider({
            id: `gemini-${Date.now()}`,
            name: card.name,
            baseUrl: card.apiUrl,
            apiKey: card.apiKey,
            websiteUrl: card.officialSite,
            enabled: card.enabled,
          })
          await AddGeminiProvider(newProvider)
        }
      }

      // 4. 刷新缓存以获取最新的 ID
      const updatedProviders = await GetGeminiProviders()
      geminiProvidersCache.value = updatedProviders

      // 5. 保存排序：按 cards.gemini 的顺序构建 ID 列表
      const orderedIds: string[] = []
      for (const card of cards.gemini) {
        const provider = updatedProviders.find(p => p.name === card.name)
        if (provider) {
          orderedIds.push(provider.id)
        }
      }
      if (orderedIds.length > 0) {
        await ReorderGeminiProviders(orderedIds)
        // 重新获取排序后的数据
        geminiProvidersCache.value = await GetGeminiProviders()
      }
    } else {
      await SaveProviders(tabId, serializeProviders(cards[tabId]))
    }
    return { ok: true }
  } catch (error) {
    console.error('Failed to save providers', error)
    const errorMsg = extractErrorMessage(error)
    showToast(t('components.main.form.saveFailed') + ': ' + errorMsg, 'error')
    return { ok: false, error: errorMsg }
  }
}

const replaceProviders = (tabId: ProviderTab, data: AutomationCard[]) => {
  cards[tabId].splice(0, cards[tabId].length, ...createAutomationCards(data))
}

const loadProvidersFromDisk = async () => {
  for (const tab of providerTabIds) {
    try {
      if (tab === 'others') {
        // 'others' Tab: 先加载自定义 CLI 工具列表，再加载每个工具的 providers
        await loadCustomCliTools()
      } else if (tab === 'gemini') {
        // Gemini 使用独立的加载逻辑
        const geminiProviders = await GetGeminiProviders()
        geminiProvidersCache.value = geminiProviders
        cards.gemini.splice(0, cards.gemini.length, ...geminiProviders.map(geminiToCard))
        sortProvidersByLevel(cards.gemini)  // 初始排序：启用优先，Level 升序
      } else {
        const saved = await LoadProviders(tab)
        if (Array.isArray(saved)) {
          replaceProviders(tab, saved as AutomationCard[])
          sortProvidersByLevel(cards[tab])  // 初始排序：启用优先，Level 升序
        } else {
          await persistProviders(tab)
        }
      }
    } catch (error) {
      console.error('Failed to load providers', error)
      // 加载供应商失败时提示用户
      showToast(t('components.main.errors.loadProvidersFailed', { tab }), 'error')
    }
  }
}

// 加载自定义 CLI 工具列表
const loadCustomCliTools = async () => {
  try {
    const tools = await listCustomCliTools()
    customCliTools.value = tools

    // 自动选择第一个工具（如果有）
    if (tools.length > 0 && !selectedToolId.value) {
      selectedToolId.value = tools[0].id
    }

    // 为每个工具加载代理状态
    for (const tool of tools) {
      try {
        const status = await getCustomCliProxyStatus(tool.id)
        customCliProxyStates[tool.id] = Boolean(status?.enabled)
      } catch (err) {
        customCliProxyStates[tool.id] = false
      }
    }

    // 如果当前选中了工具，更新 'others' Tab 的代理状态并加载 providers
    if (selectedToolId.value) {
      proxyStates.others = customCliProxyStates[selectedToolId.value] ?? false
      await loadCustomCliProviders(selectedToolId.value)
    }
  } catch (error) {
    console.error('Failed to load custom CLI tools', error)
    customCliTools.value = []
  }
}

// 加载特定 CLI 工具的 providers
const loadCustomCliProviders = async (toolId: string) => {
  if (!toolId) return
  try {
    const kind = getCustomProviderKind(toolId)
    const saved = await LoadProviders(kind)
    if (Array.isArray(saved)) {
      cards.others.splice(0, cards.others.length, ...createAutomationCards(saved as AutomationCard[]))
      sortProvidersByLevel(cards.others)
    } else {
      // 如果没有保存的数据，清空列表
      cards.others.splice(0, cards.others.length)
    }
  } catch (error) {
    console.error(`Failed to load providers for tool ${toolId}`, error)
    cards.others.splice(0, cards.others.length)
  }
}

const refreshImportStatus = async () => {
  try {
    importStatus.value = await fetchConfigImportStatus()
  } catch (error) {
    console.error('Failed to load cc-switch import status', error)
    importStatus.value = null
  }
}

// 检查是否首次使用，显示导入提示
const checkFirstRun = async () => {
  try {
    const firstRun = await isFirstRun()
    if (firstRun) {
      showFirstRunPrompt.value = true
    }
  } catch (error) {
    console.error('Failed to check first run', error)
  }
}

// 关闭首次使用提示
const dismissFirstRunPrompt = async () => {
  showFirstRunPrompt.value = false
  try {
    await markFirstRunDone()
  } catch (error) {
    console.error('Failed to mark first run done', error)
  }
}

// 打开设置页的导入功能
const goToImportSettings = async () => {
  await dismissFirstRunPrompt()
  router.push('/settings')
}

const refreshProxyState = async (tab: ProviderTab) => {
  try {
    if (tab === 'others') {
      // 'others' Tab 的代理状态依赖于选中的 CLI 工具
      if (selectedToolId.value) {
        const status = await getCustomCliProxyStatus(selectedToolId.value)
        customCliProxyStates[selectedToolId.value] = Boolean(status?.enabled)
        proxyStates[tab] = Boolean(status?.enabled)
      } else {
        proxyStates[tab] = false
      }
    } else if (tab === 'gemini') {
      const status = await fetchGeminiProxyStatus()
      proxyStates[tab] = Boolean(status?.enabled)
    } else {
      const status = await fetchProxyStatus(tab as 'claude' | 'codex')
      proxyStates[tab] = Boolean(status?.enabled)
    }
  } catch (error) {
    console.error(`Failed to fetch proxy status for ${tab}`, error)
    proxyStates[tab] = false
  }
}

const onProxyToggle = async () => {
  const tab = activeTab.value
  if (proxyBusy[tab]) return
  proxyBusy[tab] = true
  const nextState = !proxyStates[tab]
  try {
    if (tab === 'others') {
      // 'others' Tab 需要选中工具才能切换代理
      if (!selectedToolId.value) {
        showToast(t('components.main.customCli.selectToolFirst'), 'error')
        return
      }
      if (nextState) {
        await enableCustomCliProxy(selectedToolId.value)
      } else {
        await disableCustomCliProxy(selectedToolId.value)
      }
      customCliProxyStates[selectedToolId.value] = nextState
    } else if (tab === 'gemini') {
      if (nextState) {
        await enableGeminiProxy()
      } else {
        await disableGeminiProxy()
      }
    } else {
      if (nextState) {
        await enableProxy(tab as 'claude' | 'codex')
      } else {
        await disableProxy(tab as 'claude' | 'codex')
      }
    }
    proxyStates[tab] = nextState
  } catch (error) {
    console.error(`Failed to toggle proxy for ${tab}`, error)
  } finally {
    proxyBusy[tab] = false
  }
}

const loadProviderStats = async (tab: ProviderTab) => {
  // 'others' Tab 暂不加载统计数据（自定义 CLI 工具统计需要后续实现）
  if (tab === 'others') {
    providerStatsLoaded[tab] = true
    return
  }

  providerStatsLoading[tab] = true
  try {
    // Gemini 统计数据目前通过相同的日志接口，直接查询
    const stats = await fetchProviderDailyStats(tab as 'claude' | 'codex' | 'gemini')
    const mapped: Record<string, ProviderDailyStat> = {}
    ;(stats ?? []).forEach((stat) => {
      mapped[normalizeProviderKey(stat.provider)] = stat
    })
    providerStatsMap[tab] = mapped
    providerStatsLoaded[tab] = true
  } catch (error) {
    console.error(`Failed to load provider stats for ${tab}`, error)
    if (!providerStatsLoaded[tab]) {
      providerStatsLoaded[tab] = true
    }
  } finally {
    providerStatsLoading[tab] = false
  }
}

// 加载黑名单状态
const loadBlacklistStatus = async (tab: ProviderTab) => {
  // 'others' Tab 暂不加载黑名单状态
  if (tab === 'others') {
    return
  }

  try {
    const statuses = await getBlacklistStatus(tab)
    const map: Record<string, BlacklistStatus> = {}
    statuses.forEach(status => {
      map[status.providerName] = status
    })
    blacklistStatusMap[tab] = map
  } catch (err) {
    console.error(`加载 ${tab} 黑名单状态失败:`, err)
  }
}

// 手动解禁并重置（完全重置）
const handleUnblockAndReset = async (providerName: string) => {
  try {
    await Call.ByName('codeswitch/services.BlacklistService.ManualUnblockAndReset', activeTab.value, providerName)
    showToast(t('components.main.blacklist.unblockSuccess', { name: providerName }), 'success')
    await loadBlacklistStatus(activeTab.value)
  } catch (err) {
    console.error('解除拉黑失败:', err)
    showToast(t('components.main.blacklist.unblockFailed'), 'error')
  }
}

// 手动清零等级（仅重置等级）
const handleResetLevel = async (providerName: string) => {
  try {
    await Call.ByName('codeswitch/services.BlacklistService.ManualResetLevel', activeTab.value, providerName)
    showToast(t('components.main.blacklist.resetLevelSuccess', { name: providerName }), 'success')
    await loadBlacklistStatus(activeTab.value)
  } catch (err) {
    console.error('清零等级失败:', err)
    showToast(t('components.main.blacklist.resetLevelFailed'), 'error')
  }
}

// 手动解禁（向后兼容，调用 handleUnblockAndReset）
const handleUnblock = handleUnblockAndReset

// 格式化倒计时
const formatBlacklistCountdown = (remainingSeconds: number): string => {
  const minutes = Math.floor(remainingSeconds / 60)
  const seconds = remainingSeconds % 60
  return `${minutes}${t('components.main.blacklist.minutes')}${seconds}${t('components.main.blacklist.seconds')}`
}

// 获取 provider 黑名单状态
const getProviderBlacklistStatus = (providerName: string): BlacklistStatus | null => {
  return blacklistStatusMap[activeTab.value][providerName] || null
}

// 加载连通性测试结果（已废弃，保留兼容）
const loadConnectivityResults = async (tab: ProviderTab) => {
  // 'others' Tab 暂不加载连通性结果
  if (tab === 'others') {
    return
  }

  try {
    const results = await getConnectivityResults(tab)
    const map: Record<number, ConnectivityResult> = {}
    results.forEach((result) => {
      map[result.providerId] = result
    })
    connectivityResultsMap[tab] = map
  } catch (err) {
    console.error(`加载 ${tab} 连通性结果失败:`, err)
  }
}

// 加载可用性监控结果（新）
const loadAvailabilityResults = async () => {
  try {
    const allResults = await getLatestResults()

    // 转换为按平台和 ID 索引的格式
    for (const platform of Object.keys(allResults)) {
      const timelines = allResults[platform] || []
      const map: Record<number, ProviderTimeline> = {}
      timelines.forEach((timeline) => {
        map[timeline.providerId] = timeline
      })
      availabilityResultsMap[platform as ProviderTab] = map
    }
  } catch (err) {
    console.error('加载可用性监控结果失败:', err)
  }
}

// 获取 provider 连通性状态（已废弃）
const getProviderConnectivityResult = (providerId: number): ConnectivityResult | null => {
  return connectivityResultsMap[activeTab.value][providerId] || null
}

// 获取 provider 可用性状态（新）
const getProviderAvailabilityResult = (providerId: number): ProviderTimeline | null => {
  return availabilityResultsMap[activeTab.value][providerId] || null
}

// 获取连通性状态指示器样式（改用可用性监控结果）
const getConnectivityIndicatorClass = (providerId: number): string => {
  const result = getProviderAvailabilityResult(providerId)
  if (!result || !result.latest) return 'connectivity-gray'

  // 根据可用性监控状态返回样式
  switch (result.latest.status) {
    case HealthStatus.OPERATIONAL:
      return 'connectivity-green'
    case HealthStatus.DEGRADED:
      return 'connectivity-yellow'
    case HealthStatus.FAILED:
    case HealthStatus.VALIDATION_ERROR:
      return 'connectivity-red'
    default:
      return 'connectivity-gray'
  }
}

// 获取连通性状态提示文本（改用可用性监控结果）
const getConnectivityTooltip = (providerId: number): string => {
  const result = getProviderAvailabilityResult(providerId)
  if (!result || !result.latest) return t('components.main.connectivity.noData')

  let statusText = ''
  switch (result.latest.status) {
    case HealthStatus.OPERATIONAL:
      statusText = t('components.main.connectivity.available')
      break
    case HealthStatus.DEGRADED:
      statusText = t('components.main.connectivity.degraded')
      break
    case HealthStatus.FAILED:
    case HealthStatus.VALIDATION_ERROR:
      statusText = t('components.main.connectivity.unavailable')
      break
    default:
      statusText = t('components.main.connectivity.noData')
  }

  const latencyText = result.latest.latencyMs > 0 ? ` (${result.latest.latencyMs}ms)` : ''
  const uptimeText = result.uptime > 0 ? ` - ${result.uptime.toFixed(1)}%` : ''
  return statusText + latencyText + uptimeText
}

// 刷新所有数据
const refreshing = ref(false)
const refreshAllData = async () => {
  if (refreshing.value) return
  refreshing.value = true
  try {
    await Promise.all([
      reloadHeatmap(),
      loadProvidersFromDisk(),
      ...providerTabIds.map(refreshProxyState),
      ...providerTabIds.map((tab) => refreshDirectAppliedStatus(tab)),
      ...providerTabIds.map((tab) => loadProviderStats(tab)),
      ...providerTabIds.map((tab) => loadBlacklistStatus(tab)), // 同步刷新黑名单状态
      loadAvailabilityResults(), // 同步刷新可用性监控状态（改用新服务）
      refreshImportStatus()
    ])
  } catch (error) {
    console.error('Failed to refresh data', error)
  } finally {
    refreshing.value = false
  }
}

type ProviderStatDisplay =
  | { state: 'loading' | 'empty'; message: string }
  | {
      state: 'ready'
      requests: string
      tokens: string
      cost: string
      successRateLabel: string
      successRateClass: string
    }

const SUCCESS_RATE_THRESHOLDS = {
  healthy: 0.95,
  warning: 0.8,
} as const

const formatSuccessRateLabel = (value: number) => {
  const percent = clamp(value, 0, 1) * 100
  const decimals = percent >= 99.5 || percent === 0 ? 0 : 1
  return `${t('components.main.providers.successRate')}: ${percent.toFixed(decimals)}%`
}

const successRateClassName = (value: number) => {
  const rate = clamp(value, 0, 1)
  if (rate >= SUCCESS_RATE_THRESHOLDS.healthy) {
    return 'success-good'
  }
  if (rate >= SUCCESS_RATE_THRESHOLDS.warning) {
    return 'success-warn'
  }
  return 'success-bad'
}

const providerStatDisplay = (providerName: string): ProviderStatDisplay => {
  const tab = activeTab.value
  if (!providerStatsLoaded[tab]) {
    return { state: 'loading', message: t('components.main.providers.loading') }
  }
  const stat = providerStatsMap[tab]?.[normalizeProviderKey(providerName)]
  if (!stat) {
    return { state: 'empty', message: t('components.main.providers.noData') }
  }
  const totalTokens = stat.input_tokens + stat.output_tokens
  const successRateValue = Number.isFinite(stat.success_rate) ? clamp(stat.success_rate, 0, 1) : null
  const successRateLabel = successRateValue !== null ? formatSuccessRateLabel(successRateValue) : ''
  const successRateClass = successRateValue !== null ? successRateClassName(successRateValue) : ''
  return {
    state: 'ready',
    requests: `${t('components.main.providers.requests')}: ${formatMetric(stat.total_requests)}`,
    tokens: `${t('components.main.providers.tokens')}: ${formatTokenNumber(totalTokens)}`,
    cost: `${t('components.main.providers.cost')}: ${currencyFormatter.value.format(Math.max(stat.cost_total, 0))}`,
    successRateLabel,
    successRateClass,
  }
}

const normalizeUrlWithScheme = (value: string) => {
  if (!value) return ''
  try {
    const url = new URL(value)
    return url.toString()
  } catch {
    return `https://${value}`
  }
}

const openOfficialSite = (site: string) => {
  const target = normalizeUrlWithScheme(site)
  if (!target) return
  Browser.OpenURL(target).catch(() => {
    console.error('failed to open link', target)
  })
}

const formatOfficialSite = (site: string) => {
  if (!site) return ''
  try {
    const url = new URL(normalizeUrlWithScheme(site))
    return url.hostname.replace(/^www\./, '')
  } catch {
    return site
  }
}

const startProviderStatsTimer = () => {
  stopProviderStatsTimer()
  providerStatsTimer = window.setInterval(() => {
    providerTabIds.forEach((tab) => {
      void loadProviderStats(tab)
    })
    void loadAvailabilityResults() // 同步刷新可用性监控状态（改用新服务）
  }, 60_000)
}

const stopProviderStatsTimer = () => {
  if (providerStatsTimer) {
    clearInterval(providerStatsTimer)
    providerStatsTimer = undefined
  }
}

// 加载最后使用的供应商
// @author sm
const loadLastUsedProviders = async () => {
  try {
    const result = await Call.ByName('codeswitch/services.ProviderRelayService.GetAllLastUsedProviders')
    if (result) {
      Object.keys(result).forEach(platform => {
        if (result[platform]) {
          lastUsedProviders[platform] = result[platform]
        }
      })
    }
  } catch (err) {
    console.error('加载最后使用的供应商失败:', err)
  }
}

// 切换到指定平台的 Tab 并高亮供应商
// @author sm
const switchToTabAndHighlight = (platform: string, providerName: string) => {
  // 切换到对应的 Tab
  const tabIndex = tabs.findIndex(tab => tab.id === platform)
  if (tabIndex >= 0 && selectedIndex.value !== tabIndex) {
    selectedIndex.value = tabIndex
  }

  // 更新最后使用的供应商
  lastUsedProviders[platform] = {
    platform,
    provider_name: providerName,
    updated_at: Date.now(),
  }

  // 高亮闪烁供应商卡片
  highlightedProvider.value = providerName

  // 清除之前的高亮计时器
  if (highlightTimer) {
    clearTimeout(highlightTimer)
  }

  // 3 秒后取消高亮
  highlightTimer = window.setTimeout(() => {
    highlightedProvider.value = null
  }, 3000)

  // 刷新黑名单状态
  void loadBlacklistStatus(platform as ProviderTab)
}

// 处理供应商切换事件
// @author sm
const handleProviderSwitched = (event: { data: { platform: string; toProvider: string } }) => {
  const { platform, toProvider } = event.data
  console.log('[Event] provider:switched', platform, toProvider)
  switchToTabAndHighlight(platform, toProvider)
}

// 处理供应商拉黑事件
// @author sm
const handleProviderBlacklisted = (event: { data: { platform: string; providerName: string } }) => {
  const { platform, providerName } = event.data
  console.log('[Event] provider:blacklisted', platform, providerName)
  switchToTabAndHighlight(platform, providerName)
}

// 判断供应商是否是最后使用的
// @author sm
const isLastUsedProvider = (providerName: string): boolean => {
  const lastUsed = lastUsedProviders[activeTab.value]
  return lastUsed?.provider_name === providerName
}

// 滚动到指定卡片
// @author sm
const scrollToCard = (el: HTMLElement | null) => {
  if (el) {
    el.scrollIntoView({ behavior: 'smooth', block: 'center' })
  }
}

// 事件取消订阅函数
let unsubscribeSwitched: (() => void) | undefined
let unsubscribeBlacklisted: (() => void) | undefined

onMounted(async () => {
  void initHeatmap()
  await loadProvidersFromDisk()
  await Promise.all(providerTabIds.map(refreshProxyState))
  await Promise.all(providerTabIds.map((tab) => refreshDirectAppliedStatus(tab)))
  await Promise.all(providerTabIds.map((tab) => loadProviderStats(tab)))
  await loadAppSettings()
  await loadAppVersion()
  await refreshImportStatus()
  await checkFirstRun()  // 检查是否首次使用
  startProviderStatsTimer()

  // 加载初始黑名单状态
  await Promise.all(providerTabIds.map((tab) => loadBlacklistStatus(tab)))

  // 加载初始可用性监控结果（改用新服务）
  await loadAvailabilityResults()

  // 每秒更新黑名单倒计时
  blacklistTimer = window.setInterval(() => {
    const tab = activeTab.value
    Object.keys(blacklistStatusMap[tab]).forEach(providerName => {
      const status = blacklistStatusMap[tab][providerName]
      if (status && status.isBlacklisted && status.remainingSeconds > 0) {
        status.remainingSeconds--
        if (status.remainingSeconds <= 0) {
          loadBlacklistStatus(tab)
        }
      }
    })
  }, 1000)

  // 窗口焦点事件：从最小化恢复时立即刷新黑名单状态
  const handleWindowFocus = () => {
    void loadBlacklistStatus(activeTab.value)
  }
  window.addEventListener('focus', handleWindowFocus)

  // 定期轮询黑名单状态（每 10 秒）
  const blacklistPollingTimer = window.setInterval(() => {
    void loadBlacklistStatus(activeTab.value)
  }, 10_000)

  // 存储定时器 ID 以便清理
  ;(window as any).__blacklistPollingTimer = blacklistPollingTimer
  ;(window as any).__handleWindowFocus = handleWindowFocus

  window.addEventListener('app-settings-updated', handleAppSettingsUpdated)

  // 监听可用性页面的 Provider 更新事件
  const handleProvidersUpdated = () => {
    void loadProvidersFromDisk()
  }
  window.addEventListener('providers-updated', handleProvidersUpdated)
  ;(window as any).__handleProvidersUpdated = handleProvidersUpdated

  // 加载最后使用的供应商
  await loadLastUsedProviders()

  // 监听供应商切换和拉黑事件
  unsubscribeSwitched = Events.On('provider:switched', handleProviderSwitched as Events.Callback)
  unsubscribeBlacklisted = Events.On('provider:blacklisted', handleProviderBlacklisted as Events.Callback)
})

onUnmounted(() => {
  cleanupHeatmap()
  stopProviderStatsTimer()
  window.removeEventListener('app-settings-updated', handleAppSettingsUpdated)

  // 清理黑名单相关定时器和事件监听
  if (blacklistTimer) {
    window.clearInterval(blacklistTimer)
  }
  if ((window as any).__blacklistPollingTimer) {
    window.clearInterval((window as any).__blacklistPollingTimer)
  }
  if ((window as any).__handleWindowFocus) {
    window.removeEventListener('focus', (window as any).__handleWindowFocus)
  }
  if ((window as any).__handleProvidersUpdated) {
    window.removeEventListener('providers-updated', (window as any).__handleProvidersUpdated)
  }

  // 清理高亮计时器
  if (highlightTimer) {
    clearTimeout(highlightTimer)
  }

  // 取消事件订阅
  if (unsubscribeSwitched) {
    unsubscribeSwitched()
  }
  if (unsubscribeBlacklisted) {
    unsubscribeBlacklisted()
  }
})

const selectedIndex = ref(0)
const activeTab = computed<ProviderTab>(() => tabs[selectedIndex.value]?.id ?? tabs[0].id)
const activeCards = computed(() => cards[activeTab.value] ?? [])

// 连通性测试模型选项（根据平台）
const connectivityTestModelOptions = computed(() => {
  const options: Record<string, string[]> = {
    claude: ['claude-haiku-4-5-20251001', 'claude-sonnet-4-5-20250929'],
    codex: ['gpt-5.1', 'gpt-5.1-codex'],
    gemini: ['gemini-2.5-flash', 'gemini-2.5-pro'],
  }
  return options[modalState.tabId] || options.claude
})

// 连通性测试端点选项
const connectivityEndpointOptions = [
  { value: '/v1/messages', label: '/v1/messages (Anthropic)' },
  { value: '/v1/chat/completions', label: '/v1/chat/completions (OpenAI)' },
  { value: '/responses', label: '/responses (Codex)' },
]

// 连通性测试状态
const testingConnectivity = ref(false)
const connectivityTestResult = ref<{ success: boolean; message: string } | null>(null)

// 获取平台默认端点
const getDefaultEndpoint = (platform: string) => {
  const defaults: Record<string, string> = {
    claude: '/v1/messages',
    codex: '/responses',
  }
  return defaults[platform] || '/v1/chat/completions'
}

// 获取平台默认认证方式（默认 Bearer，与 v2.2.x 保持一致）
const getDefaultAuthType = (_platform: string) => 'bearer'

// 手动测试连通性
const handleTestConnectivity = async () => {
  testingConnectivity.value = true
  connectivityTestResult.value = null

  try {
    const platform = modalState.tabId
    const result = await Call.ByName(
      'codeswitch/services.ConnectivityTestService.TestProviderManual',
      platform,
      modalState.form.apiUrl,
      modalState.form.apiKey,
      modalState.form.connectivityTestModel || '',
      modalState.form.connectivityTestEndpoint || getDefaultEndpoint(platform),
      resolveEffectiveAuthType()
    )

    connectivityTestResult.value = {
      success: result.success,
      message: result.success
        ? t('components.main.form.connectivity.success', { latency: result.latencyMs })
        : result.message || t('components.main.form.connectivity.failed')
    }
  } catch (error) {
    connectivityTestResult.value = {
      success: false,
      message: t('components.main.form.connectivity.error', { error: extractErrorMessage(error) })
    }
  } finally {
    testingConnectivity.value = false
  }
}

// 监听 tab 切换，立即刷新黑名单和可用性状态
watch(activeTab, (newTab) => {
  void loadBlacklistStatus(newTab)
  // 可用性结果是全局的，不需要按 tab 刷新
})
const currentProxyLabel = computed(() => {
  const tab = activeTab.value
  if (tab === 'claude') {
    return t('components.main.relayToggle.hostClaude')
  } else if (tab === 'codex') {
    return t('components.main.relayToggle.hostCodex')
  } else if (tab === 'gemini') {
    return t('components.main.relayToggle.hostGemini')
  } else if (tab === 'others') {
    // 显示选中的工具名称
    const tool = customCliTools.value.find(t => t.id === selectedToolId.value)
    return tool?.name || t('components.main.relayToggle.hostOthers')
  }
  return t('components.main.relayToggle.hostCodex')
})
const activeProxyState = computed(() => proxyStates[activeTab.value])
const activeProxyBusy = computed(() => proxyBusy[activeTab.value])

const goToLogs = () => {
  router.push('/logs')
}

const goToMcp = () => {
  router.push('/mcp')
}

const goToSkill = () => {
  router.push('/skill')
}

const goToSettings = () => {
  router.push('/settings')
}

const toggleTheme = () => {
  const next = resolvedTheme.value === 'dark' ? 'light' : 'dark'
  themeMode.value = next
  setTheme(next)
}

const handleGithubClick = () => {
  Browser.OpenURL(releasePageUrl).catch(() => {
    console.error('failed to open github')
  })
}

// 获取 GitHub 图标的 tooltip
const getGithubTooltip = () => {
  return t('components.main.controls.github')
}

type VendorForm = {
  name: string
  apiUrl: string
  apiKey: string
  officialSite: string
  icon: string
  enabled: boolean
  supportedModels?: Record<string, boolean>
  modelMapping?: Record<string, string>
  level?: number
  apiEndpoint?: string
  cliConfig?: Record<string, any>
  // === 可用性监控配置（新） ===
  availabilityMonitorEnabled?: boolean
  connectivityAutoBlacklist?: boolean
  availabilityConfig?: {
    testModel?: string
    testEndpoint?: string
    timeout?: number
  }
  // === 旧连通性字段（已废弃） ===
  /** @deprecated */
  connectivityCheck?: boolean
  /** @deprecated */
  connectivityTestModel?: string
  /** @deprecated */
  connectivityTestEndpoint?: string
  /** @deprecated */
  connectivityAuthType?: string
}

const iconOptions = Object.keys(lobeIcons).sort((a, b) => a.localeCompare(b))
const defaultIconKey = iconOptions[0] ?? 'aicoding'

// 图标搜索筛选
const iconSearchQuery = ref('')
const filteredIconOptions = computed(() => {
  const query = iconSearchQuery.value.toLowerCase().trim()
  if (!query) return iconOptions
  return iconOptions.filter(name => name.toLowerCase().includes(query))
})

const defaultFormValues = (platform?: string): VendorForm => ({
  name: '',
  apiUrl: '',
  apiKey: '',
  officialSite: '',
  icon: defaultIconKey,
  level: 1,
  enabled: true,
  supportedModels: {},
  modelMapping: {},
  cliConfig: {},
  apiEndpoint: '', // API 端点（可选）
  upstreamProtocol: 'auto', // 上游协议类型（anthropic/openai_chat/auto）
  // 可用性监控配置（新）
  availabilityMonitorEnabled: false,
  connectivityAutoBlacklist: false,
  availabilityConfig: {
    testModel: '',
    testEndpoint: getDefaultEndpoint(platform || 'claude'),
    timeout: 15000,
  },
  // 旧连通性字段（已废弃，置空）
  connectivityCheck: false,
  connectivityTestModel: '',
  connectivityTestEndpoint: '',
  connectivityAuthType: '',
})

// Level 描述文本映射（1-10）
const getLevelDescription = (level: number) => {
  const descriptions: Record<number, string> = {
    1: t('components.main.levelDesc.highest'),
    2: t('components.main.levelDesc.high'),
    3: t('components.main.levelDesc.mediumHigh'),
    4: t('components.main.levelDesc.medium'),
    5: t('components.main.levelDesc.normal'),
    6: t('components.main.levelDesc.mediumLow'),
    7: t('components.main.levelDesc.low'),
    8: t('components.main.levelDesc.lower'),
    9: t('components.main.levelDesc.veryLow'),
    10: t('components.main.levelDesc.lowest'),
  }
  return descriptions[level] || t('components.main.levelDesc.normal')
}

// 归一化 level：空/非法视为 1（最高优先级），范围限制 1-10
const normalizeLevel = (level: number | string | undefined): number => {
  const num = Number(level)
  if (!Number.isFinite(num) || num < 1) return 1
  if (num > 10) return 10
  return Math.floor(num)  // 确保返回整数
}

// 按 enabled 和 level 排序：启用的排在前面，同启用状态下按 level 升序排序
const sortProvidersByLevel = (list: AutomationCard[]) => {
  if (!Array.isArray(list)) return
  list.sort((a, b) => {
    // 第一优先级：启用状态（enabled: true 排在前面）
    if (a.enabled !== b.enabled) {
      return a.enabled ? -1 : 1
    }
    // 第二优先级：Level 升序（1 -> 10）
    return normalizeLevel(a.level) - normalizeLevel(b.level)
  })
}

const modalState = reactive({
  open: false,
  tabId: tabs[0].id as ProviderTab,
  editingId: null as number | null,
  form: defaultFormValues(),
  errors: {
    apiUrl: '',
  },
})

// 认证方式相关状态
const selectedAuthType = ref<string>('bearer')
const customAuthHeader = ref<string>('')
const authTypeOptions = computed(() => [
  { value: 'bearer', label: 'Bearer' },
  { value: 'x-api-key', label: 'X-API-Key' },
])

// 上游协议类型选项
const upstreamProtocolOptions = computed(() => [
  { value: 'auto', label: t('components.main.form.upstreamProtocol.auto'), desc: t('components.main.form.upstreamProtocol.autoDesc') },
  { value: 'anthropic', label: t('components.main.form.upstreamProtocol.anthropic'), desc: t('components.main.form.upstreamProtocol.anthropicDesc') },
  { value: 'openai_chat', label: t('components.main.form.upstreamProtocol.openaiChat'), desc: t('components.main.form.upstreamProtocol.openaiChatDesc') },
])

const resolveEffectiveAuthType = () =>
  customAuthHeader.value.trim() || selectedAuthType.value || getDefaultAuthType(modalState.tabId)

const editingCard = ref<AutomationCard | null>(null)
const confirmState = reactive({ open: false, card: null as AutomationCard | null, tabId: tabs[0].id as ProviderTab })

const openCreateModal = () => {
  modalState.tabId = activeTab.value
  modalState.editingId = null
  editingCard.value = null
  Object.assign(modalState.form, defaultFormValues(activeTab.value))
  // 初始化认证方式为平台默认
  selectedAuthType.value = getDefaultAuthType(activeTab.value)
  customAuthHeader.value = ''
  connectivityTestResult.value = null
  modalState.errors.apiUrl = ''
  modalState.open = true
}

const openEditModal = (card: AutomationCard) => {
  modalState.tabId = activeTab.value
  modalState.editingId = card.id
  editingCard.value = card
  Object.assign(modalState.form, {
    name: card.name,
    apiUrl: card.apiUrl,
    apiKey: card.apiKey,
    officialSite: card.officialSite,
    icon: card.icon,
    level: card.level || 1,
    enabled: card.enabled,
    supportedModels: card.supportedModels || {},
    modelMapping: card.modelMapping || {},
    cliConfig: card.cliConfig || {},
    apiEndpoint: card.apiEndpoint || '',
    upstreamProtocol: card.upstreamProtocol || 'auto',
    // 可用性监控配置（新）- 兼容从旧字段迁移
    availabilityMonitorEnabled:
      card.availabilityMonitorEnabled ?? card.connectivityCheck ?? false,
    connectivityAutoBlacklist: card.connectivityAutoBlacklist ?? false,
    availabilityConfig: {
      testModel:
        card.availabilityConfig?.testModel || card.connectivityTestModel || '',
      testEndpoint:
        card.availabilityConfig?.testEndpoint ||
        card.connectivityTestEndpoint ||
        getDefaultEndpoint(activeTab.value),
      timeout: card.availabilityConfig?.timeout || 15000,
    },
    // 旧连通性字段不再写入表单
    connectivityCheck: false,
    connectivityTestModel: '',
    connectivityTestEndpoint: '',
    connectivityAuthType: card.connectivityAuthType || '',
  })
  // 初始化认证方式状态
  const storedAuth = (card.connectivityAuthType || '').trim()
  const lower = storedAuth.toLowerCase()
  if (!storedAuth) {
    selectedAuthType.value = getDefaultAuthType(activeTab.value)
    customAuthHeader.value = ''
  } else if (lower === 'bearer' || lower === 'x-api-key') {
    selectedAuthType.value = lower
    customAuthHeader.value = ''
  } else {
    // 自定义 Header 名
    selectedAuthType.value = getDefaultAuthType(activeTab.value)
    customAuthHeader.value = storedAuth
  }
  connectivityTestResult.value = null
  modalState.errors.apiUrl = ''
  modalState.open = true
}

const closeModal = () => {
  modalState.open = false
}

const closeConfirm = () => {
  confirmState.open = false
  confirmState.card = null
}

const submitModal = async (): Promise<boolean> => {
  const list = cards[modalState.tabId]
  if (!list) return false
  const name = modalState.form.name.trim()
  const apiUrl = modalState.form.apiUrl.trim()
  const apiKey = modalState.form.apiKey.trim()
  const officialSite = modalState.form.officialSite.trim()
  const icon = (modalState.form.icon || defaultIconKey).toString().trim().toLowerCase() || defaultIconKey
  modalState.errors.apiUrl = ''
  try {
    const parsed = new URL(apiUrl)
    if (!/^https?:/.test(parsed.protocol)) throw new Error('protocol')
  } catch {
    modalState.errors.apiUrl = t('components.main.form.errors.invalidUrl')
    return false
  }

  if (editingCard.value) {
    // 仅当 level 变化时才重新排序，避免破坏同级拖拽顺序
    const prevLevel = normalizeLevel(editingCard.value.level)
    const nextLevel = normalizeLevel(modalState.form.level)
    Object.assign(editingCard.value, {
      apiUrl: apiUrl || editingCard.value.apiUrl,
      apiKey,
      officialSite,
      icon,
      level: nextLevel,
      enabled: modalState.form.enabled,
      supportedModels: modalState.form.supportedModels || {},
      modelMapping: modalState.form.modelMapping || {},
      cliConfig: modalState.form.cliConfig || {},
      apiEndpoint: modalState.form.apiEndpoint || '',
      upstreamProtocol: modalState.form.upstreamProtocol || 'auto',
      // 可用性监控配置（新）
      availabilityMonitorEnabled: !!modalState.form.availabilityMonitorEnabled,
      connectivityAutoBlacklist: !!modalState.form.connectivityAutoBlacklist,
      availabilityConfig: {
        testModel: modalState.form.availabilityConfig?.testModel || '',
        testEndpoint:
          modalState.form.availabilityConfig?.testEndpoint ||
          getDefaultEndpoint(modalState.tabId),
        timeout: modalState.form.availabilityConfig?.timeout || 15000,
      },
      // 旧连通性字段清空（避免再次写入）
      connectivityCheck: false,
      connectivityTestModel: '',
      connectivityTestEndpoint: '',
      connectivityAuthType: resolveEffectiveAuthType(),
    })
    if (prevLevel !== nextLevel) {
      sortProvidersByLevel(list)
    }
    const saveResult = await persistProviders(modalState.tabId)
    if (!saveResult.ok) {
      // 保存失败，不关闭弹窗，让用户修正配置
      return false
    }
  } else {
    const newCard: AutomationCard = {
      id: Date.now(),
      name: name || 'Untitled vendor',
      apiUrl,
      apiKey,
      officialSite,
      icon,
      accent: '#0a84ff',
      tint: 'rgba(15, 23, 42, 0.12)',
      level: normalizeLevel(modalState.form.level),
      enabled: modalState.form.enabled,
      supportedModels: modalState.form.supportedModels || {},
      modelMapping: modalState.form.modelMapping || {},
      cliConfig: modalState.form.cliConfig || {},
      apiEndpoint: modalState.form.apiEndpoint || '',
      upstreamProtocol: modalState.form.upstreamProtocol || 'auto',
      // 可用性监控配置（新）
      availabilityMonitorEnabled: !!modalState.form.availabilityMonitorEnabled,
      connectivityAutoBlacklist: !!modalState.form.connectivityAutoBlacklist,
      availabilityConfig: {
        testModel: modalState.form.availabilityConfig?.testModel || '',
        testEndpoint:
          modalState.form.availabilityConfig?.testEndpoint ||
          getDefaultEndpoint(modalState.tabId),
        timeout: modalState.form.availabilityConfig?.timeout || 15000,
      },
      // 旧连通性字段清空
      connectivityCheck: false,
      connectivityTestModel: '',
      connectivityTestEndpoint: '',
      connectivityAuthType: resolveEffectiveAuthType(),
    }
    list.push(newCard)
    sortProvidersByLevel(list)
    const saveResult = await persistProviders(modalState.tabId)
    if (!saveResult.ok) {
      // 保存失败，从列表中移除刚添加的卡片，不关闭弹窗
      const idx = list.indexOf(newCard)
      if (idx !== -1) list.splice(idx, 1)
      return false
    }
  }

  // 保存 CLI 配置（仅支持 claude/codex/gemini 平台）
  const cliConfig = modalState.form.cliConfig
  const supportedPlatforms: CLIPlatform[] = ['claude', 'codex', 'gemini']
  if (cliConfig && Object.keys(cliConfig).length > 0 && supportedPlatforms.includes(modalState.tabId as CLIPlatform)) {
    try {
      await saveCLIConfig(modalState.tabId as CLIPlatform, cliConfig)
    } catch (error) {
      console.error('保存 CLI 配置失败:', error)
    }
  }

  closeModal()

  // 通知可用性页面刷新
  window.dispatchEvent(new CustomEvent('providers-updated'))
  return true
}

// 保存并应用：先保存供应商配置，再直连应用到 CLI
const submitAndApplyModal = async () => {
  // 1. 执行普通保存逻辑
  const editingId = modalState.editingId
  const tabId = modalState.tabId as ProviderTab
  if (!editingId || tabId === 'others') return

  // 获取当前编辑的卡片
  const editingCard = cards[tabId]?.find(c => c.id === editingId)
  if (!editingCard) return

  // 调用标准保存流程
  const saved = await submitModal()
  if (!saved) {
    // 保存失败，不继续应用
    return
  }

  // 2. 保存成功后，应用到 CLI（直连模式）
  try {
    if (tabId === 'claude') {
      await Call.ByName('codeswitch/services.ClaudeSettingsService.ApplySingleProvider', editingId)
    } else if (tabId === 'codex') {
      await Call.ByName('codeswitch/services.CodexSettingsService.ApplySingleProvider', editingId)
    } else if (tabId === 'gemini') {
      // Gemini 使用字符串 ID，需要从 cache 中找到原始 provider
      const index = cards.gemini.findIndex(c => c.id === editingId)
      if (index !== -1 && geminiProvidersCache.value[index]) {
        const realId = geminiProvidersCache.value[index].id
        await Call.ByName('codeswitch/services.GeminiService.ApplySingleProvider', realId)
      }
    }
    await refreshDirectAppliedStatus(tabId)
    showToast(t('components.main.directApply.success', { name: editingCard.name }), 'success')
  } catch (error) {
    console.error('Apply after save failed', error)
    showToast(t('components.main.directApply.failed'), 'error')
  }
}

const configure = (card: AutomationCard) => {
  openEditModal(card)
}

const remove = async (id: number, tabId: ProviderTab = activeTab.value) => {
  const list = cards[tabId]
  if (!list) return
  const index = list.findIndex((card) => card.id === id)
  if (index > -1) {
    list.splice(index, 1)
    await persistProviders(tabId)
  }
}

const requestRemove = (card: AutomationCard) => {
  confirmState.card = card
  confirmState.tabId = activeTab.value
  confirmState.open = true
}

// 复制供应商
const handleDuplicate = async (card: AutomationCard) => {
  try {
    const tab = activeTab.value

    if (tab === 'gemini') {
      // Gemini 使用字符串 ID，需要从 cache 中找到原始 provider
      const index = cards.gemini.findIndex(c => c.id === card.id)
      if (index === -1 || !geminiProvidersCache.value[index]) {
        console.error('[Duplicate] 未找到 Gemini provider')
        return
      }

      const originalProvider = geminiProvidersCache.value[index]
      // 调用 Gemini 的 DuplicateProvider API（字符串 ID）
      const newProvider = await Call.ByName(
        'codeswitch/services.GeminiService.DuplicateProvider',
        originalProvider.id
      )

      if (!newProvider) {
        console.warn('[Duplicate] DuplicateProvider 返回空结果，已跳过刷新')
        return
      }

      console.log(`[Duplicate] Gemini Provider "${card.name}" duplicated`)
    } else {
      // Claude/Codex 使用数字 ID
      const newProvider = await DuplicateProvider(tab, card.id)
      if (!newProvider) {
        console.warn('[Duplicate] DuplicateProvider 返回空结果，已跳过刷新')
        return
      }
      console.log(`[Duplicate] Provider "${card.name}" duplicated as "${newProvider.name}"`)
    }

    // 刷新列表以显示新副本
    await loadProvidersFromDisk()
  } catch (error) {
    console.error('[Duplicate] Failed to duplicate provider:', error)
  }
}

const confirmRemove = async () => {
  if (!confirmState.card) return
  await remove(confirmState.card.id, confirmState.tabId)
  closeConfirm()
}

const onDragStart = (id: number) => {
  draggingId.value = id
}

const onDrop = async (targetId: number) => {
  if (draggingId.value === null || draggingId.value === targetId) return
  const currentTab = activeTab.value
  const list = cards[currentTab]
  if (!list) return
  const fromIndex = list.findIndex((card) => card.id === draggingId.value)
  const toIndex = list.findIndex((card) => card.id === targetId)
  if (fromIndex === -1 || toIndex === -1) return
  const [moved] = list.splice(fromIndex, 1)
  const newIndex = fromIndex < toIndex ? toIndex - 1 : toIndex
  list.splice(newIndex, 0, moved)
  draggingId.value = null
  await persistProviders(currentTab)
}

const onDragEnd = () => {
  draggingId.value = null
}

const iconSvg = (name: string) => {
  if (!name) return ''
  return lobeIcons[name.toLowerCase()] ?? ''
}

const vendorInitials = (name: string) => {
  if (!name) return 'AI'
  return name
    .split(/\s+/)
    .filter(Boolean)
    .map((word) => word[0])
    .join('')
    .slice(0, 2)
    .toUpperCase()
}

const onTabChange = (idx: number) => {
  selectedIndex.value = idx
  const nextTab = tabs[idx]?.id
  if (nextTab) {
    void refreshProxyState(nextTab as ProviderTab)
    void refreshDirectAppliedStatus(nextTab as ProviderTab)
    void loadProviderStats(nextTab as ProviderTab)
  }
}

const handleImportClick = async () => {
  if (importBusy.value) return
  importBusy.value = true
  try {
    const result = await importFromCcSwitch()
    importStatus.value = result?.status ?? null
    const importedProviders = result?.imported_providers ?? 0
    const importedMCP = result?.imported_mcp ?? 0
    if (importedProviders > 0) {
      await loadProvidersFromDisk()
    }
    if (importedProviders > 0 || importedMCP > 0) {
      showToast(
        t('components.main.importConfig.success', {
          providers: importedProviders,
          servers: importedMCP,
        })
      )
    } else if (result?.status?.config_exists) {
      showToast(t('components.main.importConfig.empty'))
    }
  } catch (error) {
    console.error('Failed to import cc-switch config', error)
    showToast(t('components.main.importConfig.error'), 'error')
  } finally {
    importBusy.value = false
  }
}

// ========== 自定义 CLI 工具管理 ==========

// CLI 工具模态框状态
const cliToolModalState = reactive({
  open: false,
  editingId: null as string | null,
  form: {
    name: '',
    configFiles: [] as Array<{
      id: string
      label: string
      path: string
      format: 'json' | 'toml' | 'env'
      isPrimary: boolean
    }>,
    proxyInjection: [] as Array<{
      targetFileId: string
      baseUrlField: string
      authTokenField: string
    }>,
  },
})

// CLI 工具删除确认状态
const cliToolConfirmState = reactive({
  open: false,
  tool: null as CustomCliTool | null,
})

// 切换选中的 CLI 工具
const onToolSelect = async () => {
  if (selectedToolId.value) {
    // 更新当前 tab 的代理状态
    proxyStates.others = customCliProxyStates[selectedToolId.value] ?? false
    // 加载该工具的 providers 列表
    await loadCustomCliProviders(selectedToolId.value)
  } else {
    // 未选中任何工具，清空 providers 列表
    cards.others.splice(0, cards.others.length)
  }
}

// 仅在只有一个配置文件时自动选中，避免多配置场景下造成"意外选择"
const getAutoSelectedProxyTargetFileId = () => {
  const files = cliToolModalState.form.configFiles
  if (files.length === 1) return files[0].id
  return ''
}

// 打开新建 CLI 工具模态框
const openCliToolModal = () => {
  cliToolModalState.editingId = null
  cliToolModalState.form.name = ''
  cliToolModalState.form.configFiles = [{
    id: `cfg-${Date.now()}`,
    label: t('components.main.customCli.primaryConfig'),
    path: '',
    format: 'json',
    isPrimary: true,
  }]
  // 默认占位行保持全空，允许用户选择不配置代理注入
  // 保存时会自动补齐 targetFileId（如果用户填写了字段且只有一个配置文件）
  cliToolModalState.form.proxyInjection = [{
    targetFileId: '',
    baseUrlField: '',
    authTokenField: '',
  }]
  cliToolModalState.open = true
}

// 编辑当前选中的 CLI 工具
const editCurrentCliTool = async () => {
  if (!selectedToolId.value) return
  const tool = customCliTools.value.find(t => t.id === selectedToolId.value)
  if (!tool) return

  cliToolModalState.editingId = tool.id
  cliToolModalState.form.name = tool.name
  cliToolModalState.form.configFiles = tool.configFiles.length > 0
    ? tool.configFiles.map(cf => ({
        id: cf.id,
        label: cf.label,
        path: cf.path,
        format: cf.format,
        isPrimary: cf.isPrimary ?? false,
      }))
    : [{
        id: `cfg-${Date.now()}`,
        label: t('components.main.customCli.primaryConfig'),
        path: '',
        format: 'json' as const,
        isPrimary: true,
      }]
  // 加载已有的代理注入配置，默认占位行保持全空
  // 保存时会自动补齐 targetFileId（如果用户填写了字段且只有一个配置文件）
  cliToolModalState.form.proxyInjection = tool.proxyInjection && tool.proxyInjection.length > 0
    ? tool.proxyInjection.map(pi => ({
        targetFileId: pi.targetFileId ?? '',
        baseUrlField: pi.baseUrlField ?? '',
        authTokenField: pi.authTokenField ?? '',
      }))
    : [{
        targetFileId: '',
        baseUrlField: '',
        authTokenField: '',
      }]
  cliToolModalState.open = true
}

// 请求删除当前选中的 CLI 工具
const deleteCurrentCliTool = () => {
  if (!selectedToolId.value) return
  const tool = customCliTools.value.find(t => t.id === selectedToolId.value)
  if (!tool) return
  cliToolConfirmState.tool = tool
  cliToolConfirmState.open = true
}

// 关闭 CLI 工具模态框
const closeCliToolModal = () => {
  cliToolModalState.open = false
}

// 关闭 CLI 工具删除确认框
const closeCliToolConfirm = () => {
  cliToolConfirmState.open = false
  cliToolConfirmState.tool = null
}

// 添加配置文件
const addConfigFile = () => {
  cliToolModalState.form.configFiles.push({
    id: `cfg-${Date.now()}`,
    label: '',
    path: '',
    format: 'json',
    isPrimary: false,
  })
}

// 删除配置文件
const removeConfigFile = (index: number) => {
  if (cliToolModalState.form.configFiles.length <= 1) return
  cliToolModalState.form.configFiles.splice(index, 1)
}

// 添加代理注入配置
const addProxyInjection = () => {
  cliToolModalState.form.proxyInjection.push({
    targetFileId: getAutoSelectedProxyTargetFileId(),
    baseUrlField: '',
    authTokenField: '',
  })
}

// 删除代理注入配置
const removeProxyInjection = (index: number) => {
  if (cliToolModalState.form.proxyInjection.length <= 1) return
  cliToolModalState.form.proxyInjection.splice(index, 1)
}

// 提交 CLI 工具模态框
const submitCliToolModal = async () => {
  const name = cliToolModalState.form.name.trim()
  if (!name) {
    showToast(t('components.main.customCli.nameRequired'), 'error')
    return
  }

  // 过滤掉空的配置文件
  const validConfigFiles = cliToolModalState.form.configFiles.filter(cf => cf.path.trim())
  if (validConfigFiles.length === 0) {
    showToast(t('components.main.customCli.configRequired'), 'error')
    return
  }

  // 验证至少有一个主配置文件
  const hasPrimary = validConfigFiles.some(cf => cf.isPrimary)
  if (!hasPrimary) {
    // 如果没有选中主配置文件，自动将第一个设为主配置
    validConfigFiles[0].isPrimary = true
  }

  // 代理注入配置：允许全空（表示不使用），但不允许"半填"
  // 单一配置文件时，自动选中作为代理注入目标（避免用户忘记选择）
  const autoTargetFileId = validConfigFiles.length === 1 ? validConfigFiles[0].id : ''

  const proxyInjectionsToSave = cliToolModalState.form.proxyInjection
    .map(pi => {
      const baseUrlField = pi.baseUrlField.trim()
      const authTokenField = pi.authTokenField.trim()
      // 如果用户填写了字段但忘记选择目标文件，且只有一个配置文件，自动补充
      const targetFileId = pi.targetFileId.trim() || ((baseUrlField || authTokenField) ? autoTargetFileId : '')
      return { targetFileId, baseUrlField, authTokenField }
    })
    .filter(pi => pi.targetFileId || pi.baseUrlField || pi.authTokenField)

  const hasIncompleteProxyInjection = proxyInjectionsToSave.some(
    pi => !pi.targetFileId || !pi.baseUrlField
  )
  if (hasIncompleteProxyInjection) {
    showToast(t('components.main.customCli.proxyInjectionIncomplete'), 'error')
    return
  }

  // 先校验"目标 ID 是否存在"，再校验"目标文件路径是否有效"，避免报错信息误导
  const allFileIds = new Set(cliToolModalState.form.configFiles.map(cf => cf.id))
  const validFileIds = new Set(validConfigFiles.map(cf => cf.id))

  const hasInvalidProxyTarget = proxyInjectionsToSave.some(pi => !allFileIds.has(pi.targetFileId))
  if (hasInvalidProxyTarget) {
    showToast(t('components.main.customCli.invalidProxyTarget'), 'error')
    return
  }

  const hasProxyTargetPathMissing = proxyInjectionsToSave.some(pi => !validFileIds.has(pi.targetFileId))
  if (hasProxyTargetPathMissing) {
    showToast(t('components.main.customCli.proxyTargetPathRequired'), 'error')
    return
  }

  try {
    if (cliToolModalState.editingId) {
      // 更新现有工具
      await updateCustomCliTool(cliToolModalState.editingId, {
        id: cliToolModalState.editingId,
        name,
        configFiles: validConfigFiles,
        proxyInjection: proxyInjectionsToSave,
      })
      showToast(t('components.main.customCli.updateSuccess'), 'success')
    } else {
      // 创建新工具
      const newTool = await createCustomCliTool({
        name,
        configFiles: validConfigFiles,
        proxyInjection: proxyInjectionsToSave,
      })
      selectedToolId.value = newTool.id
      showToast(t('components.main.customCli.createSuccess'), 'success')
    }

    // 刷新工具列表
    await loadCustomCliTools()
    closeCliToolModal()
  } catch (error) {
    console.error('Failed to save CLI tool', error)
    // 处理各种错误类型：Error 对象、字符串、其他
    const msg = error instanceof Error ? error.message : String(error ?? '')
    if (msg.includes('ERR_CUSTOM_CLI_PROXY_INJECTION_INCOMPLETE')) {
      showToast(t('components.main.customCli.proxyInjectionIncomplete'), 'error')
      return
    }
    if (msg.includes('ERR_CUSTOM_CLI_INVALID_PROXY_TARGET')) {
      showToast(t('components.main.customCli.invalidProxyTarget'), 'error')
      return
    }
    showToast(t('components.main.customCli.saveFailed'), 'error')
  }
}

// 确认删除 CLI 工具
const confirmDeleteCliTool = async () => {
  if (!cliToolConfirmState.tool) return
  try {
    await deleteCustomCliTool(cliToolConfirmState.tool.id)
    showToast(t('components.main.customCli.deleteSuccess'), 'success')

    // 如果删除的是当前选中的工具，清空选择
    if (selectedToolId.value === cliToolConfirmState.tool.id) {
      selectedToolId.value = null
      proxyStates.others = false
    }

    // 刷新工具列表
    await loadCustomCliTools()
    closeCliToolConfirm()
  } catch (error) {
    console.error('Failed to delete CLI tool', error)
    showToast(t('components.main.customCli.deleteFailed'), 'error')
  }
}
</script>

<style scoped>
/* 正在使用的供应商卡片样式 */
/* @author sm */
.automation-card.is-last-used {
  position: relative;
  border: 2px solid rgb(16, 185, 129);
  box-shadow: 0 0 8px rgba(16, 185, 129, 0.3);
}

/* 正在使用标签 */
.last-used-badge {
  position: absolute;
  top: -10px;
  right: 12px;
  background: rgb(16, 185, 129);
  color: white;
  font-size: 10px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 4px;
  z-index: 1;
}

/* 高亮闪烁的供应商卡片（切换/拉黑时） */
.automation-card.is-highlighted {
  animation: highlight-pulse 0.6s ease-in-out 3;
  border-color: rgb(245, 158, 11);
  box-shadow: 0 0 12px rgba(245, 158, 11, 0.5);
}

@keyframes highlight-pulse {
  0%, 100% {
    box-shadow: 0 0 8px rgba(245, 158, 11, 0.3);
  }
  50% {
    box-shadow: 0 0 20px rgba(245, 158, 11, 0.7);
  }
}

/* 暗色模式适配 */
:global(.dark) .automation-card.is-last-used {
  border-color: rgb(52, 211, 153);
  box-shadow: 0 0 8px rgba(52, 211, 153, 0.3);
}

:global(.dark) .last-used-badge {
  background: rgb(52, 211, 153);
  color: rgb(6, 78, 59);
}

:global(.dark) .automation-card.is-highlighted {
  border-color: rgb(251, 191, 36);
  box-shadow: 0 0 12px rgba(251, 191, 36, 0.5);
}

.global-actions .ghost-icon svg.rotating {
  animation: import-spin 0.9s linear infinite;
}

@keyframes import-spin {
  from {
    transform: rotate(0deg);
  }

  to {
    transform: rotate(360deg);
  }
}

/* Level Badge 样式 */
.level-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 32px;
  height: 22px;
  padding: 0 7px;
  border-radius: 8px;
  font-size: 11px;
  font-weight: 600;
  line-height: 1;
  letter-spacing: 0.03em;
  text-align: center;
  transition: all 0.2s ease;
}

/* Card title row badge 定位 */
.card-title-row .level-badge {
  margin-left: 8px;
}

/* 黑名单等级徽章与调度等级徽章的间距 */
.card-title-row .blacklist-level-badge {
  margin-left: 4px;
}

/* Level 配色方案：从绿色（高优先级）到红色（低优先级）*/
.level-badge.level-1 {
  background: rgba(16, 185, 129, 0.12);
  color: rgb(5, 150, 105);
}

.level-badge.level-2 {
  background: rgba(34, 197, 94, 0.12);
  color: rgb(22, 163, 74);
}

.level-badge.level-3 {
  background: rgba(132, 204, 22, 0.12);
  color: rgb(101, 163, 13);
}

.level-badge.level-4 {
  background: rgba(234, 179, 8, 0.12);
  color: rgb(161, 98, 7);
}

.level-badge.level-5 {
  background: rgba(245, 158, 11, 0.12);
  color: rgb(180, 83, 9);
}

.level-badge.level-6 {
  background: rgba(249, 115, 22, 0.12);
  color: rgb(194, 65, 12);
}

.level-badge.level-7 {
  background: rgba(239, 68, 68, 0.12);
  color: rgb(185, 28, 28);
}

.level-badge.level-8 {
  background: rgba(220, 38, 38, 0.12);
  color: rgb(153, 27, 27);
}

.level-badge.level-9 {
  background: rgba(190, 18, 60, 0.12);
  color: rgb(136, 19, 55);
}

.level-badge.level-10 {
  background: rgba(159, 18, 57, 0.12);
  color: rgb(112, 26, 52);
}

/* 暗色模式适配 */
:global(.dark) .level-badge.level-1 {
  background: rgba(16, 185, 129, 0.18);
  color: rgb(52, 211, 153);
}

:global(.dark) .level-badge.level-2 {
  background: rgba(34, 197, 94, 0.18);
  color: rgb(74, 222, 128);
}

:global(.dark) .level-badge.level-3 {
  background: rgba(132, 204, 22, 0.18);
  color: rgb(163, 230, 53);
}

:global(.dark) .level-badge.level-4 {
  background: rgba(234, 179, 8, 0.18);
  color: rgb(250, 204, 21);
}

:global(.dark) .level-badge.level-5 {
  background: rgba(245, 158, 11, 0.18);
  color: rgb(251, 191, 36);
}

:global(.dark) .level-badge.level-6 {
  background: rgba(249, 115, 22, 0.18);
  color: rgb(251, 146, 60);
}

:global(.dark) .level-badge.level-7 {
  background: rgba(239, 68, 68, 0.18);
  color: rgb(248, 113, 113);
}

:global(.dark) .level-badge.level-8 {
  background: rgba(220, 38, 38, 0.18);
  color: rgb(239, 68, 68);
}

:global(.dark) .level-badge.level-9 {
  background: rgba(190, 18, 60, 0.18);
  color: rgb(244, 63, 94);
}

:global(.dark) .level-badge.level-10 {
  background: rgba(159, 18, 57, 0.18);
  color: rgb(236, 72, 153);
}

/* Level Select Dropdown 样式 */
.level-select {
  position: relative;
}

.level-select-button {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 12px;
  background: var(--color-bg-secondary);
  border: 1px solid var(--color-border);
  border-radius: 8px;
  font-size: 14px;
  color: var(--color-text-primary);
  cursor: pointer;
  transition: all 0.2s ease;
}

.level-select-button:hover {
  border-color: var(--color-border-hover);
  background: var(--color-bg-tertiary);
}

.level-select-button:focus {
  outline: 2px solid var(--color-accent);
  outline-offset: 2px;
}

.level-select-button svg {
  width: 16px;
  height: 16px;
  margin-left: auto;
  opacity: 0.5;
}

.level-label {
  flex: 1;
  text-align: left;
}

.level-select-options {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  right: 0;
  max-height: 280px;
  overflow-y: auto;
  background: var(--mac-surface);
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
  z-index: 50;
  padding: 4px;
}

:global(.dark) .level-select-options {
  background: var(--mac-surface);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
}

.level-option {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.level-option:hover,
.level-option.active {
  background: var(--mac-surface-strong);
}

.level-option.selected {
  background: rgba(10, 132, 255, 0.12); /* fallback for old WebKit */
  background: color-mix(in srgb, var(--mac-accent) 12%, transparent);
  font-weight: 500;
}

.level-option .level-name {
  flex: 1;
  font-size: 14px;
  color: var(--mac-text);
}

.level-option.selected .level-name {
  color: var(--mac-accent);
}

/* 黑名单横幅 */
.blacklist-banner {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
  margin-top: 8px;
  background: rgba(239, 68, 68, 0.1);
  border-left: 3px solid #ef4444;
  border-radius: 6px;
  font-size: 13px;
  color: #dc2626;
}

.blacklist-banner.dark {
  background: rgba(239, 68, 68, 0.15);
  color: #f87171;
}

.blacklist-info {
  display: flex;
  align-items: center;
  gap: 8px;
}

.blacklist-icon {
  font-size: 16px;
  flex-shrink: 0;
}

.blacklist-text {
  flex: 1;
  font-weight: 500;
}

.blacklist-actions {
  display: flex;
  gap: 6px;
  align-items: center;
}

.unblock-btn {
  padding: 4px 12px;
  font-size: 12px;
  font-weight: 500;
  color: #fff;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  transition: all 0.2s;
}

.unblock-btn.primary {
  background: #ef4444;
  flex: 1;
}

.unblock-btn.primary:hover {
  background: #dc2626;
}

.unblock-btn.secondary {
  background: #6b7280;
  flex: 1;
}

.unblock-btn.secondary:hover {
  background: #4b5563;
}

.unblock-btn:active {
  transform: scale(0.98);
}

/* 等级徽章（黑名单模式：黑色/红色） */
.blacklist-banner .level-badge,
.level-badge-standalone .level-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 2px 6px;
  min-width: 28px;
  font-size: 11px;
  font-weight: 700;
  border-radius: 6px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  line-height: 1;
  flex-shrink: 0;
  text-align: center;
}

.blacklist-banner .level-badge.level-1,
.level-badge-standalone .level-badge.level-1 {
  background: #fef3c7;
  color: #d97706;
}

.blacklist-banner .level-badge.level-2,
.level-badge-standalone .level-badge.level-2 {
  background: #fed7aa;
  color: #ea580c;
}

.blacklist-banner .level-badge.level-3,
.level-badge-standalone .level-badge.level-3 {
  background: #fecaca;
  color: #dc2626;
}

.blacklist-banner .level-badge.level-4,
.level-badge-standalone .level-badge.level-4 {
  background: #fca5a5;
  color: #b91c1c;
}

.blacklist-banner .level-badge.level-5,
.level-badge-standalone .level-badge.level-5 {
  background: #ef4444;
  color: #fff;
}

.blacklist-banner .level-badge.dark.level-1,
.level-badge-standalone .level-badge.dark.level-1 {
  background: rgba(217, 119, 6, 0.2);
  color: #fbbf24;
}

.blacklist-banner .level-badge.dark.level-2,
.level-badge-standalone .level-badge.dark.level-2 {
  background: rgba(234, 88, 12, 0.2);
  color: #fb923c;
}

.blacklist-banner .level-badge.dark.level-3,
.level-badge-standalone .level-badge.dark.level-3 {
  background: rgba(220, 38, 38, 0.2);
  color: #f87171;
}

.blacklist-banner .level-badge.dark.level-4,
.level-badge-standalone .level-badge.dark.level-4 {
  background: rgba(185, 28, 28, 0.2);
  color: #ef4444;
}

.blacklist-banner .level-badge.dark.level-5,
.level-badge-standalone .level-badge.dark.level-5 {
  background: rgba(220, 38, 38, 0.3);
  color: #fff;
}

/* 独立等级徽章（未拉黑但有等级） */
.level-badge-standalone {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  margin-top: 8px;
  background: rgba(156, 163, 175, 0.1);
  border-left: 3px solid #9ca3af;
  border-radius: 6px;
  font-size: 12px;
  color: #6b7280;
}

.level-hint {
  flex: 1;
  font-weight: 500;
}

.reset-level-mini {
  padding: 2px 6px;
  font-size: 11px;
  font-weight: 700;
  color: #6b7280;
  background: transparent;
  border: 1px solid #d1d5db;
  border-radius: 3px;
  cursor: pointer;
  transition: all 0.2s;
  line-height: 1;
}

.reset-level-mini:hover {
  background: #f3f4f6;
  color: #374151;
  border-color: #9ca3af;
}

.reset-level-mini:active {
  transform: scale(0.95);
}

/* 黑名单等级徽章（卡片标题行） */
.blacklist-level-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 32px;
  height: 22px;
  padding: 0 7px;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 600;
  line-height: 1;
  letter-spacing: 0.03em;
  transition: all 0.2s ease;
  margin-left: 4px;
}

.blacklist-level-badge.bl-level-0 {
  background: #e5e7eb;
  color: #6b7280;
}

.blacklist-level-badge.bl-level-1 {
  background: #fef3c7;
  color: #d97706;
}

.blacklist-level-badge.bl-level-2 {
  background: #fed7aa;
  color: #ea580c;
}

.blacklist-level-badge.bl-level-3 {
  background: #fecaca;
  color: #dc2626;
}

.blacklist-level-badge.bl-level-4 {
  background: #fca5a5;
  color: #b91c1c;
}

.blacklist-level-badge.bl-level-5 {
  background: #ef4444;
  color: #fff;
}

.blacklist-level-badge.dark.bl-level-0 {
  background: rgba(107, 114, 128, 0.2);
  color: #9ca3af;
}

.blacklist-level-badge.dark.bl-level-1 {
  background: rgba(217, 119, 6, 0.2);
  color: #fbbf24;
}

.blacklist-level-badge.dark.bl-level-2 {
  background: rgba(234, 88, 12, 0.2);
  color: #fb923c;
}

.blacklist-level-badge.dark.bl-level-3 {
  background: rgba(220, 38, 38, 0.2);
  color: #f87171;
}

.blacklist-level-badge.dark.bl-level-4 {
  background: rgba(185, 28, 28, 0.2);
  color: #ef4444;
}

.blacklist-level-badge.dark.bl-level-5 {
  background: rgba(220, 38, 38, 0.3);
  color: #fff;
}

/* 首次使用提示横幅 */
.first-run-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  margin-bottom: 16px;
  background: linear-gradient(135deg, rgba(59, 130, 246, 0.1) 0%, rgba(147, 51, 234, 0.1) 100%);
  border: 1px solid rgba(59, 130, 246, 0.2);
  border-radius: 12px;
  gap: 16px;
}

:global(.dark) .first-run-banner {
  background: linear-gradient(135deg, rgba(59, 130, 246, 0.15) 0%, rgba(147, 51, 234, 0.15) 100%);
  border-color: rgba(59, 130, 246, 0.3);
}

.banner-content {
  display: flex;
  align-items: center;
  gap: 10px;
}

.banner-icon {
  font-size: 18px;
}

.banner-text {
  font-size: 13px;
  color: var(--mac-text-primary);
  line-height: 1.4;
}

.banner-actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}

.banner-btn {
  padding: 6px 12px;
  font-size: 12px;
  border-radius: 6px;
  border: 1px solid rgba(0, 0, 0, 0.1);
  background: rgba(255, 255, 255, 0.8);
  color: var(--mac-text-primary);
  cursor: pointer;
  transition: all 0.15s ease;
}

.banner-btn:hover {
  background: rgba(255, 255, 255, 1);
}

.banner-btn.primary {
  background: linear-gradient(135deg, #3b82f6 0%, #8b5cf6 100%);
  border-color: transparent;
  color: white;
}

.banner-btn.primary:hover {
  filter: brightness(1.1);
}

:global(.dark) .banner-btn {
  background: rgba(255, 255, 255, 0.1);
  border-color: rgba(255, 255, 255, 0.1);
}

:global(.dark) .banner-btn:hover {
  background: rgba(255, 255, 255, 0.15);
}

:global(.dark) .banner-btn.primary {
  background: linear-gradient(135deg, #3b82f6 0%, #8b5cf6 100%);
}

/* 连通性状态指示器 */
.connectivity-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-left: 6px;
  flex-shrink: 0;
  transition: background-color 0.2s ease;
}

.connectivity-dot.connectivity-green {
  background-color: #22c55e;
  box-shadow: 0 0 4px rgba(34, 197, 94, 0.5);
}

.connectivity-dot.connectivity-yellow {
  background-color: #eab308;
  box-shadow: 0 0 4px rgba(234, 179, 8, 0.5);
}

.connectivity-dot.connectivity-red {
  background-color: #ef4444;
  box-shadow: 0 0 4px rgba(239, 68, 68, 0.5);
}

.connectivity-dot.connectivity-gray {
  background-color: #9ca3af;
}

:global(.dark) .connectivity-dot.connectivity-green {
  background-color: #4ade80;
  box-shadow: 0 0 6px rgba(74, 222, 128, 0.6);
}

:global(.dark) .connectivity-dot.connectivity-yellow {
  background-color: #facc15;
  box-shadow: 0 0 6px rgba(250, 204, 21, 0.6);
}

:global(.dark) .connectivity-dot.connectivity-red {
  background-color: #f87171;
  box-shadow: 0 0 6px rgba(248, 113, 113, 0.6);
}

:global(.dark) .connectivity-dot.connectivity-gray {
  background-color: #6b7280;
}

/* 测试连通性按钮 */
.test-connectivity-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  width: 100%;
  padding: 10px 16px;
  background: linear-gradient(135deg, #3b82f6 0%, #8b5cf6 100%);
  color: white;
  border: none;
  border-radius: 8px;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s ease;
}

.test-connectivity-btn:hover:not(:disabled) {
  filter: brightness(1.1);
}

.test-connectivity-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn-spinner {
  width: 14px;
  height: 14px;
  border: 2px solid rgba(255, 255, 255, 0.3);
  border-top-color: white;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.test-result {
  margin-top: 8px;
  padding: 8px 12px;
  border-radius: 6px;
  font-size: 13px;
}

.test-result.success {
  background: rgba(34, 197, 94, 0.1);
  color: #16a34a;
  border-left: 3px solid #22c55e;
}

.test-result.error {
  background: rgba(239, 68, 68, 0.1);
  color: #dc2626;
  border-left: 3px solid #ef4444;
}

:global(.dark) .test-result.success {
  background: rgba(34, 197, 94, 0.15);
  color: #4ade80;
}

:global(.dark) .test-result.error {
  background: rgba(239, 68, 68, 0.15);
  color: #f87171;
}

/* ========== CLI 工具选择器样式 ========== */
.cli-tool-selector {
  padding: 12px 16px;
  background: var(--mac-surface);
  border-radius: 8px;
  margin-bottom: 16px;
  border: 1px solid var(--mac-border);
}

.tool-selector-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.tool-select {
  flex: 1;
  padding: 8px 12px;
  background: var(--color-bg-secondary);
  border: 1px solid var(--color-border);
  border-radius: 6px;
  font-size: 14px;
  color: var(--color-text-primary);
  cursor: pointer;
  transition: all 0.2s ease;
}

.tool-select:hover {
  border-color: var(--color-border-hover);
}

.tool-select:focus {
  outline: 2px solid var(--color-accent);
  outline-offset: 2px;
}

.add-tool-btn {
  flex-shrink: 0;
}

.no-tools-hint {
  margin-top: 8px;
  font-size: 13px;
  color: var(--mac-text-secondary);
  text-align: center;
}

/* ========== CLI 工具表单样式 ========== */
.cli-tool-form .field-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}

.cli-tool-form .field-header span {
  font-size: 14px;
  font-weight: 500;
  color: var(--mac-text);
}

.cli-tool-form .add-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  background: var(--mac-accent);
  color: white;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.cli-tool-form .add-btn:hover {
  filter: brightness(1.1);
}

.cli-tool-form .add-btn svg {
  width: 16px;
  height: 16px;
}

.cli-tool-form .remove-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  background: transparent;
  color: var(--mac-text-secondary);
  border: 1px solid var(--mac-border);
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.cli-tool-form .remove-btn:hover:not(:disabled) {
  background: rgba(239, 68, 68, 0.1);
  border-color: #ef4444;
  color: #ef4444;
}

.cli-tool-form .remove-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.cli-tool-form .remove-btn svg {
  width: 14px;
  height: 14px;
}

/* ========== 配置文件列表样式 ========== */
.config-files-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.config-file-item {
  padding: 12px;
  background: var(--mac-surface-strong);
  border: 1px solid var(--mac-border);
  border-radius: 8px;
}

.config-file-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.config-label-input {
  flex: 1;
  min-width: 0;
}

.config-format-select {
  width: 80px;
  padding: 6px 8px;
  background: var(--color-bg-secondary);
  border: 1px solid var(--color-border);
  border-radius: 6px;
  font-size: 13px;
  color: var(--color-text-primary);
  cursor: pointer;
}

.config-format-select:focus {
  outline: 2px solid var(--color-accent);
  outline-offset: 2px;
}

.primary-checkbox {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--mac-text-secondary);
  white-space: nowrap;
  cursor: pointer;
}

.primary-checkbox input {
  width: 14px;
  height: 14px;
  accent-color: var(--mac-accent);
  cursor: pointer;
}

.config-path-input {
  width: 100%;
}

/* ========== 代理注入配置样式 ========== */
.proxy-injection-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.proxy-injection-item {
  padding: 12px;
  background: var(--mac-surface-strong);
  border: 1px solid var(--mac-border);
  border-radius: 8px;
}

.proxy-injection-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.target-file-select {
  flex: 1;
  padding: 8px 12px;
  background: var(--color-bg-secondary);
  border: 1px solid var(--color-border);
  border-radius: 6px;
  font-size: 13px;
  color: var(--color-text-primary);
  cursor: pointer;
}

.target-file-select:focus {
  outline: 2px solid var(--color-accent);
  outline-offset: 2px;
}

.proxy-fields-row {
  display: flex;
  gap: 8px;
}

.proxy-field-input {
  flex: 1;
  min-width: 0;
}

/* 暗色模式适配 */
:global(.dark) .cli-tool-selector {
  background: var(--mac-surface);
  border-color: var(--mac-border);
}

:global(.dark) .config-file-item,
:global(.dark) .proxy-injection-item {
  background: rgba(255, 255, 255, 0.03);
  border-color: rgba(255, 255, 255, 0.08);
}

:global(.dark) .tool-select,
:global(.dark) .config-format-select,
:global(.dark) .target-file-select {
  background: rgba(255, 255, 255, 0.05);
  border-color: rgba(255, 255, 255, 0.1);
  color: var(--mac-text);
}

:global(.dark) .tool-select:hover,
:global(.dark) .config-format-select:hover,
:global(.dark) .target-file-select:hover {
  border-color: rgba(255, 255, 255, 0.2);
}

/* 直连应用按钮 */
.direct-apply-btn {
  position: relative;
  transition: all 0.2s ease;
  color: var(--mac-text-secondary);
  min-width: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.direct-apply-btn .lightning-icon {
  width: 16px;
  height: 16px;
}

.direct-apply-btn:not(:disabled):not(.is-active):hover {
  color: #f59e0b;
  background: rgba(245, 158, 11, 0.1);
}

.direct-apply-btn:disabled {
  opacity: 0.3;
  cursor: not-allowed;
  filter: grayscale(100%);
}

.direct-apply-btn.is-active {
  border: 1px solid #10b981;
  background: rgba(16, 185, 129, 0.1);
  color: #10b981;
  width: auto;
  padding: 0 8px;
  border-radius: 6px;
  gap: 4px;
}

.direct-apply-btn .apply-text {
  font-size: 11px;
  font-weight: 600;
  white-space: nowrap;
}

:global(.dark) .direct-apply-btn.is-active {
  border-color: #34d399;
  background: rgba(52, 211, 153, 0.15);
  color: #34d399;
}

/* 当前使用徽章 */
.current-use-badge {
  display: inline-flex;
  align-items: center;
  padding: 2px 6px;
  margin-left: 8px;
  border-radius: 4px;
  font-size: 10px;
  font-weight: 600;
  background: linear-gradient(135deg, #10b981 0%, #059669 100%);
  color: white;
  box-shadow: 0 2px 4px rgba(16, 185, 129, 0.2);
}

:global(.dark) .current-use-badge {
  background: linear-gradient(135deg, #059669 0%, #047857 100%);
}
</style>
