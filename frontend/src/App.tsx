import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react'
import type { AuthChangeEvent, Session, User } from '@supabase/supabase-js'
import { supabase } from './lib/supabase'
import AuthScreen from './components/Auth'
import SettingsScreen from './components/Settings'
import FileTree from './components/FileTree'
import ToastContainer from './components/Toast'
import { useWebSocket } from './hooks/useWebSocket'
import { useStore } from './store'
import type { ProjectStatus, SkillLevel, UserSettings } from './store'
import { useProject } from './hooks/useProject'
import { lspClient } from './lib/lspClient'
import './App.css'
import { ApiError, LOCAL, apiFetch, apiJson, subscribeApiAuthFailure } from './lib/api'
import { readClampedNumber, writePreference } from './lib/storage'
import { isAbortError } from './lib/errors'

const DashboardScreen = lazy(() => import('./components/Dashboard'))
const Editor = lazy(() => import('./components/Editor'))
const FeedbackPanel = lazy(() => import('./components/FeedbackPanel'))
const TerminalPanel = lazy(() => import('./components/Terminal'))
const ProblemsPanel = lazy(() => import('./components/ProblemsPanel'))
const QuickOpen = lazy(() => import('./components/QuickOpen'))
const SearchPanel = lazy(() => import('./components/SearchPanel'))

const SKILL_BADGE: Record<string, string> = {
  newbie: '🌱 뉴비',
  normal: '⚡ 보통',
  experienced: '🔥 숙련자',
}

interface ClinicFailure {
  message: string
  kind: 'auth' | 'connection' | 'http' | 'parse' | 'unknown'
  status: number | null
}

function clinicFailureFrom(error: unknown): ClinicFailure {
  if (error instanceof ApiError) {
    return { message: error.message, kind: error.kind, status: error.status }
  }
  return {
    message: error instanceof Error ? error.message : '로컬 clinic 요청에 실패했습니다.',
    kind: 'unknown',
    status: null,
  }
}

function LoadingFallback({ label }: { label: string }) {
  return (
    <div style={{ minHeight: 48, height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#8b949e' }}>
      <span>{label}</span>
    </div>
  )
}

function ConnectionBanner({ status, onRetry }: { status: 'reconnecting' | 'disconnected'; onRetry: () => void }) {
  const reconnecting = status === 'reconnecting'
  return (
    <div className={`ws-reconnect-banner ${reconnecting ? '' : 'ws-disconnected'}`} role="status">
      <span className="ws-reconnect-dot" />
      <span>{reconnecting ? '로컬 clinic에 다시 연결하는 중…' : '로컬 clinic 연결이 끊겼습니다.'}</span>
      <button type="button" onClick={onRetry}>지금 다시 연결</button>
    </div>
  )
}

function normalizeSkillLevel(value: string | undefined): SkillLevel {
  if (value === 'newbie' || value === 'experienced') return value
  return 'normal'
}

// ── Resizer hook ──────────────────────────────────────────
function useResize(
  direction: 'horizontal' | 'vertical',
  onDelta: (delta: number) => void,
) {
  const onMouseDown = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault()
      const start = direction === 'horizontal' ? e.clientX : e.clientY
      document.body.style.cursor = direction === 'horizontal' ? 'col-resize' : 'row-resize'
      document.body.style.userSelect = 'none'

      const onMove = (ev: MouseEvent) => {
        const cur = direction === 'horizontal' ? ev.clientX : ev.clientY
        onDelta(cur - start)
      }
      const onUp = () => {
        document.body.style.cursor = ''
        document.body.style.userSelect = ''
        document.removeEventListener('mousemove', onMove)
        document.removeEventListener('mouseup', onUp)
      }
      document.addEventListener('mousemove', onMove)
      document.addEventListener('mouseup', onUp)
    },
    [direction, onDelta],
  )
  return onMouseDown
}

// ── Resizer handle component ──────────────────────────────
interface ResizerHandleProps {
  direction: 'horizontal' | 'vertical'
  onMouseDown: (e: React.MouseEvent) => void
  onKeyDelta: (delta: number) => void
  label: string
}
function ResizerHandle({ direction, onMouseDown, onKeyDelta, label }: ResizerHandleProps) {
  return (
    <div
      className={`resizer resizer-${direction}`}
      onMouseDown={onMouseDown}
      onKeyDown={(event) => {
        const step = event.shiftKey ? 40 : 10
        if (direction === 'horizontal' && (event.key === 'ArrowLeft' || event.key === 'ArrowRight')) {
          event.preventDefault()
          onKeyDelta(event.key === 'ArrowLeft' ? -step : step)
        }
        if (direction === 'vertical' && (event.key === 'ArrowUp' || event.key === 'ArrowDown')) {
          event.preventDefault()
          onKeyDelta(event.key === 'ArrowUp' ? -step : step)
        }
      }}
      role="separator"
      aria-label={label}
      aria-orientation={direction}
      tabIndex={0}
    >
      <div className="resizer-grip" />
    </div>
  )
}

// ── Main App ──────────────────────────────────────────────
export default function App() {
  const [authLoading, setAuthLoading] = useState(true)
  const [user, setUserLocal] = useState<User | null>(null)
  const [userSettings, setUserSettingsLocal] = useState<UserSettings | null>(null)
  const [settingsLoading, setSettingsLoading] = useState(false)
  const [clinicError, setClinicError] = useState<ClinicFailure | null>(null)
  const [showSettings, setShowSettings] = useState(false)
  const [terminalOpen, setTerminalOpen] = useState(false)
  const [problemsOpen, setProblemsOpen] = useState(false)
  const [sidebarOpen, setSidebarOpen] = useState(() => window.innerWidth >= 900)
  const [viewportWidth, setViewportWidth] = useState(() => window.innerWidth)
  const [wsRetryKey, setWsRetryKey] = useState(0)
  const [leavingProject, setLeavingProject] = useState(false)

  const { setUser, setUserSettings, projectStatus, setProjectStatus, resetWorkspace, setSkillLevel, setQuizData, setSnapshots, addToast, showQuickOpen, setShowQuickOpen, showSearchPanel, setShowSearchPanel, wsStatus } = useStore()
  const { refreshFileTree, loadQuizData, listSnapshots } = useProject()

  // Panel sizes
  const [sidebarWidth, setSidebarWidth] = useState(200)
  const [feedbackWidth, setFeedbackWidth] = useState(() => {
    const fallback = Math.max(360, Math.min(600, Math.round(window.innerWidth * 0.28)))
    return readClampedNumber('feedbackWidth', fallback, 160, 600)
  })
  const [terminalHeight, setTerminalHeight] = useState(240)
  const [problemsHeight, setProblemsHeight] = useState(180)

  useEffect(() => {
    writePreference('feedbackWidth', feedbackWidth)
  }, [feedbackWidth])

  useEffect(() => {
    const onResize = () => setViewportWidth(window.innerWidth)
    window.addEventListener('resize', onResize, { passive: true })
    return () => window.removeEventListener('resize', onResize)
  }, [])

  const sidebarRef = useRef(sidebarWidth)
  const feedbackRef = useRef(feedbackWidth)
  const terminalRef = useRef(terminalHeight)
  const problemsRef = useRef(problemsHeight)
  const settingsOwnerRef = useRef<string | null>(null)
  const settingsRequestRef = useRef<AbortController | null>(null)
  const missionReadyAbortRef = useRef<AbortController | null>(null)
  const lastAccessTokenRef = useRef<string | null>(null)

  const onSidebarResize = useResize('horizontal', useCallback((delta: number) => {
    setSidebarWidth(Math.max(120, Math.min(480, sidebarRef.current + delta)))
  }, []))

  const onFeedbackResize = useResize('horizontal', useCallback((delta: number) => {
    setFeedbackWidth(Math.max(160, Math.min(600, feedbackRef.current - delta)))
  }, []))

  const onTerminalResize = useResize('vertical', useCallback((delta: number) => {
    setTerminalHeight(Math.max(80, Math.min(600, terminalRef.current - delta)))
  }, []))

  const onProblemsResize = useResize('vertical', useCallback((delta: number) => {
    setProblemsHeight(Math.max(80, Math.min(400, problemsRef.current - delta)))
  }, []))

  const makeSidebarDown = useCallback((e: React.MouseEvent) => {
    sidebarRef.current = sidebarWidth
    onSidebarResize(e)
  }, [sidebarWidth, onSidebarResize])

  const makeFeedbackDown = useCallback((e: React.MouseEvent) => {
    feedbackRef.current = feedbackWidth
    onFeedbackResize(e)
  }, [feedbackWidth, onFeedbackResize])

  const makeTerminalDown = useCallback((e: React.MouseEvent) => {
    terminalRef.current = terminalHeight
    onTerminalResize(e)
  }, [terminalHeight, onTerminalResize])

  const makeProblemsDown = useCallback((e: React.MouseEvent) => {
    problemsRef.current = problemsHeight
    onProblemsResize(e)
  }, [problemsHeight, onProblemsResize])

  useWebSocket(Boolean(user && userSettings && !clinicError), wsRetryKey)

  useEffect(() => subscribeApiAuthFailure((failure) => {
    setClinicError({ message: failure.message, kind: 'auth', status: failure.status })
  }), [])

  // Global keyboard shortcuts
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && !e.shiftKey && e.key === 'p') {
        e.preventDefault()
        setShowQuickOpen(true)
      }
      if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key === 'f') {
        e.preventDefault()
        setShowSearchPanel(!showSearchPanel)
      }
      if ((e.metaKey || e.ctrlKey) && !e.shiftKey && e.key === 'j') {
        e.preventDefault()
        setTerminalOpen((v) => !v)
      }
      if ((e.metaKey || e.ctrlKey) && !e.shiftKey && e.key === 'b') {
        e.preventDefault()
        setSidebarOpen((v) => !v)
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [showSearchPanel, setShowQuickOpen, setShowSearchPanel])

  const loadSettings = useCallback(async (
    ownerId: string,
    accessToken: string,
    signal: AbortSignal,
    blocking = true,
  ) => {
    if (blocking) setSettingsLoading(true)
    setClinicError(null)
    try {
      const data = await apiJson<Partial<UserSettings>>('/api/user/settings', {
        accessToken,
        signal,
      })
      if (signal.aborted || settingsOwnerRef.current !== ownerId) return
      const settings = typeof data.user_id === 'string' ? data as UserSettings : null
      setUserSettingsLocal(settings)
      setUserSettings(settings)
      if (settings?.language_supported === false) setShowSettings(true)
    } catch (error: unknown) {
      if (isAbortError(error) || signal.aborted || settingsOwnerRef.current !== ownerId) return
      setUserSettingsLocal(null)
      setUserSettings(null)
      setClinicError(clinicFailureFrom(error))
    } finally {
      if (!signal.aborted && settingsOwnerRef.current === ownerId) {
        if (blocking) setSettingsLoading(false)
        setAuthLoading(false)
      }
    }
  }, [setUserSettings])

  const requestSettings = useCallback((ownerId: string, accessToken: string, blocking = true) => {
    settingsRequestRef.current?.abort()
    const abort = new AbortController()
    settingsRequestRef.current = abort
    settingsOwnerRef.current = ownerId
    return loadSettings(ownerId, accessToken, abort.signal, blocking).finally(() => {
      if (settingsRequestRef.current === abort) settingsRequestRef.current = null
    })
  }, [loadSettings])

  const stopActiveWatcher = useCallback(async (accessToken?: string | null, force = false): Promise<void> => {
    const state = useStore.getState()
    if (!force && !state.projectStatus?.loaded && !state.projectDir) return

    const abort = new AbortController()
    const timeout = window.setTimeout(() => abort.abort(), 5_000)
    try {
      await apiFetch('/api/project/stop-watcher', {
        method: 'POST',
        body: '{}',
        accessToken: accessToken || undefined,
        signal: abort.signal,
      })
    } finally {
      window.clearTimeout(timeout)
    }
  }, [])

  // Auth state listener
  useEffect(() => {
    let cancelled = false
    let syncVersion = 0

    const syncSession = (session: Session | null, event: AuthChangeEvent) => {
      const version = ++syncVersion
      void (async () => {
        if (cancelled) return
        const currentUser = session?.user ?? null
        const previousState = useStore.getState()
        const previousUser = previousState.user
        const accountChanged = previousUser?.id !== currentUser?.id

        if (accountChanged) {
          setAuthLoading(true)
          settingsRequestRef.current?.abort()
          settingsRequestRef.current = null
          const missionWasLoading = missionReadyAbortRef.current !== null
          missionReadyAbortRef.current?.abort()
          missionReadyAbortRef.current = null
          lspClient.disconnect()

          try {
            await stopActiveWatcher(lastAccessTokenRef.current, missionWasLoading)
          } catch {
            // Signing out must not be blocked when the local clinic is unavailable.
          }
          if (cancelled || version !== syncVersion) return

          resetWorkspace()
          settingsOwnerRef.current = null
          setUserSettingsLocal(null)
          setUserSettings(null)
          setClinicError(null)
          setSettingsLoading(false)
        }

        if (cancelled || version !== syncVersion) return
        lastAccessTokenRef.current = session?.access_token ?? null
        setUserLocal(currentUser)
        setUser(currentUser)

        if (!session || !currentUser) {
          settingsOwnerRef.current = null
          setAuthLoading(false)
          return
        }

        const ownerMatches = settingsOwnerRef.current === currentUser.id
        const settingsLoaded = ownerMatches && useStore.getState().userSettings !== null
        const settingsPending = ownerMatches && settingsRequestRef.current !== null
        if (settingsLoaded) {
          setAuthLoading(false)
          return
        }
        if (settingsPending && (event === 'INITIAL_SESSION' || event === 'TOKEN_REFRESHED' || event === 'SIGNED_IN')) {
          return
        }

        void requestSettings(currentUser.id, session.access_token, !settingsLoaded)
      })()
    }

    void supabase.auth.getSession()
      .then(({ data }) => syncSession(data.session, 'INITIAL_SESSION'))
      .catch(() => {
        if (!cancelled) setAuthLoading(false)
      })

    const { data: { subscription } } = supabase.auth.onAuthStateChange((event, session) => {
      syncSession(session, event)
    })

    return () => {
      cancelled = true
      syncVersion += 1
      settingsRequestRef.current?.abort()
      settingsRequestRef.current = null
      missionReadyAbortRef.current?.abort()
      missionReadyAbortRef.current = null
      subscription.unsubscribe()
    }
  }, [requestSettings, resetWorkspace, setUser, setUserSettings, stopActiveWatcher])

  async function retryClinicConnection() {
    try {
      const { data, error } = await supabase.auth.getSession()
      if (error) throw error
      if (!data.session) {
        setClinicError({ message: '로그인 세션이 없습니다. 다시 로그인해 주세요.', kind: 'auth', status: 401 })
        return
      }
      await requestSettings(data.session.user.id, data.session.access_token)
    } catch (error: unknown) {
      setClinicError(clinicFailureFrom(error))
    }
  }

  async function switchClinicAccount() {
    try {
      const missionWasLoading = missionReadyAbortRef.current !== null
      missionReadyAbortRef.current?.abort()
      missionReadyAbortRef.current = null
      lspClient.disconnect()
      try {
        await stopActiveWatcher(lastAccessTokenRef.current, missionWasLoading)
      } catch {
        // Account switching is still allowed if the clinic is already offline.
      }
      resetWorkspace()
      const { error } = await supabase.auth.signOut({ scope: 'local' })
      if (error) throw error
    } catch (error: unknown) {
      const message = error instanceof Error ? error.message : String(error)
      setClinicError({ message: `로그아웃하지 못했습니다: ${message}`, kind: 'auth', status: null })
    }
  }

  async function handleSignOut() {
    const missionWasLoading = missionReadyAbortRef.current !== null
    missionReadyAbortRef.current?.abort()
    missionReadyAbortRef.current = null
    lspClient.disconnect()
    try {
      await stopActiveWatcher(lastAccessTokenRef.current, missionWasLoading)
    } catch {
      addToast('파일 감시자 종료를 확인하지 못했지만 로그아웃을 계속합니다.', 'info')
    }
    resetWorkspace()

    try {
      const { error } = await supabase.auth.signOut()
      if (error) throw error
    } catch (error: unknown) {
      addToast(`로그아웃하지 못했습니다: ${error instanceof Error ? error.message : String(error)}`, 'error')
    }
  }

  function handleSettingsSaved(settings: UserSettings) {
    setUserSettingsLocal(settings)
    setUserSettings(settings)
    setShowSettings(false)
  }

  async function handleBackToDashboard() {
    if (leavingProject) return
    setLeavingProject(true)
    missionReadyAbortRef.current?.abort()
    missionReadyAbortRef.current = null
    lspClient.disconnect()
    try {
      await stopActiveWatcher(lastAccessTokenRef.current)
    } catch (error: unknown) {
      if (!isAbortError(error)) {
        addToast('파일 감시자 종료를 확인하지 못했습니다. 새 미션을 열 때 다시 초기화합니다.', 'info')
      }
    } finally {
      resetWorkspace()
      setLeavingProject(false)
    }
  }

  async function handleMissionReady(projectDir: string, fallbackSkillLevel: string) {
    missionReadyAbortRef.current?.abort()
    const abort = new AbortController()
    missionReadyAbortRef.current = abort
    const ownerId = useStore.getState().user?.id ?? null
    resetWorkspace()
    try {
      // A saved project's curriculum owns its teaching mode. Falling back to the
      // current profile is only for legacy projects without a stored level.
      const effectiveSkillLevel = normalizeSkillLevel(fallbackSkillLevel || userSettings?.skill_level)
      setSkillLevel(effectiveSkillLevel)
      const status = await apiJson<ProjectStatus>('/api/project/status', { signal: abort.signal })
      const [quiz, snapshots] = await Promise.all([
        effectiveSkillLevel === 'newbie' ? loadQuizData(abort.signal) : Promise.resolve(null),
        listSnapshots(abort.signal),
        refreshFileTree(projectDir, abort.signal),
      ])
      if (abort.signal.aborted || useStore.getState().user?.id !== ownerId) {
        abort.abort()
        throw abort.signal.reason
      }

      setProjectStatus(status)
      setQuizData(quiz ?? {})
      setSnapshots(snapshots)

      // Connect LSP for the project language (best-effort, fallback to regex if unavailable).
      const lang = (status.language || userSettings?.language || 'go').toLowerCase()
      void lspClient.connect(lang, status.dir || projectDir)
        .catch((error: unknown) => console.warn('LSP unavailable, using fallback:', error))
    } catch (error: unknown) {
      if (!isAbortError(error) && useStore.getState().user?.id === ownerId) {
        try {
          await stopActiveWatcher(lastAccessTokenRef.current, true)
        } catch {
          // Preserve the initialization error; the next project load restarts the watcher.
        }
        resetWorkspace()
      }
      throw error
    } finally {
      if (missionReadyAbortRef.current === abort) missionReadyAbortRef.current = null
    }
  }

  const connectionBanner = userSettings && wsStatus !== 'connected'
    ? <ConnectionBanner status={wsStatus} onRetry={() => setWsRetryKey((key) => key + 1)} />
    : null

  // Loading state
  if (authLoading || settingsLoading) {
    return <div className="loading-screen"><span>로딩 중...</span></div>
  }

  // Not logged in
  if (!user) {
    return <AuthScreen />
  }

  if (clinicError) {
    const isAccessError = clinicError.status === 401 || clinicError.status === 403 || clinicError.kind === 'auth'
    const title = isAccessError
      ? '이 계정으로 clinic을 사용할 수 없습니다'
      : clinicError.kind === 'connection'
        ? '로컬 clinic에 연결할 수 없습니다'
        : 'clinic 요청을 처리하지 못했습니다'
    const guidance = isAccessError
      ? '허용된 Google 계정으로 다시 로그인하고 clinic의 ALLOWED_USER_EMAILS 또는 ALLOWED_USER_IDS를 확인하세요.'
      : clinicError.kind === 'connection'
        ? 'clinic 실행 여부, Chrome 사이트 설정의 로컬 네트워크 액세스 권한, clinic의 ALLOWED_ORIGINS를 확인하세요.'
        : clinicError.kind === 'parse'
          ? '화면과 clinic을 같은 버전으로 다시 빌드한 뒤 재시도하세요.'
          : 'clinic 로그와 Supabase·AI 공급자 설정을 확인한 뒤 재시도하세요.'

    return (
      <div className="loading-screen" role="alert">
        <div className="clinic-error-card">
          <h2>{title}</h2>
          <p>{clinicError.message}</p>
          <p>{guidance}</p>
          {!isAccessError && <p><code>{LOCAL}</code></p>}
          <div className="clinic-error-actions">
            <button className="clinic-error-primary" type="button" onClick={() => void retryClinicConnection()}>
              다시 연결
            </button>
            <button type="button" onClick={() => void switchClinicAccount()}>
              {isAccessError ? '로그아웃 / 계정 전환' : '로그아웃'}
            </button>
          </div>
        </div>
      </div>
    )
  }

  // Settings not configured yet (or user wants to edit)
  if (!userSettings || showSettings) {
    return (
      <>
        {connectionBanner}
        <SettingsScreen
          onComplete={handleSettingsSaved}
          initial={userSettings}
        />
      </>
    )
  }

  // No project loaded today — show dashboard screen
  // 테스트 모드(?test) 또는 프로젝트 미로드 상태
  const isTestMode = new URLSearchParams(window.location.search).has('test')
  if (!projectStatus?.loaded || isTestMode) {
    return (
      <>
        {connectionBanner}
        <Suspense fallback={<LoadingFallback label="대시보드 로딩 중..." />}>
          <DashboardScreen onMissionReady={handleMissionReady} onOpenSettings={() => setShowSettings(true)} />
        </Suspense>
      </>
    )
  }

  if (viewportWidth < 680) {
    return (
      <>
        {connectionBanner}
        <div className="loading-screen" role="status">
          <div className="clinic-error-card">
            <h2>코딩 작업실은 넓은 화면이 필요합니다</h2>
            <p>파일 트리, 에디터, 피드백을 함께 안전하게 보여주려면 창 너비를 680px 이상으로 늘려주세요.</p>
            <p>대시보드에서 기록을 확인하려면 로비로 돌아갈 수 있습니다.</p>
            <button
              className="clinic-error-primary"
              type="button"
              onClick={() => void handleBackToDashboard()}
              disabled={leavingProject}
            >
              {leavingProject ? '로비로 이동 중…' : '로비로 돌아가기'}
            </button>
          </div>
        </div>
      </>
    )
  }

  // Main editor
  return (
    <div className="app">
      {showQuickOpen && (
        <Suspense fallback={null}>
          <QuickOpen />
        </Suspense>
      )}
      {connectionBanner}
      <div className="app-main">
        {showSearchPanel && (
          <div className="app-search-panel">
            <Suspense fallback={<LoadingFallback label="검색 패널 로딩 중..." />}>
              <SearchPanel />
            </Suspense>
          </div>
        )}
        {sidebarOpen && (
          <>
            <div className="app-sidebar" style={{ width: sidebarWidth }}>
              <FileTree />
            </div>
            <ResizerHandle
              direction="horizontal"
              onMouseDown={makeSidebarDown}
              onKeyDelta={(delta) => setSidebarWidth((width) => Math.max(120, Math.min(480, width + delta)))}
              label="파일 트리 너비 조절"
            />
          </>
        )}

        <div className="app-editor">
          <div className="app-monaco">
            <Suspense fallback={<LoadingFallback label="에디터 로딩 중..." />}>
              <Editor />
            </Suspense>
          </div>

          {problemsOpen && (
            <>
              <ResizerHandle
                direction="vertical"
                onMouseDown={makeProblemsDown}
                onKeyDelta={(delta) => setProblemsHeight((height) => Math.max(80, Math.min(400, height - delta)))}
                label="문제 패널 높이 조절"
              />
              <div className="app-problems" style={{ height: problemsHeight }}>
                <Suspense fallback={<LoadingFallback label="문제 패널 로딩 중..." />}>
                  <ProblemsPanel />
                </Suspense>
              </div>
            </>
          )}

          {terminalOpen && (
            <>
              <ResizerHandle
                direction="vertical"
                onMouseDown={makeTerminalDown}
                onKeyDelta={(delta) => setTerminalHeight((height) => Math.max(80, Math.min(600, height - delta)))}
                label="터미널 높이 조절"
              />
              <div className="app-terminal" style={{ height: terminalHeight }}>
                <Suspense fallback={<LoadingFallback label="터미널 로딩 중..." />}>
                  <TerminalPanel onClose={() => setTerminalOpen(false)} />
                </Suspense>
              </div>
            </>
          )}
        </div>

        <ResizerHandle
          direction="horizontal"
          onMouseDown={makeFeedbackDown}
          onKeyDelta={(delta) => setFeedbackWidth((width) => Math.max(160, Math.min(600, width - delta)))}
          label="피드백 패널 너비 조절"
        />

        <div className="app-feedback" style={{ width: feedbackWidth }}>
          <Suspense fallback={<LoadingFallback label="피드백 패널 로딩 중..." />}>
            <FeedbackPanel />
          </Suspense>
        </div>
      </div>

      <ToastContainer />
      <div className="app-statusbar">
        <StatusBar
          onTerminalToggle={() => setTerminalOpen((v) => !v)}
          terminalOpen={terminalOpen}
          onProblemsToggle={() => setProblemsOpen((v) => !v)}
          problemsOpen={problemsOpen}
          onSidebarToggle={() => setSidebarOpen((v) => !v)}
          sidebarOpen={sidebarOpen}
          onSettingsOpen={() => setShowSettings(true)}
          onBackToDashboard={handleBackToDashboard}
          onSignOut={handleSignOut}
          leavingProject={leavingProject}
          userEmail={user.email}
        />
      </div>
    </div>
  )
}

interface StatusBarProps {
  onTerminalToggle: () => void
  terminalOpen: boolean
  onProblemsToggle: () => void
  problemsOpen: boolean
  onSidebarToggle: () => void
  sidebarOpen: boolean
  onSettingsOpen: () => void
  onBackToDashboard: () => void | Promise<void>
  onSignOut: () => void | Promise<void>
  leavingProject: boolean
  userEmail?: string
}

function StatusBar({ onTerminalToggle, terminalOpen, onProblemsToggle, problemsOpen, onSidebarToggle, sidebarOpen, onSettingsOpen, onBackToDashboard, onSignOut, leavingProject, userEmail }: StatusBarProps) {
  const { projectStatus, lastSync, isStreaming, skillLevel, diagnostics, showMinimap, setShowMinimap } = useStore()

  const errorCount = diagnostics.filter(d => d.severity === 1).length
  const warnCount = diagnostics.filter(d => d.severity === 2).length

  return (
    <div className="statusbar">
      <button className="statusbar-lobby-btn" onClick={() => void onBackToDashboard()} title="로비로 돌아가기" disabled={leavingProject}>
        {leavingProject ? '이동 중…' : '🏥 로비'}
      </button>
      <span className="statusbar-item lang">{projectStatus?.language || '—'}</span>
      <span className="statusbar-item step">{projectStatus?.currentStep || '—'}</span>
      {skillLevel && (
        <span className={`statusbar-item skill-badge skill-${skillLevel}`}>
          {SKILL_BADGE[skillLevel] || skillLevel}
        </span>
      )}
      {isStreaming && <span className="statusbar-item syncing">● AI 분석 중...</span>}
      {lastSync && !isStreaming && (
        <span className="statusbar-item sync-time">
          ✓ 싱크: {new Date(lastSync).toLocaleTimeString('ko-KR')}
        </span>
      )}
      <div className="statusbar-right">
        <button
          className={`statusbar-terminal-btn ${sidebarOpen ? 'active' : ''}`}
          onClick={onSidebarToggle}
          title="파일 트리 토글"
        >
          📁 파일
        </button>
        {(errorCount > 0 || warnCount > 0) && (
          <button
            className={`statusbar-problems-btn ${problemsOpen ? 'active' : ''}`}
            onClick={onProblemsToggle}
            title="문제 패널 토글"
          >
            {errorCount > 0 && <span className="sb-err">⛔ {errorCount}</span>}
            {warnCount > 0 && <span className="sb-warn">⚠️ {warnCount}</span>}
          </button>
        )}
        {userEmail && (
          <span className="statusbar-item user-email">{userEmail}</span>
        )}
        <button className="statusbar-icon-btn" onClick={onSettingsOpen} title="설정">
          ⚙
        </button>
        <button
          className={`statusbar-terminal-btn ${showMinimap ? 'active' : ''}`}
          onClick={() => setShowMinimap(!showMinimap)}
          title="미니맵 토글"
        >
          🗺 맵
        </button>
        <button
          className={`statusbar-terminal-btn ${terminalOpen ? 'active' : ''}`}
          onClick={onTerminalToggle}
        >
          ⌨ 터미널
        </button>
        <button className="statusbar-icon-btn" onClick={() => void onSignOut()} title="로그아웃">
          ⏻
        </button>
      </div>
    </div>
  )
}
