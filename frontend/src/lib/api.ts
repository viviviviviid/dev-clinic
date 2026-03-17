// Architecture: browser talks to two servers.
//
// REMOTE = home server (tutor.abcfe.net or VITE_REMOTE_URL for dev)
//   - Gemini AI, Supabase, user auth, daily missions, chat, explain
//
// LOCAL  = local binary on the user's machine (localhost:47291)
//   - File I/O, code execution, watcher, LSP, terminal
//
// In single-binary dev mode, set VITE_REMOTE_URL='' and VITE_LOCAL_URL=''
// so both point to the same origin.

// Home server: where AI and Supabase live.
// In production this is the page origin (frontend is served from the home server).
// In dev, set VITE_REMOTE_URL=http://localhost:8080 to point at a local homeserver.
export const REMOTE: string = import.meta.env.VITE_REMOTE_URL ?? window.location.origin

// Local binary: always localhost:47291.
// Override with VITE_LOCAL_URL for unusual setups.
export const LOCAL: string = import.meta.env.VITE_LOCAL_URL ?? 'http://localhost:47291'

export const WS_BASE: string = LOCAL.replace(/^http/, 'ws')

// Convenience: AI proxy URL the local server will call back into the home server.
export const AI_PROXY_URL: string = `${REMOTE}/api/ai/proxy`
