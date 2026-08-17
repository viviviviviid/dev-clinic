import assert from 'node:assert/strict'
import test from 'node:test'

import { createEditorAutosave, installEditorAutosaveLifecycle } from './autosave.ts'

test('keeps pending saves isolated when editing A then B', async () => {
  const writes = []
  const saved = []
  const autosave = createEditorAutosave({
    delayMs: 1000,
    write: async (path, content) => { writes.push({ path, content }) },
    onSaved: path => saved.push(path),
    onError: () => assert.fail('unexpected save error'),
  })

  autosave.schedule('/project/a.go', 'content A')
  autosave.schedule('/project/b.go', 'content B')
  await autosave.flushAll()

  assert.deepEqual(writes.sort((a, b) => a.path.localeCompare(b.path)), [
    { path: '/project/a.go', content: 'content A' },
    { path: '/project/b.go', content: 'content B' },
  ])
  assert.deepEqual(saved.sort(), ['/project/a.go', '/project/b.go'])
})

test('serializes writes for one file and only marks the newest version saved', async () => {
  const writes = []
  const saved = []
  let releaseFirst
  const firstBlocked = new Promise(resolve => { releaseFirst = resolve })
  const autosave = createEditorAutosave({
    delayMs: 1000,
    write: async (_path, content) => {
      writes.push(content)
      if (content === 'version 1') await firstBlocked
    },
    onSaved: path => saved.push(path),
    onError: () => assert.fail('unexpected save error'),
  })

  autosave.schedule('/project/main.go', 'version 1')
  const first = autosave.flushPath('/project/main.go')
  autosave.schedule('/project/main.go', 'version 2')
  const second = autosave.flushPath('/project/main.go')
  await Promise.resolve()
  assert.deepEqual(writes, ['version 1'])
  releaseFirst()
  await Promise.all([first, second])

  assert.deepEqual(writes, ['version 1', 'version 2'])
  assert.deepEqual(saved, ['/project/main.go'])
})

test('reports write failures without marking the file saved', async () => {
  const errors = []
  const saved = []
  const autosave = createEditorAutosave({
    delayMs: 1000,
    write: async () => { throw new Error('disk full') },
    onSaved: path => saved.push(path),
    onError: (path, error) => errors.push({ path, message: error.message }),
  })

  const ok = await autosave.saveNow('/project/main.go', 'content')
  assert.equal(ok, false)
  assert.deepEqual(saved, [])
  assert.deepEqual(errors, [{ path: '/project/main.go', message: 'disk full' }])
})

test('tracks pending and in-flight writes until the serialized write settles', async () => {
  let releaseWrite
  const blocked = new Promise(resolve => { releaseWrite = resolve })
  const autosave = createEditorAutosave({
    delayMs: 1000,
    write: async () => { await blocked },
    onSaved: () => {},
    onError: () => assert.fail('unexpected save error'),
  })

  assert.equal(autosave.hasPendingWrites(), false)
  autosave.schedule('/project/main.go', 'content')
  assert.equal(autosave.hasPendingWrites(), true)

  const flushing = autosave.flushPath('/project/main.go')
  await Promise.resolve()
  assert.equal(autosave.hasPendingWrites(), true)

  releaseWrite()
  assert.equal(await flushing, true)
  assert.equal(autosave.hasPendingWrites(), false)
})

test('page lifecycle flushes each pending file and warns only while writes remain', async () => {
  const listeners = new Map()
  const target = {
    addEventListener(type, listener) { listeners.set(type, listener) },
    removeEventListener(type, listener) {
      if (listeners.get(type) === listener) listeners.delete(type)
    },
  }
  const writes = []
  const autosave = createEditorAutosave({
    delayMs: 1000,
    write: async (path, content) => { writes.push({ path, content }) },
    onSaved: () => {},
    onError: () => assert.fail('unexpected save error'),
  })
  const dispose = installEditorAutosaveLifecycle(autosave, target)

  autosave.schedule('/project/a.ts', 'A')
  autosave.schedule('/project/b.ts', 'B')
  let prevented = false
  const unloadEvent = {
    returnValue: undefined,
    preventDefault() { prevented = true },
  }
  listeners.get('beforeunload')(unloadEvent)

  assert.equal(prevented, true)
  assert.equal(unloadEvent.returnValue, '')
  await autosave.flushAll()
  assert.deepEqual(writes.sort((a, b) => a.path.localeCompare(b.path)), [
    { path: '/project/a.ts', content: 'A' },
    { path: '/project/b.ts', content: 'B' },
  ])

  prevented = false
  listeners.get('beforeunload')({ preventDefault() { prevented = true } })
  assert.equal(prevented, false)

  autosave.schedule('/project/c.ts', 'C')
  listeners.get('pagehide')({})
  await autosave.flushAll()
  assert.deepEqual(writes.at(-1), { path: '/project/c.ts', content: 'C' })

  dispose()
  assert.equal(listeners.size, 0)
})
