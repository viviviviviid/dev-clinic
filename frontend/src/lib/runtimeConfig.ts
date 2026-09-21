import { resolveBootConfig } from './bootConfig.ts'
import type { BootConfig, BootEnvironment } from './bootConfig.ts'
import { desktopBridge } from './desktop.ts'
import type { DesktopBridge } from './desktop.ts'

let activeConfig: BootConfig | undefined
let workspace = ''

export async function initializeRuntimeConfig(environment: BootEnvironment, bridge: DesktopBridge | undefined = desktopBridge()): Promise<BootConfig> {
  if (bridge) {
    const native = await bridge.Boot()
    activeConfig = resolveBootConfig({
      VITE_SUPABASE_URL: native.supabaseUrl,
      VITE_SUPABASE_ANON_KEY: native.supabaseAnonKey,
      VITE_LOCAL_URL: native.localUrl,
    })
    workspace = native.baseDir
  } else {
    activeConfig = resolveBootConfig(environment)
    workspace = ''
  }
  return activeConfig
}

export function runtimeConfig(): BootConfig {
  if (!activeConfig) throw new Error('앱의 연결 설정이 아직 준비되지 않았습니다.')
  return activeConfig
}

export function desktopWorkspace(): string { return workspace }
