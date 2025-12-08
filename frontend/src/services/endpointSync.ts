/**
 * 端点同步服务
 * 从 Claude、Codex、Gemini 三个平台获取供应商 API 端点
 * @author sm
 */

import { LoadProviders } from '../../bindings/codeswitch/services/providerservice'
import { GetProviders as GetGeminiProviders } from '../../bindings/codeswitch/services/geminiservice'

/**
 * 同步的端点数据结构
 */
export interface SyncedEndpoint {
  url: string                              // 标准化的基础 URL
  source: 'claude' | 'codex' | 'gemini'   // 来源平台
  providerName: string                     // 供应商名称
}

/**
 * 提取 API URL 的基础地址（去除路径部分）
 * 例如: https://api.anthropic.com/v1/messages -> https://api.anthropic.com
 * @author sm
 */
function extractBaseUrl(apiUrl: string): string {
  if (!apiUrl || !apiUrl.trim()) {
    return ''
  }

  try {
    const url = new URL(apiUrl)
    return `${url.protocol}//${url.host}`
  } catch {
    // 如果解析失败，尝试简单分割
    const trimmed = apiUrl.trim()
    const versionIndex = trimmed.indexOf('/v1')
    if (versionIndex > 0) {
      return trimmed.substring(0, versionIndex)
    }
    console.warn('无效的 API URL:', apiUrl)
    return ''
  }
}

/**
 * 从所有供应商服务获取端点列表
 * @author sm
 */
export async function fetchAllProviderEndpoints(): Promise<SyncedEndpoint[]> {
  const endpoints: SyncedEndpoint[] = []

  try {
    // 1. 获取 Claude 供应商
    const claudeProviders = await LoadProviders('claude')
    if (Array.isArray(claudeProviders)) {
      claudeProviders.forEach((p: any) => {
        if (p.apiUrl && p.apiUrl.trim()) {
          const baseUrl = extractBaseUrl(p.apiUrl)
          if (baseUrl) {
            endpoints.push({
              url: baseUrl,
              source: 'claude',
              providerName: p.name || 'Claude Provider'
            })
          }
        }
      })
    }
  } catch (error) {
    console.error('获取 Claude 供应商失败:', error)
  }

  try {
    // 2. 获取 Codex 供应商
    const codexProviders = await LoadProviders('codex')
    if (Array.isArray(codexProviders)) {
      codexProviders.forEach((p: any) => {
        if (p.apiUrl && p.apiUrl.trim()) {
          const baseUrl = extractBaseUrl(p.apiUrl)
          if (baseUrl) {
            endpoints.push({
              url: baseUrl,
              source: 'codex',
              providerName: p.name || 'Codex Provider'
            })
          }
        }
      })
    }
  } catch (error) {
    console.error('获取 Codex 供应商失败:', error)
  }

  try {
    // 3. 获取 Gemini 供应商
    const geminiProviders = await GetGeminiProviders()
    if (Array.isArray(geminiProviders)) {
      geminiProviders.forEach((p: any) => {
        if (p.baseUrl && p.baseUrl.trim()) {
          const baseUrl = extractBaseUrl(p.baseUrl)
          if (baseUrl) {
            endpoints.push({
              url: baseUrl,
              source: 'gemini',
              providerName: p.name || 'Gemini Provider'
            })
          }
        }
      })
    }
  } catch (error) {
    console.error('获取 Gemini 供应商失败:', error)
  }

  // 4. 去重：相同 URL 只保留第一个
  const uniqueEndpoints = new Map<string, SyncedEndpoint>()
  endpoints.forEach(ep => {
    if (!uniqueEndpoints.has(ep.url)) {
      uniqueEndpoints.set(ep.url, ep)
    }
  })

  return Array.from(uniqueEndpoints.values())
}
