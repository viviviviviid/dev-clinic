import assert from 'node:assert/strict'
import test from 'node:test'

import { reviewSessionDecision, shouldApplyReviewSnapshot } from './reviewSnapshot.ts'

const idle = {
  status: 'idle', revision: 4, activeRevision: null, semanticHash: 'hash-4',
  serverSessionID: 8, requestID: null,
}

test('status response applies only while the client state is unchanged', () => {
  const snapshot = { status: 'ready', revision: 5, semantic_hash: 'hash-5' }
  assert.equal(shouldApplyReviewSnapshot(snapshot, idle, 'status', idle), true)
  assert.equal(shouldApplyReviewSnapshot(snapshot, { ...idle, status: 'reviewing', activeRevision: 4 }, 'status', idle), false)
})

test('server sessions adopt new epochs and reject delayed frames from old epochs', () => {
  assert.equal(reviewSessionDecision(8, 8), 'accept')
  assert.equal(reviewSessionDecision(9, 8), 'adopt')
  assert.equal(reviewSessionDecision(7, 8), 'reject')
  assert.equal(reviewSessionDecision(1, 8, true), 'adopt')
  assert.equal(reviewSessionDecision(undefined, 8), 'reject')
})

test('late reviewing response cannot resurrect a completed or failed run', () => {
  const snapshot = { status: 'reviewing', revision: 5, semantic_hash: 'hash-5' }
  assert.equal(shouldApplyReviewSnapshot(snapshot, {
    status: 'requesting', revision: 4, activeRevision: 4, semanticHash: 'hash-4', serverSessionID: 8, requestID: 'req',
  }, 'start'), true)
  assert.equal(shouldApplyReviewSnapshot(snapshot, {
    status: 'idle', revision: 5, activeRevision: null, semanticHash: 'hash-5', serverSessionID: 8, requestID: null,
  }, 'start'), false)
  assert.equal(shouldApplyReviewSnapshot(snapshot, {
    status: 'error', revision: 5, activeRevision: null, semanticHash: 'hash-5', serverSessionID: 8, requestID: null,
  }, 'start'), false)
})

test('authoritative recovery applies but an older revision never does', () => {
  assert.equal(shouldApplyReviewSnapshot(
    { status: 'ready', revision: 4, semantic_hash: 'hash-4' },
    { status: 'requesting', revision: 4, activeRevision: 4, semanticHash: 'hash-4', serverSessionID: 8, requestID: 'req' },
    'authoritative',
  ), true)
  assert.equal(shouldApplyReviewSnapshot(
    { status: 'ready', revision: 3, semantic_hash: 'hash-3' },
    idle,
    'authoritative',
  ), false)
})
