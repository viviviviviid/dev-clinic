import { useState } from 'react'
import { supabase } from '../../lib/supabase'
import './Auth.css'

export default function AuthScreen() {
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  async function handleGoogleLogin() {
    if (pending) return
    setPending(true)
    setError('')
    try {
      const { error: signInError } = await supabase.auth.signInWithOAuth({
        provider: 'google',
        options: {
          redirectTo: window.location.origin,
          queryParams: { prompt: 'select_account' },
        },
      })
      if (signInError) {
        setError(signInError.message || 'Google 로그인을 시작하지 못했습니다.')
        setPending(false)
      }
    } catch (loginError: unknown) {
      setError(loginError instanceof Error ? loginError.message : 'Google 로그인을 시작하지 못했습니다.')
      setPending(false)
    }
  }

  return (
    <main className="auth-entrance">
      <header className="auth-brand">
        <span className="clinic-cross" aria-hidden="true" />
        <span>코딩 재활센터<small>매일 조금씩, 다시 시작하는 곳</small></span>
      </header>
      <div className="clinic-scene" aria-hidden="true">
        <div className="clinic-ceiling" />
        <div className="clinic-back-wall"><div className="clinic-window" /><div className="clinic-wall-rail" /></div>
        <div className="clinic-floor" />
        <div className="clinic-bench"><i /><i /><i /></div>
        <div className="clinic-plant"><i /><i /><i /><i /><span /></div>
        <div className="clinic-room-sign"><span className="clinic-cross" /><span>코딩 재활센터<small>당신의 다음 시작을 응원합니다.</small></span></div>
        <div className="clinic-doorway">
          <div className="clinic-door clinic-door-left"><span className="door-reflection" /><span className="door-band">작은 연습이</span><span className="door-handle" /></div>
          <div className="clinic-door clinic-door-right"><span className="door-reflection" /><span className="door-band">변화를 만듭니다</span><span className="door-handle" /></div>
        </div>
        <div className="clinic-door-frame" />
        <div className="clinic-door-sensor"><i /></div>
      </div>
      <section className="auth-card" aria-labelledby="auth-title" aria-busy={pending}>
        <p className="auth-welcome">잘 오셨어요. 여기는 코딩 재활센터입니다.</p>
        <h1 className="auth-title" id="auth-title">다시, 코드와<br />친해지는 시간.</h1>
        <p className="auth-subtitle">막혀도 괜찮아요. 작은 미션 하나부터.<br />AI 튜터와 함께 나만의 속도로 연습해요.</p>
        {error ? <p className="auth-error" id="auth-error" role="alert">{error}</p> : null}
        <button
          className="auth-google-btn"
          type="button"
          onClick={() => void handleGoogleLogin()}
          disabled={pending}
          aria-describedby={error ? 'auth-error' : undefined}
        >
          <svg className="google-icon" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true" focusable="false">
            <path fill="#4285F4" d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z"/>
            <path fill="#34A853" d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"/>
            <path fill="#FBBC05" d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z"/>
            <path fill="#EA4335" d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z"/>
          </svg>
          {pending ? 'Google로 이동 중...' : 'Google로 로그인'}
        </button>
        <p className="auth-login-note">내 Google 계정으로 학습을 이어가세요.</p>
        <details className="auth-local-note">
          <summary>처음 방문하셨나요?</summary>
          <p>로그인한 뒤 오늘의 미션을 시작할 수 있어요. 코드를 작성할 때는 이 화면을 연 Mac에서 <code>clinic</code>을 실행해 주세요.</p>
        </details>
      </section>
      <footer className="auth-footer"><span>오늘의 작은 연습이, 내일의 자신감으로.</span><a href="/privacy">개인정보 처리방침</a></footer>
    </main>
  )
}
