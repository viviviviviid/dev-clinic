import React, { useEffect, useRef, useState } from 'react'
import { useProject } from '../../hooks/useProject'
import type { TopicSuggestion } from '../../hooks/useProject'
import { useStore } from '../../store'
import { getErrorMessage, isAbortError } from '../../lib/errors'
import { NurseSseParser } from './nurseSse'
import './Dashboard.css'

interface NurseChatMsg {
  role: 'user' | 'nurse'
  content: string
}

interface Props {
  onMissionReady: (projectDir: string, skillLevel: string) => Promise<void>
  onOpenSettings: () => void
}

interface MissionRecord {
  id: string
  date: string
  topic: string
  slug: string
  project_dir: string
  status: string
}

function friendlyError(msg: string): string {
  if (msg.toLowerCase().includes('user settings not found')) return '설정 정보가 없습니다.'
  return msg
}

function isSettingsError(msg: string): boolean {
  return msg.toLowerCase().includes('settings') || msg.toLowerCase().includes('설정')
}

function getDaysInMonth(year: number, month: number) {
  return new Date(year, month + 1, 0).getDate()
}

function getFirstDayOfWeek(year: number, month: number) {
  return new Date(year, month, 1).getDay()
}

export default function DashboardScreen({ onMissionReady, onOpenSettings }: Props) {
  const { getDailyMission, getDailyHistory, confirmDailyMissionStream, loadProject, deleteProject, nurseChat } = useProject()
  const { userSettings } = useStore()

  const today = new Date()
  const todayStr = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, '0')}-${String(today.getDate()).padStart(2, '0')}`

  // 테스트 모드 확인
  const isTestMode = new URLSearchParams(window.location.search).has('test')
  const testScenarios: Array<'greeting' | 'same_day_failed' | 'yesterday_failed' | 'streak_failed'> = [
    'greeting',
    'same_day_failed',
    'yesterday_failed',
    'streak_failed',
  ]

  const [, setTopics] = useState<TopicSuggestion[]>([])
  const [todayMissions, setTodayMissions] = useState<MissionRecord[]>([])
  const [allHistory, setAllHistory] = useState<MissionRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [, setConfirming] = useState(false)
  const [loadingMissionId, setLoadingMissionId] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [selectedDate, setSelectedDate] = useState<string>(todayStr)
  const [currentMonth, setCurrentMonth] = useState({ year: today.getFullYear(), month: today.getMonth() })
  const [vnVisible, setVnVisible] = useState(false)
  const [vnFading, setVnFading] = useState(false)
  const [vnStep, setVnStep] = useState(0)
  const [vnScenario, setVnScenario] = useState<'greeting' | 'same_day_failed' | 'yesterday_failed' | 'streak_failed'>('greeting')
  const [testScenarioIndex, setTestScenarioIndex] = useState(0)
  const [testModeActive, setTestModeActive] = useState(false)
  const [creatingProgress, setCreatingProgress] = useState<{stage: string; message: string} | null>(null)

  // Nurse chat state
  const [nurseChatVisible, setNurseChatVisible] = useState(false)
  const [nurseChatFading, setNurseChatFading] = useState(false)
  const [nurseChatHistory, setNurseChatHistory] = useState<NurseChatMsg[]>([])
  const [nurseChatInput, setNurseChatInput] = useState('')
  const [nurseChatLoading, setNurseChatLoading] = useState(false)
  const [nurseChatSuggestedTopics, setNurseChatSuggestedTopics] = useState<TopicSuggestion[]>([])
  const [nurseChatMode, setNurseChatMode] = useState<'chat' | 'choice' | 'mission-select'>('chat')
  const [nurseMissionSelIdx, setNurseMissionSelIdx] = useState(0)
  const [pastTopics, setPastTopics] = useState<string[]>([])
  const nurseChatBottomRef = useRef<HTMLDivElement>(null)
  const nurseChatInputRef = useRef<HTMLInputElement>(null)
  const nurseAbortRef = useRef<AbortController | null>(null)
  const missionLoadRef = useRef<AbortController | null>(null)
  const missionCreationRef = useRef<AbortController | null>(null)
  const startupRef = useRef({ isTestMode, testScenarios, getDailyMission, getDailyHistory, today, todayStr })
  const selectMissionRef = useRef<(mission: MissionRecord) => void>(() => undefined)
  const sendNurseMessageRef = useRef<(message: string, history: NurseChatMsg[]) => void>(() => undefined)

  // VN 시나리오별 대사/이미지 정의
  const vnSequence = {
    greeting: [
      { img: '/greeting.webp', text: '어서 오세요! 오늘도 열심히 해봐요. 응원하고 있을게요~' },
    ],
    same_day_failed: [
      { img: '/same_day_failed.webp', text: '어? 아직 오늘의 미션을 완료하지 않으셨잖아요! 마저 해야 합니다!' },
    ],
    yesterday_failed: [
      { img: '/punishment.webp', text: '어제 훈련을 빠지셨네요... 꾸준함이 재활의 핵심입니다!' },
      { img: '/punishment.webp', text: '오늘은 꼭 완료하셔야 해요. 화이팅! 💪' },
    ],
    streak_failed: [
      { img: '/streak_failed.webp', text: '어제 흐름이 끊겼네요. 오늘은 가볍게 다시 시작해봐요.' },
      { img: '/streak_failed.webp', text: '작은 단계 하나부터 다시 이어가면 됩니다.' },
    ],
  }

  const currentSlides = vnSequence[vnScenario]
  const currentSlide = currentSlides[vnStep]

  function finishVnIntro() {
    setVnFading(true)
    window.setTimeout(() => {
      setVnVisible(false)
      setVnFading(false)
      if (!testModeActive) {
        setNurseChatVisible(true)
        if (todayMissions.length > 0) {
          setNurseChatMode('choice')
          setNurseChatHistory([{
            role: 'nurse',
            content: `오늘 이미 훈련이 ${todayMissions.length}개 있네요! 기존 훈련을 계속할까요, 아니면 새로운 걸 만들어볼까요?`,
          }])
        } else {
          setNurseChatMode('chat')
          sendNurseMessage('__init__', [])
        }
      }
    }, 420)
  }

  function advanceVn() {
    if (vnFading) return
    if (vnStep < currentSlides.length - 1) {
      setVnStep(prev => prev + 1)
    } else if (testModeActive) {
      // 테스트 모드: 다음 시나리오로 넘어감
      if (testScenarioIndex < testScenarios.length - 1) {
        const nextScenario = testScenarios[testScenarioIndex + 1]
        setVnScenario(nextScenario)
        setVnStep(0)
        setTestScenarioIndex(prev => prev + 1)
      } else {
        finishVnIntro()
      }
    } else {
      finishVnIntro()
    }
  }

  async function sendNurseMessage(userMsg: string, currentHistory: NurseChatMsg[]) {
    nurseAbortRef.current?.abort()
    const abort = new AbortController()
    nurseAbortRef.current = abort
    setNurseChatLoading(true)
    const isInit = userMsg === '__init__'
    const histForApi = isInit ? [] : currentHistory
    const newHistory: NurseChatMsg[] = isInit
      ? []
      : [...currentHistory, { role: 'user' as const, content: userMsg }]

    try {
      const stream = await nurseChat(
        isInit ? '안녕하세요! 오늘 어떤 훈련을 할까요?' : userMsg,
        histForApi,
        pastTopics,
        abort.signal,
      )
      let nurseReply = ''
      setNurseChatHistory([...newHistory, { role: 'nurse', content: '' }])

      const reader = stream.getReader()
      const decoder = new TextDecoder()
      const parser = new NurseSseParser()
      let streamComplete = false

      const consumeEvents = (events: ReturnType<NurseSseParser['push']>) => {
        for (const event of events) {
          if (event.type === 'message') {
            nurseReply += event.text
            setNurseChatHistory([...newHistory, { role: 'nurse', content: nurseReply }])
          } else if (event.type === 'topics') {
            setNurseChatSuggestedTopics(event.topics)
          } else if (event.type === 'done') {
            streamComplete = true
          }
        }
      }

      try {
        while (!streamComplete) {
          const { done, value } = await reader.read()
          if (done) {
            consumeEvents(parser.push(decoder.decode()))
            consumeEvents(parser.finish())
            break
          }
          consumeEvents(parser.push(decoder.decode(value, { stream: true })))
        }
        if (streamComplete) await reader.cancel().catch(() => undefined)
      } finally {
        reader.releaseLock()
      }

      if (!streamComplete) throw new Error('간호사 응답이 완료되기 전에 연결이 종료되었습니다.')
      setNurseChatHistory([...newHistory, { role: 'nurse', content: nurseReply }])
    } catch (error: unknown) {
      if (!abort.signal.aborted) {
        const message = getErrorMessage(error)
        setNurseChatHistory([...newHistory, { role: 'nurse', content: `응답을 불러오지 못했습니다: ${message}` }])
        setError(message)
      }
    } finally {
      if (nurseAbortRef.current === abort) {
        nurseAbortRef.current = null
        setNurseChatLoading(false)
        setTimeout(() => {
          nurseChatBottomRef.current?.scrollIntoView({ behavior: 'smooth' })
          nurseChatInputRef.current?.focus()
        }, 50)
      }
    }
  }

  function handleNurseChatSubmit() {
    const msg = nurseChatInput.trim()
    if (!msg || nurseChatLoading) return
    setNurseChatInput('')
    setNurseChatSuggestedTopics([])
    sendNurseMessage(msg, nurseChatHistory)
  }

  function closeNurseChat() {
    nurseAbortRef.current?.abort()
    nurseAbortRef.current = null
    setNurseChatLoading(false)
    setNurseChatFading(true)
    setTimeout(() => {
      setNurseChatVisible(false)
      setNurseChatFading(false)
      setNurseChatHistory([])
      setNurseChatSuggestedTopics([])
      setNurseChatMode('chat')
      setNurseMissionSelIdx(0)
    }, 300)
  }

  function confirmNurseTopic(t: TopicSuggestion) {
    closeNurseChat()
    setTimeout(() => handleCreateNewMission(t.name, t.slug), 350)
  }

  const mainRef = useRef<HTMLDivElement>(null)
  const calendarRef = useRef<HTMLDivElement>(null)
  const initializerRef = useRef(false)
  selectMissionRef.current = (mission) => {
    closeNurseChat()
    setTimeout(() => handleLoadMission(mission), 350)
  }
  sendNurseMessageRef.current = (message, history) => {
    void sendNurseMessage(message, history)
  }

  // 키보드: mission-select 모드
  useEffect(() => {
    if (!nurseChatVisible || nurseChatMode !== 'mission-select') return
    function handler(e: KeyboardEvent) {
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        setNurseMissionSelIdx(prev => Math.max(0, prev - 1))
      } else if (e.key === 'ArrowDown') {
        e.preventDefault()
        setNurseMissionSelIdx(prev => Math.min(todayMissions.length - 1, prev + 1))
      } else if (e.key === 'Enter') {
        e.preventDefault()
        const m = todayMissions[nurseMissionSelIdx]
        if (m) selectMissionRef.current(m)
      } else if (e.key === 'Escape') {
        setNurseChatMode('choice')
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [nurseChatVisible, nurseChatMode, nurseMissionSelIdx, todayMissions])

  // 키보드: choice 모드 (1=기존, 2=새로운)
  useEffect(() => {
    if (!nurseChatVisible || nurseChatMode !== 'choice') return
    function handler(e: KeyboardEvent) {
      if (e.key === '1') {
        setNurseChatMode('mission-select')
        setNurseMissionSelIdx(0)
      } else if (e.key === '2') {
        setNurseChatMode('chat')
        setNurseChatHistory([])
        sendNurseMessageRef.current('__init__', [])
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [nurseChatVisible, nurseChatMode])

  useEffect(() => () => {
    nurseAbortRef.current?.abort()
    missionLoadRef.current?.abort()
    missionCreationRef.current?.abort()
  }, [])

  useEffect(() => {
    if (initializerRef.current) return
    initializerRef.current = true
    const {
      isTestMode: initialTestMode,
      testScenarios: initialTestScenarios,
      getDailyMission: fetchDailyMission,
      getDailyHistory: fetchDailyHistory,
      today: initialToday,
      todayStr: initialTodayStr,
    } = startupRef.current

    // 테스트 모드: 바로 첫 시나리오 표시
    if (initialTestMode) {
      setTestModeActive(true)
      setVnFading(false)
      setVnScenario(initialTestScenarios[0])
      setVnStep(0)
      setVnVisible(true)
      setTestScenarioIndex(0)
      setLoading(false)
      return
    }

    Promise.all([
      fetchDailyMission(),
      fetchDailyHistory(),
    ])
      .then(([daily, hist]) => {
        const missions: MissionRecord[] = daily.missions || []
        setTodayMissions(missions)
        if (daily.topics) setTopics(daily.topics)
        if (daily.error) setError(daily.error)
        const history: MissionRecord[] = Array.isArray(hist) ? hist : []
        setAllHistory(history)
        const seen = new Set<string>()
        const past: string[] = []
        for (const m of history) {
          if (!seen.has(m.topic)) { seen.add(m.topic); past.push(m.topic) }
        }
        setPastTopics(past)

        // 마지막 접속 날짜 확인
        const lastAccessDate = localStorage.getItem('lastAccessDate')
        const isSameDayAccess = lastAccessDate === initialTodayStr

        // 어제, 그저께 날짜 계산
        const yesterday = new Date(initialToday)
        yesterday.setDate(yesterday.getDate() - 1)
        const yesterdayStr = `${yesterday.getFullYear()}-${String(yesterday.getMonth() + 1).padStart(2, '0')}-${String(yesterday.getDate()).padStart(2, '0')}`

        const dayBeforeYesterday = new Date(initialToday)
        dayBeforeYesterday.setDate(dayBeforeYesterday.getDate() - 2)
        const dayBeforeYesterdayStr = `${dayBeforeYesterday.getFullYear()}-${String(dayBeforeYesterday.getMonth() + 1).padStart(2, '0')}-${String(dayBeforeYesterday.getDate()).padStart(2, '0')}`

        // 어제/그저께 완료 여부 확인 (status='completed'인 경우만 완료로 판단)
        const yesterdayMissions = history.filter(m => m.date === yesterdayStr)
        const dayBeforeYesterdayMissions = history.filter(m => m.date === dayBeforeYesterdayStr)
        const yesterdayCompleted = yesterdayMissions.some(m => m.status === 'completed')
        const dayBeforeYesterdayCompleted = dayBeforeYesterdayMissions.some(m => m.status === 'completed')

        // 오늘 미션 상태 확인
        const todayHasActive = missions.some(m => m.status === 'active')

        // 시나리오 결정
        let scenario: 'greeting' | 'same_day_failed' | 'yesterday_failed' | 'streak_failed'

        if (isSameDayAccess && todayHasActive) {
          // 같은 날 재접속했는데 미션 미완료
          scenario = 'same_day_failed'
        } else if (!isSameDayAccess && !yesterdayCompleted && !dayBeforeYesterdayCompleted) {
          // 새 날 접속하고 어제, 그저께 모두 미완료 (2일 이상 연속)
          scenario = 'streak_failed'
        } else if (!isSameDayAccess && !yesterdayCompleted) {
          // 새 날 접속하고 어제 미완료
          scenario = 'yesterday_failed'
        } else {
          // 그 외: 정상 인사
          scenario = 'greeting'
        }

        setVnFading(false)
        setVnScenario(scenario)
        setVnStep(0)
        setVnVisible(true)

        // 현재 접속 날짜 저장
        localStorage.setItem('lastAccessDate', initialTodayStr)
      })
      .catch((error: unknown) => {
        setVnVisible(false)
        setError(getErrorMessage(error))
      })
      .finally(() => setLoading(false))
  }, [])

  // 날짜별 미션 맵
  const missionsByDate = new Map<string, MissionRecord[]>()
  allHistory.forEach((m) => {
    const list = missionsByDate.get(m.date) || []
    if (!list.find((x) => x.id === m.id)) list.push(m)
    missionsByDate.set(m.date, list)
  })
  todayMissions.forEach((m) => {
    const list = missionsByDate.get(m.date) || []
    if (!list.find((x) => x.id === m.id)) list.push(m)
    missionsByDate.set(m.date, list)
  })

  const selectedMissions = missionsByDate.get(selectedDate) || []

  function handleDateClick(dateStr: string) {
    setSelectedDate(dateStr)
  }

  async function handleDeleteMission(mission: MissionRecord) {
    try {
      await deleteProject(mission.project_dir)
      setAllHistory(prev => prev.filter(m => m.id !== mission.id))
      setTodayMissions(prev => prev.filter(m => m.id !== mission.id))
    } catch (error: unknown) {
      setError(getErrorMessage(error))
    }
  }

  async function handleLoadMission(mission: MissionRecord) {
    if (missionLoadRef.current) return
    const abort = new AbortController()
    missionLoadRef.current = abort
    setLoadingMissionId(mission.id)
    try {
      const data = await loadProject(mission.project_dir, abort.signal)
      if (data.error) throw new Error(data.error)
      await onMissionReady(mission.project_dir, data.skillLevel || 'normal')
    } catch (error: unknown) {
      if (!isAbortError(error)) setError(getErrorMessage(error))
    } finally {
      if (missionLoadRef.current === abort) {
        missionLoadRef.current = null
        setLoadingMissionId(null)
      }
    }
  }

  async function handleCreateNewMission(topic: string, slug: string) {
    if (!topic || !slug || missionCreationRef.current) return
    const abort = new AbortController()
    missionCreationRef.current = abort
    setConfirming(true)
    setError('')
    setCreatingProgress({ stage: 'setup', message: '준비 중...' })
    try {
      const data = await confirmDailyMissionStream(topic, slug, (stage, message) => {
        setCreatingProgress({ stage, message })
      }, abort.signal)
      if (!data?.project_dir) throw new Error('프로젝트 생성에 실패했습니다')
      await onMissionReady(data.project_dir, userSettings?.skill_level || 'normal')
    } catch (error: unknown) {
      if (!isAbortError(error)) setError(getErrorMessage(error))
    } finally {
      if (missionCreationRef.current === abort) {
        missionCreationRef.current = null
        setConfirming(false)
        setCreatingProgress(null)
      }
    }
  }

  function prevMonth() {
    setCurrentMonth(prev =>
      prev.month === 0 ? { year: prev.year - 1, month: 11 } : { year: prev.year, month: prev.month - 1 }
    )
  }

  function nextMonth() {
    setCurrentMonth(prev =>
      prev.month === 11 ? { year: prev.year + 1, month: 0 } : { year: prev.year, month: prev.month + 1 }
    )
  }

  const monthNames = ['1월', '2월', '3월', '4월', '5월', '6월', '7월', '8월', '9월', '10월', '11월', '12월']
  const weekDays = ['SUN', 'MON', 'TUE', 'WED', 'THU', 'FRI', 'SAT']

  function renderDays() {
    const { year, month } = currentMonth
    const daysInMonth = getDaysInMonth(year, month)
    const firstDay = getFirstDayOfWeek(year, month)
    const cells: React.ReactElement[] = []

    for (let i = 0; i < firstDay; i++) {
      cells.push(<div key={`empty-${i}`} className="calendar-day empty" />)
    }

    for (let day = 1; day <= daysInMonth; day++) {
      const dateStr = `${year}-${String(month + 1).padStart(2, '0')}-${String(day).padStart(2, '0')}`
      const missions = missionsByDate.get(dateStr) || []
      const count = missions.length
      const hasCompleted = missions.some(m => m.status === 'completed')
      const hasActive = missions.some(m => m.status !== 'completed')
      const activityLevel = count >= 3 ? 3 : count === 2 ? 2 : count === 1 ? 1 : 0
      const isToday = dateStr === todayStr
      const isSelected = dateStr === selectedDate

      cells.push(
        <button
          type="button"
          key={dateStr}
          className={[
            'calendar-day',
            hasCompleted ? 'has-completed' : (hasActive ? `activity-${activityLevel}` : ''),
            isToday ? 'today' : '',
            isSelected && !isToday ? 'selected' : '',
          ].filter(Boolean).join(' ')}
          onClick={() => handleDateClick(dateStr)}
          title={`${dateStr} (${count}개 미션)`}
          aria-pressed={isSelected}
        >
          <span className="day-number">{day}</span>
          {count > 0 && <span className="day-dot" />}
        </button>
      )
    }

    return cells
  }

  if (loading) {
    return (
      <div className="dashboard-overlay">
        <div className="dashboard-container" style={{ alignItems: 'center', justifyContent: 'center' }}>
          <div className="rehab-loading">
            <div className="loading-pill">💊</div>
            <p>재활 프로그램 준비중...</p>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="dashboard-overlay">
      <div className="dashboard-container">

        {/* ── Sidebar ── */}
        <div className="dashboard-sidebar">
          <div className="dashboard-logo-area">
            <span className="dashboard-logo">💊</span>
            <div>
              <div className="dashboard-title">재활센터</div>
              <div className="dashboard-subtitle">코딩 치료 클리닉</div>
            </div>
          </div>

          <nav className="dashboard-nav">
            <div className="nav-item active">
              <span aria-hidden="true">🏥</span><span className="nav-label">재활 대시보드</span>
            </div>
          </nav>

          {/* 선택된 날짜 훈련 기록 */}
          <div className="sidebar-date-panel">
            <div className="sidebar-date-title">
              <span>
                {selectedDate === todayStr ? '오늘 · ' : ''}
                {selectedDate.slice(5).replace('-', '월 ')}일
              </span>
              <span className="sidebar-date-count">{selectedMissions.length}개</span>
            </div>

            {selectedMissions.length === 0 ? (
              <p className="sidebar-no-missions">훈련 기록 없음</p>
            ) : (
              <div className="sidebar-mission-list">
                {selectedMissions.map((m) => (
                  <div
                    key={m.id}
                    className="sidebar-mission-item"
                  >
                    <button
                      type="button"
                      className="sidebar-mission-open"
                      onClick={() => void handleLoadMission(m)}
                      disabled={loadingMissionId !== null}
                    >
                      <span className="sidebar-mission-topic">{m.topic}</span>
                      <span className="sidebar-mission-meta">
                        {loadingMissionId === m.id ? '준비중…' : m.project_dir.split('/').slice(-1)[0]}
                      </span>
                      <span className={`project-status ${m.status === 'completed' ? 'completed' : ''}`}>
                        {m.status === 'completed' ? '완료' : '진행 중'}
                      </span>
                    </button>
                    <button
                      type="button"
                      className="mission-delete-btn"
                      onClick={(e) => {
                        e.stopPropagation()
                        if (window.confirm(`"${m.topic}" 미션을 삭제할까요?`)) {
                          handleDeleteMission(m)
                        }
                      }}
                      aria-label={`${m.topic} 미션 삭제`}
                    >
                      🗑
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>

          <div className="sidebar-settings-btn">
            <button type="button" className="nav-item" onClick={onOpenSettings}>
              <span aria-hidden="true">⚙</span><span className="nav-label">설정</span>
            </button>
            <button
              type="button"
              className={`nav-item ${testModeActive ? 'active' : ''}`}
              onClick={() => {
                const newTestMode = !testModeActive
                setTestModeActive(newTestMode)
                if (newTestMode) {
                  // 테스트 모드 활성화
                  setVnFading(false)
                  setVnScenario(testScenarios[0])
                  setVnStep(0)
                  setVnVisible(true)
                  setTestScenarioIndex(0)
                }
              }}
              title="시나리오 테스트 모드"
            >
              <span aria-hidden="true">🧪</span><span className="nav-label">테스트</span>
            </button>
          </div>
        </div>

        {/* ── 프로젝트 생성 진행 오버레이 ── */}
        {creatingProgress && (
          <div className="creation-overlay">
            <div className="creation-modal">
              <div className="creation-spinner" />
              <div className="creation-stage">{creatingProgress.stage === 'curriculum' ? '📚' : creatingProgress.stage === 'code' ? '💻' : creatingProgress.stage === 'quiz' ? '📝' : '⚙️'}</div>
              <div className="creation-message">{creatingProgress.message}</div>
            </div>
          </div>
        )}

        {/* ── VN 인트로 오버레이 ── */}
        {vnVisible && (
          <div
            className={`vn-intro ${vnFading ? 'vn-fade-out' : 'vn-fade-in'}`}
            onClick={advanceVn}
            onKeyDown={(event) => {
              if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault()
                advanceVn()
              }
              if (event.key === 'Escape') finishVnIntro()
            }}
            role="dialog"
            aria-modal="true"
            aria-label="오늘의 학습 안내"
            tabIndex={0}
          >
            <button
              type="button"
              className="vn-skip-btn"
              onClick={(event) => {
                event.stopPropagation()
                finishVnIntro()
              }}
            >
              건너뛰기
            </button>
            {/* 우측 캐릭터 — 이미지 전환 시 key로 재마운트해 애니메이션 재실행 */}
            <div className="vn-character" key={`${vnScenario}-${vnStep}`}>
              <img
                src={currentSlide.img}
                alt="간호사"
                onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
              />
              <div className="vn-character-placeholder">👩‍⚕️</div>
            </div>

            {/* 하단 대사창 */}
            <div className="vn-dialogue">
              <div className="vn-name-tag">담당 간호사</div>
              <p className="vn-dialogue-text" key={vnStep}>{currentSlide.text}</p>
              <div className="vn-continue-hint">
                <span className="vn-arrow">▼</span>
                {vnStep < currentSlides.length - 1 ? '클릭하여 계속' : '클릭하여 시작'}
              </div>
            </div>
          </div>
        )}

        {/* ── Nurse Chat 오버레이 ── */}
        {nurseChatVisible && (
          <div className={`vn-intro nurse-chat-overlay ${nurseChatFading ? 'vn-fade-out' : 'vn-fade-in'}`}>
            <div className="vn-character nurse-chat-char">
              <img
                src="/greeting.webp"
                alt="간호사"
                onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
              />
              <div className="vn-character-placeholder">👩‍⚕️</div>
            </div>

            <div className="nurse-chat-panel">
              <div className="nurse-chat-header">
                <span className="nurse-name-tag">담당 간호사</span>
                <button className="nurse-chat-skip" onClick={closeNurseChat}>닫기 ✕</button>
              </div>

              {/* 메시지 영역 */}
              <div className="nurse-chat-messages">
                {nurseChatHistory.map((msg, i) => (
                  <div key={i} className={`nurse-chat-msg nurse-chat-msg--${msg.role}`}>
                    {msg.role === 'nurse' && <span className="nurse-chat-avatar">👩‍⚕️</span>}
                    <div className="nurse-chat-bubble">
                      {msg.content.replace(/\[TOPICS\][\s\S]*?\[\/TOPICS\]/g, '').trim()}
                    </div>
                  </div>
                ))}
                {nurseChatLoading && nurseChatHistory.length === 0 && (
                  <div className="nurse-chat-msg nurse-chat-msg--nurse">
                    <span className="nurse-chat-avatar">👩‍⚕️</span>
                    <div className="nurse-chat-bubble nurse-chat-typing">
                      <span /><span /><span />
                    </div>
                  </div>
                )}
                <div ref={nurseChatBottomRef} />
              </div>

              {/* choice 모드: 기존 vs 새로운 */}
              {nurseChatMode === 'choice' && (
                <div className="nurse-chat-topics">
                  <div className="nurse-chat-topics-label">어떻게 할까요? (1 / 2)</div>
                  <button
                    className="nurse-chat-topic-btn"
                    onClick={() => { setNurseChatMode('mission-select'); setNurseMissionSelIdx(0) }}
                  >
                    <span className="nurse-chat-diff diff-low">1</span>
                    <span className="nurse-chat-topic-name">기존 훈련 계속하기</span>
                    <span className="nurse-chat-topic-arrow">↑↓ Enter</span>
                  </button>
                  <button
                    className="nurse-chat-topic-btn"
                    onClick={() => { setNurseChatMode('chat'); setNurseChatHistory([]); sendNurseMessage('__init__', []) }}
                  >
                    <span className="nurse-chat-diff diff-high">2</span>
                    <span className="nurse-chat-topic-name">새로운 훈련 만들기</span>
                    <span className="nurse-chat-topic-arrow">→</span>
                  </button>
                </div>
              )}

              {/* mission-select 모드: 오늘 미션 키보드 선택 */}
              {nurseChatMode === 'mission-select' && (
                <div className="nurse-chat-topics">
                  <div className="nurse-chat-topics-label">오늘의 훈련 — ↑↓ 선택, Enter 시작, Esc 뒤로</div>
                  {todayMissions.map((m, i) => (
                    <button
                      key={m.id}
                      className={`nurse-chat-topic-btn${nurseMissionSelIdx === i ? ' selected' : ''}`}
                      onClick={() => { closeNurseChat(); setTimeout(() => handleLoadMission(m), 350) }}
                      onMouseEnter={() => setNurseMissionSelIdx(i)}
                    >
                      <span className={`nurse-chat-diff ${m.status === 'completed' ? 'diff-low' : 'diff-mid'}`}>
                        {m.status === 'completed' ? '완료' : '진행'}
                      </span>
                      <span className="nurse-chat-topic-name">{m.topic}</span>
                      {loadingMissionId === m.id
                        ? <span className="nurse-chat-topic-arrow">준비중...</span>
                        : <span className="nurse-chat-topic-arrow">→</span>
                      }
                    </button>
                  ))}
                </div>
              )}

              {/* chat 모드: 주제 추천 + 입력창 */}
              {nurseChatMode === 'chat' && (
                <>
                  {nurseChatSuggestedTopics.length > 0 && (
                    <div className="nurse-chat-topics">
                      <div className="nurse-chat-topics-label">추천 훈련 주제</div>
                      {nurseChatSuggestedTopics.map((t) => (
                        <button
                          key={t.slug}
                          className="nurse-chat-topic-btn"
                          onClick={() => confirmNurseTopic(t)}
                        >
                          <span className={`nurse-chat-diff diff-${t.difficulty === '상' ? 'high' : t.difficulty === '중' ? 'mid' : 'low'}`}>
                            {t.difficulty}
                          </span>
                          <span className="nurse-chat-topic-name">{t.name}</span>
                          <span className="nurse-chat-topic-arrow">→</span>
                        </button>
                      ))}
                    </div>
                  )}
                  <div className="nurse-chat-input-row">
                    <input
                      ref={nurseChatInputRef}
                      className="nurse-chat-input"
                      placeholder="간호사에게 말하기... (예: 알고리즘 연습하고 싶어)"
                      value={nurseChatInput}
                      disabled={nurseChatLoading}
                      onChange={(e) => setNurseChatInput(e.target.value)}
                      onKeyDown={(e) => { if (e.key === 'Enter') handleNurseChatSubmit() }}
                    />
                    <button
                      className="nurse-chat-send"
                      onClick={handleNurseChatSubmit}
                      disabled={nurseChatLoading || !nurseChatInput.trim()}
                    >
                      {nurseChatLoading ? '...' : '전송'}
                    </button>
                  </div>
                </>
              )}
            </div>
          </div>
        )}

        {/* ── Main: Full-height Calendar ── */}
        <div className="dashboard-main" ref={mainRef}>
         <div
           className="calendar-content"
           ref={calendarRef}
           style={vnVisible ? { filter: 'blur(5px)', pointerEvents: 'none' } : undefined}
         >
          {/* Month navigation */}
          <div className="calendar-header-row">
            <div className="calendar-month-label">
              {monthNames[currentMonth.month]}
              <span>{currentMonth.year}</span>
            </div>
            <div className="cal-nav-group">
              <button className="cal-nav-btn" onClick={prevMonth}>‹</button>
              <button className="cal-nav-btn" onClick={nextMonth}>›</button>
            </div>
          </div>

          {/* Weekday headers */}
          <div className="calendar-weekdays">
            {weekDays.map(d => (
              <div key={d} className="calendar-weekday">{d}</div>
            ))}
          </div>

          {/* Day cells — fills remaining height */}
          <div className="calendar-wrapper">
            <div className="calendar-days-grid">
              {renderDays()}
            </div>
          </div>

          {error && (
            <div className="error-banner" style={{ marginTop: '1rem' }}>
              <p className="error-message">{friendlyError(error)}</p>
              {isSettingsError(error) && (
                <button className="btn btn-primary" onClick={onOpenSettings}>설정으로 이동</button>
              )}
              {!isSettingsError(error) && (
                <button className="btn btn-primary" onClick={() => window.location.reload()}>다시 시도</button>
              )}
            </div>
          )}
         </div>
        </div>

      </div>
    </div>
  )
}

