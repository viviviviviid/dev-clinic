import { useEffect, useRef } from 'react'
import { useStore } from '../store'
import { WS_BASE } from '../lib/api'

interface WSMessage {
  type: string
  content?: string
  last_sync?: string
  changed?: boolean
  error?: string
  passed?: boolean
  summary?: string
}

export function useWebSocket() {
  const ws = useRef<WebSocket | null>(null)
  const { startFeedback, addFeedbackChunk, endFeedback, setLastSync, setStepComplete, setTestResult, setWsStatus, addToast } = useStore()

  useEffect(() => {
    let destroyed = false
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null

    function connect() {
      if (destroyed) return
      setWsStatus('reconnecting')
      const url = `${WS_BASE}/ws`
      const sock = new WebSocket(url)
      ws.current = sock

      sock.onopen = () => {
        setWsStatus('connected')
      }

      sock.onmessage = (evt) => {
        try {
          const msg: WSMessage = JSON.parse(evt.data)
          switch (msg.type) {
            case 'feedback_start':
              startFeedback()
              break
            case 'feedback_chunk':
              if (msg.content) {
                addFeedbackChunk(msg.content)
                if (msg.content.includes('[STEP_COMPLETE]')) {
                  setStepComplete(true)
                }
              }
              break
            case 'feedback_end':
              endFeedback()
              break
            case 'sync_status':
              if (msg.last_sync) setLastSync(msg.last_sync)
              break
            case 'test_result':
              setTestResult({ passed: msg.passed ?? false, summary: msg.summary ?? '' })
              break
            case 'error':
              addToast(msg.error || 'WebSocket 오류', 'error')
              break
          }
        } catch (e) {
          console.error('WS parse error:', e)
        }
      }

      sock.onclose = () => {
        if (destroyed) return
        setWsStatus('reconnecting')
        reconnectTimer = setTimeout(connect, 2000)
      }

      sock.onerror = () => {
        setWsStatus('reconnecting')
      }
    }

    connect()
    return () => {
      destroyed = true
      if (reconnectTimer) clearTimeout(reconnectTimer)
      ws.current?.close()
      setWsStatus('disconnected')
    }
  }, [])
}
