# coding-tutor

AI 코딩 튜터 플랫폼. Google 로그인 → 개인 설정 → 매일 AI가 주제 3개 제안 → 선택한 주제로 프로젝트 자동 생성. 의도적으로 구멍/버그가 있는 코드를 학습자에게 제공하고, 파일 변경을 감지해 Gemini가 실시간 피드백을 스트리밍한다.

## 아키텍처 개요

두 개의 독립 바이너리로 완전 분리:

```
tutor.abcfe.net  (홈서버, cmd/homeserver)
├── 프론트엔드 frontend/dist/ 서빙
├── /api/user/*, /api/daily/*, /api/chat, /api/explain
├── /api/project/create|confirm|nextstep|complete|delete
├── /api/ai/proxy  ← 로컬 watcher가 Gemini를 여기를 통해 호출
└── Gemini API key + Supabase credentials 전부 여기만

localhost:47291  (로컬 바이너리, cmd/clinic — 유저가 실행)
├── /api/fs/*               — 파일 R/W
├── /api/run, /api/test     — 코드 실행
├── /api/project/setup|apply-step|read-all|status|load|...  — 로컬 프로젝트 상태
├── /ws                     — 파일 변경 감지 + AI 피드백 브로드캐스트
└── /ws/lsp, /ws/terminal
```

**로컬 바이너리가 아는 것: 없음.** Supabase/Gemini 키 제로.
브라우저가 REMOTE(AI 생성) → LOCAL(파일 쓰기) 오케스트레이션.

## 실행

```bash
# 홈서버 (최초 1회 설정)
cp homeserver.toml.example homeserver.toml
# homeserver.toml에 gemini.api_key, supabase.* 입력

cp frontend/.env.example frontend/.env
# frontend/.env에 VITE_SUPABASE_URL, VITE_SUPABASE_ANON_KEY 입력

# 개발
make dev-homeserver          # 홈서버 :8080
make dev-be DIR=~/learning   # 로컬 서버 :47291 (유저 측)
make dev-fe                  # Vite 프론트엔드 :5173

# 또는 직접 실행
go run ./cmd/homeserver/main.go
go run ./cmd/clinic/main.go ~/learning

브라우저: http://localhost:5173 (개발) 또는 https://tutor.abcfe.net (프로덕션)
```

## 프로젝트 구조

```
coding-tutor/
├── cmd/
│   ├── server/main.go       # 로컬 바이너리 진입점 — config 로드(선택) → CLI 인자로 base_dir → 로컬 API 마운트
│   │                        # auth 미들웨어 없음. CORS: tutor.abcfe.net + localhost만 허용. 127.0.0.1:47291 바인딩
│   └── homeserver/main.go   # 홈서버 진입점 — homeserver.toml 로드 → ai.Init() → auth 미들웨어 → 모든 API + 프론트 서빙
├── internal/
│   ├── config/config.go         # 로컬 서버용 config (Port + BaseDir만). Supabase/Gemini 섹션 없음
│   ├── ai/client.go             # Gemini API. InitProxy(url): 런타임에 proxy URL 받아 초기화 (로컬 서버용)
│   │                            # GenerateCodeFiles: existingFiles 있을 때 함수 시그니처/패키지명 유지 규칙 강제 (5가지 하드 룰)
│   │                            #   newbie HOLE 주석: 자연어 설명만, 코드 조각/패턴 포함 금지
│   │                            #   BUG: 실제 호출 함수 안에 인라인. BuggyXxx() 별도 함수 생성 금지
│   │                            # GenerateQuizData: HOLE/BUG별 question + hints 3단계만 (options/correctCode 없음)
│   │                            # NurseChat: 간호사 페르소나 → [TOPICS] 블록으로 주제 3개 제안
│   │                            # GenerateNextStep: "누적 확장" 방식 — 단계 수 자유 결정, 3단계 작업 프로세스
│   ├── localapi/project.go      # 로컬 서버 전용 project 핸들러
│   │                            # SetupProject: AI 생성 파일 수신 → 디스크 쓰기 → go mod → ai.InitProxy → watcher 시작
│   │                            # ApplyStep: snapshot.Save → 새 파일 쓰기 → project.Global 갱신 → watcher 재시작
│   │                            # ReadAllFiles: 현재 파일 전체 반환 (브라우저가 REMOTE nextstep 요청에 포함)
│   │                            # LoadProject: ai_proxy_url + token 수신 → ai.InitProxy + ai.Global.SetToken
│   │                            # GetProjectStatus, GetQuiz, SaveQuiz, ListSnapshots, RestoreSnapshot, StopWatcher, DeleteProjectFiles
│   ├── api/
│   │   ├── user.go              # GET/PUT /api/user/settings — 홈서버 전용
│   │   ├── daily.go             # GET /api/daily, /history, POST /confirm, /confirm-stream, /nurse-chat — 홈서버 전용
│   │   │                        # confirm-stream done 이벤트: {dir_suffix, files, curriculum, skill_level, language} 반환
│   │   │                        #   (파일 직접 쓰지 않음 — 브라우저가 LOCAL /api/project/setup으로 전달)
│   │   ├── project.go           # 홈서버 전용 project 핸들러
│   │   │                        # AdvanceToNextStep: body로 {curriculum, current_files, skill_level} 수신 → {new_curriculum, new_files, quiz_data} 반환
│   │   │                        #   (파일 R/W 없음 — 브라우저가 LOCAL apply-step으로 전달)
│   │   │                        # CompleteProject: body의 {project_dir}으로 Supabase status → 'completed' (dir_suffix 또는 full path 양쪽 매칭)
│   │   │                        # DeleteProject: Supabase DB만 정리 (파일 삭제는 LOCAL DELETE /api/project/files)
│   │   ├── fs.go                # GET /api/fs/list|read|validate|git-diff|search/files|search/content, POST /api/fs/write|rename, DELETE /api/fs/delete
│   │   ├── run.go               # GET /api/run, /api/test — 코드 실행 SSE
│   │   ├── explain.go           # POST /api/explain — 오답 AI 설명 SSE (홈서버 or 로컬 — ai.Global 사용)
│   │   ├── chat.go              # POST /api/chat — AI 채팅
│   │   └── goto.go              # POST /api/goto — 정의 이동
│   ├── lsp/proxy.go             # GET /ws/lsp — WS↔LS stdio 브리지. resolveBin()으로 gopls 등 탐색
│   ├── middleware/auth.go       # JWT 검증 (Supabase HS256/ES256) — 홈서버에서만 사용
│   ├── supabase/client.go       # Supabase REST API 헬퍼 (service_role_key)
│   ├── snapshot/snapshot.go     # Save/Restore/List — projectDir/.snapshots/{stepLabel}/
│   ├── ws/hub.go                # WebSocket 허브 — BroadcastTestResult(passed, summary) 포함
│   ├── watcher/watcher.go       # fsnotify + debounce 3초 + 강제싱크 2분 + 테스트 실행
│   │                            # ai.Global.ProxyStream()으로 홈서버 /api/ai/proxy 경유 Gemini 호출
│   ├── diff/diff.go             # go-diff 래퍼
│   └── project/project.go       # TUTORSYS.md 파싱 & 전역 상태. parseProgress(): totalSteps/currentStepNum 반환
├── frontend/src/
│   ├── lib/api.ts               # REMOTE = window.location.origin (홈서버), LOCAL = http://localhost:47291
│   │                            # WS_BASE = ws://localhost:47291, AI_PROXY_URL = REMOTE + /api/ai/proxy
│   ├── lib/supabase.ts          # Supabase 클라이언트
│   ├── lib/lspClient.ts         # LSP JSON-RPC 2.0 싱글톤 (/ws/lsp)
│   ├── hooks/
│   │   ├── useProject.ts        # fetchRemote(auth헤더)/fetchLocal(no-auth) 분기
│   │   │                        # confirmDailyMissionStream: REMOTE SSE → LOCAL /api/project/setup
│   │   │                        # advanceToNextStep: LOCAL read-all → REMOTE nextstep → LOCAL apply-step
│   │   │                        # completeMission: REMOTE complete + LOCAL stop-watcher 병렬
│   │   │                        # deleteProject: LOCAL delete/files → REMOTE delete
│   │   └── useWebSocket.ts      # WS 연결·재연결. test_result → setTestResult, error → addToast
│   ├── components/
│   │   ├── Auth/                # Google 로그인
│   │   ├── Settings/            # 언어/수준 설정 (base_dir 표시 없음 — 로컬 서버 CLI 인자 전용)
│   │   ├── Dashboard/           # 간호사 채팅 → [TOPICS] 파싱 → 주제 선택 → confirm-stream SSE
│   │   ├── FileTree/            # 파일 탐색기. 우클릭: 이름 변경/삭제 → LOCAL fs API
│   │   ├── QuickOpen/           # Cmd+P 파일명 fuzzy 검색 → LOCAL fs/search/files
│   │   ├── SearchPanel/         # Cmd+Shift+F 전체 텍스트 grep → LOCAL fs/search/content
│   │   ├── Toast/               # error/success/info, 4초 자동 닫기
│   │   ├── Confetti/            # Canvas 파티클, 단계완료+테스트통과 시 발동
│   │   ├── ProblemsPanel/       # LSP diagnostics 패널
│   │   └── Editor/
│   │       ├── index.tsx        # Monaco + 다중 탭 + HOLE/BUG decoration + LSP providers
│   │       │                    # git diff gutter: LOCAL /api/fs/git-diff
│   │       ├── QuizOverlay.tsx  # 뉴비 힌트 오버레이 — glyph 버튼 → view zone 카드. 직접 타이핑 후 확인 → 에디터 삽입
│   │       └── ConceptPanel.tsx # 개념 설명 슬라이드 패널
│   │   └── FeedbackPanel/       # AI 피드백 스트리밍. stepComplete && testResult?.passed → "다음 단계로" 활성
│   └── store/index.ts           # Zustand 전역 상태
├── config.toml.example          # 로컬 서버 설정 템플릿 (port만, 없어도 동작)
├── homeserver.toml.example      # 홈서버 설정 템플릿 (Gemini + Supabase)
├── frontend/.env.example        # VITE_SUPABASE_*, VITE_REMOTE_URL, VITE_LOCAL_URL
└── Makefile
```

## 설정

### 로컬 서버 (config.toml — 선택사항)

```toml
# 없어도 됨. port 기본값 47291
[server]
port = "47291"
```

`base_dir`은 config에 없고 **CLI 인자 → `BASE_DIR` 환경변수 → 현재 디렉토리(`.`)** 순서:
```bash
go run ./cmd/clinic ~/learning
BASE_DIR=~/learning go run ./cmd/clinic
go run ./cmd/clinic   # 현재 디렉토리
```

### 홈서버 (homeserver.toml)

```toml
[gemini]
api_key = "AIzaSy..."
model   = "gemini-2.0-flash"

[server]
port = "8080"

[supabase]
url              = "https://your-project.supabase.co"
anon_key         = "..."
service_role_key = "..."
jwt_secret       = "..."
```

환경변수 오버라이드: `GEMINI_API_KEY`, `GEMINI_MODEL`, `PORT`, `SUPABASE_URL`, `SUPABASE_ANON_KEY`, `SUPABASE_SERVICE_ROLE_KEY`, `SUPABASE_JWT_SECRET`

## Supabase DB 스키마

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
  project_dir  TEXT NOT NULL,   -- dir_suffix만 저장 (e.g. "250317-HelloGo"), 로컬 BaseDir과 무관
  status       TEXT DEFAULT 'active',
  created_at   TIMESTAMPTZ DEFAULT NOW()
);
```

## 핵심 데이터 흐름

```
프로젝트 생성 (confirmDailyMissionStream)
  1. REMOTE POST /api/daily/confirm-stream (SSE)
     → AI 커리큘럼·코드·퀴즈 생성
     → done 이벤트: {dir_suffix, files, curriculum, skill_level, language}
  2. LOCAL POST /api/project/setup
     → 파일 디스크 쓰기, go mod, ai.InitProxy(REMOTE/api/ai/proxy), watcher 시작
  3. LOCAL GET /api/project/status → UI 갱신

단계 진행 (advanceToNextStep)
  1. LOCAL GET /api/project/read-all → {files, curriculum}
  2. REMOTE POST /api/project/nextstep {curriculum, current_files, skill_level}
     → AI 다음 단계 커리큘럼·코드·퀴즈 생성 → {new_curriculum, new_files, quiz_data}
  3. LOCAL POST /api/project/apply-step
     → snapshot.Save → 파일 쓰기 → watcher 재시작
  4. LOCAL GET /api/project/status → UI 갱신

파일 변경 → AI 피드백
  → fsnotify → debounce 3초 → diff 계산 → 테스트 실행
  → allTestsPassed() → BroadcastTestResult
  → ai.Global.ProxyStream() → REMOTE /api/ai/proxy → Gemini
  → [STEP_COMPLETE] 하드 게이팅 (테스트 실패 시 토큰 제거)
  → WS 브로드캐스트 → FeedbackPanel 렌더링

미션 완료
  → REMOTE POST /api/project/complete {project_dir} (DB status → 'completed')
  → LOCAL POST /api/project/stop-watcher (병렬)

프로젝트 삭제
  → LOCAL DELETE /api/project/files {project_dir} (os.RemoveAll + watcher stop)
  → REMOTE DELETE /api/project {project_dir} (Supabase DB 정리)
```

## API 엔드포인트

### REMOTE (홈서버, auth 필수)

| Method | Path | 설명 |
|--------|------|------|
| GET | `/api/user/settings` | 사용자 설정 조회 |
| PUT | `/api/user/settings` | 사용자 설정 저장 (language, skill_level) |
| GET | `/api/daily` | `{ missions, topics }` 반환 |
| GET | `/api/daily/history` | 날짜별 히스토리 |
| POST | `/api/daily/confirm` | 주제 확정 (DB Insert) |
| POST | `/api/daily/confirm-stream` | 프로젝트 생성 SSE → done: `{dir_suffix, files, curriculum, skill_level, language}` |
| POST | `/api/daily/nurse-chat` | 간호사 채팅 SSE |
| POST | `/api/project/nextstep` | `{curriculum, current_files, skill_level}` → `{new_curriculum, new_files, quiz_data}` |
| POST | `/api/project/complete` | `{project_dir}` → DB status 'completed' |
| DELETE | `/api/project` | `{project_dir}` → Supabase DB 정리만 |
| POST | `/api/chat` | AI 채팅 |
| POST | `/api/explain` | 오답 AI 설명 SSE |
| POST | `/api/ai/proxy` | watcher용 Gemini 프록시 |

### LOCAL (로컬 서버, auth 없음)

| Method | Path | 설명 |
|--------|------|------|
| POST | `/api/project/setup` | `{dir_suffix, files, curriculum, skill_level, language, ai_proxy_url}` → 파일 쓰기 + watcher |
| POST | `/api/project/apply-step` | `{new_curriculum, new_files, quiz_data?}` → snapshot + 파일 쓰기 + watcher |
| GET | `/api/project/read-all` | 현재 파일 전체 + curriculum 반환 |
| GET | `/api/project/status` | 프로젝트 상태 |
| POST | `/api/project/load` | `{project_dir, ai_proxy_url, token}` → 기존 프로젝트 로드 |
| POST | `/api/project/stop-watcher` | watcher 중지 |
| DELETE | `/api/project/files` | `{project_dir}` → os.RemoveAll + watcher stop |
| GET | `/api/project/snapshots` | 스냅샷 목록 |
| POST | `/api/project/snapshot/restore` | `{step}` 복원 |
| GET | `/api/quiz` | quiz.json 반환 |
| GET | `/api/fs/list\|read\|validate\|git-diff\|search/files\|search/content` | 파일 시스템 |
| POST | `/api/fs/write\|rename` | 파일 쓰기/이름 변경 |
| DELETE | `/api/fs/delete` | 파일/디렉토리 삭제 |
| GET | `/api/run` | 코드 실행 SSE |
| GET | `/api/test` | 테스트 실행 SSE |
| POST | `/api/goto` | 정의 이동 |
| WS | `/ws` | 피드백 스트리밍 |
| WS | `/ws/terminal` | 터미널 |
| WS | `/ws/lsp?lang=go&root=...` | LSP 프록시 (auth 없음 — token 쿼리 파라미터) |

## WebSocket 메시지 형식

```json
{"type": "feedback_start"}
{"type": "feedback_chunk", "content": "..."}
{"type": "feedback_end"}
{"type": "sync_status", "last_sync": "RFC3339", "changed": true}
{"type": "test_result", "passed": true, "summary": "ok  coding-tutor 0.123s"}
{"type": "error", "error": "..."}
```

## TUTORSYS.md 구조

```markdown
# TUTORSYS
## 학습자 목표
## 언어 & 환경
## 학습 수준           ← newbie | normal | experienced
## 최종 결과물
## 커리큘럼 단계       ← 주제 복잡도에 따라 2~8단계 유동
## 현재 단계
## 이 단계에서 추가하는 것
## 현재 과제
## 개념 설명
## 파일 구성
## 의도된 구멍 (HOLE)
## 의도된 버그 (BUG)
## 진행 기록
```

코드 파일 내 마커:
- `[TUTOR:HOLE]` — 구현 필요 (Monaco 노란 하이라이트)
- `[TUTOR:BUG]` — 의도된 버그 (Monaco 빨간 하이라이트)

AI 응답에 `[STEP_COMPLETE]` 포함 → stepComplete=true. `testResult?.passed=true`일 때만 "다음 단계로" 버튼 활성.

## 학습 수준 (skillLevel)

| 값 | 동작 |
|----|------|
| `newbie` | HOLE 블러 + 힌트 패널 (3단계 점진 공개) + 직접 타이핑. HOLE 순차 잠금. |
| `normal` | HOLE/BUG 하이라이트. 힌트+가이드 피드백. |
| `experienced` | 하이라이트. 간결 피드백(오류 위치만). |

### 뉴비 전용 기능

- **quiz.json**: confirm-stream/apply-step 시 각 HOLE/BUG에 `question` + `hints` 3단계 생성. 키: `filename:hole:0`
  - `options`, `correctCode` 없음 — 3지선다 방식 폐기
- **QuizOverlay (HintCard)**: glyph 마진 아이콘 → view zone 카드. 사용자가 직접 코드 타이핑 후 "확인" → 에디터에 삽입
  - 힌트는 하단에 숨겨두고 버튼으로 단계별 공개 (1→2→3)
  - `replaceHoleAtIndex`: HOLE 마커 + 힌트 주석 블록 전체를 사용자 코드로 교체
  - `replaceBugAtIndex`: BUG 마커 + 힌트 주석 + 버그 코드 라인 전체를 사용자 코드로 교체
- **BUG 구조**: 실제 호출되는 함수 안에 인라인으로 심음. 별도 BuggyXxx() 함수 없음
- **HOLE 순차 잠금**: 첫 번째 미해결 HOLE만 활성, 나머지 🔒

### 직접 fetch 시 인증 (로컬 서버 호출)

Editor의 `/api/run`은 LOCAL을 향하며 auth 헤더 불필요.

## 스냅샷 시스템

- 저장: `{projectDir}/.snapshots/{stepLabel}/` (소스 파일 + TUTORSYS.md + quiz.json)
- apply-step 전 자동 저장, FeedbackPanel 드롭다운으로 복원

## 빌드

```bash
make build              # 로컬 바이너리 bin/coding-tutor (build-fe 포함)
make build-homeserver   # 홈서버 bin/coding-tutor-server (build-fe 포함)
```

## 주요 의존성

**Go**: `gin`, `gorilla/websocket`, `fsnotify`, `go-diff`, `google.golang.org/genai`, `go-toml`, `golang-jwt/jwt/v4`

**Frontend**: `@monaco-editor/react`, `@supabase/supabase-js`, `zustand`, `react-markdown`, `remark-gfm`, `@xterm/xterm`

## LSP 설정

```bash
go install golang.org/x/tools/gopls@latest           # Go
npm install -g typescript-language-server typescript  # TypeScript/JS
pip install python-lsp-server                         # Python
rustup component add rust-analyzer                    # Rust
```

`/ws/lsp?lang=go&root=/abs/path` — 미설치 시 503, 프론트엔드 정규식 fallback.

## 알려진 주의사항

- **project_dir 저장 형식**: Supabase daily_missions.project_dir = dir_suffix만 (`250317-HelloGo`). 로컬 full path 모름
- **ai.InitProxy 타이밍**: setup/load 요청 시 ai_proxy_url 수신 후 초기화. watcher 시작 전에 반드시 호출됨
- **CORS 보안**: 로컬 서버는 `tutor.abcfe.net` + `localhost:` (콜론 포함) origin만 허용. JWT 검증 없음
- **daily_missions UNIQUE**: `UNIQUE(user_id, date)` 제약 없어야 하루 여러 미션 허용
- **Go go.mod**: apply-step/setup 시 `ensureGoMod()` 자동 실행 (비치명적)
- **[STEP_COMPLETE] 하드 게이팅**: watcher가 AI 응답 전체 수집 후 allTestsPassed()=false면 토큰 제거
- **changedFiles 절대경로**: FileTree·Editor 모두 fullPath 기준
- **.snapshots/ diff 오염**: watcher snapshot()·triggerSync() 두 Walk 모두 `.snapshots/` 가드 필요
- **LSP notifyClose 누락**: 탭 닫기 시 반드시 호출, 미호출 시 gopls 메모리 누수
- **Go 탭 설정**: `model.updateOptions({ tabSize: 4, insertSpaces: false })` — 전역 옵션 덮어씀
- **Git diff gutter**: `gitDiffCollectionRef` 별도 유지 (HOLE/BUG decoration과 충돌 방지)
- **Confetti 중복**: `prevStepComplete` ref로 false→true 엣지만 감지
- **WS 재연결 배너**: `wsStatus === 'reconnecting'` → App.tsx 상단 고정 배너
