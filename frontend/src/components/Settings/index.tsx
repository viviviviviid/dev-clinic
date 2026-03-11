import { useState } from 'react'
import type { SkillLevel, UserSettings } from '../../store'
import { supabase } from '../../lib/supabase'
import './Settings.css'

const LANGUAGES = ['Go', 'TypeScript', 'JavaScript', 'Rust', 'Python', 'Solidity']

const SKILL_LEVELS: { value: SkillLevel; icon: string; title: string; desc: string }[] = [
  { value: 'newbie', icon: '🌱', title: '뉴비 / 재활', desc: '자세한 설명, 퀴즈 제공' },
  { value: 'normal', icon: '⚡', title: '보통', desc: '힌트와 가이드 제공' },
  { value: 'experienced', icon: '🔥', title: '숙련자', desc: '스스로 해결합니다' },
]

interface Props {
  onComplete: (settings: UserSettings) => void
  initial?: UserSettings | null
}

export default function SettingsScreen({ onComplete, initial }: Props) {
  const [activeTab, setActiveTab] = useState<'general' | 'appearance' | 'editor'>('general')

  // Settings State
  const [baseDir, setBaseDir] = useState(initial?.base_dir ?? '')
  const [language, setLanguage] = useState(initial?.language ?? '')
  const [skillLevel, setSkillLevel] = useState<SkillLevel>((initial?.skill_level as SkillLevel) ?? 'normal')
  const [theme, setTheme] = useState('dark') // Visual only for now
  const [minimap, setMinimap] = useState(true)
  const [wordWrap, setWordWrap] = useState(true)

  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  async function handleSave() {
    if (!baseDir || !language) {
      setError('필수 항목(저장 디렉토리, 언어)을 입력해주세요.')
      return
    }
    setLoading(true)
    setError('')
    try {
      const { data: { session } } = await supabase.auth.getSession()
      const headers: Record<string, string> = { 'Content-Type': 'application/json' }
      if (session?.access_token) {
        headers['Authorization'] = `Bearer ${session.access_token}`
      }

      // In a real app we might also save theme, minimap, wordWrap to local store or DB
      const res = await fetch('/api/user/settings', {
        method: 'PUT',
        headers,
        body: JSON.stringify({ base_dir: baseDir, language, skill_level: skillLevel }),
      })
      const data = await res.json()
      if (data.error) throw new Error(data.error)
      onComplete(data as UserSettings)
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  function renderContent() {
    switch (activeTab) {
      case 'general':
        return (
          <>
            <h3>일반 설정</h3>
            <div className="settings-section">
              <label className="settings-label">학습 파일 저장 디렉토리</label>
              <p className="settings-hint">오늘의 미션이 이 경로 안에 날짜별 폴더로 자동 생성됩니다.</p>
              <input
                className="settings-input"
                type="text"
                placeholder="/Users/username/daily-coding"
                value={baseDir}
                onChange={(e) => setBaseDir(e.target.value)}
              />
            </div>

            <div className="settings-section">
              <label className="settings-label">주 언어 설정</label>
              <p className="settings-hint">AI가 코드를 분석하고 피드백을 제공할 때 기준이 되는 언어입니다.</p>
              <div className="options-grid">
                {LANGUAGES.map((lang) => (
                  <div
                    key={lang}
                    className={`option-card ${language === lang ? 'active' : ''}`}
                    onClick={() => setLanguage(lang)}
                  >
                    <strong>{lang}</strong>
                  </div>
                ))}
              </div>
            </div>

            <div className="settings-section">
              <label className="settings-label">학습 수준</label>
              <p className="settings-hint">선택한 수준에 따라 AI의 문제 출제 난이도와 힌트의 깊이가 달라집니다.</p>
              <div className="options-grid">
                {SKILL_LEVELS.map((sl) => (
                  <div
                    key={sl.value}
                    className={`option-card ${skillLevel === sl.value ? 'active' : ''}`}
                    onClick={() => setSkillLevel(sl.value)}
                  >
                    <span className="option-icon">{sl.icon}</span>
                    <strong>{sl.title}</strong>
                    <p>{sl.desc}</p>
                  </div>
                ))}
              </div>
            </div>
          </>
        )
      case 'appearance':
        return (
          <>
            <h3>화면 테마</h3>
            <div className="settings-section">
              <label className="settings-label">테마 설정</label>
              <p className="settings-hint">에디터의 전체 디자인 테마를 설정합니다.</p>
              <div className="options-grid">
                <div
                  className={`option-card ${theme === 'dark' ? 'active' : ''}`}
                  onClick={() => setTheme('dark')}
                >
                  <span className="option-icon">🌙</span>
                  <strong>어두운 테마</strong>
                  <p>기본 고대비 테마</p>
                </div>
                <div
                  className={`option-card ${theme === 'light' ? 'active' : ''}`}
                  onClick={() => setTheme('light')}
                >
                  <span className="option-icon">☀️</span>
                  <strong>밝은 테마</strong>
                  <p>눈이 편안한 테마 (추후 지원)</p>
                </div>
              </div>
            </div>
          </>
        )
      case 'editor':
        return (
          <>
            <h3>에디터 환경</h3>
            <div className="settings-section">
              <div className="toggle-row">
                <div className="toggle-info">
                  <strong>미니맵 표시</strong>
                  <p>에디터 우측에 코드 전체 구조를 축소해서 보여줍니다.</p>
                </div>
                <label className="switch">
                  <input type="checkbox" checked={minimap} onChange={(e) => setMinimap(e.target.checked)} />
                  <span className="slider"></span>
                </label>
              </div>
              <div className="toggle-row">
                <div className="toggle-info">
                  <strong>자동 줄바꿈 (Word Wrap)</strong>
                  <p>창 크기를 넘어가는 긴 코드를 보기 쉽게 줄바꿈합니다.</p>
                </div>
                <label className="switch">
                  <input type="checkbox" checked={wordWrap} onChange={(e) => setWordWrap(e.target.checked)} />
                  <span className="slider"></span>
                </label>
              </div>
            </div>
          </>
        )
    }
  }

  return (
    <div className="settings-overlay">
      <div className="settings-container">
        {/* Sidebar */}
        <div className="settings-sidebar">
          <div className="settings-sidebar-header">
            <h2>설정 환경</h2>
          </div>
          <nav className="settings-nav">
            <div
              className={`settings-nav-item ${activeTab === 'general' ? 'active' : ''}`}
              onClick={() => setActiveTab('general')}
            >
              사용자 프로필
            </div>
            <div
              className={`settings-nav-item ${activeTab === 'appearance' ? 'active' : ''}`}
              onClick={() => setActiveTab('appearance')}
            >
              테마 및 외관
            </div>
            <div
              className={`settings-nav-item ${activeTab === 'editor' ? 'active' : ''}`}
              onClick={() => setActiveTab('editor')}
            >
              코드 에디터
            </div>
          </nav>
        </div>

        {/* Main Content Area */}
        <div className="settings-main">
          <div className="settings-content">
            {renderContent()}
          </div>

          <div className="settings-footer">
            {error && <span className="settings-error">{error}</span>}
            <button className="btn btn-secondary" onClick={() => {
              if (initial) onComplete(initial);
            }}>취소</button>
            <button
              className="btn btn-primary"
              onClick={handleSave}
              disabled={loading}
            >
              {loading ? '저장 중...' : '변경사항 저장'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
