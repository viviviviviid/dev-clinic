import assert from 'node:assert/strict'
import test from 'node:test'

import { DEFAULT_LOCAL_URL, resolveBootConfig, resolveSupabaseConfig, publicClinicConfig } from './bootConfig.ts'

const validEnvironment = {
  VITE_SUPABASE_URL: 'https://project.supabase.co/',
  VITE_SUPABASE_ANON_KEY: 'sb_publishable_test',
}

test('resolves and normalizes the browser boot configuration', () => {
  assert.deepEqual(resolveBootConfig(validEnvironment), {
    supabaseUrl: 'https://project.supabase.co',
    supabaseAnonKey: 'sb_publishable_test',
    localUrl: DEFAULT_LOCAL_URL,
  })

  assert.equal(
    resolveBootConfig({ ...validEnvironment, VITE_LOCAL_URL: ' http://localhost:5000/ ' }).localUrl,
    'http://localhost:5000',
  )
})

test('reports missing Supabase configuration before the app loads', () => {
  assert.throws(
    () => resolveSupabaseConfig({ VITE_SUPABASE_ANON_KEY: 'sb_publishable_test' }),
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
      VITE_SUPABASE_ANON_KEY: 'sb_publishable_test',
    }))
  }
})

test('allows loopback HTTP Supabase during local development', () => {
  assert.equal(resolveSupabaseConfig({
    VITE_SUPABASE_URL: 'http://127.0.0.1:54321',
    VITE_SUPABASE_ANON_KEY: 'sb_publishable_test',
  }).supabaseUrl, 'http://127.0.0.1:54321')
})


test('exports only public connection settings, ignoring private environment variables', () => {
  assert.deepEqual(publicClinicConfig({
    ...validEnvironment,
    SUPABASE_SERVICE_ROLE_KEY: 'do-not-publish',
    SUPABASE_JWT_SECRET: 'do-not-publish',
    GEMINI_API_KEY: 'do-not-publish',
    VITE_LOCAL_URL: 'http://localhost:5000',
  }), { version: 1, supabaseUrl: 'https://project.supabase.co', supabaseAnonKey: 'sb_publishable_test' })
})

test('rejects secret/service-role keys and accepts legacy anon keys', () => {
  const jwt = (role) => `eyJhbGciOiJIUzI1NiJ9.${Buffer.from(JSON.stringify({ role })).toString('base64url')}.signature`
  for (const key of ['sb_secret_private', jwt('service_role'), 'malformed']) {
    assert.throws(() => publicClinicConfig({ ...validEnvironment, VITE_SUPABASE_ANON_KEY: key }), /공개/)
  }
  assert.equal(publicClinicConfig({ ...validEnvironment, VITE_SUPABASE_ANON_KEY: jwt('anon') }).supabaseAnonKey, jwt('anon'))
})
