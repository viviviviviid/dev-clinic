import assert from 'node:assert/strict'
import test from 'node:test'

import { DEFAULT_LOCAL_URL, resolveBootConfig, resolveSupabaseConfig } from './bootConfig.ts'

const validEnvironment = {
  VITE_SUPABASE_URL: 'https://project.supabase.co/',
  VITE_SUPABASE_ANON_KEY: 'anon-key',
}

test('resolves and normalizes the browser boot configuration', () => {
  assert.deepEqual(resolveBootConfig(validEnvironment), {
    supabaseUrl: 'https://project.supabase.co',
    supabaseAnonKey: 'anon-key',
    localUrl: DEFAULT_LOCAL_URL,
  })

  assert.equal(
    resolveBootConfig({ ...validEnvironment, VITE_LOCAL_URL: ' http://localhost:5000/ ' }).localUrl,
    'http://localhost:5000',
  )
})

test('reports missing Supabase configuration before the app loads', () => {
  assert.throws(
    () => resolveSupabaseConfig({ VITE_SUPABASE_ANON_KEY: 'anon-key' }),
    /VITE_SUPABASE_URL/,
  )
  assert.throws(
    () => resolveSupabaseConfig({ VITE_SUPABASE_URL: 'https://project.supabase.co' }),
    /VITE_SUPABASE_ANON_KEY/,
  )
})

test('rejects malformed or insecure remote Supabase URLs', () => {
  for (const url of [
    'not-a-url',
    'http://project.supabase.co',
    'https://database.example.com',
    'https://user:password@project.supabase.co',
    'https://project.supabase.co?token=unsafe',
  ]) {
    assert.throws(() => resolveSupabaseConfig({
      VITE_SUPABASE_URL: url,
      VITE_SUPABASE_ANON_KEY: 'anon-key',
    }))
  }
})

test('allows loopback HTTP Supabase during local development', () => {
  assert.equal(resolveSupabaseConfig({
    VITE_SUPABASE_URL: 'http://127.0.0.1:54321',
    VITE_SUPABASE_ANON_KEY: 'anon-key',
  }).supabaseUrl, 'http://127.0.0.1:54321')
})
