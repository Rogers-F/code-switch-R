import { Call } from '@wailsio/runtime'

// 本地类型定义，避免依赖 CI 生成的绑定文件
export interface ClaudeProxyStatus {
  enabled: boolean
  base_url: string
}

type Platform = 'claude' | 'codex'

const serviceNames: Record<Platform, string> = {
  claude: 'codeswitch/services.ClaudeSettingsService',
  codex: 'codeswitch/services.CodexSettingsService',
}

const callByPlatform = async <T = unknown>(platform: Platform, method: string, payload?: any[]): Promise<T> => {
  const service = serviceNames[platform]
  const args = payload ?? []
  return Call.ByName(`${service}.${method}`, ...args)
}

export const fetchProxyStatus = async (platform: Platform): Promise<ClaudeProxyStatus> => {
  return callByPlatform<ClaudeProxyStatus>(platform, 'ProxyStatus')
}

export const enableProxy = async (platform: Platform): Promise<void> => {
  await callByPlatform(platform, 'EnableProxy')
}

export const disableProxy = async (platform: Platform): Promise<void> => {
  await callByPlatform(platform, 'DisableProxy')
}
