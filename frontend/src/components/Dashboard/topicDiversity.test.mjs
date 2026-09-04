import assert from 'node:assert/strict'
import test from 'node:test'

import { rememberSuggestedTopics } from './topicDiversity.ts'

test('remembers displayed suggestions before older mission topics', () => {
  assert.deepEqual(
    rememberSuggestedTopics(
      ['기존 로그 분석기', '오래된 게임'],
      [{ name: '우주선 고장 탐정' }, { name: '픽셀 생태계' }, { name: '기존 로그 분석기' }],
    ),
    ['우주선 고장 탐정', '픽셀 생태계', '기존 로그 분석기', '오래된 게임'],
  )
})

test('deduplicates case-insensitively and caps the exclusion list', () => {
  assert.deepEqual(
    rememberSuggestedTopics(['Cache Lab', 'older'], [{ name: ' cache lab ' }, { name: 'new' }], 2),
    ['cache lab', 'new'],
  )
})
