import assert from 'node:assert/strict'
import test from 'node:test'

import { advanceButtonLabel } from './advanceUx.ts'

test('labels the final transition as mission completion', () => {
  assert.equal(advanceButtonLabel({
    pendingSemanticSync: false,
    advancing: false,
    recovering: false,
    finalStep: true,
  }), '미션 완료하기 →')
  assert.equal(advanceButtonLabel({
    pendingSemanticSync: false,
    advancing: true,
    recovering: false,
    finalStep: true,
  }), '완료 처리 중…')
})

test('describes long-running next-step generation explicitly', () => {
  assert.equal(advanceButtonLabel({
    pendingSemanticSync: false,
    advancing: true,
    recovering: false,
    finalStep: false,
  }), '다음 단계 생성 중…')
})
