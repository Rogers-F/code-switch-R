import { createApp, watch } from 'vue'
import App from './App.vue'
import './style.css'
import { i18n, setupI18n } from './utils/i18n'
import { initTheme } from './utils/ThemeManager'
import router from './router/index'
import { availableLocales } from './locales'
import type { Locale } from './locales'

initTheme()
const isMac = navigator.userAgent.includes('Mac')
if (isMac) {
  document.documentElement.classList.add('mac')
}

// 语言选择的本地存储键
const LOCALE_STORAGE_KEY = 'app-locale'

// 读取上次选择的语言，无记录或值非法时回落到默认中文
function resolveInitialLocale(): Locale {
  try {
    const saved = localStorage.getItem(LOCALE_STORAGE_KEY)
    if (saved && (availableLocales as readonly string[]).includes(saved)) {
      return saved as Locale
    }
  } catch {
    // 本地存储不可用时忽略，走默认语言
  }
  return 'zh'
}

async function bootstrap(){
    await setupI18n(resolveInitialLocale())//恢复上次选择的语言，缺省中文
    // 语言切换后写入本地存储，重启时恢复
    watch(i18n.global.locale, (locale) => {
      try {
        localStorage.setItem(LOCALE_STORAGE_KEY, locale)
      } catch {
        // 本地存储不可用时忽略，不影响切换本身
      }
    })
    createApp(App).use(router).use(i18n).mount('#app')
}
bootstrap()
