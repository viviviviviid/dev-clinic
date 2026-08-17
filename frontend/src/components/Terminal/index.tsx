import { useEffect, useRef } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import '@xterm/xterm/css/xterm.css'
import { useStore } from '../../store'
import { WS_BASE } from '../../lib/api'
import { supabase } from '../../lib/supabase'
import './Terminal.css'

interface Props {
  onClose: () => void
}

export default function TerminalPanel({ onClose }: Props) {
  const containerRef = useRef<HTMLDivElement>(null)
  const projectDir = useStore((state) => state.projectStatus?.dir ?? '')

  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    let disposed = false
    let socket: WebSocket | null = null

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
    term.open(container)
    fitAddon.fit()

    // 입력 → WebSocket
    const dataSubscription = term.onData((data) => {
      if (socket?.readyState === WebSocket.OPEN) {
        socket.send(data)
      }
    })

    // 리사이즈
    const resizeSubscription = term.onResize(({ cols, rows }) => {
      if (socket?.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ cols, rows }))
      }
    })

    void (async () => {
      try {
        const { data, error } = await supabase.auth.getSession()
        if (disposed) return
        const accessToken = data.session?.access_token
        if (error || !accessToken) {
          term.write('\r\n\x1b[31m[인증 오류] 다시 로그인해주세요.\x1b[0m\r\n')
          return
        }

        const wsUrl = `${WS_BASE}/ws/terminal?dir=${encodeURIComponent(projectDir)}`
        const ws = new WebSocket(wsUrl, ['coding-tutor', accessToken])
        ws.binaryType = 'arraybuffer'
        socket = ws

        ws.onopen = () => {
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
          if (!disposed) term.write('\r\n\x1b[90m[연결 종료]\x1b[0m\r\n')
        }

        ws.onerror = () => {
          if (!disposed) term.write('\r\n\x1b[31m[연결 오류]\x1b[0m\r\n')
        }
      } catch (error: unknown) {
        if (!disposed) {
          const message = error instanceof Error ? error.message : '알 수 없는 오류'
          term.write(`\r\n\x1b[31m[연결 오류] ${message}\x1b[0m\r\n`)
        }
      }
    })()

    // ResizeObserver로 컨테이너 크기 변화 감지
    const observer = new ResizeObserver(() => {
      fitAddon.fit()
    })
    observer.observe(container)

    return () => {
      disposed = true
      observer.disconnect()
      dataSubscription.dispose()
      resizeSubscription.dispose()
      socket?.close()
      term.dispose()
    }
  }, [projectDir])

  return (
    <div className="terminal-panel">
      <div className="terminal-header">
        <span className="terminal-title">
          터미널
          {projectDir && (
            <span className="terminal-dir"> — {projectDir}</span>
          )}
        </span>
        <button className="terminal-close" onClick={onClose} title="닫기">✕</button>
      </div>
      <div className="terminal-body" ref={containerRef} />
    </div>
  )
}
