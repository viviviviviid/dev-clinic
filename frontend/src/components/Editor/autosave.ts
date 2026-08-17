export interface EditorAutosave {
  schedule(path: string, content: string): void
  flushPath(path: string): Promise<boolean>
  flushAll(): Promise<void>
  saveNow(path: string, content: string): Promise<boolean>
  hasPendingWrites(): boolean
}

interface PendingSave {
  version: number
  content: string
  timer: ReturnType<typeof setTimeout>
}

interface FileSaveState {
  version: number
  pending?: PendingSave
  tail: Promise<void>
  queuedWrites: number
}

export interface AutosaveLifecycleTarget {
  addEventListener(type: string, listener: EventListener): void
  removeEventListener(type: string, listener: EventListener): void
}

interface AutosaveOptions {
  delayMs: number
  write: (path: string, content: string) => Promise<void>
  onSaved: (path: string) => void
  onError: (path: string, error: unknown) => void
}

export function createEditorAutosave(options: AutosaveOptions): EditorAutosave {
  const states = new Map<string, FileSaveState>()

  function stateFor(path: string): FileSaveState {
    const existing = states.get(path)
    if (existing) return existing
    const state: FileSaveState = { version: 0, tail: Promise.resolve(), queuedWrites: 0 }
    states.set(path, state)
    return state
  }

  function schedule(path: string, content: string): void {
    const state = stateFor(path)
    state.version++
    if (state.pending) clearTimeout(state.pending.timer)
    const version = state.version
    const timer = setTimeout(() => { void flushPath(path) }, options.delayMs)
    state.pending = { version, content, timer }
  }

  function enqueue(path: string, version: number, content: string): Promise<boolean> {
    const state = stateFor(path)

    state.queuedWrites++
    const result = state.tail.then(async () => {
      try {
        await options.write(path, content)
        if (state.version === version && !state.pending) options.onSaved(path)
        return true
      } catch (error: unknown) {
        options.onError(path, error)
        return false
      } finally {
        state.queuedWrites--
      }
    })
    state.tail = result.then(() => undefined)
    return result
  }

  async function flushPath(path: string): Promise<boolean> {
    const state = states.get(path)
    if (!state?.pending) {
      if (state) await state.tail
      return true
    }
    const pending = state.pending
    clearTimeout(pending.timer)
    state.pending = undefined
    return enqueue(path, pending.version, pending.content)
  }

  async function flushAll(): Promise<void> {
    await Promise.all(Array.from(states.keys(), path => flushPath(path)))
  }

  async function saveNow(path: string, content: string): Promise<boolean> {
    schedule(path, content)
    return flushPath(path)
  }

  function hasPendingWrites(): boolean {
    for (const state of states.values()) {
      if (state.pending || state.queuedWrites > 0) return true
    }
    return false
  }

  return { schedule, flushPath, flushAll, saveNow, hasPendingWrites }
}

export function installEditorAutosaveLifecycle(
  autosave: EditorAutosave,
  target: AutosaveLifecycleTarget = window,
): () => void {
  const flushOnPageHide: EventListener = () => {
    void autosave.flushAll()
  }
  const warnAndFlushBeforeUnload: EventListener = (event) => {
    if (!autosave.hasPendingWrites()) return
    event.preventDefault()
    ;(event as BeforeUnloadEvent).returnValue = ''
    void autosave.flushAll()
  }

  target.addEventListener('pagehide', flushOnPageHide)
  target.addEventListener('beforeunload', warnAndFlushBeforeUnload)
  return () => {
    target.removeEventListener('pagehide', flushOnPageHide)
    target.removeEventListener('beforeunload', warnAndFlushBeforeUnload)
  }
}
