import { useStore } from '../../store'
import './ProblemsPanel.css'

const SEVERITY_ICON: Record<number, string> = {
  1: '⛔',
  2: '⚠️',
  3: 'ℹ️',
  4: '·',
}

const SEVERITY_CLASS: Record<number, string> = {
  1: 'sev-error',
  2: 'sev-warning',
  3: 'sev-info',
  4: 'sev-hint',
}

export default function ProblemsPanel() {
  const { diagnostics, addTab, setPendingNavigate } = useStore()

  const errors = diagnostics.filter(d => d.severity === 1).length
  const warnings = diagnostics.filter(d => d.severity === 2).length

  async function handleItemClick(filePath: string, line: number, column: number) {
    const existingTab = useStore.getState().openTabs.find(t => t.path === filePath)
    if (existingTab) {
      addTab(filePath, existingTab.content)
    } else {
      // 현재 열린 파일이 이미 같은 경로라면 이동만
      const current = useStore.getState().openFile
      if (current !== filePath) return
    }
    setPendingNavigate({ path: filePath, line, column })
  }

  return (
    <div className="problems-panel">
      <div className="problems-header">
        <span className="problems-title">문제</span>
        {errors > 0 && <span className="problems-count sev-error">{errors} 오류</span>}
        {warnings > 0 && <span className="problems-count sev-warning">{warnings} 경고</span>}
        {diagnostics.length === 0 && (
          <span className="problems-empty-label">감지된 문제 없음</span>
        )}
      </div>
      <div className="problems-list">
        {diagnostics.map((item, i) => (
          <div
            key={i}
            className={`problem-item ${SEVERITY_CLASS[item.severity] ?? ''}`}
            onClick={() => handleItemClick(item.filePath, item.startLine, item.startColumn)}
            title={item.filePath}
          >
            <span className="problem-icon">{SEVERITY_ICON[item.severity] ?? '·'}</span>
            <span className="problem-message">{item.message}</span>
            <span className="problem-location">{item.fileName}:{item.startLine}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
