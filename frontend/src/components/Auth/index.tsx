import { useState, type FormEvent } from 'react'
import { supabase } from '../../lib/supabase'
import './Auth.css'

export default function AuthScreen() {
  const [email, setEmail] = useState('')
  const [pending, setPending] = useState(false)
  const [sent, setSent] = useState(false)
  const [error, setError] = useState('')

  async function handleEmailLogin(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (pending) return
    const normalizedEmail = email.trim().toLowerCase()
    if (!normalizedEmail) return

    setPending(true)
    setSent(false)
    setError('')
    try {
      const { error: signInError } = await supabase.auth.signInWithOtp({
        email: normalizedEmail,
        options: {
          emailRedirectTo: window.location.origin,
          shouldCreateUser: true,
        },
      })
      if (signInError) {
        setError(signInError.message || '로그인 메일을 보내지 못했습니다.')
        return
      }
      setSent(true)
    } catch (loginError: unknown) {
      setError(loginError instanceof Error ? loginError.message : '로그인 메일을 보내지 못했습니다.')
    } finally {
      setPending(false)
    }
  }

  return (
    <main className="auth-overlay">
      <section className="auth-card" aria-labelledby="auth-title" aria-busy={pending}>
        <div className="auth-logo" aria-hidden="true">{'</>'}</div>
        <h1 className="auth-title" id="auth-title">코딩 튜터</h1>
        <p className="auth-subtitle">AI 기반 데일리 코딩 학습 플랫폼</p>
        <div className="auth-features">
          <div className="auth-feature">
            <span className="feature-icon" aria-hidden="true">🎯</span>
            <span>매일 새로운 미션</span>
          </div>
          <div className="auth-feature">
            <span className="feature-icon" aria-hidden="true">🤖</span>
            <span>AI 실시간 피드백</span>
          </div>
          <div className="auth-feature">
            <span className="feature-icon" aria-hidden="true">📈</span>
            <span>수준별 맞춤 학습</span>
          </div>
        </div>
        <p className="auth-local-note">
          프로젝트를 사용할 때는 이 화면을 연 Mac에서 <code>clinic</code>을 실행해 주세요.
        </p>
        {sent ? (
          <p className="auth-success" id="auth-success" role="status">
            로그인 링크를 보냈습니다. 메일에서 링크를 열면 이 화면으로 돌아옵니다.
          </p>
        ) : null}
        {error ? <p className="auth-error" id="auth-error" role="alert">{error}</p> : null}
        <form className="auth-email-form" onSubmit={(event) => void handleEmailLogin(event)}>
          <label className="auth-email-label" htmlFor="auth-email">이메일</label>
          <input
            className="auth-email-input"
            id="auth-email"
            type="email"
            autoComplete="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            placeholder="you@example.com"
            required
            disabled={pending}
            aria-describedby={error ? 'auth-error' : sent ? 'auth-success' : undefined}
          />
          <button className="auth-email-btn" type="submit" disabled={pending || !email.trim()}>
            {pending ? '로그인 링크 보내는 중...' : sent ? '로그인 링크 다시 보내기' : '이메일로 로그인'}
          </button>
        </form>
      </section>
    </main>
  )
}
