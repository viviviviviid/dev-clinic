import { useEffect, useRef } from 'react'
import { useStore } from '../store'

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
  const { startFeedback, addFeedbackChunk, endFeedback, setLastSync, setStepComplete, setTestResult } = useStore()

  useEffect(() => {
    let destroyed = false
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null

    function connect() {
      if (destroyed) return
      const url = `ws://${window.location.host}/ws`
      const sock = new WebSocket(url)
      ws.current = sock

      sock.onopen = () => {
        console.log('WebSocket connected')
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
              console.error('WS error from server:', msg.error)
              break
          }
        } catch (e) {
          console.error('WS parse error:', e)
        }
      }

      sock.onclose = () => {
        if (destroyed) return
        console.log('WebSocket closed, reconnecting in 2s')
        reconnectTimer = setTimeout(connect, 2000)
      }

      sock.onerror = (err) => {
        console.error('WebSocket error:', err)
      }
    }

    connect()
    return () => {
      destroyed = true
      if (reconnectTimer) clearTimeout(reconnectTimer)
      ws.current?.close()
    }
  }, [])
}
