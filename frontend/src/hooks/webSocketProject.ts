export function shouldApplyWebSocketMessage(
  messageProjectDir: string | undefined,
  connectionProjectDir: string,
  currentProjectDir: string,
  connectionSessionEpoch = 0,
  currentSessionEpoch = connectionSessionEpoch,
): boolean {
  if (connectionSessionEpoch !== currentSessionEpoch) return false
  if (connectionProjectDir !== currentProjectDir) return false
  return messageProjectDir === undefined || messageProjectDir === currentProjectDir
}

const REVIEW_OPEN_EVENTS = new Set(['review_ready', 'review_started', 'feedback_start'])

/**
 * Review streams are revision-scoped. A late chunk/end from a cancelled review
 * must never mutate the newest ready revision.
 */
export function shouldApplyReviewEvent(
  type: string,
  messageRevision: number | undefined,
  latestRevision: number,
  activeRevision: number | null,
  messageRequestID?: string,
  currentRequestID?: string | null,
  currentStatus?: string,
): boolean {
  if (messageRequestID && currentRequestID) {
    if ((type === 'review_started' || type === 'feedback_start') &&
        currentStatus !== 'requesting' && currentStatus !== 'reviewing' &&
        messageRequestID === currentRequestID) {
      return false
    }
    if ((currentStatus === 'requesting' || currentStatus === 'reviewing') &&
        messageRequestID !== currentRequestID) {
      return false
    }
    if (!REVIEW_OPEN_EVENTS.has(type) && messageRequestID !== currentRequestID) {
      return false
    }
  }
  if (messageRevision === undefined) return true
  if (REVIEW_OPEN_EVENTS.has(type)) return messageRevision >= latestRevision
  if (activeRevision !== null) return messageRevision === activeRevision
  return messageRevision >= latestRevision
}

type ReviewStatusMessage = {
  reason?: string
  status?: 'idle' | 'ready' | 'reviewing'
  revision?: number
  semantic_hash?: string
  files?: string[]
  request_id?: string
}

interface ReviewCancellationTarget {
  cancelFeedback: (revision?: number) => void
  setReviewIdle: (revision?: number, semanticHash?: string, files?: string[], requestID?: string) => void
  markReviewReady: (
    revision: number,
    semanticHash?: string,
    files?: string[],
    testStatus?: 'not_run' | 'passed' | 'failed',
    requestID?: string,
  ) => void
}

/**
 * A user cancellation and a source revert both carry the authoritative server
 * state. Source-change cancellation is different: its metadata belongs to the
 * old run and is immediately followed by a new review_ready event.
 */
export function applyReviewCancellation(
  target: ReviewCancellationTarget,
  message: ReviewStatusMessage,
  fallbackRevision: number,
  testStatus: 'not_run' | 'passed' | 'failed',
) {
  const authoritative = message.reason === 'user' ||
    message.reason === 'reverted' ||
    message.reason === 'tests_changed'
  if (authoritative && message.status === 'idle') {
    target.setReviewIdle(
      message.revision ?? fallbackRevision,
      message.semantic_hash,
      message.files ?? [],
      message.request_id,
    )
    return
  }
  if (authoritative && message.status === 'ready') {
    target.markReviewReady(
      message.revision ?? fallbackRevision,
      message.semantic_hash,
      message.files,
      testStatus,
      message.request_id,
    )
    return
  }
  target.cancelFeedback(message.revision)
}

interface SyncStatusTarget {
  setLastSync: (time: string) => void
  setPendingSemanticSync: (pending: boolean) => void
  setStepComplete: (complete: boolean) => void
  setTestResult: (result: null) => void
  setReviewTestStatus: (status: 'not_run') => void
}

export function applySyncStatus(
  target: SyncStatusTarget,
  message: { last_sync?: string; changed?: boolean },
) {
  if (message.last_sync) target.setLastSync(message.last_sync)
  target.setPendingSemanticSync(false)
  if (!message.changed) return
  target.setStepComplete(false)
  target.setTestResult(null)
  target.setReviewTestStatus('not_run')
}
