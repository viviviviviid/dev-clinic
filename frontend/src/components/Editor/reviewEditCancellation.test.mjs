import assert from 'node:assert/strict'
import test from 'node:test'

import { shouldCancelReviewOnEdit } from './reviewEditCancellation.ts'

test('only the first edit of an active review requests cancellation', () => {
  assert.equal(shouldCancelReviewOnEdit('reviewing', false), true)
  assert.equal(shouldCancelReviewOnEdit('requesting', false), true)
  assert.equal(shouldCancelReviewOnEdit('reviewing', true), false)
  assert.equal(shouldCancelReviewOnEdit('ready', false), false)
  assert.equal(shouldCancelReviewOnEdit('idle', false), false)
})
