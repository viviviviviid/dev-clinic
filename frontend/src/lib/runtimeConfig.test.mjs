import assert from 'node:assert/strict'
import test from 'node:test'
import { initializeRuntimeConfig, runtimeConfig, desktopWorkspace } from './runtimeConfig.ts'

test('desktop boot uses the reserved local port and public native settings', async () => {
  const expected = { supabaseUrl: 'https://project.supabase.co', supabaseAnonKey: 'sb_publishable_test', localUrl: 'http://127.0.0.1:50123' }
  const config = await initializeRuntimeConfig({}, { Boot: async () => ({ ...expected, baseDir: '/Users/test/Coding Tutor' }) })
  assert.deepEqual(config, expected)
  assert.deepEqual(runtimeConfig(), expected)
  assert.equal(desktopWorkspace(), '/Users/test/Coding Tutor')
})

test('desktop boot rejects remote API targets and administrator keys', async () => {
  for (const override of [{ localUrl: 'https://evil.example' }, { supabaseAnonKey: 'sb_secret_private' }]) {
    await assert.rejects(initializeRuntimeConfig({}, { Boot: async () => ({
      supabaseUrl: 'https://project.supabase.co', supabaseAnonKey: 'sb_publishable_test',
      localUrl: 'http://127.0.0.1:50123', baseDir: '/tmp/learning', ...override,
    }) }))
  }
})

test('web boot still uses the Vite configuration', async () => {
  const config = await initializeRuntimeConfig({ VITE_SUPABASE_URL: 'https://web.supabase.co', VITE_SUPABASE_ANON_KEY: 'sb_publishable_web' })
  assert.equal(config.localUrl, 'http://127.0.0.1:47291')
  assert.equal(config.supabaseUrl, 'https://web.supabase.co')
  assert.equal(desktopWorkspace(), '')
})
