import assert from 'node:assert/strict'
import test from 'node:test'

import { shouldApplyWebSocketMessage } from './webSocketProject.ts'

test('rejects messages after the workspace changes underneath a socket', () => {
  assert.equal(shouldApplyWebSocketMessage(undefined, '/projects/old', '/projects/new'), false)
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
