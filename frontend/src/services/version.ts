import { Call } from '@wailsio/runtime'

export const fetchCurrentVersion = async (): Promise<string> => {
  const version = await Call.ByName('main.VersionService.CurrentVersion') as string
  return version ?? ''
}
