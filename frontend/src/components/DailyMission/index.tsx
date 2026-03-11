import { useEffect, useState } from 'react'
import { useProject } from '../../hooks/useProject'
import type { TopicSuggestion } from '../../hooks/useProject'
import './DailyMission.css'

interface Props {
  onMissionReady: (projectDir: string, skillLevel: string) => void
  onOpenSettings: () => void
}

interface MissionRecord {
  id: string
  date: string       // "2026-03-08"
  topic: string
  slug: string
  project_dir: string
  status: string     // "active" | "completed"
}

const WEEKDAYS = ['월', '화', '수', '목', '금', '토', '일']

export default function DailyMissionScreen({ onMissionReady, onOpenSettings }: Props) {
  const { getDailyMission, getDailyHistory, confirmDailyMission, loadProject } = useProject()

  const [topics, setTopics] = useState<TopicSuggestion[]>([])
  const [todayMissions, setTodayMissions] = useState<MissionRecord[]>([])
  const [history, setHistory] = useState<MissionRecord[]>([])
  const [selected, setSelected] = useState<TopicSuggestion | null>(null)
  const [customTopic, setCustomTopic] = useState('')
  const [customSlug, setCustomSlug] = useState('')
  const [showCustom, setShowCustom] = useState(false)
  const [showAddNew, setShowAddNew] = useState(false)
  const [loading, setLoading] = useState(true)
  const [confirming, setConfirming] = useState(false)
  const [loadingMissionId, setLoadingMissionId] = useState<string | null>(null)
  const [error, setError] = useState('')

  // Calendar
  const today = new Date()
  const [calMonth, setCalMonth] = useState(new Date(today.getFullYear(), today.getMonth(), 1))

  useEffect(() => {
    Promise.all([
      getDailyMission().catch(() => ({})),
      getDailyHistory(),
    ])
      .then(([daily, hist]) => {
        const missions: MissionRecord[] = daily.missions || []
        setTodayMissions(missions)
        if (daily.topics) setTopics(daily.topics)
        if (daily.error) setError(daily.error)
        // If no missions yet, show add form immediately
        if (missions.length === 0) setShowAddNew(true)
        if (Array.isArray(hist)) setHistory(hist)
      })
      .finally(() => setLoading(false))
  }, [])

  // Build map: date → missions[] for calendar
  const missionsByDate = new Map<string, MissionRecord[]>()
  history.forEach((m) => {
    const list = missionsByDate.get(m.date) || []
    if (!list.find((x) => x.id === m.id)) list.push(m)
    missionsByDate.set(m.date, list)
  })
  todayMissions.forEach((m) => {
    const list = missionsByDate.get(m.date) || []
    if (!list.find((x) => x.id === m.id)) list.push(m)
    missionsByDate.set(m.date, list)
  })

  function getDateStatus(missions: MissionRecord[]): string {
    if (missions.some((m) => m.status === 'active')) return 'active'
    return 'completed'
  }

  // Calendar grid: weeks for calMonth
  function buildCalendarDays() {
    const year = calMonth.getFullYear()
    const month = calMonth.getMonth()
    const firstDay = new Date(year, month, 1)
    const lastDay = new Date(year, month + 1, 0)

    let startDow = firstDay.getDay() - 1
    if (startDow < 0) startDow = 6

    const days: (Date | null)[] = []
    for (let i = 0; i < startDow; i++) days.push(null)
    for (let d = 1; d <= lastDay.getDate(); d++) days.push(new Date(year, month, d))
    while (days.length % 7 !== 0) days.push(null)
    return days
  }

  function fmtDate(d: Date) {
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  }

  function isToday(d: Date) {
    return fmtDate(d) === fmtDate(today)
  }

  const calDays = buildCalendarDays()
  const calMonthLabel = calMonth.toLocaleDateString('ko-KR', { year: 'numeric', month: 'long' })

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

  async function handleConfirm() {
    const topic = showCustom ? customTopic : selected?.name
    const slug = showCustom ? customSlug : selected?.slug
    if (!topic || !slug) return
    setConfirming(true)
    setError('')
    try {
      const data = await confirmDailyMission(topic, slug)
      if (data.error) throw new Error(data.error)
      onMissionReady(data.project_dir, 'normal')
    } catch (e: any) {
      setError(e.message)
    } finally {
      setConfirming(false)
    }
  }

  if (loading) {
    return (
      <div className="daily-overlay">
        <div className="daily-card">
          <div className="daily-loading">AI가 오늘의 미션을 준비하는 중...</div>
        </div>
      </div>
    )
  }

  return (
    <div className="daily-overlay">
      <div className="daily-card">

        {/* Header */}
        <div className="daily-header">
          <span className="daily-logo">{'</>'}</span>
          <div>
            <h1>코딩 튜터</h1>
            <p className="daily-date-label">
              {today.toLocaleDateString('ko-KR', { year: 'numeric', month: 'long', day: 'numeric', weekday: 'long' })}
            </p>
          </div>
          <button className="daily-settings-btn" onClick={onOpenSettings} title="설정">
            ⚙
          </button>
        </div>

        {/* Calendar */}
        <div className="calendar-section">
          <div className="calendar-nav">
            <button onClick={() => setCalMonth(new Date(calMonth.getFullYear(), calMonth.getMonth() - 1, 1))}>‹</button>
            <span className="calendar-month-label">{calMonthLabel}</span>
            <button onClick={() => setCalMonth(new Date(calMonth.getFullYear(), calMonth.getMonth() + 1, 1))}>›</button>
          </div>
          <div className="calendar-grid">
            {WEEKDAYS.map((d) => (
              <div key={d} className="calendar-weekday">{d}</div>
            ))}
            {calDays.map((day, i) => {
              if (!day) return <div key={`e-${i}`} className="calendar-cell empty" />
              const dateStr = fmtDate(day)
              const dayMissions = missionsByDate.get(dateStr) || []
              const status = dayMissions.length > 0 ? getDateStatus(dayMissions) : null
              return (
                <div
                  key={dateStr}
                  className={[
                    'calendar-cell',
                    isToday(day) ? 'today' : '',
                    status ? `mission-${status}` : '',
                  ].join(' ')}
                  title={dayMissions.map((m) => m.topic).join(', ')}
                >
                  <span className="calendar-day-num">{day.getDate()}</span>
                  {dayMissions.length > 0 && <span className="calendar-dot" />}
                  {dayMissions.length > 1 && (
                    <span className="calendar-count">{dayMissions.length}</span>
                  )}
                </div>
              )
            })}
          </div>
          <div className="calendar-legend">
            <span className="legend-item"><span className="legend-dot active" />진행중</span>
            <span className="legend-item"><span className="legend-dot completed" />완료</span>
          </div>
        </div>

        {/* Today's missions */}
        <div className="daily-mission-section">
          <div className="daily-section-header">
            <h2 className="daily-section-title">
              오늘의 미션
              {todayMissions.length > 0 && (
                <span className="mission-count-badge">{todayMissions.length}</span>
              )}
            </h2>
          </div>

          {/* Mission list */}
          {todayMissions.length > 0 && (
            <div className="mission-list">
              {todayMissions.map((m) => (
                <div key={m.id} className="daily-existing">
                  <div className="existing-badge">{m.status === 'completed' ? '완료' : '진행 중'}</div>
                  <div className="existing-topic">{m.topic}</div>
                  <p className="existing-dir">{m.project_dir}</p>
                  <button
                    className="daily-btn primary"
                    onClick={() => handleLoadMission(m)}
                    disabled={loadingMissionId !== null}
                  >
                    {loadingMissionId === m.id ? '불러오는 중...' : '이어서 풀기 →'}
                  </button>
                </div>
              ))}
            </div>
          )}

          {/* Add new mission toggle */}
          {todayMissions.length > 0 && !showAddNew && (
            <button
              className="daily-btn add-new-btn"
              onClick={() => setShowAddNew(true)}
              disabled={loadingMissionId !== null}
            >
              + 새 미션 추가
            </button>
          )}

          {/* Topic selection (new mission) */}
          {showAddNew && topics.length > 0 && (
            <>
              <p className="daily-subtitle">
                {todayMissions.length === 0
                  ? 'AI가 추천하는 주제를 선택하거나 직접 입력하세요.'
                  : '추가할 주제를 선택하거나 직접 입력하세요.'}
              </p>
              <div className="daily-topics">
                {topics.map((t) => (
                  <div
                    key={t.slug}
                    className={`topic-card ${selected?.slug === t.slug ? 'selected' : ''}`}
                    onClick={() => { setSelected(t); setShowCustom(false) }}
                  >
                    <div className="topic-name">{t.name}</div>
                    <div className="topic-slug">{t.slug}</div>
                  </div>
                ))}
                <div
                  className={`topic-card custom-card ${showCustom ? 'selected' : ''}`}
                  onClick={() => { setShowCustom(true); setSelected(null) }}
                >
                  <div className="topic-name">직접 입력</div>
                  <div className="topic-slug">Custom</div>
                </div>
              </div>

              {showCustom && (
                <div className="custom-inputs">
                  <input
                    className="daily-input"
                    placeholder="주제 이름 (예: HTTP 서버 만들기)"
                    value={customTopic}
                    onChange={(e) => setCustomTopic(e.target.value)}
                  />
                  <input
                    className="daily-input"
                    placeholder="슬러그 영문 (예: HttpServer)"
                    value={customSlug}
                    onChange={(e) => setCustomSlug(e.target.value)}
                  />
                </div>
              )}

              {error && <p className="daily-error">{error}</p>}

              <div className="confirm-row">
                {todayMissions.length > 0 && (
                  <button
                    className="daily-btn cancel-btn"
                    onClick={() => { setShowAddNew(false); setSelected(null); setShowCustom(false); setError('') }}
                    disabled={confirming}
                  >
                    취소
                  </button>
                )}
                <button
                  className="daily-btn primary"
                  onClick={handleConfirm}
                  disabled={confirming || (!selected && (!customTopic || !customSlug))}
                >
                  {confirming ? 'AI가 미션을 생성하는 중...' : '미션 시작 →'}
                </button>
              </div>
            </>
          )}

          {error && todayMissions.length === 0 && !showAddNew && (
            <p className="daily-error">{error}</p>
          )}
        </div>

      </div>
    </div>
  )
}
