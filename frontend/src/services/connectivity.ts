import { Call } from '@wailsio/runtime'

// 连通性状态常量（与 relay-pulse 一致）
export const StatusAvailable = 1    // 绿色：可用
export const StatusDegraded = 2     // 黄色：波动
export const StatusUnavailable = 0  // 红色：不可用
export const StatusMissing = -1     // 灰色：无数据

// 细分状态常量
export const SubStatusNone = ''
export const SubStatusSlowLatency = 'slow_latency'
export const SubStatusRateLimit = 'rate_limit'
export const SubStatusServerError = 'server_error'
export const SubStatusClientError = 'client_error'
export const SubStatusAuthError = 'auth_error'
export const SubStatusInvalidRequest = 'invalid_request'
export const SubStatusNetworkError = 'network_error'
export const SubStatusContentMismatch = 'content_mismatch'

// 连通性测试结果接口
export interface ConnectivityResult {
  providerId: number
  providerName: string
  platform: string
  status: number
  subStatus: string
  latencyMs: number
  lastChecked: string  // ISO 时间字符串
  message?: string
  httpCode?: number
}

const SERVICE = 'codeswitch/services.ConnectivityTestService'

/**
 * 测试指定平台的所有启用检测的供应商
 * @param platform 'claude' | 'codex' | 'gemini'
 */
export const testAllProviders = async (platform: string): Promise<ConnectivityResult[]> => {
  return Call.ByName(`${SERVICE}.TestAll`, platform)
}

/**
 * 获取指定平台的测试结果（不触发新测试）
 * @param platform 'claude' | 'codex' | 'gemini'
 */
export const getConnectivityResults = async (platform: string): Promise<ConnectivityResult[]> => {
  return Call.ByName(`${SERVICE}.GetResults`, platform)
}

/**
 * 获取所有平台的测试结果
 */
export const getAllConnectivityResults = async (): Promise<Record<string, ConnectivityResult[]>> => {
  return Call.ByName(`${SERVICE}.GetAllResults`)
}

/**
 * 手动触发单个供应商测试
 * @param platform 'claude' | 'codex' | 'gemini'
 * @param providerId provider ID
 */
export const runSingleTest = async (platform: string, providerId: number): Promise<ConnectivityResult> => {
  return Call.ByName(`${SERVICE}.RunSingleTest`, platform, providerId)
}

/**
 * 设置自动测试开关
 * @param enabled 是否启用
 */
export const setAutoTestEnabled = async (enabled: boolean): Promise<void> => {
  return Call.ByName(`${SERVICE}.SetAutoTestEnabled`, enabled)
}

/**
 * 获取自动测试开关状态
 */
export const getAutoTestEnabled = async (): Promise<boolean> => {
  return Call.ByName(`${SERVICE}.GetAutoTestEnabled`)
}

/**
 * 获取状态的显示颜色类名
 */
export const getStatusColorClass = (status: number): string => {
  switch (status) {
    case StatusAvailable:
      return 'connectivity-green'
    case StatusDegraded:
      return 'connectivity-yellow'
    case StatusUnavailable:
      return 'connectivity-red'
    case StatusMissing:
    default:
      return 'connectivity-gray'
  }
}

/**
 * 获取状态的显示文本 key（用于 i18n）
 */
export const getStatusTextKey = (status: number): string => {
  switch (status) {
    case StatusAvailable:
      return 'components.main.connectivity.status.available'
    case StatusDegraded:
      return 'components.main.connectivity.status.degraded'
    case StatusUnavailable:
      return 'components.main.connectivity.status.unavailable'
    case StatusMissing:
    default:
      return 'components.main.connectivity.status.missing'
  }
}

/**
 * 获取 SubStatus 的显示文本 key（用于 i18n）
 */
export const getSubStatusTextKey = (subStatus: string): string => {
  const keyMap: Record<string, string> = {
    [SubStatusSlowLatency]: 'components.main.connectivity.subStatus.slowLatency',
    [SubStatusRateLimit]: 'components.main.connectivity.subStatus.rateLimit',
    [SubStatusServerError]: 'components.main.connectivity.subStatus.serverError',
    [SubStatusClientError]: 'components.main.connectivity.subStatus.clientError',
    [SubStatusAuthError]: 'components.main.connectivity.subStatus.authError',
    [SubStatusInvalidRequest]: 'components.main.connectivity.subStatus.invalidRequest',
    [SubStatusNetworkError]: 'components.main.connectivity.subStatus.networkError',
    [SubStatusContentMismatch]: 'components.main.connectivity.subStatus.contentMismatch',
  }
  return keyMap[subStatus] || ''
}
