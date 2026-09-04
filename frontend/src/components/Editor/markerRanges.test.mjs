import assert from 'node:assert/strict'
import test from 'node:test'

import { findTutorMarkerEditLineRange, findTutorMarkerLineRange, replaceTutorMarkerAtIndex } from './markerRanges.ts'

test('finds the complete marker range for direct editor editing', () => {
  const content = `func total() int {
  // [TUTOR:HOLE] calculate total
  return 0
  // [TUTOR:END]
}`

  assert.deepEqual(findTutorMarkerLineRange(content, 'hole', 0), {
    startLineNumber: 2,
    endLineNumber: 4,
  })
  assert.equal(findTutorMarkerLineRange(content, 'bug', 0), null)
})

test('selects only the function body when a marker wraps a legacy function', () => {
  const content = `// [TUTOR:HOLE] initialize state
func create() *Thing {
  return &Thing{}
}
// [TUTOR:END]`

  assert.deepEqual(findTutorMarkerEditLineRange(content, 'hole', 0), {
    startLineNumber: 3,
    endLineNumber: 3,
  })
})

test('replaces a multiline Go HOLE range including both markers', () => {
  const content = `func total(values []int) int {
\t// [TUTOR:HOLE] sum the values
\tresult := 0
\tfor _, value := range values {
\t\tresult += value
\t}
\t// [TUTOR:END]
\treturn result
}`
  const code = `    result := 0
    for _, value := range values {
        result += value * 2
    }`

  assert.equal(replaceTutorMarkerAtIndex(content, 'hole', 0, code), `func total(values []int) int {
\tresult := 0
\tfor _, value := range values {
\t    result += value * 2
\t}
\treturn result
}`)
})

test('replaces the requested TypeScript BUG range and leaves the other range intact', () => {
  const content = `function first() {
  // [TUTOR:BUG] wrong branch
  if (ready) {
    return false
  }
  // [TUTOR:END]
}
function second() {
  // [TUTOR:BUG] wrong value
  return 0
  // [TUTOR:END]
}`

  assert.equal(
    replaceTutorMarkerAtIndex(content, 'bug', 1, 'return 1'),
    `function first() {
  // [TUTOR:BUG] wrong branch
  if (ready) {
    return false
  }
  // [TUTOR:END]
}
function second() {
  return 1
}`,
  )
})

test('preserves relative indentation in a nested multiline Python snippet', () => {
  const content = `def render(items):
    # [TUTOR:HOLE] render non-empty items
    pass
    # [TUTOR:END]
    return "done"`
  const code = `        if items:
            for item in items:
                print(item)`

  assert.equal(replaceTutorMarkerAtIndex(content, 'hole', 0, code), `def render(items):
    if items:
        for item in items:
            print(item)
    return "done"`)
})

test('does not edit an unclosed or nested marker range', () => {
  const unclosed = '// [TUTOR:HOLE]\nvalue := 0'
  assert.equal(replaceTutorMarkerAtIndex(unclosed, 'hole', 0, 'value := 1'), unclosed)

  const nested = '// [TUTOR:HOLE]\n// [TUTOR:BUG]\nvalue := 0\n// [TUTOR:END]'
  assert.equal(replaceTutorMarkerAtIndex(nested, 'hole', 0, 'value := 1'), nested)
  assert.equal(replaceTutorMarkerAtIndex(nested, 'bug', 0, 'value := 1'), nested)

  const strayEnd = '// [TUTOR:END]\n// [TUTOR:BUG]\nvalue := 0\n// [TUTOR:END]'
  assert.equal(replaceTutorMarkerAtIndex(strayEnd, 'bug', 0, 'value := 1'), strayEnd)
})
