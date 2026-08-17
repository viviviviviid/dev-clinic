import assert from 'node:assert/strict'
import test from 'node:test'

import {
  MIN_EDITOR_WIDTH,
  clearPendingMissionFinalize,
  describeMissionGeneration,
  isEditorWidthReady,
  readPendingMissionFinalize,
  writePendingMissionFinalize,
} from './missionUx.ts'

test('reports explicit AI stage counts for each skill level', () => {
  assert.equal(
    describeMissionGeneration('curriculum', '커리큘럼', 'normal').title,
    'AI가 1/2단계를 생성 중입니다',
  )
  assert.equal(
    describeMissionGeneration('quiz', '퀴즈', 'newbie').title,
    'AI가 3/3단계를 생성 중입니다',
  )
})

test('does not offer cancellation during local apply or record finalization', () => {
  const localApply = describeMissionGeneration('watcher', '파일 감시자 준비', 'normal')
  assert.equal(localApply.cancellable, false)
  assert.match(localApply.title, /로컬 파일/)
  assert.match(localApply.detail, /완료되면/)

  const finalize = describeMissionGeneration('finalize', '기록 확정', 'normal')
  assert.equal(finalize.cancellable, false)
  assert.match(finalize.title, /확정/)
})

test('requires the editor minimum width before mission creation', () => {
  assert.equal(isEditorWidthReady(MIN_EDITOR_WIDTH - 1), false)
  assert.equal(isEditorWidthReady(MIN_EDITOR_WIDTH), true)
})

test('persists an unfinished mission finalization across a dashboard reload', () => {
  const values = new Map()
  const storage = {
    getItem: key => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: key => values.delete(key),
  }
  const pending = {
    topic: 'Generics',
    slug: 'Generics',
    dir_suffix: '260817-Generics',
    setup_token: '0123456789abcdef0123456789abcdef',
    project_dir: '/projects/260817-Generics',
    files: ['TUTORSYS.md', 'main.ts'],
    skill_level: 'normal',
    setup_files: { 'TUTORSYS.md': '# curriculum', 'main.ts': 'export {}' },
    curriculum: '# curriculum',
    language: 'typescript',
  }

  assert.equal(writePendingMissionFinalize(storage, 'pending', pending), true)
  assert.deepEqual(readPendingMissionFinalize(storage, 'pending'), pending)
  assert.equal(clearPendingMissionFinalize(storage, 'pending'), true)
  assert.equal(readPendingMissionFinalize(storage, 'pending'), null)
})

test('ignores malformed pending mission records and inaccessible storage', () => {
  assert.equal(readPendingMissionFinalize({ getItem: () => '{bad json' }, 'pending'), null)
  assert.equal(readPendingMissionFinalize({ getItem: () => JSON.stringify({ topic: 'missing fields' }) }, 'pending'), null)
  assert.equal(writePendingMissionFinalize({ setItem: () => { throw new Error('blocked') } }, 'pending', {
    topic: 'T', slug: 'T', dir_suffix: '260817-T', setup_token: '0123456789abcdef0123456789abcdef',
    project_dir: '/p', files: [], skill_level: 'normal',
  }), false)
})
