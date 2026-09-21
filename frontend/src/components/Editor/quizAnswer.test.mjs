import assert from 'node:assert/strict'
import test from 'node:test'
import { createQuizAnswerRequest } from './quizAnswer.ts'
import { createChatPayload } from '../../lib/chatCodeContext.ts'

const content = 'package main\n// [TUTOR:HOLE] first\nfirst()\n// [TUTOR:END]\n// [TUTOR:BUG] fix\nbroken()\n// [TUTOR:END]\n// [TUTOR:HOLE] second\nsecond()\n// [TUTOR:END]'

test('answer requests capture the chosen HOLE or BUG and full surrounding file without changing code', () => {
  for (const [markerType, markerIndex, startLine, endLine] of [['hole', 1, 8, 10], ['bug', 0, 5, 7]]) {
    const request = createQuizAnswerRequest('internal/main.go', content, { markerType, markerIndex, question: '문제 설명' })
    assert.equal(request.fileContent, content)
    assert.equal(request.answer.reference.startLine, startLine)
    assert.equal(request.answer.reference.endLine, endLine)
    assert.equal(request.answer.reference.code, content.split('\n').slice(startLine - 1, endLine).join('\n'))
    const payload = createChatPayload('답지 요청', request.fileContent, [], [request.answer.reference], request.answer)
    assert.equal(payload.fileContent, content)
    assert.deepEqual(payload.answerRequest, request.answer)
    assert.equal(createChatPayload('후속 질문', content, []).answerRequest, undefined)
  }
})

test('removed, malformed, and oversized answer targets are rejected rather than guessed or truncated', () => {
  const item = { markerType: 'hole', markerIndex: 3, question: '질문' }
  assert.throws(() => createQuizAnswerRequest('main.go', content, item), /범위가 변경/)
  assert.throws(() => createQuizAnswerRequest('main.go', content.replaceAll('// [TUTOR:END]', ''), { ...item, markerIndex: 0 }), /범위가 변경/)
  assert.throws(() => createQuizAnswerRequest('main.go', content, { ...item, markerType: 'other' }), /유형|문제를 찾지/)
  assert.throws(() => createQuizAnswerRequest('main.go', content + '\n' + 'x'.repeat(128 << 10), { ...item, markerIndex: 0 }), /128KB/)
})
