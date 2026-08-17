import { createClient } from '@supabase/supabase-js'
import { resolveSupabaseConfig } from './bootConfig'

const { supabaseUrl, supabaseAnonKey } = resolveSupabaseConfig(import.meta.env)

export const supabase = createClient(supabaseUrl, supabaseAnonKey)
