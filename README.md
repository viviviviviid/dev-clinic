# coding-tutor

AI 기반 코딩 튜터 플랫폼. Google 로그인 후 매일 AI가 학습 주제 3개를 제안하고, 선택한 주제로 프로젝트를 자동 생성합니다. 의도적으로 구멍(HOLE)과 버그(BUG)가 있는 코드를 제공하고, 파일 변경을 감지해 Gemini가 실시간 피드백을 스트리밍합니다.

## 아키텍처

두 개의 독립 바이너리로 완전 분리되어 있습니다.

```
tutor.abcfe.net  (홈서버, cmd/homeserver)
├── 프론트엔드 frontend/dist/ 서빙
├── /api/user/*, /api/daily/*, /api/chat, /api/explain
├── /api/project/nextstep|complete|delete
├── /api/ai/proxy  ← 로컬 watcher가 Gemini를 여기를 통해 호출
└── Gemini API key + Supabase credentials 전부 여기만

localhost:47291  (로컬 바이너리, cmd/clinic — 유저가 실행)
├── /api/fs/*               — 파일 R/W
├── /api/run, /api/test     — 코드 실행
├── /api/project/setup|apply-step|read-all|status|load|...
├── /ws                     — 파일 변경 감지 + AI 피드백 브로드캐스트
└── /ws/lsp, /ws/terminal
```

**로컬 바이너리에는 Supabase/Gemini 키가 없습니다.** 브라우저가 REMOTE(AI 생성) → LOCAL(파일 쓰기)를 오케스트레이션합니다.

## 사전 요구사항

| 도구 | 버전 | 용도 |
|------|------|------|
| Go | 1.21+ | 백엔드 서버 |
| Node.js | 18+ | 프론트엔드 빌드 |
| npm | 9+ | 패키지 관리 |

외부 서비스:
- **[Supabase](https://supabase.com)** — 인증(Google OAuth) + DB
- **[Google AI Studio](https://aistudio.google.com)** — Gemini API Key

## 빠른 시작

### 1. 저장소 클론

```bash
git clone https://github.com/coding-tutor/coding-tutor.git
cd coding-tutor
```

### 2. 홈서버 설정

```bash
cp homeserver.toml.example homeserver.toml
```

`homeserver.toml` 편집:

```toml
[gemini]
api_key = "AIzaSy..."                          # Google AI Studio에서 발급
model   = "gemini-2.0-flash"

[server]
port = "8080"

[supabase]
url              = "https://xxxx.supabase.co"  # Supabase 프로젝트 URL
anon_key         = "eyJ..."                    # Settings > API > anon key
service_role_key = "eyJ..."                    # Settings > API > service_role key
jwt_secret       = "your-jwt-secret"           # Settings > API > JWT Secret
```

### 3. 프론트엔드 설정

```bash
cp frontend/.env.example frontend/.env
```

`frontend/.env` 편집:

```env
VITE_SUPABASE_URL=https://xxxx.supabase.co
VITE_SUPABASE_ANON_KEY=eyJ...
VITE_REMOTE_URL=https://tutor.abcfe.net   # 홈서버 URL (로컬 개발 시 http://localhost:8080)
VITE_LOCAL_URL=http://localhost:47291
```

### 4. Supabase DB 초기화

Supabase 대시보드 → SQL Editor에서 아래 쿼리 실행:

```sql
CREATE TABLE user_settings (
  user_id      UUID PRIMARY KEY REFERENCES auth.users(id) ON DELETE CASCADE,
  base_dir     TEXT NOT NULL,
  language     TEXT NOT NULL DEFAULT 'go',
  skill_level  TEXT NOT NULL DEFAULT 'normal',
  updated_at   TIMESTAMPTZ DEFAULT NOW()
);

-- UNIQUE 제약 없음 — 하루에 여러 미션 허용
CREATE TABLE daily_missions (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      UUID REFERENCES auth.users(id) ON DELETE CASCADE,
  date         DATE NOT NULL,
  topic        TEXT NOT NULL,
  slug         TEXT NOT NULL,
  project_dir  TEXT NOT NULL,
  status       TEXT DEFAULT 'active',
  created_at   TIMESTAMPTZ DEFAULT NOW()
);
```

### 5. Supabase Google OAuth 활성화

Supabase 대시보드 → Authentication → Providers → Google 활성화 후 Client ID/Secret 입력.

Redirect URL: `https://xxxx.supabase.co/auth/v1/callback`

### 6. 의존성 설치 및 실행

```bash
# 프론트엔드 의존성 설치
cd frontend && npm install && cd ..

# 개발 모드 (터미널 3개)
make dev-homeserver          # 홈서버 :8080
make dev-be DIR=~/learning   # 로컬 서버 :47291 (학습 파일 저장 디렉토리 지정)
make dev-fe                  # 프론트엔드 :5173 (Vite dev server)
```

브라우저에서 `http://localhost:5173` 접속.

> **로컬 서버의 base_dir** 은 config 파일이 아닌 CLI 인자로 지정합니다.
> `DIR` 생략 시 현재 디렉토리, `BASE_DIR` 환경변수도 사용 가능합니다.

## 프로덕션 빌드

```bash
make build              # 로컬 바이너리: bin/coding-tutor (build-fe 포함)
make build-homeserver   # 홈서버 바이너리: bin/coding-tutor-server (build-fe 포함)

# 실행
./bin/coding-tutor-server          # 홈서버 :8080
./bin/coding-tutor ~/learning      # 로컬 서버 :47291
```

환경변수로 homeserver.toml을 오버라이드할 수 있습니다:

```bash
GEMINI_API_KEY=... SUPABASE_URL=... ./bin/coding-tutor-server
```

## LSP 자동완성 (선택)

에디터에서 `fmt.` 입력 시 자동완성을 사용하려면 언어 서버를 설치합니다. 미설치 시에도 기본 기능은 동작하며 정규식 기반 fallback이 사용됩니다.

```bash
# Go
go install golang.org/x/tools/gopls@latest

# TypeScript / JavaScript
npm install -g typescript-language-server typescript

# Python
pip install python-lsp-server

# Rust
rustup component add rust-analyzer
```

## 주요 기능

- **매일 미션**: AI 간호사 채팅으로 학습 주제 3개 제안. 하루에 여러 미션 추가 가능
- **실시간 피드백**: 파일 저장 시 Gemini가 코드 변경을 분석해 WebSocket으로 피드백 스트리밍
- **HOLE / BUG 마커**: 구현해야 할 부분(노란 하이라이트)과 의도된 버그(빨간 하이라이트)
- **뉴비 모드**: 인라인 퀴즈, 단계적 힌트, AI 오답 설명, HOLE 순차 잠금
- **LSP 자동완성**: gopls 등 언어 서버 연동, `Cmd+S`로 포맷 & 저장
- **단계 진행**: AI가 다음 단계 커리큘럼과 코드를 자동 생성, 스냅샷으로 이전 단계 복원 가능
- **Cmd+P / Cmd+Shift+F**: 파일 빠른 열기 / 전체 텍스트 검색

## 환경변수 목록

| 변수 | homeserver.toml 키 | 설명 |
|------|-------------------|------|
| `GEMINI_API_KEY` | `gemini.api_key` | Gemini API 키 |
| `GEMINI_MODEL` | `gemini.model` | 사용할 모델명 |
| `PORT` | `server.port` | 홈서버 포트 (기본 8080) |
| `SUPABASE_URL` | `supabase.url` | Supabase 프로젝트 URL |
| `SUPABASE_ANON_KEY` | `supabase.anon_key` | Supabase anon key |
| `SUPABASE_SERVICE_ROLE_KEY` | `supabase.service_role_key` | Supabase service role key |
| `SUPABASE_JWT_SECRET` | `supabase.jwt_secret` | Supabase JWT secret |
