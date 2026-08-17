import { useStore } from '../../store'
import './Toast.css'

export default function ToastContainer() {
  const { toasts, removeToast } = useStore()
  if (!toasts.length) return null
  const liveMode = toasts.some((toast) => toast.type === 'error') ? 'assertive' : 'polite'
  return (
    <div className="toast-container" role="region" aria-label="알림" aria-live={liveMode} aria-relevant="additions text">
      {toasts.map(t => (
        <div
          key={t.id}
          className={`toast toast-${t.type}`}
          aria-atomic="true"
        >
          <span className="toast-icon" aria-hidden="true">
            {t.type === 'error' ? '⛔' : t.type === 'success' ? '✅' : 'ℹ️'}
          </span>
          <span className="toast-message">{t.message}</span>
          <button className="toast-close" type="button" onClick={() => removeToast(t.id)} aria-label="알림 닫기">✕</button>
        </div>
      ))}
    </div>
  )
}
