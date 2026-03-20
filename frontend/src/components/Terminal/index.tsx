import { useEffect, useRef } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import '@xterm/xterm/css/xterm.css'
import { useStore } from '../../store'
import { WS_BASE } from '../../lib/api'
import './Terminal.css'

interface Props {
  onClose: () => void
}

export default function TerminalPanel({ onClose }: Props) {
  const containerRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const { projectStatus } = useStore()

  useEffect(() => {
    if (!containerRef.current) return

    const term = new Terminal({
      theme: {
        background: '#0d1117',
        foreground: '#e6edf3',
        cursor: '#58a6ff',
        black: '#0d1117',
        brightBlack: '#484f58',
        red: '#f85149',
        brightRed: '#f85149',
        green: '#3fb950',
        brightGreen: '#3fb950',
        yellow: '#d29922',
        brightYellow: '#e3b341',
        blue: '#58a6ff',
        brightBlue: '#79c0ff',
        magenta: '#bc8cff',
        brightMagenta: '#d2a8ff',
        cyan: '#76e3ea',
        brightCyan: '#b3f0ff',
        white: '#b1bac4',
        brightWhite: '#f0f6fc',
      },
      fontFamily: "'Consolas', 'Courier New', monospace",
      fontSize: 13,
      lineHeight: 1.4,
      cursorBlink: true,
      allowProposedApi: true,
    })

    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.loadAddon(new WebLinksAddon())
    term.open(containerRef.current)
    fitAddon.fit()

    termRef.current = term
    fitRef.current = fitAddon

    // WebSocket 연결
    const dir = projectStatus?.dir ?? ''
    const wsUrl = `${WS_BASE}/ws/terminal?dir=${encodeURIComponent(dir)}`
    const ws = new WebSocket(wsUrl)
    ws.binaryType = 'arraybuffer'
    wsRef.current = ws

    ws.onopen = () => {
      // 초기 크기 전송
      const { cols, rows } = term
      ws.send(JSON.stringify({ cols, rows }))
    }

    ws.onmessage = (evt) => {
      if (evt.data instanceof ArrayBuffer) {
        term.write(new Uint8Array(evt.data))
      } else {
        term.write(evt.data)
      }
    }

    ws.onclose = () => {
      term.write('\r\n\x1b[90m[연결 종료]\x1b[0m\r\n')
    }

    ws.onerror = () => {
      term.write('\r\n\x1b[31m[연결 오류]\x1b[0m\r\n')
    }

    // 입력 → WebSocket
    term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(data)
      }
    })

    // 리사이즈
    term.onResize(({ cols, rows }) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ cols, rows }))
      }
    })

    // ResizeObserver로 컨테이너 크기 변화 감지
    const observer = new ResizeObserver(() => {
      fitAddon.fit()
    })
    observer.observe(containerRef.current)

    return () => {
      observer.disconnect()
      ws.close()
      term.dispose()
    }
  }, [])

  return (
    <div className="terminal-panel">
      <div className="terminal-header">
        <span className="terminal-title">
          터미널
          {projectStatus?.dir && (
            <span className="terminal-dir"> — {projectStatus.dir}</span>
          )}
        </span>
        <button className="terminal-close" onClick={onClose} title="닫기">✕</button>
      </div>
      <div className="terminal-body" ref={containerRef} />
    </div>
  )
}
