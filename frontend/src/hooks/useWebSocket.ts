import { useEffect, useRef } from 'react'
import { useStore } from '../store'
import { apiJson, WS_BASE } from '../lib/api'
import { supabase } from '../lib/supabase'
import {
  applyReviewSnapshot,
  currentReviewSnapshot,
  type ReviewSnapshot,
} from './useProject'
import { reviewSessionDecision } from './reviewSnapshot'
import {
  applyReviewCancellation,
  applySyncStatus,
  shouldApplyReviewEvent,
  shouldApplyWebSocketMessage,
} from './webSocketProject'

interface WSMessage {
  type: string
  content?: string
  last_sync?: string
  changed?: boolean
  error?: string
  passed?: boolean
  summary?: string
  test_scope?: 'full' | 'targeted'
  test_input_hash?: string
  project_dir?: string
  revision?: number
  semantic_hash?: string
  files?: string[]
  reason?: string
  status?: 'idle' | 'ready' | 'reviewing'
  session_id?: number
  request_id?: string
}

const REVIEW_EVENT_TYPES = new Set([
  'review_ready',
  'review_started',
  'review_cancelled',
  'review_end',
  'review_error',
  'feedback_start',
  'feedback_chunk',
  'feedback_end',
])

export function useWebSocket(enabled = true, retryKey = 0) {
  const ws = useRef<WebSocket | null>(null)
  const projectDir = useStore((state) => state.projectStatus?.dir ?? state.projectDir)
  const projectSessionEpoch = useStore((state) => state.projectSessionEpoch)

  useEffect(() => {
    if (!enabled) {
      useStore.getState().setWsStatus('disconnected')
      return
    }

    let destroyed = false
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null
    const connectionProjectDir = projectDir
    const connectionSessionEpoch = projectSessionEpoch

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
        if (currentProjectDir !== connectionProjectDir ||
            useStore.getState().projectSessionEpoch !== connectionSessionEpoch) {
          sock.close()
          return
        }
        useStore.getState().setWsStatus('connected')
        const expected = currentReviewSnapshot()
        void apiJson<ReviewSnapshot>('/api/review/status?refresh=1').then((snapshot) => {
          if (destroyed || ws.current !== sock ||
              useStore.getState().projectSessionEpoch !== connectionSessionEpoch) return
          applyReviewSnapshot(snapshot, 'status', expected)
        }).catch(() => {
          // Lobby connections have no active project; the next project-scoped
          // socket reconnect will reconcile review state and missed feedback.
        })
      }

      sock.onmessage = (evt) => {
        if (destroyed || ws.current !== sock) return
        try {
          const msg: WSMessage = JSON.parse(evt.data)
          let store = useStore.getState()
          const currentProjectDir = store.projectStatus?.dir ?? store.projectDir
          if (!shouldApplyWebSocketMessage(
            msg.project_dir,
            connectionProjectDir,
            currentProjectDir,
            connectionSessionEpoch,
            store.projectSessionEpoch,
          )) {
            return
          }
          if (msg.project_dir) {
            const sessionDecision = reviewSessionDecision(msg.session_id, store.reviewServerSessionID)
            if (sessionDecision === 'reject') return
            if (sessionDecision === 'adopt' && msg.session_id !== undefined) {
              store.adoptReviewServerSession(msg.session_id)
              store = useStore.getState()
            }
          }
          if (REVIEW_EVENT_TYPES.has(msg.type) && !shouldApplyReviewEvent(
            msg.type,
            msg.revision,
            store.reviewRevision,
            store.activeReviewRevision,
            msg.request_id,
            store.reviewRequestID,
            store.reviewStatus,
          )) return

          const reviewTestStatus = store.testResult
            ? store.testResult.passed ? 'passed' as const : 'failed' as const
            : 'not_run' as const
          switch (msg.type) {
            case 'review_ready':
              store.markReviewReady(
                msg.revision ?? store.reviewRevision,
                msg.semantic_hash,
                msg.files,
                reviewTestStatus,
                msg.request_id,
              )
              break
            case 'review_started':
              store.startFeedback(
                msg.revision,
                msg.semantic_hash,
                msg.files,
                reviewTestStatus,
                msg.request_id,
              )
              break
            case 'review_cancelled':
              applyReviewCancellation(store, msg, store.reviewRevision, reviewTestStatus)
              break
            case 'review_end':
              store.endFeedback(msg.revision)
              break
            case 'review_error':
              store.failFeedback(msg.error || msg.reason || 'AI 검토 중 오류가 발생했습니다.', msg.revision)
              break
            case 'feedback_start':
              if (!store.isStreaming || store.activeReviewRevision !== msg.revision) {
                store.startFeedback(msg.revision, msg.semantic_hash, msg.files, reviewTestStatus, msg.request_id)
              }
              break
            case 'feedback_chunk':
              if (msg.content) {
                store.addFeedbackChunk(msg.content, msg.revision)
              }
              break
            case 'feedback_end':
              store.endFeedback(msg.revision)
              break
            case 'sync_status':
              applySyncStatus(store, msg)
              break
            case 'test_result': {
              const passed = msg.passed === true
              store.setTestResult({
                passed,
                summary: msg.summary ?? '',
                scope: msg.test_scope,
                inputHash: msg.test_input_hash,
              })
              store.setReviewTestStatus(passed ? 'passed' : 'failed')
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
  }, [enabled, projectDir, projectSessionEpoch, retryKey])
}
