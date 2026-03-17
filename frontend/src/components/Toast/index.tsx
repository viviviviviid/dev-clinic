import { useStore } from '../../store'
import './Toast.css'

export default function ToastContainer() {
  const { toasts, removeToast } = useStore()
  if (!toasts.length) return null
  return (
    <div className="toast-container">
      {toasts.map(t => (
        <div key={t.id} className={`toast toast-${t.type}`}>
          <span className="toast-icon">
            {t.type === 'error' ? '⛔' : t.type === 'success' ? '✅' : 'ℹ️'}
          </span>
          <span className="toast-message">{t.message}</span>
          <button className="toast-close" onClick={() => removeToast(t.id)}>✕</button>
        </div>
      ))}
    </div>
  )
}
