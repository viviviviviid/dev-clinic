import { useState } from 'react'
import type { SkillLevel, UserSettings } from '../../store'
import { apiJson } from '../../lib/api'
import './Settings.css'

const LANGUAGES = ['Go', 'TypeScript', 'JavaScript', 'Rust', 'Python']

const SKILL_LEVELS: { value: SkillLevel; icon: string; title: string; desc: string }[] = [
  { value: 'newbie', icon: '🌱', title: '뉴비 / 재활', desc: '자세한 설명, 퀴즈 제공' },
  { value: 'normal', icon: '⚡', title: '보통', desc: '힌트와 가이드 제공' },
  { value: 'experienced', icon: '🔥', title: '숙련자', desc: '스스로 해결합니다' },
]

function normalizeSkillLevel(value: string | undefined): SkillLevel {
  if (value === 'newbie' || value === 'experienced') return value
  return 'normal'
}

interface Props {
  onComplete: (settings: UserSettings) => void
  initial?: UserSettings | null
}

export default function SettingsScreen({ onComplete, initial }: Props) {
  const initialLanguageSupported = !initial?.language || LANGUAGES.includes(initial.language)

  const [language, setLanguage] = useState(initialLanguageSupported ? initial?.language ?? '' : '')
  const [skillLevel, setSkillLevel] = useState<SkillLevel>(() => normalizeSkillLevel(initial?.skill_level))
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(
    initialLanguageSupported
      ? ''
      : `기존 언어 ${initial?.language}은(는) 현재 실행/테스트를 지원하지 않습니다. 지원 언어를 새로 선택해주세요.`,
  )

  async function handleSave() {
    if (!language) {
      setError('언어를 선택해주세요.')
      return
    }
    setLoading(true)
    setError('')
    try {
      const data = await apiJson<UserSettings>('/api/user/settings', {
        method: 'PUT',
        body: JSON.stringify({ language, skill_level: skillLevel }),
      })
      onComplete(data)
    } catch (error: unknown) {
      setError(error instanceof Error ? error.message : '설정을 저장하지 못했습니다.')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="settings-overlay">
      <form
        className="settings-container"
        role="dialog"
        aria-modal="true"
        aria-labelledby="settings-title"
        aria-describedby="settings-description"
        aria-busy={loading}
        onSubmit={(event) => {
          event.preventDefault()
          void handleSave()
        }}
      >
        <aside className="settings-sidebar">
          <div className="settings-sidebar-header">
            <span className="settings-kicker">Coding clinic</span>
            <h2 id="settings-title">학습 설정</h2>
            <p id="settings-description">
              실제 미션 생성과 AI 피드백에 적용되는 항목만 설정합니다.
            </p>
          </div>
          <div className="settings-sidebar-note">
            <span aria-hidden="true">💡</span>
            <p>{initial ? '변경사항은 다음 미션부터 가장 정확하게 반영됩니다.' : '처음 한 번만 선택하면 바로 학습을 시작할 수 있습니다.'}</p>
          </div>
        </aside>

        <div className="settings-main">
          <div className="settings-content">
            <h3>사용자 프로필</h3>

            <section className="settings-section" aria-labelledby="storage-directory-title">
              <h4 className="settings-label" id="storage-directory-title">학습 파일 저장 디렉토리</h4>
              <p className="settings-hint">clinic 실행 시 CLI 인자로 지정합니다. 예: <code>./run.sh ~/learning</code></p>
              {initial?.base_dir ? (
                <p className="settings-current-path">현재 경로 <code>{initial.base_dir}</code></p>
              ) : (
                <p className="settings-hint">저장 경로는 실행 중인 clinic에서 관리합니다.</p>
              )}
            </section>

            <fieldset className="settings-section settings-fieldset">
              <legend className="settings-label">주 언어</legend>
              <p className="settings-hint">미션 코드 실행·테스트와 AI 피드백의 기준 언어입니다.</p>
              <div className="options-grid language-options">
                {LANGUAGES.map((lang) => (
                  <label key={lang} className={`option-card ${language === lang ? 'active' : ''}`}>
                    <input
                      className="option-radio"
                      type="radio"
                      name="language"
                      value={lang}
                      checked={language === lang}
                      autoFocus={language ? language === lang : lang === LANGUAGES[0]}
                      onChange={() => {
                        setLanguage(lang)
                        setError('')
                      }}
                    />
                    <strong>{lang}</strong>
                  </label>
                ))}
              </div>
            </fieldset>

            <fieldset className="settings-section settings-fieldset">
              <legend className="settings-label">학습 수준</legend>
              <p className="settings-hint">문제 난이도와 AI 힌트의 깊이를 결정합니다.</p>
              <div className="options-grid">
                {SKILL_LEVELS.map((level) => (
                  <label key={level.value} className={`option-card ${skillLevel === level.value ? 'active' : ''}`}>
                    <input
                      className="option-radio"
                      type="radio"
                      name="skill-level"
                      value={level.value}
                      checked={skillLevel === level.value}
                      onChange={() => setSkillLevel(level.value)}
                    />
                    <span className="option-icon" aria-hidden="true">{level.icon}</span>
                    <strong>{level.title}</strong>
                    <span className="option-description">{level.desc}</span>
                  </label>
                ))}
              </div>
            </fieldset>
          </div>

          <div className="settings-footer">
            {error ? <span className="settings-error" role="alert">{error}</span> : null}
            {initial && initialLanguageSupported ? (
              <button className="btn btn-secondary" type="button" onClick={() => onComplete(initial)} disabled={loading}>
                취소
              </button>
            ) : null}
            <button
              className="btn btn-primary"
              type="submit"
              disabled={loading || !language}
            >
              {loading ? '저장 중...' : '변경사항 저장'}
            </button>
          </div>
        </div>
      </form>
    </div>
  )
}
