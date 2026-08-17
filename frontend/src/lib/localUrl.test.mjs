import assert from 'node:assert/strict'
import test from 'node:test'

import { normalizeLocalUrl } from './localUrl.ts'

test('normalizes supported loopback clinic URLs', () => {
  assert.equal(normalizeLocalUrl('http://127.0.0.1:47291/'), 'http://127.0.0.1:47291')
  assert.equal(normalizeLocalUrl('http://localhost:5000'), 'http://localhost:5000')
})

test('rejects missing ports, non-loopback, TLS, credentials, paths, and query strings', () => {
  for (const value of [
    'http://127.0.0.1',
    'http://localhost',
    'https://127.0.0.1:47291',
    'http://192.168.0.10:47291',
    'http://127.0.0.2:47291',
    'http://[::1]:47291',
    'http://example.com:47291',
    'http://user:password@127.0.0.1:47291',
    'http://127.0.0.1:47291/api',
    'http://127.0.0.1:47291/?target=remote',
  ]) {
    assert.throws(() => normalizeLocalUrl(value))
  }
})
