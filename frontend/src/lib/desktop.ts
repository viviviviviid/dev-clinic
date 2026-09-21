import type { BootConfig } from './bootConfig.ts'

export const DESKTOP_CALLBACK_URL = 'coding-tutor://auth/callback'

export interface DesktopBoot extends BootConfig {
  baseDir: string
}

export interface DesktopBridge {
  Boot(): Promise<DesktopBoot>
  Login(authorizationURL: string): Promise<string>
  CancelLogin(): Promise<void>
  EnableCloseHandler(): Promise<void>
  FinishClose(saved: boolean): Promise<void>
}

export function desktopBridge(): DesktopBridge | undefined {
  if (typeof window === 'undefined') return undefined
  return (window as Window & { go?: { main?: { Desktop?: DesktopBridge } } }).go?.main?.Desktop
}

export function isDesktop(): boolean { return Boolean(desktopBridge()) }
