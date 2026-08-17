import assert from 'node:assert/strict'
import test from 'node:test'

import {
  applyReviewCancellation,
  applySyncStatus,
  shouldApplyReviewEvent,
  shouldApplyWebSocketMessage,
} from './webSocketProject.ts'

test('rejects messages after the workspace changes underneath a socket', () => {
  assert.equal(shouldApplyWebSocketMessage(undefined, '/projects/old', '/projects/new'), false)
})

test('rejects queued frames immediately after a same-directory watcher restart', () => {
  assert.equal(
    shouldApplyWebSocketMessage('/projects/current', '/projects/current', '/projects/current', 4, 5),
    false,
  )
})

test('accepts unscoped legacy messages only on the socket current workspace', () => {
  assert.equal(shouldApplyWebSocketMessage(undefined, '/projects/current', '/projects/current'), true)
})

test('rejects explicitly scoped messages for another project', () => {
  assert.equal(
    shouldApplyWebSocketMessage('/projects/old', '/projects/current', '/projects/current'),
    false,
  )
  assert.equal(
    shouldApplyWebSocketMessage('/projects/current', '/projects/current', '/projects/current'),
    true,
  )
})

test('review events accept a new revision and reject late stream events', () => {
  assert.equal(shouldApplyReviewEvent('review_ready', 8, 7, 7), true)
  assert.equal(shouldApplyReviewEvent('review_started', 8, 7, 7), true)
  assert.equal(shouldApplyReviewEvent('feedback_chunk', 6, 7, 7), false)
  assert.equal(shouldApplyReviewEvent('feedback_end', 7, 7, 7), true)
  assert.equal(shouldApplyReviewEvent('review_cancelled', 7, 8, null), false)
})

test('legacy review events remain compatible when no revision is supplied', () => {
  assert.equal(shouldApplyReviewEvent('feedback_chunk', undefined, 4, 4), true)
})

test('review request ids reject late chunks and completed requests cannot restart', () => {
  assert.equal(
    shouldApplyReviewEvent('feedback_chunk', 4, 4, 4, 'request-old', 'request-new', 'reviewing'),
    false,
  )
  assert.equal(
    shouldApplyReviewEvent('review_started', 4, 4, null, 'request-done', 'request-done', 'ready'),
    false,
  )
  assert.equal(
    shouldApplyReviewEvent('review_started', 4, 4, 4, 'request-current', 'request-current', 'requesting'),
    true,
  )
})

test('authoritative cancellation restores idle or ready while source changes only stop the old run', () => {
  const calls = []
  const target = {
    cancelFeedback: (revision) => calls.push(['cancel', revision]),
    setReviewIdle: (revision, hash, files) => calls.push(['idle', revision, hash, files]),
    markReviewReady: (revision, hash, files, testStatus) => calls.push(['ready', revision, hash, files, testStatus]),
  }

  applyReviewCancellation(target, {
    reason: 'reverted', status: 'idle', revision: 9, semantic_hash: 'hash-9', files: [],
  }, 8, 'not_run')
  applyReviewCancellation(target, {
    reason: 'user', status: 'ready', revision: 10, semantic_hash: 'hash-10', files: ['main.ts'],
  }, 9, 'failed')
  applyReviewCancellation(target, {
    reason: 'source_changed', status: 'ready', revision: 10,
  }, 11, 'not_run')
  applyReviewCancellation(target, {
    reason: 'tests_changed', status: 'ready', revision: 12, semantic_hash: 'hash-12', files: ['main.ts'],
  }, 11, 'not_run')

  assert.deepEqual(calls, [
    ['idle', 9, 'hash-9', []],
    ['ready', 10, 'hash-10', ['main.ts'], 'failed'],
    ['cancel', 10],
    ['ready', 12, 'hash-12', ['main.ts'], 'not_run'],
  ])
})

test('sync acknowledgement unlocks navigation but only semantic changes invalidate completion', () => {
  const calls = []
  const target = {
    setLastSync: (value) => calls.push(['lastSync', value]),
    setPendingSemanticSync: (value) => calls.push(['pending', value]),
    setStepComplete: (value) => calls.push(['complete', value]),
    setTestResult: (value) => calls.push(['test', value]),
    setReviewTestStatus: (value) => calls.push(['reviewTest', value]),
  }

  applySyncStatus(target, { last_sync: 'now', changed: false })
  assert.deepEqual(calls, [['lastSync', 'now'], ['pending', false]])

  calls.length = 0
  applySyncStatus(target, { changed: true })
  assert.deepEqual(calls, [
    ['pending', false],
    ['complete', false],
    ['test', null],
    ['reviewTest', 'not_run'],
  ])
})
