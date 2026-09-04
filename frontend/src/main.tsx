import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import BootErrorScreen from './components/BootError'
import AppErrorBoundary from './components/ErrorBoundary'
import { resolveBootConfig } from './lib/bootConfig'

function findOrCreateRoot(): HTMLElement {
  const existing = document.getElementById('root')
  if (existing) return existing
  const fallback = document.createElement('div')
  fallback.id = 'root'
  document.body.append(fallback)
  return fallback
}

const root = createRoot(findOrCreateRoot())

// A browser can keep an old entry chunk open while a Vercel deployment removes
// one of its lazy-loaded Monaco language chunks. Reload once into the current
// deployment instead of leaving the editor in a broken partial state.
const staleChunkReloadKey = 'clinic:stale-chunk-reload'
window.addEventListener('vite:preloadError', event => {
  if (sessionStorage.getItem(staleChunkReloadKey) === '1') return
  sessionStorage.setItem(staleChunkReloadKey, '1')
  event.preventDefault()
  window.location.reload()
})
window.setTimeout(() => sessionStorage.removeItem(staleChunkReloadKey), 5_000)

function renderBootError(error: unknown) {
  root.render(
    <StrictMode>
      <BootErrorScreen error={error} />
    </StrictMode>,
  )
}

async function bootstrap() {
  try {
    resolveBootConfig(import.meta.env)
    const { default: App } = await import('./App.tsx')
    root.render(
      <StrictMode>
        <AppErrorBoundary>
          <App />
        </AppErrorBoundary>
      </StrictMode>,
    )
  } catch (error: unknown) {
    renderBootError(error)
  }
}

void bootstrap()
