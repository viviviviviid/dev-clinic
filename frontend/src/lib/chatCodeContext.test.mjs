import assert from 'node:assert/strict'
import test from 'node:test'
import {
  chatCodeContext, chatCodeLabel, createChatCodeReference, createChatPayload,
  MAX_CHAT_CODE_BYTES, MAX_CHAT_CODE_REFERENCES, mergeChatCodeReferences,
} from './chatCodeContext.ts'

const reference = (path = 'client.go', code = 'cancel()') => createChatCodeReference(path, {
  startLineNumber: 15, startColumn: 3, endLineNumber: 15, endColumn: 11,
}, code)

test('labels use inclusive selected lines while preserving the exact code snapshot', () => {
  const code = 'ctx := context.Background()\n\tcancel()\n'
  const selected = createChatCodeReference('internal/client.go', {
    startLineNumber: 15, startColumn: 2, endLineNumber: 17, endColumn: 1,
  }, code)
  assert.equal(chatCodeLabel(selected), 'internal/client.go:15–16')
  assert.equal(selected.code, code)
  assert.equal(selected.endColumn, '\tcancel()'.length + 1)
  const throughNewline = createChatCodeReference('client.go', {
    startLineNumber: 15, startColumn: 3, endLineNumber: 16, endColumn: 1,
  }, 'cancel()\r\n')
  assert.equal(chatCodeLabel(throughNewline), 'client.go:15')
  assert.equal(throughNewline.endColumn, 11)
  assert.equal(chatCodeLabel(reference()), 'client.go:15')
  assert.equal(createChatCodeReference('client.go', {
    startLineNumber: 1, startColumn: 1, endLineNumber: 2, endColumn: 1,
  }, ' \n'), null)
})

test('reattaching a selection refreshes its snapshot and preserves other files and ranges', () => {
  const otherRange = { ...reference(), startLine: 20, endLine: 20 }
  const current = [reference(), reference('server.go'), otherRange]
  const merged = mergeChatCodeReferences(current, [reference('client.go', 'cancelLater()')])
  assert.equal(merged.length, 3)
  assert.equal(merged[0].code, 'cancelLater()')
  assert.deepEqual(merged.slice(1), current.slice(1))
  assert.equal(current[0].code, 'cancel()')
})

test('attachment limits reject the entire addition, measuring serialized UTF-8 bytes', () => {
  const current = Array.from({ length: MAX_CHAT_CODE_REFERENCES }, (_, i) => reference(`${i}.go`))
  assert.equal(mergeChatCodeReferences(current, [reference('extra.go')]), null)
  assert.equal(mergeChatCodeReferences(current, [reference('0.go', 'updated()')]).length, MAX_CHAT_CODE_REFERENCES)
  const oversized = reference('한글.go', '한'.repeat(MAX_CHAT_CODE_BYTES / 2))
  assert.ok(chatCodeContext([oversized]).length < MAX_CHAT_CODE_BYTES)
  assert.equal(mergeChatCodeReferences([], [oversized]), null)
  assert.equal(current.length, MAX_CHAT_CODE_REFERENCES)
})

test('chat sends selected snapshots even after switching files and retains earlier references in history', () => {
  const earlier = reference('earlier.go', 'earlier()')
  const selected = reference('selected.go', 'selected()')
  const payload = createChatPayload('이 부분을 설명해줘', 'unrelated current file', [
    { role: 'user', content: '이전 질문', codeReferences: [earlier] },
    { role: 'ai', content: '이전 답변' },
  ], [selected])
  assert.equal(payload.message, '이 부분을 설명해줘')
  assert.deepEqual(JSON.parse(payload.fileContent).selectedCode, [{ ...selected, location: 'selected.go:15' }])
  assert.equal(payload.chatHistory[0].content, `이전 질문\n\n${chatCodeContext([earlier])}`)
  assert.deepEqual(payload.chatHistory[1], { role: 'ai', content: '이전 답변' })
  assert.deepEqual(createChatPayload('plain question', 'current file', []), {
    message: 'plain question', fileContent: 'current file', chatHistory: [],
  })
})
