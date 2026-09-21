import { useEffect, useRef, useState, useCallback, useMemo } from 'react'
import type { editor } from 'monaco-editor'
import { useStore } from '../../store'
import type { QuizData, QuizItem } from '../../store'
import { quizMarkers } from './quizMarkers'
import { createQuizAnswerRequest } from './quizAnswer'

interface QuizOverlayProps {
  editor: editor.IStandaloneCodeEditor
  filename: string
  content: string
  quizData: QuizData
  solvedHoles: Set<string>
  onFocusMarker: (markerType: string, markerIndex: number) => void
}

interface OpenZone {
  zone: editor.IViewZone
  zoneId: string
}

export default function QuizOverlay({ editor, filename, content, quizData, solvedHoles, onFocusMarker }: QuizOverlayProps) {
  const answerBusy = useStore(state => state.isChatStreaming || state.pendingChatAnswer !== null || state.workspaceMutationLocked)
  const [scrollTop, setScrollTop] = useState(editor.getScrollTop())
  const [, forceUpdate] = useState(0)
  const openZonesRef = useRef<Map<string, OpenZone>>(new Map())
  const [openWidgets, setOpenWidgets] = useState<Set<string>>(new Set())
  const [zoneTops, setZoneTops] = useState<Record<string, number>>({})
  const [hintLevels, setHintLevels] = useState<Record<string, number>>({})

  const quizLines = useMemo(() => quizMarkers(content, filename, quizData, solvedHoles), [content, filename, quizData, solvedHoles])

  useEffect(() => {
    const d1 = editor.onDidScrollChange(() => setScrollTop(editor.getScrollTop()))
    const d2 = editor.onDidLayoutChange(() => forceUpdate(n => n + 1))
    // File props render before Monaco switches models. Recalculate against the
    // new model so markers do not keep the previous file's line positions.
    const d3 = editor.onDidChangeModel(() => {
      setScrollTop(editor.getScrollTop())
      forceUpdate(n => n + 1)
    })
    return () => { d1.dispose(); d2.dispose(); d3.dispose() }
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
      // Mirror removal of external Monaco view zones so undo can reopen a marker.
      // eslint-disable-next-line react-hooks/set-state-in-effect
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
    if (zone.zone.heightInPx === capped) return
    zone.zone.heightInPx = capped
    editor.changeViewZones(accessor => accessor.layoutZone(zone.zoneId))
  }, [editor])

  function openHint(key: string, lineNumber: number) {
    if (openWidgets.has(key)) return
    const savedScroll = editor.getScrollTop()
    const dom = document.createElement('div')
    const zone: editor.IViewZone = {
      afterLineNumber: lineNumber,
      domNode: dom,
      heightInPx: 240,
      suppressMouseDown: true,
      onDomNodeTop: top => setZoneTops(previous => previous[key] === top ? previous : { ...previous, [key]: top }),
    }
    let newZoneId = ''
    editor.changeViewZones(accessor => {
      newZoneId = accessor.addZone(zone)
    })

    openZonesRef.current.set(key, { zone, zoneId: newZoneId })
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

  function generateAnswer(item: QuizItem, key: string) {
    const store = useStore.getState()
    try {
      // Read the live model so the answer sees edits made since the card opened.
      const request = createQuizAnswerRequest(filename, editor.getValue(), item)
      if (!store.requestChatAnswer(request)) {
        store.addToast('진행 중인 채팅이나 단계 변경이 끝나면 답지를 생성할 수 있습니다.', 'info')
        return
      }
      closeHint(key)
    } catch (error) {
      store.addToast(error instanceof Error ? error.message : '답지를 요청하지 못했습니다.', 'error')
    }
  }

  return (
    <div className="quiz-overlay">
      {quizLines.map(({ key, lineNumber, item }) => {
        const viewTop = editor.getTopForLineNumber(lineNumber) - scrollTop
        const isOpen = openWidgets.has(key)
        const isBug = item.markerType === 'bug'
        return (
          <button
            type="button"
            key={key}
            className={`quiz-glyph-btn${isBug ? ' bug' : ' hole'}${isOpen ? ' open' : ''}`}
            style={{ top: viewTop }}
            onClick={() => {
              if (isOpen) closeHint(key)
              else openHint(key, lineNumber)
            }}
            title={`${isBug ? '버그' : '빈칸'} 과제 ${isOpen ? '닫기' : '열기'}`}
            aria-label={`${isBug ? '버그' : '빈칸'} 과제 ${isOpen ? '닫기' : '열기'}`}
            aria-expanded={isOpen}
            aria-controls={`quiz-card-${key.replace(/[^a-zA-Z0-9_-]/g, '-')}`}
          />
        )
      })}

      {quizLines.map(({ key, item }) => {
        if (!openWidgets.has(key)) return null
        const top = zoneTops[key]
        if (top === undefined) return null
        const hints = item.hints ?? []
        return (
          <HintCard
            key={key}
            quizKey={key}
            item={item}
            top={top}
            hints={hints}
            hintLevel={hintLevels[key] ?? 0}
            answerBusy={answerBusy}
            onGenerateAnswer={() => generateAnswer(item, key)}
            onFocusMarker={() => {
              onFocusMarker(item.markerType || 'hole', item.markerIndex ?? 0)
              closeHint(key)
            }}
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
  answerBusy: boolean
  onGenerateAnswer: () => void
  onFocusMarker: () => void
  onRevealHint: () => void
  onClose: () => void
  onHeightChange: (h: number) => void
}

function HintCard({
  quizKey, item, top, hints, hintLevel,
  onFocusMarker, answerBusy, onGenerateAnswer,
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
  const cardId = `quiz-card-${quizKey.replace(/[^a-zA-Z0-9_-]/g, '-')}`

  return (
    <div
      ref={containerRef}
      id={cardId}
      className="quiz-content"
      style={{ top, zIndex: 10 }}
      role="region"
      aria-label={`${isBug ? '버그' : '빈칸'} 과제`}
    >
      <div className={`quiz-content-header ${isBug ? 'bug' : 'hole'}`}>
        <span className="quiz-content-title">{isBug ? '🐛 BUG' : '📝 HOLE'}</span>
        <button type="button" className="quiz-content-close" onClick={onClose} aria-label="과제 닫기">✕</button>
      </div>

      <div className="quiz-body">
        <div className="quiz-question">{item.question}</div>

        <div className="write-mode">
          <button
            type="button"
            className="write-submit-btn"
            onClick={onFocusMarker}
          >
            에디터에서 직접 수정 →
          </button>
          <button
            type="button"
            className="write-submit-btn quiz-answer-btn"
            onClick={onGenerateAnswer}
            disabled={answerBusy}
            title={answerBusy ? '진행 중인 채팅이나 단계 변경이 끝나면 사용할 수 있습니다.' : '이 문제의 답안과 해설을 채팅에서 확인합니다.'}
          >
            AI 답지 생성 → 채팅
          </button>
        </div>

        <p className="quiz-verification-note" id={`${cardId}-verification-note`}>
          함수 틀은 그대로 두고 선택된 본문만 에디터에서 바꾸세요. ⌘Z/Ctrl+Z로 되돌린 뒤 전체 테스트로 확인할 수 있습니다.
        </p>

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
