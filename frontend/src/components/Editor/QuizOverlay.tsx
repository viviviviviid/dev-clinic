import { useEffect, useRef, useState, useCallback, useMemo } from 'react'
import type { editor } from 'monaco-editor'
import type { QuizData, QuizItem } from '../../store'

interface QuizOverlayProps {
  editor: editor.IStandaloneCodeEditor
  filename: string
  content: string
  quizData: QuizData
  solvedHoles: Set<string>
  onSolve: (key: string, code: string, markerType: string, markerIndex: number) => void
}

interface QuizLine {
  key: string
  lineNumber: number
  item: QuizItem
  isLocked: boolean
}

interface OpenZone {
  dom: HTMLDivElement
  zoneId: string
  docTop: number
}

export default function QuizOverlay({ editor, filename, content, quizData, solvedHoles, onSolve }: QuizOverlayProps) {
  const [scrollTop, setScrollTop] = useState(editor.getScrollTop())
  const [, forceUpdate] = useState(0)
  const openZonesRef = useRef<Map<string, OpenZone>>(new Map())
  const [openWidgets, setOpenWidgets] = useState<Set<string>>(new Set())
  const [hintLevels, setHintLevels] = useState<Record<string, number>>({})
  const [writeInputs, setWriteInputs] = useState<Record<string, string>>({})

  const quizLines = useMemo<QuizLine[]>(() => {
    const lines: QuizLine[] = []
    let holeIdx = 0
    let bugIdx = 0
    let firstActiveHoleFound = false

    content.split('\n').forEach((line, idx) => {
      if (line.includes('[TUTOR:HOLE]')) {
        const newKey = `${filename}:hole:${holeIdx}`
        const legacyKey = `${filename}:${holeIdx}`
        const item = quizData[newKey] ?? quizData[legacyKey]
        const resolvedKey = quizData[newKey] ? newKey : legacyKey
        const isSolved = solvedHoles.has(resolvedKey) || solvedHoles.has(newKey)
        if (item && !isSolved) {
          const isLocked = firstActiveHoleFound
          if (!firstActiveHoleFound) firstActiveHoleFound = true
          lines.push({ key: resolvedKey, lineNumber: idx + 1, item, isLocked })
        }
        holeIdx++
      }
      if (line.includes('[TUTOR:BUG]')) {
        const key = `${filename}:bug:${bugIdx}`
        const item = quizData[key]
        if (item && !solvedHoles.has(key)) {
          lines.push({ key, lineNumber: idx + 1, item, isLocked: false })
        }
        bugIdx++
      }
    })

    return lines
  }, [content, filename, quizData, solvedHoles])

  useEffect(() => {
    const d1 = editor.onDidScrollChange(() => setScrollTop(editor.getScrollTop()))
    const d2 = editor.onDidLayoutChange(() => forceUpdate(n => n + 1))
    return () => { d1.dispose(); d2.dispose() }
  }, [editor])

  useEffect(() => {
    return () => {
      editor.changeViewZones(accessor => {
        openZonesRef.current.forEach(({ zoneId }) => accessor.removeZone(zoneId))
      })
      openZonesRef.current = new Map()
    }
  }, [editor, filename])

  useEffect(() => {
    const validKeys = new Set(quizLines.map(q => q.key))
    const toRemove: string[] = []
    openZonesRef.current.forEach((zone, key) => {
      if (!validKeys.has(key)) {
        editor.changeViewZones(accessor => accessor.removeZone(zone.zoneId))
        toRemove.push(key)
      }
    })
    if (toRemove.length > 0) {
      toRemove.forEach(k => openZonesRef.current.delete(k))
      setOpenWidgets(prev => {
        const next = new Set(prev)
        toRemove.forEach(k => next.delete(k))
        return next
      })
    }
  }, [quizLines, editor])

  const updateZoneHeight = useCallback((key: string, height: number) => {
    const zone = openZonesRef.current.get(key)
    if (!zone) return
    const MAX_ZONE = 400
    const capped = Math.min(Math.max(40, Math.ceil(height)), MAX_ZONE)
    zone.dom.style.height = `${capped}px`
    editor.changeViewZones(accessor => accessor.layoutZone(zone.zoneId))
  }, [editor])

  function openHint(key: string, lineNumber: number) {
    if (openWidgets.has(key)) return
    const savedScroll = editor.getScrollTop()
    const dom = document.createElement('div')
    dom.style.height = '300px'
    const capturedKey = key
    const initialDocTop = editor.getTopForLineNumber(lineNumber)

    let newZoneId = ''
    editor.changeViewZones(accessor => {
      newZoneId = accessor.addZone({
        afterLineNumber: lineNumber,
        domNode: dom,
        suppressMouseDown: true,
        onDomNodeTop: (top) => {
          const zone = openZonesRef.current.get(capturedKey)
          if (zone) { zone.docTop = top; forceUpdate(n => n + 1) }
        },
      })
    })

    openZonesRef.current.set(key, { dom, zoneId: newZoneId, docTop: initialDocTop })
    editor.setScrollTop(savedScroll)
    setOpenWidgets(prev => new Set([...prev, key]))
    forceUpdate(n => n + 1)
  }

  function closeHint(key: string) {
    const zone = openZonesRef.current.get(key)
    if (zone) {
      editor.changeViewZones(accessor => accessor.removeZone(zone.zoneId))
      openZonesRef.current.delete(key)
    }
    setOpenWidgets(prev => { const n = new Set(prev); n.delete(key); return n })
    forceUpdate(n => n + 1)
  }

  function handleSubmit(item: QuizItem) {
    const code = writeInputs[item.key] ?? ''
    if (!code.trim()) return
    onSolve(item.key, code, item.markerType || 'hole', item.markerIndex ?? 0)
    closeHint(item.key)
  }

  return (
    <div className="quiz-overlay">
      {quizLines.map(({ key, lineNumber, item, isLocked }) => {
        const viewTop = editor.getTopForLineNumber(lineNumber) - scrollTop
        const isOpen = openWidgets.has(key)
        const isBug = item.markerType === 'bug'
        return (
          <button
            key={key}
            className={`quiz-glyph-btn${isBug ? ' bug' : ' hole'}${isLocked ? ' locked' : ''}${isOpen ? ' open' : ''}`}
            style={{ top: viewTop }}
            onClick={() => { if (!isLocked) { isOpen ? closeHint(key) : openHint(key, lineNumber) } }}
            title={isLocked ? '앞 HOLE을 먼저 해결하세요' : (isOpen ? '닫기' : '열기')}
          />
        )
      })}

      {quizLines.map(({ key, item }) => {
        if (!openWidgets.has(key)) return null
        const zone = openZonesRef.current.get(key)
        if (!zone) return null
        const hints = item.hints ?? []
        return (
          <HintCard
            key={key}
            quizKey={key}
            item={item}
            top={zone.docTop - scrollTop}
            hints={hints}
            hintLevel={hintLevels[key] ?? 0}
            writeInput={writeInputs[key] ?? ''}
            onWriteInputChange={(v) => setWriteInputs(prev => ({ ...prev, [key]: v }))}
            onSubmit={() => handleSubmit(item)}
            onRevealHint={() => setHintLevels(prev => ({ ...prev, [key]: Math.min((prev[key] ?? 0) + 1, hints.length - 1) }))}
            onClose={() => closeHint(key)}
            onHeightChange={h => updateZoneHeight(key, h)}
          />
        )
      })}
    </div>
  )
}

// ── 힌트 카드 ─────────────────────────────────────────
interface HintCardProps {
  quizKey: string
  item: QuizItem
  top: number
  hints: string[]
  hintLevel: number
  writeInput: string
  onWriteInputChange: (val: string) => void
  onSubmit: () => void
  onRevealHint: () => void
  onClose: () => void
  onHeightChange: (h: number) => void
}

function HintCard({
  quizKey: _quizKey, item, top, hints, hintLevel,
  writeInput, onWriteInputChange, onSubmit,
  onRevealHint, onClose, onHeightChange,
}: HintCardProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const isBug = item.markerType === 'bug'

  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const observer = new ResizeObserver(entries => {
      const h = Math.ceil(entries[0].contentRect.height)
      if (h > 0) onHeightChange(h)
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [onHeightChange])

  const revealedHints = hints.slice(0, hintLevel + 1)
  const canRevealMore = hintLevel < hints.length - 1

  return (
    <div
      ref={containerRef}
      className="quiz-content"
      style={{ top, zIndex: 10 }}
    >
      <div className={`quiz-content-header ${isBug ? 'bug' : 'hole'}`}>
        <span className="quiz-content-title">{isBug ? '🐛 BUG' : '📝 HOLE'}</span>
        <button className="quiz-content-close" onClick={onClose}>✕</button>
      </div>

      <div className="quiz-body">
        <div className="quiz-question">{item.question}</div>

        {/* 코드 입력 */}
        <div className="write-mode">
          <div className="write-input-area">
            <textarea
              className="write-textarea"
              value={writeInput}
              onChange={e => onWriteInputChange(e.target.value)}
              placeholder="코드를 직접 입력하세요..."
              onKeyDown={e => {
                if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                  e.preventDefault()
                  onSubmit()
                }
              }}
              spellCheck={false}
              rows={3}
              autoFocus
            />
            <div className="write-input-hint">⌘/Ctrl+Enter 로 제출</div>
          </div>

          <button
            className="write-submit-btn"
            onClick={onSubmit}
            disabled={!writeInput.trim()}
          >
            확인 →
          </button>
        </div>

        {/* 힌트 섹션 */}
        {hints.length > 0 && (
          <div className="hint-section">
            {hintLevel >= 0 && revealedHints.length > 0 && (
              <div className="hint-list">
                {revealedHints.map((hint, i) => (
                  <div key={i} className="hint-item">
                    <span className="hint-label">힌트 {i + 1}</span>
                    <span className="hint-text">{hint}</span>
                  </div>
                ))}
              </div>
            )}
            {canRevealMore ? (
              <button className="hint-reveal-btn" onClick={onRevealHint}>
                💡 힌트 {hintLevel + 2} 보기
                <span className="hint-cost">({hintLevel + 2}/{hints.length})</span>
              </button>
            ) : hintLevel === 0 && hints.length > 0 ? (
              <button className="hint-reveal-btn" onClick={onRevealHint}>
                💡 힌트 보기
                <span className="hint-cost">(1/{hints.length})</span>
              </button>
            ) : (
              <div className="hint-exhausted">힌트를 모두 봤어요!</div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
