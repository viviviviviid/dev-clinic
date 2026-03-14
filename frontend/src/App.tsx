import { useCallback, useEffect, useRef, useState } from 'react'
import type { User } from '@supabase/supabase-js'
import { supabase } from './lib/supabase'
import AuthScreen from './components/Auth'
import SettingsScreen from './components/Settings'
import DashboardScreen from './components/Dashboard'
import FileTree from './components/FileTree'
import Editor from './components/Editor'
import FeedbackPanel from './components/FeedbackPanel'
import TerminalPanel from './components/Terminal'
import ProblemsPanel from './components/ProblemsPanel'
import { useWebSocket } from './hooks/useWebSocket'
import { useStore } from './store'
import type { UserSettings } from './store'
import { useProject } from './hooks/useProject'
import { lspClient } from './lib/lspClient'
import './App.css'

const SKILL_BADGE: Record<string, string> = {
  newbie: '🌱 뉴비',
  normal: '⚡ 보통',
  experienced: '🔥 숙련자',
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
}
function ResizerHandle({ direction, onMouseDown }: ResizerHandleProps) {
  return (
    <div className={`resizer resizer-${direction}`} onMouseDown={onMouseDown}>
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
  const [showSettings, setShowSettings] = useState(false)
  const [terminalOpen, setTerminalOpen] = useState(false)
  const [problemsOpen, setProblemsOpen] = useState(false)

  const { setUser, setUserSettings, projectStatus, setProjectStatus, setSkillLevel, setQuizData } = useStore()
  const { refreshFileTree, loadQuizData } = useProject()

  // Panel sizes
  const [sidebarWidth, setSidebarWidth] = useState(200)
  const [feedbackWidth, setFeedbackWidth] = useState(() => {
    const saved = localStorage.getItem('feedbackWidth')
    if (saved) return Math.max(160, Math.min(600, parseInt(saved)))
    return Math.max(360, Math.min(600, Math.round(window.innerWidth * 0.28)))
  })
  const [terminalHeight, setTerminalHeight] = useState(240)
  const [problemsHeight, setProblemsHeight] = useState(180)

  useEffect(() => {
    localStorage.setItem('feedbackWidth', String(feedbackWidth))
  }, [feedbackWidth])

  const sidebarRef = useRef(sidebarWidth)
  const feedbackRef = useRef(feedbackWidth)
  const terminalRef = useRef(terminalHeight)
  const problemsRef = useRef(problemsHeight)

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

  useWebSocket()

  // Auth state listener
  useEffect(() => {
    supabase.auth.getSession().then(({ data: { session } }) => {
      const u = session?.user ?? null
      setUserLocal(u)
      setUser(u)
      if (u) {
        loadSettings(session!.access_token)
      } else {
        setAuthLoading(false)
      }
    })

    const { data: { subscription } } = supabase.auth.onAuthStateChange((_event, session) => {
      const u = session?.user ?? null
      setUserLocal(u)
      setUser(u)
      if (u && session) {
        loadSettings(session.access_token)
      } else {
        setUserSettingsLocal(null)
        setUserSettings(null)
        setAuthLoading(false)
      }
    })

    return () => subscription.unsubscribe()
  }, [])

  async function loadSettings(token: string) {
    setSettingsLoading(true)
    try {
      const res = await fetch('/api/user/settings', {
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      setUserSettingsLocal(data)
      setUserSettings(data)
    } catch {
      setUserSettingsLocal(null)
    } finally {
      setSettingsLoading(false)
      setAuthLoading(false)
    }
  }

  function handleSettingsSaved(settings: UserSettings) {
    setUserSettingsLocal(settings)
    setUserSettings(settings)
    setShowSettings(false)
  }

  function handleBackToDashboard() {
    lspClient.disconnect()
    setProjectStatus(null)
  }

  async function handleMissionReady(projectDir: string, fallbackSkillLevel: string) {
    // userSettings의 skill_level 우선 사용 (daily confirm은 'normal' 하드코딩이라 무시)
    const effectiveSkillLevel = (userSettings?.skill_level || fallbackSkillLevel) as any
    setSkillLevel(effectiveSkillLevel)
    await refreshFileTree(projectDir)
    if (effectiveSkillLevel === 'newbie') {
      const quiz = await loadQuizData()
      setQuizData(quiz)
    }
    // Fetch project status to update store
    const { data: { session } } = await supabase.auth.getSession()
    if (session) {
      const res = await fetch('/api/project/status', {
        headers: { Authorization: `Bearer ${session.access_token}` },
      })
      const data = await res.json()
      setProjectStatus(data)

      // Connect LSP for the project language (best-effort, fallback to regex if unavailable)
      const lang = (userSettings?.language || 'go').toLowerCase()
      lspClient.connect(lang, projectDir, session.access_token)
        .catch((err) => console.warn('LSP unavailable, using fallback:', err))
    }
  }

  // Loading state
  if (authLoading || settingsLoading) {
    return <div className="loading-screen"><span>로딩 중...</span></div>
  }

  // Not logged in
  if (!user) {
    return <AuthScreen />
  }

  // Settings not configured yet (or user wants to edit)
  if (!userSettings || showSettings) {
    return (
      <SettingsScreen
        onComplete={handleSettingsSaved}
        initial={userSettings}
      />
    )
  }

  // No project loaded today — show dashboard screen
  // 테스트 모드(?test) 또는 프로젝트 미로드 상태
  const isTestMode = new URLSearchParams(window.location.search).has('test')
  if (!projectStatus?.loaded || isTestMode) {
    return <DashboardScreen onMissionReady={handleMissionReady} onOpenSettings={() => setShowSettings(true)} />
  }

  // Main editor
  return (
    <div className="app">
      <div className="app-main">
        <div className="app-sidebar" style={{ width: sidebarWidth }}>
          <FileTree />
        </div>

        <ResizerHandle direction="horizontal" onMouseDown={makeSidebarDown} />

        <div className="app-editor">
          <div className="app-monaco">
            <Editor />
          </div>

          {problemsOpen && (
            <>
              <ResizerHandle direction="vertical" onMouseDown={makeProblemsDown} />
              <div className="app-problems" style={{ height: problemsHeight }}>
                <ProblemsPanel />
              </div>
            </>
          )}

          {terminalOpen && (
            <>
              <ResizerHandle direction="vertical" onMouseDown={makeTerminalDown} />
              <div className="app-terminal" style={{ height: terminalHeight }}>
                <TerminalPanel onClose={() => setTerminalOpen(false)} />
              </div>
            </>
          )}
        </div>

        <ResizerHandle direction="horizontal" onMouseDown={makeFeedbackDown} />

        <div className="app-feedback" style={{ width: feedbackWidth }}>
          <FeedbackPanel />
        </div>
      </div>

      <div className="app-statusbar">
        <StatusBar
          onTerminalToggle={() => setTerminalOpen((v) => !v)}
          terminalOpen={terminalOpen}
          onProblemsToggle={() => setProblemsOpen((v) => !v)}
          problemsOpen={problemsOpen}
          onSettingsOpen={() => setShowSettings(true)}
          onBackToDashboard={handleBackToDashboard}
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
  onSettingsOpen: () => void
  onBackToDashboard: () => void
  userEmail?: string
}

function StatusBar({ onTerminalToggle, terminalOpen, onProblemsToggle, problemsOpen, onSettingsOpen, onBackToDashboard, userEmail }: StatusBarProps) {
  const { projectStatus, lastSync, isStreaming, skillLevel, diagnostics } = useStore()

  const errorCount = diagnostics.filter(d => d.severity === 1).length
  const warnCount = diagnostics.filter(d => d.severity === 2).length

  async function handleSignOut() {
    await supabase.auth.signOut()
  }

  return (
    <div className="statusbar">
      <button className="statusbar-lobby-btn" onClick={onBackToDashboard} title="로비로 돌아가기">
        🏥 로비
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
          className={`statusbar-terminal-btn ${terminalOpen ? 'active' : ''}`}
          onClick={onTerminalToggle}
        >
          ⌨ 터미널
        </button>
        <button className="statusbar-icon-btn" onClick={handleSignOut} title="로그아웃">
          ⏻
        </button>
      </div>
    </div>
  )
}
