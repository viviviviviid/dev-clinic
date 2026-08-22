import assert from 'node:assert/strict'
import test from 'node:test'

import { readClampedNumber, readPreference, writePreference } from './storage.ts'

function memoryStorage(initial = {}) {
  const values = new Map(Object.entries(initial))
  return {
    getItem(key) {
      return values.get(key) ?? null
    },
    setItem(key, value) {
      values.set(key, value)
    },
    value(key) {
      return values.get(key)
    },
  }
}

test('reads finite preferences and clamps them to the requested range', () => {
  assert.equal(readClampedNumber('width', 360, 160, 600, memoryStorage({ width: '480' })), 480)
  assert.equal(readClampedNumber('width', 360, 160, 600, memoryStorage({ width: '999' })), 600)
  assert.equal(readClampedNumber('width', 360, 160, 600, memoryStorage({ width: '-1' })), 160)
})

test('falls back when preferences are absent, malformed, or inaccessible', () => {
  assert.equal(readClampedNumber('width', 360, 160, 600, memoryStorage()), 360)
  assert.equal(readClampedNumber('width', 360, 160, 600, memoryStorage({ width: 'NaN' })), 360)
  assert.equal(readClampedNumber('width', 360, 160, 600, {
    getItem() { throw new Error('blocked') },
    setItem() {},
  }), 360)
})

test('writes preferences without leaking storage exceptions', () => {
  const storage = memoryStorage()
  assert.equal(writePreference('width', 420, storage), true)
  assert.equal(storage.value('width'), '420')
  assert.equal(writePreference('width', 420, {
    getItem() { return null },
    setItem() { throw new Error('quota exceeded') },
  }), false)
})

test('reads string preferences without leaking storage exceptions', () => {
  assert.equal(readPreference('intro', memoryStorage({ intro: '2026-08-23' })), '2026-08-23')
  assert.equal(readPreference('intro', {
    getItem() { throw new Error('blocked') },
    setItem() {},
  }), null)
})
