import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

interface ConceptPanelProps {
  concept: string
  tasks: string
  onClose: () => void
}

export default function ConceptPanel({ concept, tasks, onClose }: ConceptPanelProps) {
  return (
    <div className="concept-panel">
      <div className="concept-panel-header">
        <span className="concept-panel-title">📖 개념 설명</span>
        <button className="concept-panel-close" onClick={onClose} title="닫기">✕</button>
      </div>
      <div className="concept-panel-body">
        {concept && (
          <section className="concept-section">
            <div className="concept-section-label">개념</div>
            <div className="concept-md">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{concept}</ReactMarkdown>
            </div>
          </section>
        )}
        {tasks && (
          <section className="concept-section">
            <div className="concept-section-label">현재 과제</div>
            <div className="concept-md">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{tasks}</ReactMarkdown>
            </div>
          </section>
        )}
        {!concept && !tasks && (
          <div className="concept-empty">개념 설명이 없습니다.</div>
        )}
      </div>
    </div>
  )
}
