export interface PreferenceStorage {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
}

function browserStorage(): PreferenceStorage | null {
  try {
    return typeof window === 'undefined' ? null : window.localStorage
  } catch {
    return null
  }
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value))
}

export function readClampedNumber(
  key: string,
  fallback: number,
  min: number,
  max: number,
  storage: PreferenceStorage | null = browserStorage(),
): number {
  const safeFallback = Number.isFinite(fallback) ? clamp(fallback, min, max) : min
  if (!storage) return safeFallback

  try {
    const raw = storage.getItem(key)
    if (raw === null || raw.trim() === '') return safeFallback
    const parsed = Number(raw)
    return Number.isFinite(parsed) ? clamp(parsed, min, max) : safeFallback
  } catch {
    return safeFallback
  }
}

export function writePreference(
  key: string,
  value: string | number | boolean,
  storage: PreferenceStorage | null = browserStorage(),
): boolean {
  if (!storage) return false
  try {
    storage.setItem(key, String(value))
    return true
  } catch {
    return false
  }
}
