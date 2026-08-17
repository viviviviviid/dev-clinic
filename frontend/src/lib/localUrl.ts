const ALLOWED_LOOPBACK_HOSTS = new Set(['127.0.0.1', 'localhost'])

/** Mirrors vercel.json: clinic traffic may use either allowed loopback host with an explicit port. */
export function normalizeLocalUrl(value: string): string {
  let parsed: URL
  try {
    parsed = new URL(value)
  } catch (cause) {
    throw new Error('VITE_LOCAL_URL이 올바른 URL이 아닙니다.', { cause })
  }

  const isLoopback = ALLOWED_LOOPBACK_HOSTS.has(parsed.hostname)
  if (parsed.protocol !== 'http:' || !isLoopback || !parsed.port || parsed.username || parsed.password ||
      parsed.pathname !== '/' || parsed.search || parsed.hash) {
    throw new Error('VITE_LOCAL_URL은 경로가 없는 http://127.0.0.1:<port> 또는 http://localhost:<port>여야 합니다.')
  }
  return parsed.origin
}
