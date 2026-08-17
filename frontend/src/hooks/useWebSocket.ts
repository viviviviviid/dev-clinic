import { useEffect, useRef } from 'react'
import { useStore } from '../store'
import { WS_BASE } from '../lib/api'
import { supabase } from '../lib/supabase'
import { shouldApplyWebSocketMessage } from './webSocketProject'

interface WSMessage {
  type: string
  content?: string
  last_sync?: string
  changed?: boolean
  error?: string
  passed?: boolean
  summary?: string
  project_dir?: string
}

export function useWebSocket(enabled = true, retryKey = 0) {
  const ws = useRef<WebSocket | null>(null)
  const projectDir = useStore((state) => state.projectStatus?.dir ?? state.projectDir)

  useEffect(() => {
    if (!enabled) {
      useStore.getState().setWsStatus('disconnected')
      return
    }

    let destroyed = false
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null
    const connectionProjectDir = projectDir

    function scheduleReconnect() {
      if (destroyed) return
      if (reconnectTimer) clearTimeout(reconnectTimer)
      reconnectTimer = setTimeout(() => void connect(), 2000)
    }

    async function connect() {
      if (destroyed) return
      useStore.getState().setWsStatus('reconnecting')

      let accessToken: string | undefined
      try {
        const { data, error } = await supabase.auth.getSession()
        if (!error) accessToken = data.session?.access_token
      } catch {
        // A failed session lookup is retried with the connection.
      }
      if (destroyed) return
      if (!accessToken) {
        useStore.getState().setWsStatus('disconnected')
        scheduleReconnect()
        return
      }

      let sock: WebSocket
      try {
        sock = new WebSocket(`${WS_BASE}/ws`, ['coding-tutor', accessToken])
      } catch {
        useStore.getState().setWsStatus('disconnected')
        scheduleReconnect()
        return
      }
      ws.current = sock

      sock.onopen = () => {
        if (destroyed || ws.current !== sock) {
          sock.close()
          return
        }
        const currentProjectDir = useStore.getState().projectStatus?.dir ?? useStore.getState().projectDir
        if (currentProjectDir !== connectionProjectDir) {
          sock.close()
          return
        }
        useStore.getState().setWsStatus('connected')
      }

      sock.onmessage = (evt) => {
        if (destroyed || ws.current !== sock) return
        try {
          const msg: WSMessage = JSON.parse(evt.data)
          const store = useStore.getState()
          const currentProjectDir = store.projectStatus?.dir ?? store.projectDir
          if (!shouldApplyWebSocketMessage(msg.project_dir, connectionProjectDir, currentProjectDir)) {
            return
          }
          switch (msg.type) {
            case 'feedback_start':
              store.startFeedback()
              break
            case 'feedback_chunk':
              if (msg.content) {
                store.addFeedbackChunk(msg.content)
              }
              break
            case 'feedback_end':
              store.endFeedback()
              break
            case 'sync_status':
              if (msg.last_sync) store.setLastSync(msg.last_sync)
              if (msg.changed) {
                store.setStepComplete(false)
                store.setTestResult(null)
              }
              break
            case 'test_result': {
              const passed = msg.passed === true
              store.setTestResult({ passed, summary: msg.summary ?? '' })
              if (!passed) store.setStepComplete(false)
              break
            }
            case 'step_complete': {
              const complete = msg.passed === true
              store.setStepComplete(complete)
              if (!complete && store.testResult?.passed && store.testResult.summary) {
                store.addToast(store.testResult.summary, 'info')
              }
              break
            }
            case 'error':
              store.addToast(msg.error || 'WebSocket 오류', 'error')
              break
          }
        } catch (e) {
          console.error('WS parse error:', e)
        }
      }

      sock.onclose = () => {
        if (destroyed) return
        if (ws.current === sock) ws.current = null
        useStore.getState().setWsStatus('reconnecting')
        scheduleReconnect()
      }

      sock.onerror = () => {
        if (destroyed || ws.current !== sock) return
        useStore.getState().setWsStatus('reconnecting')
      }
    }

    void connect()
    return () => {
      destroyed = true
      if (reconnectTimer) clearTimeout(reconnectTimer)
      ws.current?.close()
      ws.current = null
      useStore.getState().setWsStatus('disconnected')
    }
  }, [enabled, projectDir, retryKey])
}
