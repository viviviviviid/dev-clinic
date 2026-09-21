import assert from 'node:assert/strict'
import test from 'node:test'
import { isProjectFile, loadProjectLocations } from './navigation.ts'

const location = path => ({ uri: `file://${path}`, range: { start: { line: 1, character: 2 }, end: { line: 1, character: 8 } } })
const filePath = uri => new URL(uri).protocol === 'file:' ? decodeURIComponent(new URL(uri).pathname) : null

test('modifier previews never read stdlib, sibling projects or traversed paths', async () => {
  const loaded = []
  const results = await loadProjectLocations([
    location('/usr/local/go/src/context/context.go'), location('/workspace-other/main.go'),
    location('/workspace/../secret.go'), location('/workspace/main.go'),
  ], '/workspace', filePath, async (_, path) => { loaded.push(path); return path })
  assert.deepEqual(loaded, ['/workspace/main.go'])
  assert.deepEqual(results, ['/workspace/main.go'])
  assert.equal(isProjectFile('/workspace/main.go', ''), false)
  assert.equal(isProjectFile('/workspace/main.go', '/workspace/'), true)
})

test('references include unopened files and tolerate an unavailable preview', async () => {
  const results = await loadProjectLocations([
    location('/workspace/shared.go'), location('/workspace/deleted.go'),
    location('/workspace/first.go'), location('/workspace/sub/second.go'),
  ], '/workspace', filePath, async (_, path) => {
    if (path.endsWith('/deleted.go')) throw new Error('unavailable')
    return path
  })
  assert.deepEqual(results, ['/workspace/shared.go', '/workspace/first.go', '/workspace/sub/second.go'])
})
