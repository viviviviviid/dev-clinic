import { useEffect, useState } from 'react'
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
  date: string       // "2026-03-08"
  topic: string
  slug: string
  project_dir: string
  status: string     // "active" | "completed"
}

export default function DashboardScreen({ onMissionReady, onOpenSettings }: Props) {
  const { getDailyMission, getDailyHistory, confirmDailyMission, loadProject } = useProject()
  const { userSettings } = useStore()

  const [topics, setTopics] = useState<TopicSuggestion[]>([])
  const [todayMissions, setTodayMissions] = useState<MissionRecord[]>([])
  const [history, setHistory] = useState<MissionRecord[]>([])
  const [customTopic, setCustomTopic] = useState('')
  const [customSlug, setCustomSlug] = useState('')
  const [loading, setLoading] = useState(true)
  const [confirming, setConfirming] = useState(false)
  const [loadingMissionId, setLoadingMissionId] = useState<string | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    Promise.all([
      getDailyMission().catch(() => ({ missions: [], topics: [] })),
      getDailyHistory(),
    ])
      .then(([daily, hist]) => {
        const missions: MissionRecord[] = daily.missions || []
        setTodayMissions(missions)
        if (daily.topics) setTopics(daily.topics)
        if (daily.error) setError(daily.error)
        // 오늘 날짜 미션은 todayMissions에서 표시하므로 history에서 제외
        const todayStr = new Date().toISOString().slice(0, 10)
        if (Array.isArray(hist)) setHistory(hist.filter((m: MissionRecord) => m.date !== todayStr))
      })
      .finally(() => setLoading(false))
  }, [])

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

  // Placeholder for simple streak generation (last 28 days)
  function getRecentStreak() {
    const dates = []
    const today = new Date()
    for (let i = 27; i >= 0; i--) {
      const d = new Date(today)
      d.setDate(d.getDate() - i)
      const dateStr = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
      const mList = missionsByDate.get(dateStr) || []
      const level = mList.length >= 3 ? 4 : mList.length === 2 ? 3 : mList.length === 1 ? 2 : 0
      dates.push({ dateStr, level, isToday: i === 0 })
    }
    return dates
  }

  const streakData = getRecentStreak()

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

  if (loading) {
    return (
      <div className="dashboard-overlay">
        <div className="dashboard-container" style={{ alignItems: 'center', justifyContent: 'center' }}>
          <div style={{ color: '#8b949e' }}>데이터 패치중...</div>
        </div>
      </div>
    )
  }

  return (
    <div className="dashboard-overlay">
      <div className="dashboard-container">
        {/* Sidebar */}
        <div className="dashboard-sidebar">
          <div className="dashboard-logo-area">
            <span className="dashboard-logo">{'</>'}</span>
            <span className="dashboard-title">Coding Tutor</span>
          </div>
          <nav className="dashboard-nav">
            <div className="nav-item active">🏠 홈 (대시보드)</div>
            <div style={{ flex: 1 }}></div>
            <div className="nav-item" onClick={onOpenSettings}>⚙ 설정</div>
          </nav>
        </div>

        {/* Main Area */}
        <div className="dashboard-main">
          <div className="dashboard-header">
            <div className="dashboard-greeting">
              <h1>오늘도 코딩 시작해볼까요?</h1>
              <p>최근 프로젝트를 이어서 하거나 새로운 기술을 배워보세요.</p>
            </div>
            <div className="dashboard-actions">
            </div>
          </div>

          <div className="dashboard-grid">
            {/* Left Column: Projects & Generator */}
            <div className="left-col">
              <div className="dashboard-card" style={{ marginBottom: '1.5rem' }}>
                <div className="card-title">진행중인 학습 및 프로젝트</div>
                <div className="project-list">
                  {todayMissions.length === 0 && history.slice(0, 3).length === 0 && (
                    <p style={{ color: '#8b949e', fontSize: '0.875rem' }}>진행중인 프로젝트가 없습니다.</p>
                  )}
                  {todayMissions.map((m) => (
                    <div key={m.id} className="project-item" onClick={() => handleLoadMission(m)}>
                      <div className="project-info">
                        <h3>{m.topic}</h3>
                        <p>{m.project_dir}</p>
                      </div>
                      <div className="project-meta">
                        <span className={`project-status ${m.status === 'completed' ? 'completed' : ''}`}>
                          {m.status === 'completed' ? '완료' : '진행 중'}
                        </span>
                        <span>{loadingMissionId === m.id ? '준비중...' : '이어서 하기 →'}</span>
                      </div>
                    </div>
                  ))}
                  {history.slice(0, 3).map((m) => (
                    <div key={m.id} className="project-item" onClick={() => handleLoadMission(m)}>
                      <div className="project-info">
                        <h3>{m.topic}</h3>
                        <p>{m.project_dir}</p>
                      </div>
                      <div className="project-meta">
                        <span className="project-status completed">이전 미션</span>
                        <span>돌아가기 →</span>
                      </div>
                    </div>
                  ))}
                </div>
              </div>

              <div className="dashboard-card topic-generator">
                <div className="card-title">새로운 프로젝트 만들기</div>
                <p style={{ fontSize: '0.875rem', color: '#8b949e', marginBottom: '1rem' }}>
                  AI가 새로운 개발 환경과 추천 템플릿을 생성해줍니다.
                </p>
                <div className="topic-input-group">
                  <input
                    className="topic-input"
                    placeholder="배우고 싶은 주제 (예: 간단한 웹 서버 구현)"
                    value={customTopic}
                    onChange={(e) => {
                      setCustomTopic(e.target.value)
                      // Optional: Auto-generate simple slug
                      if (!customSlug && e.target.value.length > 2) {
                        setCustomSlug(e.target.value.replace(/\s+/g, '').slice(0, 15))
                      }
                    }}
                  />
                  <input
                    className="topic-input"
                    placeholder="디렉토리 영문명"
                    value={customSlug}
                    onChange={(e) => setCustomSlug(e.target.value)}
                    style={{ flex: '0.5' }}
                  />
                  <button
                    className="btn btn-primary"
                    onClick={() => handleCreateNewMission(customTopic, customSlug)}
                    disabled={confirming || !customTopic || !customSlug}
                  >
                    {confirming ? '생성중...' : '시작하기'}
                  </button>
                </div>
                {error && <p style={{ color: '#f85149', fontSize: '0.875rem', marginBottom: '1rem' }}>{error}</p>}

                {topics.length > 0 && (
                  <>
                    <p style={{ fontSize: '0.75rem', color: '#8b949e', marginBottom: '0.5rem' }}>추천 주제 (클릭하여 시작)</p>
                    <div className="topic-suggestions">
                      {topics.map(t => (
                        <div
                          key={t.slug}
                          className="suggestion-chip"
                          onClick={() => {
                            setCustomTopic(t.name)
                            setCustomSlug(t.slug)
                          }}
                        >
                          {t.name}
                        </div>
                      ))}
                    </div>
                  </>
                )}
              </div>
            </div>

            {/* Right Column: Calendar & Deadlines */}
            <div className="right-col">
              <div className="dashboard-card">
                <div className="card-title">학습 활동 (최근 4주)</div>
                <div className="calendar-widget">
                  <div className="streak-graph">
                    {streakData.map((d, i) => (
                      <div
                        key={i}
                        className={`streak-cell level-${d.level} ${d.isToday ? 'today' : ''}`}
                        title={d.dateStr}
                      ></div>
                    ))}
                  </div>
                </div>

              </div>
            </div>

          </div>
        </div>
      </div>
    </div>
  )
}
