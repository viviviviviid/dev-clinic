import assert from 'node:assert/strict'
import test from 'node:test'

import { createTestRunRequest } from './testRun.ts'

test('full suite calls /api/test without a func selector', () => {
  assert.deepEqual(createTestRunRequest(), {
    endpoint: '/api/test',
    title: '전체 테스트 결과',
  })
  assert.equal(createTestRunRequest('').endpoint.includes('func='), false)
})

test('CodeLens test requests stay explicitly targeted', () => {
  assert.deepEqual(createTestRunRequest('handles spaces / symbols'), {
    endpoint: '/api/test?func=handles%20spaces%20%2F%20symbols',
    title: '테스트: handles spaces / symbols',
  })
})
