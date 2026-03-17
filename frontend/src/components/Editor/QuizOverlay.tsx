import { useEffect, useRef, useState, useCallback, useMemo } from 'react'
import type { editor } from 'monaco-editor'
import type { QuizData, QuizItem } from '../../store'
import { supabase } from '../../lib/supabase'
import { LOCAL } from '../../lib/api'

interface QuizOverlayProps {
  editor: editor.IStandaloneCodeEditor
  filename: string
  content: string
  quizData: QuizData
  solvedHoles: Set<string>
  onSolve: (key: string, correctCode: string, markerType: string, markerIndex: number) => void
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

type QuizMode = 'quiz' | 'write'

export default function QuizOverlay({ editor, filename, content, quizData, solvedHoles, onSolve }: QuizOverlayProps) {
  const [scrollTop, setScrollTop] = useState(editor.getScrollTop())
  const [, forceUpdate] = useState(0)
  const openZonesRef = useRef<Map<string, OpenZone>>(new Map())
  const [openWidgets, setOpenWidgets] = useState<Set<string>>(new Set())

  // Quiz mode state
  const [hintLevels, setHintLevels] = useState<Record<string, number>>({})
  const [shakingKey, setShakingKey] = useState<string | null>(null)
  const [wrongKeys, setWrongKeys] = useState<Set<string>>(new Set())
  const [explanations, setExplanations] = useState<Record<string, string>>({})
  const [isExplaining, setIsExplaining] = useState<Record<string, boolean>>({})

  // Nurse VN overlay for wrong answers
  const [nurseExpl, setNurseExpl] = useState('')
  const [showNurse, setShowNurse] = useState(false)
  const [isNurseExplaining, setIsNurseExplaining] = useState(false)
  const [wrongCounts, setWrongCounts] = useState<Record<string, number>>({})
  const [nurseExplKey, setNurseExplKey] = useState<string | null>(null)

  // Write mode state
  const [modes, setModes] = useState<Record<string, QuizMode>>({})
  const [writeInputs, setWriteInputs] = useState<Record<string, string>>({})
  const [writeErrors, setWriteErrors] = useState<Record<string, boolean>>({})
  const [writeHintLevels, setWriteHintLevels] = useState<Record<string, number>>({})

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
    const MAX_ZONE = 480
    const capped = Math.min(Math.max(40, Math.ceil(height)), MAX_ZONE)
    zone.dom.style.height = `${capped}px`
    editor.changeViewZones(accessor => accessor.layoutZone(zone.zoneId))
  }, [editor])

  function openQuiz(key: string, lineNumber: number) {
    if (openWidgets.has(key)) return
    const savedScroll = editor.getScrollTop()
    const dom = document.createElement('div')
    dom.style.height = '500px'
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

  function closeQuiz(key: string) {
    const zone = openZonesRef.current.get(key)
    if (zone) {
      editor.changeViewZones(accessor => accessor.removeZone(zone.zoneId))
      openZonesRef.current.delete(key)
    }
    setOpenWidgets(prev => { const n = new Set(prev); n.delete(key); return n })
    forceUpdate(n => n + 1)
  }

  function revealNextHint(key: string, total: number) {
    setHintLevels(prev => ({ ...prev, [key]: Math.min((prev[key] ?? 0) + 1, total) }))
  }

  function revealNextWriteHint(key: string, extraCount: number) {
    setWriteHintLevels(prev => ({ ...prev, [key]: Math.min((prev[key] ?? 0) + 1, extraCount) }))
  }

  async function fetchExplanation(key: string, item: QuizItem, wrongLabel: string) {
    setNurseExpl('')
    setShowNurse(true)
    setNurseExplKey(key)
    setIsNurseExplaining(true)
    setExplanations(prev => ({ ...prev, [key]: '' }))
    setIsExplaining(prev => ({ ...prev, [key]: true }))
    try {
      const { data: { session } } = await supabase.auth.getSession()
      const headers: Record<string, string> = { 'Content-Type': 'application/json' }
      if (session?.access_token) headers['Authorization'] = `Bearer ${session.access_token}`
      const res = await fetch(`${LOCAL}/api/explain`, {
        method: 'POST',
        headers,
        body: JSON.stringify({ question: item.question, wrongChoice: wrongLabel, correctCode: item.correctCode, markerType: item.markerType || 'hole' }),
      })
      if (!res.ok || !res.body) return
      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const chunks = buffer.split('\n\n')
        buffer = chunks.pop() ?? ''
        for (const chunk of chunks) {
          if (chunk.startsWith('event: done')) continue
          const match = chunk.match(/^data: (.*)$/m)
          if (match) {
            try {
              const parsed = JSON.parse(match[1]) as { text: string }
              setNurseExpl(prev => prev + parsed.text)
              setExplanations(prev => ({ ...prev, [key]: (prev[key] ?? '') + parsed.text }))
            } catch { /* ignore */ }
          }
        }
      }
    } catch { /* ignore */ } finally {
      setIsNurseExplaining(false)
      setIsExplaining(prev => ({ ...prev, [key]: false }))
    }
  }

  function handleOptionClick(item: QuizItem, optionIdx: number) {
    const option = item.options[optionIdx]
    if (option.isCorrect) {
      onSolve(item.key, item.correctCode, item.markerType || 'hole', item.markerIndex ?? 0)
    } else {
      setWrongCounts(prev => ({ ...prev, [item.key]: (prev[item.key] ?? 0) + 1 }))
      setShakingKey(item.key)
      setWrongKeys(prev => new Set(prev).add(item.key))
      setTimeout(() => setShakingKey(null), 450)
      setTimeout(() => setWrongKeys(prev => { const n = new Set(prev); n.delete(item.key); return n }), 700)
      fetchExplanation(item.key, item, option.label)
    }
  }

  function normalizeCode(s: string): string {
    return s.replace(/\[TUTOR:(HOLE|BUG)\]/g, '').trim().replace(/\s+/g, ' ')
  }

  async function handleWriteSubmit(item: QuizItem) {
    const key = item.key
    const userInput = writeInputs[key] ?? ''
    if (!userInput.trim()) return
    const normalized = normalizeCode(userInput)
    const correct = normalizeCode(item.correctCode)
    if (normalized === correct) {
      onSolve(key, item.correctCode, item.markerType || 'hole', item.markerIndex ?? 0)
    } else {
      setShakingKey(key)
      setWriteErrors(prev => ({ ...prev, [key]: true }))
      setTimeout(() => setShakingKey(null), 450)
      setTimeout(() => setWriteErrors(prev => ({ ...prev, [key]: false })), 700)
      fetchExplanation(key, item, userInput)
    }
  }

  const nurseImg = nurseExplKey && wrongCounts[nurseExplKey] >= 2 ? '/angry.png' : '/shocked.png'

  return (
    <div className="quiz-overlay">
      {showNurse && (
        <div className="nurse-explanation-overlay">
          <div className="nurse-overlay-character">
            <img
              src={nurseImg}
              alt="간호사"
              onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
            />
          </div>
          <div className="vn-dialogue nurse-vn-dialogue">
            <div className="vn-name-tag">담당 간호사</div>
            <p className="vn-dialogue-text nurse-dialogue-text">
              {nurseExpl || (isNurseExplaining ? '설명 중...' : '')}
              {isNurseExplaining && <span className="quiz-explanation-cursor" />}
            </p>
            <button className="nurse-close-btn" onClick={() => setShowNurse(false)}>✕ 닫기</button>
          </div>
        </div>
      )}

      {quizLines.map(({ key, lineNumber, item, isLocked }) => {
        const viewTop = editor.getTopForLineNumber(lineNumber) - scrollTop
        const isOpen = openWidgets.has(key)
        const isBug = item.markerType === 'bug'
        return (
          <button
            key={key}
            className={`quiz-glyph-btn${isBug ? ' bug' : ' hole'}${isLocked ? ' locked' : ''}${isOpen ? ' open' : ''}`}
            style={{ top: viewTop }}
            onClick={() => { if (!isLocked) { isOpen ? closeQuiz(key) : openQuiz(key, lineNumber) } }}
            title={isLocked ? '앞 HOLE을 먼저 해결하세요' : (isOpen ? '닫기' : '열기')}
          />
        )
      })}

      {quizLines.map(({ key, item }) => {
        if (!openWidgets.has(key)) return null
        const zone = openZonesRef.current.get(key)
        if (!zone) return null
        const mode: QuizMode = modes[key] ?? 'quiz'
        const hints = item.hints ?? []
        const extraHints = hints.slice(1) // hints[1..n] for write mode
        return (
          <QuizContent
            key={key}
            quizKey={key}
            item={item}
            top={zone.docTop - scrollTop}
            isShaking={shakingKey === key}
            hints={hints}
            revealedCount={hintLevels[key] ?? 0}
            wrongKeys={wrongKeys}
            explanations={explanations}
            isExplaining={isExplaining}
            mode={mode}
            onModeChange={(m) => setModes(prev => ({ ...prev, [key]: m }))}
            writeInput={writeInputs[key] ?? ''}
            onWriteInputChange={(v) => setWriteInputs(prev => ({ ...prev, [key]: v }))}
            onWriteSubmit={() => handleWriteSubmit(item)}
            writeError={writeErrors[key] ?? false}
            writeHintLevel={writeHintLevels[key] ?? 0}
            extraHints={extraHints}
            onRevealWriteHint={() => revealNextWriteHint(key, extraHints.length)}
            onClose={() => closeQuiz(key)}
            onRevealHint={() => revealNextHint(key, hints.length)}
            onOptionClick={i => handleOptionClick(item, i)}
            onHeightChange={h => updateZoneHeight(key, h)}
          />
        )
      })}
    </div>
  )
}

// ── 퀴즈 콘텐츠 카드 ─────────────────────────────────────────
interface QuizContentProps {
  quizKey: string
  item: QuizItem
  top: number
  isShaking: boolean
  hints: string[]
  revealedCount: number
  wrongKeys: Set<string>
  explanations: Record<string, string>
  isExplaining: Record<string, boolean>
  mode: QuizMode
  onModeChange: (mode: QuizMode) => void
  writeInput: string
  onWriteInputChange: (val: string) => void
  onWriteSubmit: () => void
  writeError: boolean
  writeHintLevel: number
  extraHints: string[]
  onRevealWriteHint: () => void
  onClose: () => void
  onRevealHint: () => void
  onOptionClick: (i: number) => void
  onHeightChange: (h: number) => void
}

function formatCodeLabel(s: string): string {
  if (!s.includes('\n') && s.includes('; ')) {
    return s.split('; ').join('\n')
  }
  return s
}

function QuizContent({
  quizKey, item, top, isShaking,
  hints, revealedCount, wrongKeys, explanations, isExplaining,
  mode, onModeChange, writeInput, onWriteInputChange, onWriteSubmit,
  writeError, writeHintLevel, extraHints, onRevealWriteHint,
  onClose, onRevealHint, onOptionClick, onHeightChange,
}: QuizContentProps) {
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

  const hasExplanation = !!(explanations[quizKey] || isExplaining[quizKey])

  return (
    <div
      ref={containerRef}
      className={`quiz-content ${isShaking ? 'shake' : ''}`}
      style={{ top, zIndex: 10 }}
    >
      <div className={`quiz-content-header ${isBug ? 'bug' : 'hole'}`}>
        <span className="quiz-content-title">{isBug ? '🐛 BUG' : '📝 HOLE'}</span>
        <div className="quiz-mode-tabs">
          <button
            className={`quiz-mode-tab ${mode === 'quiz' ? 'active' : ''}`}
            onClick={() => onModeChange('quiz')}
          >
            퀴즈
          </button>
          <button
            className={`quiz-mode-tab ${mode === 'write' ? 'active' : ''}`}
            onClick={() => onModeChange('write')}
          >
            직접 작성
          </button>
        </div>
        <button className="quiz-content-close" onClick={onClose}>✕</button>
      </div>

      <div className="quiz-body">
        <div className="quiz-question">{item.question}</div>

        {mode === 'quiz' ? (
          <>
            {hints.length > 0 && (
              <div className="hint-section">
                {revealedCount > 0 && (
                  <div className="hint-list">
                    {hints.slice(0, revealedCount).map((hint, i) => (
                      <div key={i} className="hint-item">
                        <span className="hint-label">힌트 {i + 1}</span>
                        <span className="hint-text">{hint}</span>
                      </div>
                    ))}
                  </div>
                )}
                {revealedCount < hints.length ? (
                  <button className="hint-reveal-btn" onClick={onRevealHint}>
                    💡 힌트 {revealedCount + 1} 보기
                    <span className="hint-cost">({revealedCount + 1}/{hints.length})</span>
                  </button>
                ) : (
                  <div className="hint-exhausted">힌트를 모두 봤어요. 이제 답을 골라보세요!</div>
                )}
              </div>
            )}

            <div className="quiz-options">
              {item.options.map((opt, i) => (
                <button
                  key={i}
                  className={`quiz-option ${wrongKeys.has(quizKey) && !opt.isCorrect ? 'wrong-flash' : ''}`}
                  onClick={() => onOptionClick(i)}
                >
                  <span className="quiz-option-num">{i + 1}.</span>
                  <pre className="quiz-option-code"><code>{formatCodeLabel(opt.label)}</code></pre>
                </button>
              ))}
            </div>

            {hasExplanation && (
              <div className="quiz-explanation">
                <span className="quiz-explanation-label">AI 설명</span>
                <p className="quiz-explanation-text">
                  {explanations[quizKey]}
                  {isExplaining[quizKey] && <span className="quiz-explanation-cursor" />}
                </p>
              </div>
            )}
          </>
        ) : (
          /* ── 직접 작성 모드 ── */
          <div className="write-mode">
            {/* 가이드라인: hints[0] 항상 표시 */}
            {hints.length > 0 && (
              <div className="write-guideline">
                <span className="guideline-label">📌 가이드라인</span>
                <span className="guideline-text">{hints[0]}</span>
              </div>
            )}

            {/* 추가 힌트 (hints[1..]) */}
            {extraHints.length > 0 && (
              <div className="hint-section">
                {writeHintLevel > 0 && (
                  <div className="hint-list">
                    {extraHints.slice(0, writeHintLevel).map((hint, i) => (
                      <div key={i} className="hint-item">
                        <span className="hint-label">힌트 {i + 2}</span>
                        <span className="hint-text">{hint}</span>
                      </div>
                    ))}
                  </div>
                )}
                {writeHintLevel < extraHints.length ? (
                  <button className="hint-reveal-btn" onClick={onRevealWriteHint}>
                    💡 추가 힌트 보기
                    <span className="hint-cost">({writeHintLevel + 1}/{extraHints.length})</span>
                  </button>
                ) : (
                  <div className="hint-exhausted">힌트를 모두 봤어요!</div>
                )}
              </div>
            )}

            {/* 코드 입력 */}
            <div className="write-input-area">
              <textarea
                className={`write-textarea ${writeError ? 'write-error-input' : ''}`}
                value={writeInput}
                onChange={e => onWriteInputChange(e.target.value)}
                placeholder="코드를 직접 입력하세요..."
                onKeyDown={e => {
                  if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                    e.preventDefault()
                    onWriteSubmit()
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
              onClick={onWriteSubmit}
              disabled={!writeInput.trim()}
            >
              확인 →
            </button>

            {hasExplanation && (
              <div className="quiz-explanation">
                <span className="quiz-explanation-label">AI 설명</span>
                <p className="quiz-explanation-text">
                  {explanations[quizKey]}
                  {isExplaining[quizKey] && <span className="quiz-explanation-cursor" />}
                </p>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
