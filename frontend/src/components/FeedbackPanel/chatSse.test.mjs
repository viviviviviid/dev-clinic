import assert from 'node:assert/strict'
import test from 'node:test'

import { ChatSseParser } from './chatSse.ts'

test('parses split message and done events', () => {
  const parser = new ChatSseParser()
  assert.deepEqual(parser.push('data: {"te'), [])
  assert.deepEqual(parser.push('xt":"안녕"}\n\nevent: done\ndata: {}\n\n'), [
    { type: 'message', text: '안녕' },
    { type: 'done' },
  ])
  assert.deepEqual(parser.finish(), [])
})

test('rejects malformed message JSON instead of silently dropping it', () => {
  const parser = new ChatSseParser()
  assert.throws(() => parser.push('data: {broken}\n\n'), /손상된 JSON/)
})

test('exposes a missing done event to the caller', () => {
  const parser = new ChatSseParser()
  assert.deepEqual(parser.push('data: {"text":"부분 응답"}\n\n'), [
    { type: 'message', text: '부분 응답' },
  ])
  assert.deepEqual(parser.finish(), [])
})
