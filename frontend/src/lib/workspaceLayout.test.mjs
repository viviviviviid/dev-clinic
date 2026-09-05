import assert from 'node:assert/strict'
import test from 'node:test'
import { MIN_CODE_WIDTH, workspaceLayout } from './workspaceLayout.ts'

test('keeps code readable with previously expanded panels when the window shrinks', () => {
  const preferred = { left: 360, feedback: 600 }
  assert.deepEqual(workspaceLayout(1280, preferred.left, preferred.feedback), { leftWidth: 360, feedbackWidth: 424 })
  for (const width of [1001, 1024, 1100, 1280, 1440, 1920]) {
    for (const left of [0, 120, 200, 280, 360, 480]) {
      for (const feedback of [160, 360, 600]) {
        const actual = workspaceLayout(width, left, feedback)
        const code = width - actual.leftWidth - actual.feedbackWidth - (left ? 16 : 8)
        assert.ok(code >= MIN_CODE_WIDTH, `${width}px: code only has ${code}px`)
        assert.ok(actual.feedbackWidth > 0)
      }
    }
  }
  assert.deepEqual(workspaceLayout(1920, preferred.left, preferred.feedback), { leftWidth: 360, feedbackWidth: 600 })
})

test('compact workspace gives feedback the full width without changing the drawer preference', () => {
  assert.deepEqual(workspaceLayout(768, 200, 600), { leftWidth: 200, feedbackWidth: 768 })
})
