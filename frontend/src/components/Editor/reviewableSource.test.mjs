import assert from 'node:assert/strict'
import test from 'node:test'

import { isReviewableSourcePath } from './reviewableSource.ts'

test('matches every source extension watched by clinic', () => {
  for (const extension of ['go', 'ts', 'tsx', 'mts', 'cts', 'js', 'jsx', 'mjs', 'cjs', 'rs', 'sol', 'py']) {
    assert.equal(isReviewableSourcePath(`/project/src/example.${extension}`), true)
  }
  assert.equal(isReviewableSourcePath('/project/src/EXAMPLE.TSX'), true)
})

test('does not wait for semantic sync on unobserved project files', () => {
  assert.equal(isReviewableSourcePath('/project/package.json'), false)
  assert.equal(isReviewableSourcePath('/project/README.md'), false)
  assert.equal(isReviewableSourcePath('/project/Makefile'), false)
  assert.equal(isReviewableSourcePath('/project/node_modules/package/index.ts'), false)
  assert.equal(isReviewableSourcePath('/project/.generated/index.ts'), false)
  assert.equal(isReviewableSourcePath('/home/.local/projects/app/index.ts', '/home/.local/projects/app'), true)
})
