# coding-tutor

개인용 AI 코딩 튜터입니다. Vercel은 정적 React 화면만 호스팅하고, 파일 접근·코드 실행·AI 호출은 Mac에서 실행하는 단일 `clinic` 프로세스가 담당합니다.

## 확정 아키텍처

```text
Vercel (frontend/dist)
  ├─ Supabase Auth: 이메일 매직링크 로그인
  └─ HTTPS → http/ws://127.0.0.1:47291
                  clinic
                  ├─ Codex CLI 또는 Gemini
                  ├─ 프로젝트 파일 / watcher / snapshot
                  ├─ run / test / LSP / terminal
                  └─ Supabase REST: 설정·미션 기록
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

## 준비

- Go 1.25+
- Node.js 24 LTS 및 npm
- Codex CLI와 ChatGPT 로그인(기본 모드)
- Supabase 프로젝트(Email Auth, 설정·미션 DB)
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

## 로컬 설정

```bash
cp config.toml.example config.toml
cp frontend/.env.example frontend/.env
```

`config.toml`에는 Supabase 서버 자격 증명을 넣습니다. `service_role_key`와 Gemini 키는 절대 `frontend/.env` 또는 `VITE_*` 변수에 넣지 마세요.

```toml
ai_provider = "codex"

[codex]
executable = "" # 비우면 PATH와 ~/.local/bin/codex를 검색
model = ""      # 비우면 Codex CLI 기본 모델

[server]
port = "47291"

[supabase]
url = "https://your-project.supabase.co"
anon_key = "your-anon-key"
service_role_key = "your-service-role-key"
jwt_secret = "" # Supabase가 HS256 토큰을 쓰는 경우에만 필요
```

`frontend/.env`에는 공개 가능한 anon 값만 둡니다.

```dotenv
VITE_SUPABASE_URL=https://your-project.supabase.co
VITE_SUPABASE_ANON_KEY=your-anon-key
```

혼자만 쓰도록 계정을 고정하려면 clinic 실행 환경에 Supabase 사용자 UUID를 지정합니다.

```bash
ALLOWED_USER_ID=your-user-uuid ./bin/clinic ~/learning
```

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

alter table user_settings enable row level security;
alter table daily_missions enable row level security;

create policy "user settings are private" on user_settings
  for all using (auth.uid() = user_id) with check (auth.uid() = user_id);
create policy "daily missions are private" on daily_missions
  for all using (auth.uid() = user_id) with check (auth.uid() = user_id);
```

Supabase Authentication의 URL Configuration에서 Vercel production URL을 Site URL과 Redirect URLs에 등록하세요. 혼자 쓰는 설치는 사용할 이메일 계정을 먼저 만든 뒤 신규 가입을 비활성화하고, 해당 사용자 UUID를 `ALLOWED_USER_ID`로 고정합니다.

## 개발과 빌드

```bash
npm --prefix frontend ci
make dev DIR=~/learning

make test
make build
./bin/clinic ~/learning
```

개발 화면은 `http://localhost:5173`, clinic은 `http://127.0.0.1:47291`입니다. Vercel 기본 도메인(`https://<project>.vercel.app`)이나 다른 custom domain을 쓰면 해당 production origin을 `ALLOWED_ORIGINS=https://your-domain.example` 형태로 clinic 실행 환경에 추가합니다.

## Vercel 배포

Vercel 프로젝트 설정은 다음과 같습니다.

| 항목 | 값 |
|---|---|
| Root Directory | `frontend` |
| Install Command | `npm ci` |
| Build Command | `npm run build` |
| Output Directory | `dist` |

`frontend/vercel.json`이 SPA rewrite와 정적 자산 캐시·보안 헤더를 설정합니다. Vercel에는 `VITE_SUPABASE_URL`, `VITE_SUPABASE_ANON_KEY`만 등록합니다. 기본 clinic 주소는 `http://127.0.0.1:47291`이므로 Vercel에서 `VITE_LOCAL_URL`을 따로 설정하지 않습니다. CSP는 `127.0.0.1`과 `localhost`의 명시적 포트를 허용합니다. 포트를 변경할 때는 `config.toml`의 `[server].port`와 빌드 시 `VITE_LOCAL_URL`만 같은 loopback 주소·포트로 맞추면 되며, `frontend/vercel.json`은 수정하지 않습니다.

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
- `/health` 외 REST 요청은 로그인 JWT가 필요합니다.
- WebSocket JWT는 URL/query가 아닌 subprotocol로 전달합니다.
- terminal/LSP 자식 프로세스에는 Gemini·Supabase·OpenAI 비밀 환경변수를 전달하지 않습니다.
- Codex 실행은 임시 빈 작업공간, read-only sandbox, 도구 비활성화, 단일 동시 실행, JSON Schema를 사용합니다.
- Vercel preview domain은 기본 허용하지 않습니다. production/custom domain을 명시적으로 등록하세요.

Safari는 HTTPS 페이지에서 평문 loopback 연결을 제한할 수 있으므로 현재 배포 구조는 Chrome을 기준으로 합니다.
