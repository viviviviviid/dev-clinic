import { createClient } from '@supabase/supabase-js'
import { runtimeConfig } from './runtimeConfig'
import { isDesktop } from './desktop'

const { supabaseUrl, supabaseAnonKey } = runtimeConfig()

export const supabase = createClient(supabaseUrl, supabaseAnonKey, {
  auth: isDesktop() ? { flowType: 'pkce', detectSessionInUrl: false } : undefined,
})
