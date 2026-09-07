# coding-tutor

개인용 AI 코딩 튜터입니다. Vercel은 정적 React 화면만 호스팅하고, 파일 접근·코드 실행·AI 호출은 Mac에서 실행하는 단일 `clinic` 프로세스가 담당합니다.

## 확정 아키텍처

```text
Vercel (frontend/dist)
  ├─ clinic-config.json: 공개 연결 정보 자동 제공
  ├─ Supabase Auth: Google OAuth 로그인
  └─ HTTPS → http/ws://127.0.0.1:47291
                  clinic
                  ├─ Codex CLI 또는 Gemini
                  ├─ 프로젝트 파일 / watcher / snapshot
                  ├─ run / test / LSP / terminal
                  └─ Supabase REST: 사용자 JWT + RLS로 설정·미션 기록
```

홈서버는 필요하지 않습니다. Vercel 화면은 계속 열 수 있지만 프로젝트 생성·편집·AI 피드백을 사용할 때는 해당 Mac에서 `clinic`만 실행하면 됩니다. 외부에 포트를 열거나 24시간 켜둘 필요도 없습니다. `127.0.0.1`은 화면을 연 기기 자신을 뜻하므로 Vercel URL도 clinic이 실행 중인 같은 Mac의 Chrome에서 여세요. 휴대폰이나 다른 PC에서 연 화면은 Mac의 clinic에 연결되지 않습니다.

## ChatGPT 구독 사용

기본 AI 공급자는 `codex`입니다. 로컬의 공식 Codex CLI가 저장된 ChatGPT 로그인을 사용하며, 앱이 OpenAI API 키를 호출하지 않습니다.

```bash
codex login
codex login status
```

ChatGPT 구독료가 OpenAI API 크레딧으로 전환되는 것은 아닙니다. 이 프로젝트의 Codex 모드는 공식 `codex exec` 자동화 기능을 사용하므로 Codex가 포함된 ChatGPT 요금제의 사용량 제한을 따릅니다. 로그아웃되었거나 한도에 도달하면 clinic이 원인을 오류로 표시합니다.

Gemini는 선택형 예비 공급자입니다.

```toml
ai_provider = "gemini"

[gemini]
api_key = "..."
model = "gemini-3.6-flash"
```

## 일반 사용자 실행

```bash
codex login              # 처음 한 번
./run.sh ~/learning       # 원하는 학습 파일 저장 경로
```

그다음 같은 Mac의 Chrome에서 https://tutor.abcfe.net 을 열고 Google로 로그인하세요. `config.toml`, `.env.local`, `frontend/.env`를 만들거나 Supabase 키를 입력할 필요가 없습니다. 인자를 생략하면 저장소의 `data/`를 사용합니다. 상대 경로와 공백이 있는 경로도 지원합니다 (`./run.sh "~/My Learning"`). 명시한 경로가 `BASE_DIR`보다 우선합니다.

clinic은 시작할 때 배포 사이트의 `/clinic-config.json`에서 Supabase URL과 공개 키를 가져옵니다. 파일·실행·AI는 로컬에서 처리하고, DB는 웹에서 로그인한 사용자의 JWT로 접근하므로 사용자별 RLS가 적용됩니다. 이 구조에는 Supabase 관리자 키나 JWT 서명 비밀키가 필요하지 않습니다. 기존 `config.toml`/환경변수에 남아 있는 `service_role_key`, `jwt_secret`은 읽지 않습니다.

## 준비

- Go 1.25+
- Node.js 24 LTS 및 npm (Codex CLI 또는 JS/TS 학습에 사용)
- Codex CLI와 ChatGPT 로그인(기본 모드)
- Chrome 권장: 최초 접속 시 로컬 네트워크 권한을 허용해야 합니다.

학습 프로젝트를 실행·테스트할 언어 도구도 Mac에 미리 설치해야 합니다. clinic은 패키지를 자동 설치하지 않으며 Node 도구는 `npx --no-install`로만 실행합니다.

| 언어 | 실행·테스트 계약 |
|---|---|
| Go | `go run .`, `go test ./...` |
| Python | `python3 main.py`, `python3 -m pytest` |
| Rust | `cargo run`, `cargo test` |
| TypeScript | 로컬 `ts-node`, `jest`; 진입점 `src/index.ts` |
| JavaScript | `node index.js`, 로컬 `jest` |

언어별 진입점·실행·테스트·테스트 파일·설정 파일 판별은 `internal/toolchain` 레지스트리 한곳에서 관리합니다. TypeScript는 `.ts/.tsx`, Jest 테스트, `package*.json`, `tsconfig*.json`, Jest/Babel 설정을 동일 계약으로 추적하므로 이후 실행기나 테스트 규칙도 다른 API를 고치지 않고 레지스트리에서 확장할 수 있습니다.

## AI 검토 동작

코드는 500ms 뒤 자동 저장되지만 AI 검토는 자동 실행되지 않습니다. 의미 있는 소스 변경이 저장되면 화면의 `AI 검토`가 준비 상태가 되고, 버튼이나 `Cmd/Ctrl+Enter`를 눌렀을 때만 Codex/Gemini 호출이 시작됩니다. 공백·일반 주석·포맷 변경만으로는 검토가 준비되지 않습니다.

- 검토 중 코드를 다시 편집하면 이전 요청을 취소하고 오래된 응답을 버립니다.
- 테스트 실패 원인도 사용자가 `실패 원인 AI 검토`를 눌렀을 때만 전송합니다.
- 호출은 최소 15초 간격, 분당 최대 4회로 제한됩니다.
- 새 미션 추천 역시 로비를 여는 것만으로 실행되지 않으며 `AI에게 새 미션 추천받기`를 눌러야 시작됩니다.

## 배포자·프론트엔드 개발자 설정

일반 사용자에게는 아래 설정 파일을 배포하지 않습니다. 서비스 운영자만 Supabase 프로젝트와 Google OAuth, 아래 DB 스키마를 준비합니다.

```bash
cp frontend/.env.example frontend/.env
```

```dotenv
VITE_SUPABASE_URL=https://your-project.supabase.co
VITE_SUPABASE_ANON_KEY=sb_publishable_your_public_key
```

`VITE_SUPABASE_ANON_KEY`에는 publishable 키 또는 기존 anon 키를 넣습니다. 빌드는 관리자 키를 거부하고, 두 공개 값만 `/clinic-config.json`에 기록합니다. Vercel에도 같은 두 환경변수를 등록합니다. DB의 RLS와 `authenticated` 역할 권한을 적용한 뒤 **새 frontend를 먼저 배포**해야 설정 없는 clinic 시작이 가능합니다. 이전 배포에는 공개 설정 파일이 없으므로 clinic이 원인을 표시하고 종료합니다.

개발은 `npm --prefix frontend ci` 후 `make dev`로 시작합니다. 개발용 clinic은 로컬 Vite의 공개 설정을 읽습니다. backend만 따로 실행하려면 먼저 `make dev-fe`, 다른 터미널에서 `make dev-be`를 실행합니다.

## 선택형 로컬 설정

기본 사이트 외 자체 배포를 사용하는 경우에는 `CLINIC_SITE_URL=https://your-site.example ./run.sh ~/learning`으로 사이트만 지정합니다. 해당 origin은 HTTP와 WebSocket에 함께 허용됩니다. HTTPS 사이트 또는 개발용 loopback HTTP만 지원하며, 다른 사이트로의 리다이렉트는 따르지 않습니다.

Codex 모델·Gemini·포트 등의 고급 설정이 필요한 경우에만 `config.toml.example` 또는 `.env.example`을 복사합니다. 개발자는 `SUPABASE_URL`, `SUPABASE_ANON_KEY`를 모두 지정해 공개 설정 자동 조회를 생략할 수도 있습니다. 로컬의 명시적 공개 설정은 배포 사이트 설정보다 우선합니다.

이 Mac의 접근 계정을 추가로 제한하려면 선택형 `.env.local`에 `ALLOWED_USER_EMAILS=first@example.com,second@example.com`을 설정합니다. 기본값은 배포된 Supabase 프로젝트에서 인증된 사용자입니다. 이메일과 기존 `ALLOWED_USER_IDS`는 둘 중 하나가 일치하면 통과합니다. 이는 로컬 접근 제한이며 서비스 전체의 가입 제한은 운영자가 Supabase Auth에서 관리합니다.

## Supabase 스키마

```sql
create table user_settings (
  user_id uuid primary key references auth.users(id) on delete cascade,
  base_dir text not null default '',
  language text not null default 'Go',
  skill_level text not null default 'normal',
  updated_at timestamptz not null default now()
);

create table daily_missions (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null references auth.users(id) on delete cascade,
  date date not null,
  topic text not null,
  slug text not null,
  project_dir text not null,
  status text not null default 'active',
  created_at timestamptz not null default now()
);

-- 기존 설치는 먼저 중복을 확인하세요. 결과가 있으면 백업 후 아래 CTE로
-- completed 행을 우선 보존하고 나머지 중복만 정리합니다.
select user_id, project_dir, count(*)
from daily_missions
group by user_id, project_dir
having count(*) > 1;

with ranked as (
  select id, row_number() over (
    partition by user_id, project_dir
    order by (status = 'completed') desc nulls last,
             created_at desc nulls last, id
  ) as duplicate_rank
  from daily_missions
)
delete from daily_missions d
using ranked r
where d.id = r.id and r.duplicate_rank > 1;

-- /api/daily/finalize의 동시·재시도를 한 행으로 직렬화합니다.
create unique index if not exists daily_missions_user_project_dir_uidx
  on daily_missions (user_id, project_dir);

grant usage on schema public to authenticated;
grant select, insert, update, delete on user_settings, daily_missions to authenticated;

alter table user_settings enable row level security;
alter table daily_missions enable row level security;

create policy "user settings are private" on user_settings
  for all using (auth.uid() = user_id) with check (auth.uid() = user_id);
create policy "daily missions are private" on daily_missions
  for all using (auth.uid() = user_id) with check (auth.uid() = user_id);
```

Supabase Authentication에서 Google provider를 활성화하고 Google OAuth client ID/secret을 등록하세요. URL Configuration의 Site URL과 Redirect URLs에는 Vercel production URL을 등록합니다. clinic은 선택형 `ALLOWED_USER_EMAILS` 또는 기존 `ALLOWED_USER_IDS`로 해당 Mac의 접근을 추가 제한합니다. 단일 값용 `ALLOWED_USER_EMAIL`과 `ALLOWED_USER_ID`도 지원합니다.

## 개발과 빌드

```bash
npm --prefix frontend ci
make dev

make test
make build
./run.sh
```

인자를 생략하면 학습 프로젝트는 Git에서 제외된 저장소의 `data/` 아래에 저장됩니다. 다른 위치를 사용하려면 `./run.sh ~/learning` 또는 `make dev DIR=~/learning`처럼 지정합니다. 개발 화면은 `http://localhost:5173`, clinic은 `http://127.0.0.1:47291`입니다. 자체 배포 도메인은 `CLINIC_SITE_URL`로 지정하면 공개 설정 조회와 Origin 허용에 함께 적용됩니다.

## Vercel 배포

Vercel 프로젝트 설정은 다음과 같습니다.

| 항목 | 값 |
|---|---|
| Root Directory | `frontend` |
| Install Command | `npm ci` |
| Build Command | `npm run build` |
| Output Directory | `dist` |

빌드는 `/clinic-config.json`도 생성합니다. 이 파일은 SPA rewrite에서 제외하고 캐시하지 않습니다. `frontend/vercel.json`이 SPA rewrite와 정적 자산 캐시·보안 헤더를 설정합니다. Vercel에는 `VITE_SUPABASE_URL`, `VITE_SUPABASE_ANON_KEY`만 등록합니다. 기본 clinic 주소는 `http://127.0.0.1:47291`이므로 Vercel에서 `VITE_LOCAL_URL`을 따로 설정하지 않습니다. CSP는 `127.0.0.1`과 `localhost`의 명시적 포트를 허용합니다. 포트를 변경할 때는 `config.toml`의 `[server].port`와 빌드 시 `VITE_LOCAL_URL`만 같은 loopback 주소·포트로 맞추면 되며, `frontend/vercel.json`은 수정하지 않습니다.

## 확인된 문제와 조치

| 우선순위 | 기존 문제 | 조치 |
|---|---|---|
| P0 | 홈서버와 로컬 서버를 동시에 유지해야 함 | Vercel 정적 UI + 단일 clinic으로 통합 |
| P0 | 로컬 REST/terminal/LSP가 인증 없이 열림 | Supabase JWT, 고정 Origin·loopback Host 검증, WS subprotocol 인증 |
| P0 | API·AI 파일명이 BaseDir 밖을 읽거나 쓸 수 있음 | canonical path/symlink 검사와 파일 수·크기 제한 |
| P0 | 생성 코드가 임의 테스트로 자동 실행됨 | 자동 실행 기본 비활성, 명시적 Run/Test만 허용 |
| P1 | Gemini 전용이고 preview 모델이 오래됨 | Codex CLI 기본, Gemini stable 선택형 |
| P1 | 자유 형식 JSON·TUTORSYS·프롬프트 인젝션 가능 | JSON Schema, 커리큘럼 전이·언어별 실행 계약 의미 검증, 불신 데이터 경계, 입력 예산 적용 |
| P1 | 모델의 완료 문구만으로 다음 단계가 열림 | 전체 테스트 성공과 HOLE/BUG/END 0개를 서버가 직접 확인 |
| P1 | DB 미션 생성과 로컬 파일 생성 사이에 상태가 갈림 | 파일 setup 성공 뒤 멱등 `daily/finalize`로 active 행 확정 |
| P1 | 빠른 탭 전환 시 자동 저장이 다른 파일을 덮어쓸 수 있음 | 파일별 저장 큐, 전환 시 flush, 실패 toast 적용 |
| P1 | 한 글자·공백 입력만으로 AI 피드백이 자동 호출되고 화면 탭을 빼앗음 | 자동 저장과 검토를 분리하고 의미 변경 후 명시적 검토, 최신 revision만 반영, 중단·unread 표시 적용 |
| P1 | loopback·인증 오류가 영구 로딩이나 무응답으로 보임 | 오류 종류/상태별 안내, 재연결·계정 전환, 스트림 `finally` 정리 적용 |
| P1 | 파일 rename/delete 실패를 성공처럼 처리함 | 공통 JWT API와 HTTP 상태 검증 후에만 탭·트리 갱신 |
| P1 | symlink 삭제·rename이 실제 대상을 지우거나 기존 파일을 덮음 | destructive 경로의 symlink 거부, rename 충돌 409 처리 |
| P1 | 스냅샷 복원이 새 단계 파일을 남기거나 현재 변경을 경고 없이 되돌림 | 관리 파일 exact restore, 비관리 파일 보존, 사용자 확인 적용 |
| P1 | 실행 자식 프로세스가 clinic 비밀 환경변수를 상속함 | Codex·run/test·watcher·terminal·LSP·gopls 공통 secret scrub 적용 |
| P1 | 초기 JS 973KB | route/component lazy loading으로 최대 chunk 약 412KB |
| P1 | 정적 이미지 약 49MB | 실제 사용 이미지 WebP 변환 후 약 440KB |
| P1 | watcher가 의존성·숨김 폴더까지 순회 | 공통 ignore, 새 디렉터리·삭제 diff 처리 |

## 보안 동작

- clinic은 `127.0.0.1`에만 바인딩합니다.
- `/health` 외 REST 요청은 로그인 JWT가 필요합니다. ES256은 공개 JWKS, 기존 HS256은 Supabase Auth의 `/auth/v1/user`로 검증하며 비밀키를 설치하지 않습니다. HS256은 인증 요청마다 네트워크 검증이 추가됩니다.
- DB 요청은 공개 키와 해당 요청의 사용자 JWT만 사용합니다. 사용자 JWT는 프로세스 전역에 저장하지 않습니다.
- WebSocket JWT는 URL/query가 아닌 subprotocol로 전달합니다.
- terminal/LSP 자식 프로세스에는 Gemini·Supabase·OpenAI 비밀 환경변수를 전달하지 않습니다.
- Codex 실행은 임시 빈 작업공간, read-only sandbox, 도구 비활성화, 단일 동시 실행, JSON Schema를 사용합니다.
- Vercel preview domain은 기본 허용하지 않습니다. production/custom domain을 명시적으로 등록하세요.

Safari는 HTTPS 페이지에서 평문 loopback 연결을 제한할 수 있으므로 현재 배포 구조는 Chrome을 기준으로 합니다.

공개 키·RLS 권한과 HS256 검증 방식은 [Supabase API 키 문서](https://supabase.com/docs/guides/api/api-keys), [JWT 검증 문서](https://supabase.com/docs/guides/auth/jwts)를 따릅니다.
