create table if not exists public.user_settings (
  user_id uuid primary key references auth.users(id) on delete cascade,
  base_dir text not null default '',
  language text not null default 'Go',
  skill_level text not null default 'normal',
  updated_at timestamptz not null default now()
);

create table if not exists public.daily_missions (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null references auth.users(id) on delete cascade,
  date date not null,
  topic text not null,
  slug text not null,
  project_dir text not null,
  status text not null default 'active',
  created_at timestamptz not null default now()
);

create unique index if not exists daily_missions_user_project_dir_uidx
  on public.daily_missions (user_id, project_dir);

alter table public.user_settings enable row level security;
alter table public.daily_missions enable row level security;

drop policy if exists "user settings are private" on public.user_settings;
create policy "user settings are private" on public.user_settings
  for all using (auth.uid() = user_id) with check (auth.uid() = user_id);

drop policy if exists "daily missions are private" on public.daily_missions;
create policy "daily missions are private" on public.daily_missions
  for all using (auth.uid() = user_id) with check (auth.uid() = user_id);
