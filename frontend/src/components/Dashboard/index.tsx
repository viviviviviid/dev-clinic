import { useEffect, useRef, useState } from 'react'
import { useProject } from '../../hooks/useProject'
import type { TopicSuggestion } from '../../hooks/useProject'
import { useStore } from '../../store'
import './Dashboard.css'

interface Props {
  onMissionReady: (projectDir: string, skillLevel: string) => void
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
  const { getDailyMission, getDailyHistory, confirmDailyMission, loadProject } = useProject()
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

  const [topics, setTopics] = useState<TopicSuggestion[]>([])
  const [todayMissions, setTodayMissions] = useState<MissionRecord[]>([])
  const [allHistory, setAllHistory] = useState<MissionRecord[]>([])
  const [customTopic, setCustomTopic] = useState('')
  const [customSlug, setCustomSlug] = useState('')
  const [loading, setLoading] = useState(true)
  const [confirming, setConfirming] = useState(false)
  const [loadingMissionId, setLoadingMissionId] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [selectedDate, setSelectedDate] = useState<string>(todayStr)
  const [currentMonth, setCurrentMonth] = useState({ year: today.getFullYear(), month: today.getMonth() })
  const [todayPanelOpen, setTodayPanelOpen] = useState(false)
  const [calendarPushUp, setCalendarPushUp] = useState(0)
  const [vnVisible, setVnVisible] = useState(false)
  const [vnFading, setVnFading] = useState(false)
  const [vnStep, setVnStep] = useState(0)
  const [vnScenario, setVnScenario] = useState<'greeting' | 'same_day_failed' | 'yesterday_failed' | 'streak_failed'>('greeting')
  const [testScenarioIndex, setTestScenarioIndex] = useState(0)
  const [testModeActive, setTestModeActive] = useState(false)

  // VN 시나리오별 대사/이미지 정의
  const vnSequence = {
    greeting: [
      { img: '/greeting.png', text: '어서 오세요! 오늘도 열심히 해봐요. 응원하고 있을게요~' },
    ],
    same_day_failed: [
      { img: '/same_day_failed.png', text: '어? 아직 오늘의 미션을 완료하지 않으셨잖아요! 마저 해야 합니다!' },
    ],
    yesterday_failed: [
      { img: '/punishment.png', text: '어제 훈련을 빠지셨네요... 꾸준함이 재활의 핵심입니다!' },
      { img: '/punishment.png', text: '오늘은 꼭 완료하셔야 해요. 화이팅! 💪' },
    ],
    streak_failed: [
      { img: '/streak_failed.png', text: '진짜 죽어볼래요?' },
      { img: '/streak_failed.png', text: '(말이 없다)' },
    ],
  }

  const currentSlides = vnSequence[vnScenario]
  const currentSlide = currentSlides[vnStep]

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
        // 모든 시나리오 완료
        setVnFading(true)
        setTimeout(() => {
          setVnVisible(false)
          setVnFading(false)
        }, 420)
      }
    } else {
      setVnFading(true)
      setTimeout(() => {
        setVnVisible(false)
        setVnFading(false)
      }, 420)
    }
  }

  const mainRef = useRef<HTMLDivElement>(null)
  const calendarRef = useRef<HTMLDivElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const initializerRef = useRef(false)

  useEffect(() => {
    if (!todayPanelOpen) {
      setCalendarPushUp(0)
      return
    }
    requestAnimationFrame(() => {
      const panel = panelRef.current
      const main = mainRef.current
      const calendar = calendarRef.current
      if (!panel || !main || !calendar) return

      const panelH = panel.offsetHeight
      const needed = panelH + 10
      const mainH = main.clientHeight
      const calH = calendar.offsetHeight

      // 메인 영역에 달력 + 패널 + 여백이 들어갈 공간이 있을 때만 밀어올림
      if (mainH >= calH + needed + 32) {
        setCalendarPushUp(needed)
      }
    })
  }, [todayPanelOpen])

  useEffect(() => {
    if (initializerRef.current) return
    initializerRef.current = true

    // 테스트 모드: 바로 첫 시나리오 표시
    if (isTestMode) {
      setTestModeActive(true)
      setVnFading(false)
      setVnScenario(testScenarios[0])
      setVnStep(0)
      setVnVisible(true)
      setTestScenarioIndex(0)
      setLoading(false)
      return
    }

    Promise.all([
      getDailyMission().catch(() => ({ missions: [], topics: [] })),
      getDailyHistory(),
    ])
      .then(([daily, hist]) => {
        const missions: MissionRecord[] = daily.missions || []
        setTodayMissions(missions)
        if (daily.topics) setTopics(daily.topics)
        if (daily.error) setError(daily.error)
        const history: MissionRecord[] = Array.isArray(hist) ? hist : []
        setAllHistory(history)

        // 마지막 접속 날짜 확인
        const lastAccessDate = localStorage.getItem('lastAccessDate')
        const isSameDayAccess = lastAccessDate === todayStr

        // 어제, 그저께 날짜 계산
        const yesterday = new Date(today)
        yesterday.setDate(yesterday.getDate() - 1)
        const yesterdayStr = `${yesterday.getFullYear()}-${String(yesterday.getMonth() + 1).padStart(2, '0')}-${String(yesterday.getDate()).padStart(2, '0')}`

        const dayBeforeYesterday = new Date(today)
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
        localStorage.setItem('lastAccessDate', todayStr)
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
    if (dateStr === todayStr) {
      setTodayPanelOpen(prev => !prev)
    } else {
      setTodayPanelOpen(false)
    }
  }

  async function handleLoadMission(mission: MissionRecord) {
    setLoadingMissionId(mission.id)
    try {
      const data = await loadProject(mission.project_dir)
      if (data.error) throw new Error(data.error)
      onMissionReady(mission.project_dir, data.skillLevel || 'normal')
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoadingMissionId(null)
    }
  }

  async function handleCreateNewMission(topic: string, slug: string) {
    if (!topic || !slug) return
    setConfirming(true)
    setError('')
    try {
      const data = await confirmDailyMission(topic, slug)
      if (data.error) throw new Error(data.error)
      onMissionReady(data.project_dir, userSettings?.skill_level || 'normal')
    } catch (e: any) {
      setError(e.message)
    } finally {
      setConfirming(false)
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

  const difficultyColor: Record<string, string> = { '하': 'diff-low', '중': 'diff-mid', '상': 'diff-high' }
  const monthNames = ['1월', '2월', '3월', '4월', '5월', '6월', '7월', '8월', '9월', '10월', '11월', '12월']
  const weekDays = ['SUN', 'MON', 'TUE', 'WED', 'THU', 'FRI', 'SAT']

  function renderDays() {
    const { year, month } = currentMonth
    const daysInMonth = getDaysInMonth(year, month)
    const firstDay = getFirstDayOfWeek(year, month)
    const cells: JSX.Element[] = []

    for (let i = 0; i < firstDay; i++) {
      cells.push(<div key={`empty-${i}`} className="calendar-day empty" />)
    }

    for (let day = 1; day <= daysInMonth; day++) {
      const dateStr = `${year}-${String(month + 1).padStart(2, '0')}-${String(day).padStart(2, '0')}`
      const missions = missionsByDate.get(dateStr) || []
      const count = missions.length
      const activityLevel = count >= 3 ? 3 : count === 2 ? 2 : count === 1 ? 1 : 0
      const isToday = dateStr === todayStr
      const isSelected = dateStr === selectedDate

      cells.push(
        <div
          key={dateStr}
          className={[
            'calendar-day',
            activityLevel > 0 ? `activity-${activityLevel}` : '',
            isToday ? 'today' : '',
            isToday && todayPanelOpen ? 'panel-open' : '',
            isSelected && !isToday ? 'selected' : '',
          ].filter(Boolean).join(' ')}
          onClick={() => handleDateClick(dateStr)}
          title={`${dateStr} (${count}개 미션)`}
        >
          <span className="day-number">{day}</span>
          {count > 0 && <span className="day-dot" />}
        </div>
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
            <div className="nav-item active">🏥 재활 대시보드</div>
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
                  <div key={m.id} className="sidebar-mission-item" onClick={() => handleLoadMission(m)}>
                    <div className="sidebar-mission-topic">{m.topic}</div>
                    <div className="sidebar-mission-meta">
                      {loadingMissionId === m.id ? '준비중...' : m.project_dir.split('/').slice(-1)[0]}
                    </div>
                    <span className={`project-status ${m.status === 'completed' ? 'completed' : ''}`}>
                      {m.status === 'completed' ? '완료' : '진행 중'}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </div>

          <div className="sidebar-settings-btn">
            <div className="nav-item" onClick={onOpenSettings}>⚙ 설정</div>
            <div
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
              🧪 테스트
            </div>
          </div>
        </div>

        {/* ── VN 인트로 오버레이 ── */}
        {vnVisible && (
          <div
            className={`vn-intro ${vnFading ? 'vn-fade-out' : 'vn-fade-in'}`}
            onClick={advanceVn}
          >
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

        {/* ── Main: Full-height Calendar ── */}
        <div className="dashboard-main" ref={mainRef} style={{ paddingBottom: calendarPushUp > 0 ? `${calendarPushUp}px` : undefined }}>
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

          {/* Today panel — slides up from bottom */}
          <div ref={panelRef} className={`today-panel ${todayPanelOpen ? 'visible' : ''}`}>
            <div className="today-panel-handle" />
            <div className="today-panel-header">
              <div className="today-panel-title">
                🏥 오늘의 재활 훈련 — {todayStr.slice(5).replace('-', '월 ')}일
              </div>
              <button className="today-panel-close" onClick={() => setTodayPanelOpen(false)}>✕</button>
            </div>

            {topics.length > 0 && (
              <div className="topic-cards">
                {topics.map(t => (
                  <div
                    key={t.slug}
                    className={`topic-card ${difficultyColor[t.difficulty] || ''}`}
                    onClick={() => !confirming && handleCreateNewMission(t.name, t.slug)}
                  >
                    <div className={`difficulty-badge ${difficultyColor[t.difficulty] || ''}`}>
                      난이도 {t.difficulty}
                    </div>
                    <div className="topic-card-name">{t.name}</div>
                    <button className="topic-card-btn" disabled={confirming}>
                      {confirming ? '생성중...' : '시작하기 →'}
                    </button>
                  </div>
                ))}
              </div>
            )}

            <div className="custom-topic-row">
              <span style={{ color: '#52525b', fontSize: '0.8125rem', flexShrink: 0 }}>직접 입력</span>
              <input
                className="topic-input"
                placeholder="주제 이름"
                value={customTopic}
                onChange={(e) => {
                  setCustomTopic(e.target.value)
                  if (!customSlug && e.target.value.length > 2) {
                    setCustomSlug(e.target.value.replace(/\s+/g, '').slice(0, 15))
                  }
                }}
              />
              <input
                className="topic-input"
                placeholder="영문 디렉토리명"
                value={customSlug}
                onChange={(e) => setCustomSlug(e.target.value)}
                style={{ maxWidth: '150px' }}
              />
              <button
                className="btn btn-primary"
                onClick={() => handleCreateNewMission(customTopic, customSlug)}
                disabled={confirming || !customTopic || !customSlug}
              >
                {confirming ? '생성중...' : '시작'}
              </button>
            </div>

            {error && (
              <div className="error-banner">
                <p className="error-message">{friendlyError(error)}</p>
                {isSettingsError(error) && (
                  <button className="btn btn-primary" onClick={onOpenSettings}>설정으로 이동</button>
                )}
              </div>
            )}
          </div>
         </div>
        </div>

      </div>
    </div>
  )
}

