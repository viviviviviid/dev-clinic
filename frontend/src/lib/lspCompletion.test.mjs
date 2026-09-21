import assert from 'node:assert/strict'
import test from 'node:test'
import { toLspCompletionContext, toMonacoCompletions } from './lspCompletion.ts'

const fallback = { startLineNumber: 54, startColumn: 16, endLineNumber: 54, endColumn: 19 }
const range = (start, end) => ({ start: { line: 53, character: start }, end: { line: 53, character: end } })
const kind = value => value

test('gopls field completions remain refreshable after typing a lowercase prefix', () => {
  const result = toMonacoCompletions({
    isIncomplete: true,
    items: [{ label: 'Elapsed', kind: 5, filterText: 'Elapsed', preselect: true, insertTextFormat: 2,
      textEdit: { range: range(15, 18), newText: 'Elapsed' } }],
  }, fallback, kind)
  assert.equal(result.incomplete, true)
  assert.equal(result.suggestions[0].preselect, true)
  assert.equal(result.suggestions[0].filterText, 'Elapsed')
  assert.deepEqual(result.suggestions[0].range, fallback)
  const source = '\t\tif events[i].ela < events[j].Elapsed {'
  const edit = result.suggestions[0]
  assert.equal(source.slice(0, edit.range.startColumn - 1) + edit.insertText + source.slice(edit.range.endColumn - 1), '\t\tif events[i].Elapsed < events[j].Elapsed {')
})

test('completion triggers distinguish initial dot, continued typing, and manual requests', () => {
  assert.deepEqual(toLspCompletionContext({ triggerKind: 1, triggerCharacter: '.' }), { triggerKind: 2, triggerCharacter: '.' })
  assert.deepEqual(toLspCompletionContext({ triggerKind: 2 }), { triggerKind: 3 })
  assert.deepEqual(toLspCompletionContext({ triggerKind: 0 }), { triggerKind: 1 })
})

test('server replacement ranges remove existing suffixes and preserve function snippets and imports', () => {
  const result = toMonacoCompletions({ isIncomplete: true, items: [{
    label: 'Elapsed.Round', insertText: 'ignored', insertTextFormat: 2,
    textEdit: { range: range(15, 28), newText: 'Elapsed.Round(${1:})' },
    additionalTextEdits: [{ range: { start: { line: 2, character: 0 }, end: { line: 2, character: 0 } }, newText: 'import "time"\n' }],
  }] }, fallback, kind)
  const item = result.suggestions[0]
  assert.equal(item.insertText, 'Elapsed.Round(${1:})')
  assert.equal(item.insertTextRules, 4)
  assert.equal(item.range.endColumn, 29)
  assert.deepEqual(item.additionalTextEdits, [{ range: { startLineNumber: 3, startColumn: 1, endLineNumber: 3, endColumn: 1 }, text: 'import "time"\n' }])
})

test('insert/replace edits and complete legacy arrays keep their distinct semantics', () => {
  const result = toMonacoCompletions([{ label: { label: 'Elapsed' }, textEdit: { insert: range(15, 18), replace: range(15, 22), newText: 'Elapsed' } }], fallback, kind)
  assert.equal(result.incomplete, false)
  assert.deepEqual(result.suggestions[0].range, {
    insert: fallback,
    replace: { ...fallback, endColumn: 23 },
  })
  const plain = toMonacoCompletions([{ label: 'fallback', insertText: 'actual' }], fallback, kind)
  assert.equal(plain.suggestions[0].insertText, 'actual')
  assert.deepEqual(plain.suggestions[0].range, fallback)
})
