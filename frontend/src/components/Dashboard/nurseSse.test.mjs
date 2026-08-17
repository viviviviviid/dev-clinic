import assert from 'node:assert/strict'
import test from 'node:test'

import { NurseSseParser } from './nurseSse.ts'

test('message event preserves a literal [TOPICS] marker', () => {
  const parser = new NurseSseParser()
  const stream = [
    'event: message\ndata: {"text":"\uBB38\uC7A5 \uC548\uC758 [TOP',
    'ICS] \uD45C\uC2DC\uB294 \uADF8\uB300\uB85C \uB0A8\uC544\uC57C \uD569\uB2C8\uB2E4."}\n\n',
    'event: topics\ndata: {"topics":[{"name":"\uBC30\uC5F4 \uC5F0\uC2B5","slug":"arrays","difficulty":"\uD558"}]}\n\n',
    'event: done\ndata: {}\n\n',
  ]

  const events = stream.flatMap(chunk => parser.push(chunk)).concat(parser.finish())

  assert.deepEqual(events, [
    { type: 'message', text: '\uBB38\uC7A5 \uC548\uC758 [TOPICS] \uD45C\uC2DC\uB294 \uADF8\uB300\uB85C \uB0A8\uC544\uC57C \uD569\uB2C8\uB2E4.' },
    { type: 'topics', topics: [{ name: '\uBC30\uC5F4 \uC5F0\uC2B5', slug: 'arrays', difficulty: '\uD558' }] },
    { type: 'done' },
  ])
})
