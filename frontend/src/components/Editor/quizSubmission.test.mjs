import assert from 'node:assert/strict'
import test from 'node:test'

import { applyQuizCandidate, persistQuizCandidate } from './quizSubmission.ts'

test('does not call apply for empty quiz input', async () => {
  let called = false
  const result = await applyQuizCandidate('   ', async () => {
    called = true
    return true
  })

  assert.equal(called, false)
  assert.equal(result.applied, false)
  assert.match(result.error, /입력/)
})

test('preserves a retryable error when editor application fails', async () => {
  const rejected = await applyQuizCandidate('answer()', async () => false)
  assert.equal(rejected.applied, false)
  assert.match(rejected.error, /다시 시도/)

  const failed = await applyQuizCandidate('answer()', async () => {
    throw new Error('save failed')
  })
  assert.equal(failed.applied, false)
  assert.match(failed.error, /보존/)
})

test('reports success only after editor application succeeds', async () => {
  assert.deepEqual(
    await applyQuizCandidate('answer()', async () => true),
    { applied: true, error: '' },
  )
})

test('commits quiz content only after persistence succeeds', async () => {
  let committed = false
  assert.equal(await persistQuizCandidate(async () => false, () => { committed = true }), false)
  assert.equal(committed, false)

  assert.equal(await persistQuizCandidate(async () => true, () => { committed = true }), true)
  assert.equal(committed, true)
})

test('does not commit a quiz candidate when the source changes while saving', async () => {
  let committed = false
  let unchanged = true
  const applied = await persistQuizCandidate(
    async () => {
      unchanged = false
      return true
    },
    () => { committed = true },
    () => unchanged,
  )

  assert.equal(applied, false)
  assert.equal(committed, false)
})
