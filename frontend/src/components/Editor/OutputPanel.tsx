import { useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'

export default function OutputPanel({ children }: { children: ReactNode }) {
  const panelRef = useRef<HTMLDivElement>(null)
  const dragRef = useRef<{ y: number; height: number } | null>(null)
  const [height, setHeight] = useState(180)
  const [maxHeight, setMaxHeight] = useState(600)
  const clamp = (value: number) => Math.max(80, Math.min(maxHeight, value))
  const visibleHeight = clamp(height)

  useEffect(() => {
    const container = panelRef.current?.parentElement
    if (!container) return
    const observer = new ResizeObserver(() => {
      // Leave room for the toolbar and an editable portion of the source.
      setMaxHeight(Math.max(80, container.clientHeight - 160))
    })
    observer.observe(container)
    return () => observer.disconnect()
  }, [])

  return (
    <div ref={panelRef} className="run-output-panel" style={{ height: visibleHeight }}>
      <div
        className="run-output-resizer"
        role="separator"
        aria-label="실행 및 테스트 결과 창 높이 조절"
        aria-orientation="horizontal"
        aria-valuemin={80}
        aria-valuemax={maxHeight}
        aria-valuenow={visibleHeight}
        tabIndex={0}
        onPointerDown={event => {
          if (event.button !== 0) return
          event.preventDefault()
          dragRef.current = { y: event.clientY, height: visibleHeight }
          event.currentTarget.setPointerCapture(event.pointerId)
        }}
        onPointerMove={event => {
          const drag = dragRef.current
          if (drag) setHeight(clamp(drag.height + drag.y - event.clientY))
        }}
        onPointerUp={event => {
          dragRef.current = null
          if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId)
        }}
        onLostPointerCapture={() => { dragRef.current = null }}
        onKeyDown={event => {
          if (event.key !== 'ArrowUp' && event.key !== 'ArrowDown') return
          event.preventDefault()
          const step = event.shiftKey ? 40 : 10
          setHeight(clamp(visibleHeight + (event.key === 'ArrowUp' ? step : -step)))
        }}
      ><span /></div>
      {children}
    </div>
  )
}
