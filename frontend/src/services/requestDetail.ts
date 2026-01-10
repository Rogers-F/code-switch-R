import { Call } from '@wailsio/runtime'

export type RecordMode = 'off' | 'fail_only' | 'all'

export interface RequestDetail {
  sequence_id: number
  platform: string
  provider: string
  model: string
  request_url: string
  request_body: string
  response_body: string
  headers: Record<string, string>
  response_headers: Record<string, string>
  http_code: number
  timestamp: string
  duration_ms: number
  truncated: boolean
  request_size: number
  response_size: number
}

export interface CacheStats {
  mode: RecordMode
  capacity: number
  count: number
}

// 设置记录模式
export const setRecordMode = async (mode: RecordMode): Promise<void> => {
  return Call.ByName('codeswitch/services.RequestDetailService.SetRecordMode', mode)
}

// 获取当前记录模式
export const getRecordMode = async (): Promise<RecordMode> => {
  return Call.ByName('codeswitch/services.RequestDetailService.GetRecordMode')
}

// 获取单条请求详情
export const getRequestDetail = async (sequenceId: number): Promise<RequestDetail | null> => {
  return Call.ByName('codeswitch/services.RequestDetailService.GetDetail', sequenceId)
}

// 获取最近的请求详情列表
export const getRecentDetails = async (limit: number = 20): Promise<RequestDetail[]> => {
  return Call.ByName('codeswitch/services.RequestDetailService.GetRecentDetails', limit)
}

// 获取可用的 sequence_id 列表
export const getAvailableIds = async (): Promise<number[]> => {
  return Call.ByName('codeswitch/services.RequestDetailService.GetAvailableIDs')
}

// 获取缓存统计
export const getCacheStats = async (): Promise<CacheStats> => {
  return Call.ByName('codeswitch/services.RequestDetailService.GetCacheStats')
}

// 清空缓存
export const clearCache = async (): Promise<void> => {
  return Call.ByName('codeswitch/services.RequestDetailService.ClearCache')
}
