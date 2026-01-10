import { Call } from '@wailsio/runtime'

export type ConfigBackupExportOptions = {
  include_secrets: boolean
  include_database: boolean
}

export type ConfigBackupManifestFile = {
  path: string
  size: number
  sha256: string
}

export type ConfigBackupManifest = {
  schema_version: number
  app: string
  exported_at: string
  include_secrets: boolean
  include_database: boolean
  files: ConfigBackupManifestFile[]
}

export type ConfigBackupExportResult = {
  path: string
  file_count: number
  manifest: ConfigBackupManifest
}

export type ConfigBackupImportOptions = {
  import_database: boolean
  preserve_existing_secrets: boolean
}

export type ConfigBackupImportResult = {
  imported_files: number
  skipped_files: number
  backups_created: number
  warnings?: string[]
}

export const getDefaultExportPath = async (): Promise<string> => {
  const response = await Call.ByName('codeswitch/services.ConfigBackupService.GetDefaultExportPath')
  return response as string
}

export const exportConfig = async (
  path: string,
  options: ConfigBackupExportOptions
): Promise<ConfigBackupExportResult> => {
  const response = await Call.ByName('codeswitch/services.ConfigBackupService.ExportConfig', path, options)
  return response as ConfigBackupExportResult
}

export const importConfig = async (
  path: string,
  options: ConfigBackupImportOptions
): Promise<ConfigBackupImportResult> => {
  const response = await Call.ByName('codeswitch/services.ConfigBackupService.ImportConfig', path, options)
  return response as ConfigBackupImportResult
}

