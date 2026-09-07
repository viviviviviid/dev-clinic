import { normalizeLocalUrl } from './localUrl.ts'

export const DEFAULT_LOCAL_URL = 'http://127.0.0.1:47291'

export interface BootEnvironment {
  [key: string]: unknown
  VITE_SUPABASE_URL?: unknown
  VITE_SUPABASE_ANON_KEY?: unknown
  VITE_LOCAL_URL?: unknown
}

export interface SupabaseConfig {
  supabaseUrl: string
  supabaseAnonKey: string
}

export interface BootConfig extends SupabaseConfig {
  localUrl: string
}

function requiredString(name: keyof BootEnvironment, value: unknown): string {
  const normalized = typeof value === 'string' ? value.trim() : ''
  if (!normalized) {
    throw new Error(`환경변수 ${name}이(가) 없습니다. Vercel 환경변수 또는 frontend/.env를 확인하세요.`)
  }
  return normalized
}

function isLoopback(hostname: string): boolean {
  return hostname === '127.0.0.1' || hostname === 'localhost'
}

function isHostedSupabase(hostname: string): boolean {
  return hostname.endsWith('.supabase.co')
}

function normalizeSupabaseUrl(value: unknown): string {
  const configured = requiredString('VITE_SUPABASE_URL', value)
  let parsed: URL
  try {
    parsed = new URL(configured)
  } catch (cause) {
    throw new Error('VITE_SUPABASE_URL이 올바른 URL이 아닙니다.', { cause })
  }

  const secureRemote = parsed.protocol === 'https:' && isHostedSupabase(parsed.hostname)
  const localDevelopment = parsed.protocol === 'http:' && isLoopback(parsed.hostname) && Boolean(parsed.port)
  if ((!secureRemote && !localDevelopment) || parsed.username || parsed.password || parsed.pathname !== '/' || parsed.search || parsed.hash) {
    throw new Error('VITE_SUPABASE_URL은 https://<project>.supabase.co 형식이어야 합니다. 로컬 개발에서는 포트가 있는 loopback HTTP 주소만 허용됩니다.')
  }

  return parsed.origin
}

export function resolveSupabaseConfig(environment: BootEnvironment): SupabaseConfig {
  const key = requiredString('VITE_SUPABASE_ANON_KEY', environment.VITE_SUPABASE_ANON_KEY)
  let isPublic = key.startsWith('sb_publishable_') && key.length > 'sb_publishable_'.length
  const parts = key.split('.')
  if (parts.length === 3) {
    try {
      isPublic = JSON.parse(atob(parts[1].replace(/-/g, '+').replace(/_/g, '/'))).role === 'anon'
    } catch { /* Reject malformed keys without printing their contents. */ }
  }
  if (!isPublic) throw new Error('VITE_SUPABASE_ANON_KEY에는 공개 publishable/anon 키만 사용할 수 있습니다.')
  return {
    supabaseUrl: normalizeSupabaseUrl(environment.VITE_SUPABASE_URL),
    supabaseAnonKey: key,
  }
}

// This exact whitelist is served to clinic; never serialize the environment.
export function publicClinicConfig(environment: BootEnvironment) {
  return { version: 1, ...resolveSupabaseConfig(environment) }
}

export function resolveBootConfig(environment: BootEnvironment): BootConfig {
  const supabaseConfig = resolveSupabaseConfig(environment)
  const configuredLocalUrl = typeof environment.VITE_LOCAL_URL === 'string'
    ? environment.VITE_LOCAL_URL.trim()
    : ''

  return {
    ...supabaseConfig,
    localUrl: normalizeLocalUrl(configuredLocalUrl || DEFAULT_LOCAL_URL),
  }
}
