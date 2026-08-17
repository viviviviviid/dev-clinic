import './BootError.css'

interface Props {
  error: unknown
  title?: string
  description?: string
  onReload?: () => void
}

function reloadPage() {
  window.location.reload()
}

function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message.trim()) return error.message
  if (typeof error === 'string' && error.trim()) return error
  return '알 수 없는 오류가 발생했습니다.'
}

export default function BootErrorScreen({
  error,
  title = '코딩 튜터를 시작하지 못했습니다',
  description = '배포 설정이나 네트워크 상태를 확인한 뒤 새로고침해 주세요. 문제가 계속되면 아래 오류 정보를 확인하세요.',
  onReload = reloadPage,
}: Props) {
  return (
    <main className="boot-error-screen" role="alert" aria-live="assertive">
      <section className="boot-error-card" aria-labelledby="boot-error-title">
        <p className="boot-error-kicker">Startup recovery</p>
        <h1 className="boot-error-title" id="boot-error-title">{title}</h1>
        <p className="boot-error-description">{description}</p>
        <details className="boot-error-details">
          <summary>오류 정보 보기</summary>
          <code className="boot-error-message">{errorMessage(error)}</code>
        </details>
        <div className="boot-error-actions">
          <button className="boot-error-reload" type="button" onClick={onReload} autoFocus>
            새로고침
          </button>
        </div>
      </section>
    </main>
  )
}
