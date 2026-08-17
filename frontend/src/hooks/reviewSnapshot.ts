export type ServerReviewStatus = 'idle' | 'ready' | 'reviewing'
export type ClientReviewStatus = ServerReviewStatus | 'requesting' | 'canceled' | 'error'
export type ReviewSnapshotMode = 'status' | 'start' | 'authoritative'

export interface ServerReviewSnapshot {
  status: ServerReviewStatus
  revision: number
  semantic_hash: string
  session_id?: number
  request_id?: string
}

export interface ClientReviewSnapshot {
  status: ClientReviewStatus
  revision: number
  activeRevision: number | null
  semanticHash: string
  serverSessionID: number | null
  requestID: string | null
}

export function sameClientReviewSnapshot(
  left: ClientReviewSnapshot,
  right: ClientReviewSnapshot,
): boolean {
  return left.status === right.status &&
    left.revision === right.revision &&
    left.activeRevision === right.activeRevision &&
    left.semanticHash === right.semanticHash &&
    left.serverSessionID === right.serverSessionID &&
    left.requestID === right.requestID
}

export type ReviewSessionDecision = 'accept' | 'adopt' | 'reject'

export function reviewSessionDecision(
  incoming: number | undefined,
  current: number | null,
  authoritative = false,
): ReviewSessionDecision {
  if (incoming === undefined || !Number.isSafeInteger(incoming) || incoming <= 0) {
    return current === null ? 'accept' : 'reject'
  }
  if (current === null) return 'adopt'
  if (incoming === current) return 'accept'
  if (authoritative || incoming > current) return 'adopt'
  return 'reject'
}

/**
 * HTTP review responses race with WebSocket completion/error events. Status
 * reads apply only if local state did not move, and a start response applies
 * only while that start is still pending. Cancel/error recovery is explicitly
 * authoritative, while older revisions are rejected in every mode.
 */
export function shouldApplyReviewSnapshot(
  snapshot: ServerReviewSnapshot,
  current: ClientReviewSnapshot,
  mode: ReviewSnapshotMode,
  expected?: ClientReviewSnapshot,
): boolean {
  if (snapshot.revision < current.revision) return false
  if (mode === 'status') {
    return expected !== undefined && sameClientReviewSnapshot(current, expected)
  }
  if (mode === 'start' && snapshot.status === 'reviewing') {
    if (current.status === 'requesting') return snapshot.revision >= current.revision
    return current.status === 'reviewing' && current.activeRevision === snapshot.revision
  }
  return mode === 'authoritative'
}
