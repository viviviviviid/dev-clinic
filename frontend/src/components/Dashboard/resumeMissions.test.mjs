import assert from 'node:assert/strict'
import test from 'node:test'
import { resumableMissions } from './missionUx.ts'

const mission = (id, date, status = 'active') => ({ id, project_dir: `/projects/${id}`, date, status })

test('offers unfinished work from earlier dates when today has no missions', () => {
  const older = mission('older', '2026-08-20')
  const yesterday = mission('yesterday', '2026-09-04')
  const history = [older, mission('done', '2026-09-05', 'completed'), yesterday]
  assert.deepEqual(resumableMissions(history, []), [yesterday, older])
  assert.equal(history[0], older, 'does not reorder the calendar history')
})

test('deduplicates project paths and uses the current daily status', () => {
  const today = mission('today', '2026-09-05')
  const done = { ...today, status: 'completed' }
  assert.deepEqual(resumableMissions([today], [today]), [today])
  assert.deepEqual(resumableMissions([today], [done]), [])
})
