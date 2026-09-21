import assert from 'node:assert/strict'
import test from 'node:test'
import { quizMarkers } from './quizMarkers.ts'

test('every HOLE and BUG remains available even when quiz data is missing', () => {
  const markers = quizMarkers('// [TUTOR:HOLE] first\n// [TUTOR:END]\n// [TUTOR:HOLE] second\n// [TUTOR:END]\n// [TUTOR:BUG] cancel upstream', 'main.go', {}, new Set())
  assert.deepEqual(markers.map(({ item }) => [item.markerType, item.markerIndex, item.question]), [
    ['hole', 0, 'first'], ['hole', 1, 'second'], ['bug', 0, 'cancel upstream'],
  ])
  assert.deepEqual(markers.map(({ lineNumber }) => lineNumber), [1, 3, 5])
})

test('relative quiz paths take priority over legacy basenames with independent marker indices', () => {
  const markers = quizMarkers('// [TUTOR:HOLE] one\n// [TUTOR:BUG] two', 'cmd/main.go', {
    'cmd/main.go:hole:0': { question: 'relative', hints: ['first'] },
    'main.go:hole:0': { question: 'wrong file' },
    'main.go:bug:0': { question: 'legacy bug', hints: [] },
  }, new Set())
  assert.equal(markers[0].item.question, 'relative')
  assert.deepEqual(markers[0].item.hints, ['first'])
  assert.equal(markers[1].item.question, 'legacy bug')
  assert.equal(markers[1].item.markerType, 'bug')
  assert.equal(markers[1].item.markerIndex, 0)
})

test('legacy HOLE data and solved keys are recognized', () => {
  const data = { 'main.go:0': { question: 'legacy', hints: [] } }
  assert.equal(quizMarkers('// [TUTOR:HOLE]', 'main.go', data, new Set())[0].item.question, 'legacy')
  assert.deepEqual(quizMarkers('// [TUTOR:HOLE]', 'main.go', data, new Set(['main.go:0'])), [])
})
