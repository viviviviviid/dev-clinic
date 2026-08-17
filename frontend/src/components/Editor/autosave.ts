export interface EditorAutosave {
  schedule(path: string, content: string): void
  flushPath(path: string): Promise<boolean>
  flushAll(): Promise<boolean>
  saveNow(path: string, content: string): Promise<boolean>
  hasPendingWrites(): boolean
}

interface PendingSave {
  version: number
  content: string
  timer: ReturnType<typeof setTimeout>
}

interface FailedSave {
  version: number
  content: string
}

interface FileSaveState {
  version: number
  persistedVersion: number
  pending?: PendingSave
  failed?: FailedSave
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
  let scheduleEpoch = 0

  function stateFor(path: string): FileSaveState {
    const existing = states.get(path)
    if (existing) return existing
    const state: FileSaveState = { version: 0, persistedVersion: 0, tail: Promise.resolve(), queuedWrites: 0 }
    states.set(path, state)
    return state
  }

  function schedule(path: string, content: string): void {
    scheduleEpoch++
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
        state.persistedVersion = Math.max(state.persistedVersion, version)
        if (state.failed && state.failed.version <= version) state.failed = undefined
        if (state.version === version && !state.pending) options.onSaved(path)
        return true
      } catch (error: unknown) {
        if (state.version === version && !state.pending) state.failed = { version, content }
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
    if (!state) return true
    for (;;) {
      const pending = state.pending
      if (pending) {
        clearTimeout(pending.timer)
        state.pending = undefined
        await enqueue(path, pending.version, pending.content)
      } else if (state.failed && state.failed.version === state.version) {
        const failed = state.failed
        state.failed = undefined
        await enqueue(path, failed.version, failed.content)
      } else {
        await state.tail
      }
      // An edit can arrive while an older write is in flight. Flush that
      // version too so explicit Run/Test/Review always sees editor contents.
      if (state.pending || state.queuedWrites > 0) continue
      return state.persistedVersion === state.version
    }
  }

  async function flushAll(): Promise<boolean> {
    for (;;) {
      const startEpoch = scheduleEpoch
      const results = await Promise.all(Array.from(states.keys(), path => flushPath(path)))
      // A brand-new file can be edited while existing writes are in flight.
      // Re-snapshot the map until no schedule occurred during this flush.
      if (scheduleEpoch !== startEpoch) continue
      return results.every(Boolean)
    }
  }

  async function saveNow(path: string, content: string): Promise<boolean> {
    schedule(path, content)
    return flushPath(path)
  }

  function hasPendingWrites(): boolean {
    for (const state of states.values()) {
      if (state.pending || state.failed || state.queuedWrites > 0) return true
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
