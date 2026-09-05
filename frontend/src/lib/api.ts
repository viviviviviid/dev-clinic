import { supabase } from './supabase'
import { normalizeLocalUrl } from './localUrl'

const configuredLocalUrl = import.meta.env.VITE_LOCAL_URL?.trim()
const defaultLocalUrl = 'http://127.0.0.1:47291'

// The Vercel app is static. Every application API is served by the local clinic.
export const LOCAL: string = normalizeLocalUrl(configuredLocalUrl || defaultLocalUrl)
export const WS_BASE: string = LOCAL.replace(/^http/, 'ws')

type ApiErrorKind = 'auth' | 'connection' | 'http' | 'parse'

export class ApiError extends Error {
  readonly kind: ApiErrorKind
  readonly status: number | null
  readonly details?: unknown
  readonly code?: string

  constructor(message: string, options: { kind: ApiErrorKind; status?: number; details?: unknown; cause?: unknown }) {
    super(message, { cause: options.cause })
    this.name = 'ApiError'
    this.kind = options.kind
    this.status = options.status ?? null
    this.details = options.details
    this.code = errorCode(options.details)
  }
}

interface ApiFetchOptions extends RequestInit {
  /** Avoids a second Supabase lookup while handling an auth-state callback. */
  accessToken?: string
}

export interface ApiAuthFailure {
  status: 401 | 403
  message: string
  code?: string
}

const authFailureListeners = new Set<(failure: ApiAuthFailure) => void>()

export function subscribeApiAuthFailure(listener: (failure: ApiAuthFailure) => void): () => void {
  authFailureListeners.add(listener)
  return () => authFailureListeners.delete(listener)
}

function notifyAuthFailure(status: 401 | 403, message: string, code?: string) {
  for (const listener of authFailureListeners) listener({ status, message, code })
}

function errorCode(details: unknown): string | undefined {
  if (!details || typeof details !== 'object') return undefined
  const code = (details as Record<string, unknown>).code
  return typeof code === 'string' ? code : undefined
}

function apiUrl(path: string): string {
  return `${LOCAL}${path.startsWith('/') ? path : `/${path}`}`
}

async function readErrorBody(response: Response): Promise<unknown> {
  const contentType = response.headers.get('content-type') ?? ''
  try {
    if (contentType.includes('application/json')) return await response.json()
    const text = await response.text()
    return text || undefined
  } catch {
    return undefined
  }
}

function errorMessage(status: number, details: unknown): string {
  if (typeof details === 'string' && details.trim()) return details
  if (details && typeof details === 'object') {
    const body = details as Record<string, unknown>
    if (typeof body.error === 'string' && body.error.trim()) return body.error
    if (typeof body.message === 'string' && body.message.trim()) return body.message
  }
  return `clinic 요청에 실패했습니다. (HTTP ${status})`
}

export async function apiFetch(path: string, options: ApiFetchOptions = {}): Promise<Response> {
  const { accessToken: providedToken, headers: providedHeaders, ...requestInit } = options
  let accessToken = providedToken

  if (!accessToken) {
    const { data, error } = await supabase.auth.getSession()
    if (error) {
      const message = '로그인 정보를 확인할 수 없습니다.'
      notifyAuthFailure(401, message)
      throw new ApiError(message, {
        kind: 'auth',
        status: 401,
        details: error,
        cause: error,
      })
    }
    accessToken = data.session?.access_token
  }

  if (!accessToken) {
    const message = '로그인 세션이 없습니다. 다시 로그인해 주세요.'
    notifyAuthFailure(401, message)
    throw new ApiError(message, {
      kind: 'auth',
      status: 401,
    })
  }

  const headers = new Headers(providedHeaders)
  if (!headers.has('Authorization')) {
    headers.set('Authorization', `Bearer ${accessToken}`)
  }
  if (typeof requestInit.body === 'string' && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  let response: Response
  try {
    response = await fetch(apiUrl(path), { ...requestInit, headers })
  } catch (cause) {
    if (cause instanceof Error && cause.name === 'AbortError') throw cause
    throw new ApiError(`로컬 clinic에 연결할 수 없습니다. ${LOCAL}에서 clinic이 실행 중인지 확인하세요.`, {
      kind: 'connection',
      cause,
    })
  }

  if (!response.ok) {
    const details = await readErrorBody(response)
    const message = errorMessage(response.status, details)
    if (response.status === 401 || response.status === 403) {
      notifyAuthFailure(response.status, message, errorCode(details))
    }
    throw new ApiError(message, {
      kind: 'http',
      status: response.status,
      details,
    })
  }

  return response
}

export async function readApiJson<T>(response: Response): Promise<T> {
  try {
    return await response.json() as T
  } catch (cause) {
    throw new ApiError('clinic이 올바른 JSON 응답을 반환하지 않았습니다.', {
      kind: 'parse',
      status: response.status,
      cause,
    })
  }
}

export async function apiJson<T>(path: string, options?: ApiFetchOptions): Promise<T> {
  return readApiJson<T>(await apiFetch(path, options))
}
