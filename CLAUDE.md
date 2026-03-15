# coding-tutor

AI 코딩 튜터 플랫폼. Google 로그인 → 개인 설정 → 매일 AI가 주제 3개 제안 → 선택한 주제로 프로젝트 자동 생성. 의도적으로 구멍/버그가 있는 코드를 학습자에게 제공하고, 파일 변경을 감지해 Gemini가 실시간 피드백을 스트리밍한다.

## 실행

```bash
# 설정 파일 준비 (최초 1회)
cp config.toml.example config.toml
# config.toml에 gemini.api_key, supabase.* 입력

cp frontend/.env.example frontend/.env
# frontend/.env에 VITE_SUPABASE_URL, VITE_SUPABASE_ANON_KEY 입력

cd frontend && npm install && cd ..

# 개발 (터미널 2개)
make dev-be   # Go 백엔드 :8080
make dev-fe   # Vite 프론트엔드 :5173
```

브라우저: `http://localhost:5173`

## 프로젝트 구조

```
coding-tutor/
├── cmd/server/main.go           # 진입점 — config 로드 → ai.Init() → gin 라우터 (모든 API에 auth 미들웨어)
├── internal/
│   ├── config/config.go         # config.toml 파싱 + env 오버라이드 (Gemini, Server, Supabase)
│   ├── ai/client.go             # Gemini API (스트리밍 피드백, 커리큘럼/코드/퀴즈 생성, 다음단계 생성, 오답 설명, 데일리 주제 생성, 간호사 채팅)
│   │                            # GenerateCodeFiles: existingFiles 있을 때 함수 시그니처/패키지명 유지 규칙 강제 (5가지 하드 룰)
│   │                            # NurseChat: 간호사 페르소나로 대화 → 충분한 정보 모이면 [TOPICS] 블록으로 주제 3개 제안
│   │                            # GenerateDailyTopics: pastTopics 파라미터로 과거 주제 중복 방지
│   │                            # GenerateNextStep: "누적 확장" 방식 — 단계 수 자유 결정, 3단계 작업 프로세스(마커완성→새기능→테스트)
│   ├── lsp/proxy.go             # GET /ws/lsp — WS↔LS stdio 브리지 (gopls 등 언어 서버 프록시). resolveBin()으로 $GOPATH/bin, ~/.local/bin 등 탐색
│   │                            # stderr 고루틴: 언어 서버 stderr를 log.Printf("lsp %s: ...")로 서버 로그에 출력
│   ├── middleware/auth.go       # JWT 검증 (Supabase HS256/ES256) → user_id를 gin context에 저장. JWTKeyFunc 공개 (lsp 패키지 재사용)
│   ├── supabase/client.go       # Supabase REST API 헬퍼 (Get, Upsert, Insert, Patch, Delete) — service_role_key 사용
│   ├── snapshot/snapshot.go     # 스냅샷 시스템 — Save/Restore/List. projectDir/.snapshots/{stepLabel}/ 에 소스 파일 복사
│   ├── api/
│   │   ├── user.go              # GET/PUT /api/user/settings — user_settings 테이블 CRUD. 설정 없을 때 {} 반환
│   │   ├── daily.go             # GET /api/daily (missions+topics 항상 반환), GET /api/daily/history, POST /api/daily/confirm (Insert, 하루 여러 미션 허용)
│   │   │                        # POST /api/daily/nurse-chat — 간호사 채팅 SSE. 과거 주제 자동 조회해 중복 방지
│   │   ├── fs.go                # GET /api/fs/list|read|validate, POST /api/fs/write
│   │   ├── project.go           # POST /api/project/create|confirm|load|nextstep|complete, GET /api/project/status|quiz|snapshots
│   │   │                        # POST /api/project/snapshot/restore — AdvanceToNextStep 전 snapshot.Save 자동 호출
│   │   │                        # DELETE /api/project — 프로젝트 디렉토리 삭제 + DB 정리 (base_dir 내부만 허용)
│   │   │                        # POST /api/project/complete — 미션 status → 'completed' + watcher stop
│   │   │                        # ensureGoMod(): ConfirmProject/ConfirmDaily/AdvanceToNextStep 시 go mod init+tidy 자동 실행 (비치명적)
│   │   ├── run.go               # GET /api/run — 코드 실행 SSE 스트리밍. GET /api/test — 테스트 실행 SSE (언어별 커맨드 자동 선택)
│   │   ├── explain.go           # POST /api/explain — 오답 AI 설명 SSE 스트리밍
│   │   ├── chat.go              # POST /api/chat — AI 채팅
│   │   └── goto.go              # POST /api/goto — 정의 이동
│   ├── ws/hub.go                # WebSocket 허브 — 피드백 청크 브로드캐스트. BroadcastTestResult(passed, summary) 포함
│   ├── watcher/watcher.go       # fsnotify + debounce 3초 + 강제싱크 2분 + 테스트 실행
│   │                            # allTestsPassed()로 테스트 결과 파싱 → test_result WS 브로드캐스트
│   │                            # AI 응답 전체 수집 후 테스트 실패면 [STEP_COMPLETE] 제거 (하드 게이팅)
│   │                            # .snapshots/ 디렉토리는 snapshot()·triggerSync() Walk에서 제외 (diff 오염 방지)
│   │                            # Rename 이벤트 감지 포함 (fsnotify.Rename). snapshots+lastChangeTime Lock 단일화
│   ├── diff/diff.go             # go-diff 래퍼 (unified diff 생성)
│   └── project/project.go       # TUTORSYS.md 파싱 & 전역 상태 (skillLevel 포함)
├── frontend/src/
│   ├── lib/supabase.ts          # Supabase 클라이언트 (VITE_SUPABASE_URL, VITE_SUPABASE_ANON_KEY)
│   ├── lib/lspClient.ts         # LSP JSON-RPC 2.0 싱글톤 (WS over /ws/lsp). connect/disconnect/notifyOpen/notifyChange/notifySave/notifyClose/definition/completion/formatting/hover/signatureHelp/codeAction/rename
│   │                            # initialize() capabilities: signatureHelp·codeAction·rename·workspaceFolders·relatedInformation·didSave 포함
│   ├── components/
│   │   ├── Auth/                # Google 로그인 화면 (supabase.auth.signInWithOAuth)
│   │   ├── Settings/            # 최초 1회 설정 (base_dir, 언어, 수준) + 재진입 가능
│   │   ├── Dashboard/           # 대시보드 — 진행중 미션 목록, 새 프로젝트 생성, 최근 4주 활동 히트맵, 간호사 캐릭터 채팅 UI
│   │   │                        # 간호사 캐릭터: 상황별 이미지(greeting/pleasure/angry/shocked/punishment/same_day_failed/streak_failed/기다리네)
│   │   │                        # 간호사 채팅 → [TOPICS] 블록 파싱 → 주제 선택 UI로 전환. 화이트 테마.
│   │   ├── FileTree/            # 파일 탐색기 — 파일 클릭 시 addTab으로 탭 열기. changedFiles는 절대경로 기준
│   │   ├── ProblemsPanel/       # 에러/경고 진단 패널 — diagnostics 목록 표시, 클릭 시 addTab + pendingNavigate로 에디터 이동
│   │   ├── Editor/
│   │   │   ├── index.tsx        # Monaco Editor + 다중 탭 + HOLE/BUG decoration + LSP completion/definition/formatting/hover/signatureHelp/codeAction/rename provider
│   │   │   │                    # Cmd+S → formatDocument → 저장 + markFileSaved + lspClient.notifySave(). 브라우저 기본 save 차단
│   │   │   │                    # 탭 닫기 → lspClient.notifyClose() 호출 (gopls 메모리 해제)
│   │   │   │                    # Go 파일: model.updateOptions({ tabSize: 4, insertSpaces: false }) 자동 적용
│   │   │   │                    # onDidChangeMarkers → setDiagnostics. pendingNavigate 감지 → revealLineInCenter
│   │   │   │                    # 탭바: openTabs 목록, unsaved 표시(●), ✕ 닫기 버튼
│   │   │   ├── QuizOverlay.tsx  # 뉴비 퀴즈 오버레이. glyph 마진 아이콘 클릭 → view zone 카드
│   │   │   ├── ConceptPanel.tsx # 개념 설명 슬라이드 패널 (TUTORSYS.md 내용)
│   │   │   └── Editor.css
│   │   └── FeedbackPanel/       # AI 피드백 스트리밍 (마크다운)
│   │                            # stepComplete && testResult?.passed 일 때만 "다음 단계로" 버튼 활성
│   │                            # 테스트 미통과 시 안내 배너. 스냅샷 드롭다운으로 이전 단계 복원 가능
│   ├── hooks/
│   │   ├── useWebSocket.ts      # WS 연결·재연결·메시지 파싱. test_result 메시지 → setTestResult
│   │   └── useProject.ts        # API 호출 헬퍼 (모든 fetch에 auth 헤더 포함). listSnapshots/restoreSnapshot/completeMission 포함
│   └── store/index.ts           # Zustand 전역 상태
│                                # openTabs, addTab, closeTab, updateTabContent (다중 탭)
│                                # changedFiles, markFileSaved (unsaved 표시)
│                                # diagnostics, setDiagnostics (에러 패널)
│                                # pendingNavigate, setPendingNavigate (Problems→Editor 이동)
│                                # testResult, setTestResult (단계완료 게이팅)
│                                # snapshots, setSnapshots (스냅샷 목록)
│                                # projectComplete, setProjectComplete (전체 미션 완료 상태)
│                                # clearProjectStatus() (프로젝트 상태 초기화)
├── frontend/public/             # 간호사 캐릭터 이미지 (greeting/pleasure/angry/shocked/punishment/same_day_failed/streak_failed/기다리네.png)
├── config.toml.example          # 설정 템플릿 (커밋됨)
├── config.toml                  # 실제 설정 (gitignore)
├── frontend/.env.example        # Vite 환경변수 템플릿
├── frontend/.env                # 실제 Vite 환경변수 (gitignore)
└── Makefile
```

## 설정 (config.toml)

```toml
[gemini]
api_key = "AIzaSy..."
model   = "gemini-2.0-flash"

[server]
port = "8080"

[supabase]
url              = "https://your-project.supabase.co"
anon_key         = "your_supabase_anon_key"
service_role_key = "your_supabase_service_role_key"
jwt_secret       = "your_supabase_jwt_secret"
```

환경변수로 오버라이드 가능: `GEMINI_API_KEY`, `GEMINI_MODEL`, `PORT`, `SUPABASE_URL`, `SUPABASE_ANON_KEY`, `SUPABASE_SERVICE_ROLE_KEY`, `SUPABASE_JWT_SECRET`

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
  project_dir  TEXT NOT NULL,
  status       TEXT DEFAULT 'active',
  created_at   TIMESTAMPTZ DEFAULT NOW()
);
```

## 사용자 흐름

```
앱 접속
  → Supabase 세션 확인
    → 미로그인: Google 로그인 화면 (AuthScreen)
  → 로그인됨, user_settings 없음: 설정 화면 (SettingsScreen)
  → 설정 완료: 대시보드 (DashboardScreen)
      진행중인 미션 목록 표시 + 새 프로젝트 생성 + 최근 4주 활동 히트맵
      간호사 캐릭터 채팅 → 학습 주제 대화 → [TOPICS] 블록 감지 → 주제 3개 선택 UI
        → 미션 클릭 또는 신규 생성 → handleMissionReady → 에디터 화면
        → 전체 단계 완료 시 POST /api/project/complete → 미션 status='completed'
```

## 핵심 데이터 흐름

```
파일 변경
  → fsnotify 이벤트
  → 30초 ticker + 5초 쿨다운 후 triggerSync
  → diff 계산 (go-diff, 테스트 파일 제외)
  → 테스트 실행 (go test / pytest / cargo test 등)
  → allTestsPassed()로 결과 판별 → BroadcastTestResult(passed, summary)
  → Gemini API 응답 전체 수집
    → 테스트 실패면 [STEP_COMPLETE] 제거 (하드 게이팅)
  → WebSocket으로 브로드캐스트
  → FeedbackPanel 실시간 렌더링

단계 완료 게이팅
  → test_result WS 메시지 → setTestResult({ passed, summary })
  → stepComplete && testResult?.passed 일 때만 "다음 단계로 →" 버튼 활성
  → 테스트 미통과 시 "테스트를 통과해야 다음 단계로..." 안내 배너

단계 진행 (Step N → Step N+1)
  → 배너 "다음 단계로 →" 클릭
  → snapshot.Save(dir, currentStep) — 롤백 지점 저장
  → POST /api/project/nextstep
  → GenerateNextStep: 현재 TUTORSYS.md + 현재 코드 파일 기반으로 다음 단계 커리큘럼 재생성
    → 누적 확장 방식: 이전 단계 코드 유지 + 새 기능 추가 (단계 수는 주제 복잡도에 따라 2~8개 유동)
    → 3단계 작업: ①이전 HOLE/BUG 완성 ②새 기능 추가(새 HOLE/BUG) ③기존 테스트 유지+새 테스트 추가
  → GenerateCodeFiles: 다음 단계용 코드 파일 (HOLE/BUG 포함) 재생성
    → 기존 파일 있으면 함수 시그니처/패키지명/임포트 유지 (5가지 하드 룰)
  → Go 프로젝트이면 ensureGoMod() — go mod init+tidy 자동 실행 (비치명적)
  → 뉴비면 GenerateQuizData로 quiz.json 재생성
  → watcher 재시작 → 프론트엔드 파일 트리/퀴즈/솔브 상태 갱신
  → listSnapshots() → setSnapshots() (FeedbackPanel 드롭다운 갱신)

간호사 채팅 흐름
  → Dashboard에서 간호사 캐릭터와 대화 (POST /api/daily/nurse-chat SSE)
  → 과거 주제 자동 조회 → Gemini가 중복 방지 주제 추천
  → 충분한 정보 모이면 응답 끝에 [TOPICS]...[/TOPICS] 블록 포함
  → 프론트엔드가 블록 파싱 → 주제 3개 선택 UI 표시 → POST /api/daily/confirm
```

## API 엔드포인트

모든 `/api/*` 엔드포인트는 `Authorization: Bearer <supabase_jwt>` 헤더 필수.

| Method | Path | 설명 |
|--------|------|------|
| GET | `/api/user/settings` | 사용자 설정 조회 (없으면 `{}` 반환) |
| PUT | `/api/user/settings` | 사용자 설정 저장 (base_dir, language, skill_level) |
| GET | `/api/daily` | `{ missions: MissionRecord[], topics: string[] }` 항상 반환 |
| GET | `/api/daily/history` | 날짜별 미션 히스토리 (달력용) |
| POST | `/api/daily/confirm` | 주제 확정 → 프로젝트 생성 + watcher 시작 (Insert, 중복 허용) |
| POST | `/api/daily/nurse-chat` | 간호사 채팅 SSE — `{ message, history, pastTopics }` 요청, 과거 주제 자동 조회 |
| GET | `/api/fs/list?path=` | 디렉토리 트리 |
| GET | `/api/fs/read?path=` | 파일 내용 |
| POST | `/api/fs/write` | 파일 저장 |
| GET | `/api/fs/validate?path=` | 디렉토리 유효성 + TUTORSYS.md 존재 여부 |
| GET | `/api/project/status` | 현재 프로젝트 상태 (skillLevel 포함) |
| POST | `/api/project/create` | AI 커리큘럼 생성 |
| POST | `/api/project/confirm` | 커리큘럼 확정 → 파일 생성 + watcher 시작 |
| POST | `/api/project/load` | 기존 프로젝트 로드 (TUTORSYS.md) |
| POST | `/api/project/nextstep` | 다음 단계로 진행 — 스냅샷 저장 → 커리큘럼·코드·퀴즈 재생성 |
| POST | `/api/project/complete` | 미션 완료 처리 — daily_missions.status → 'completed' + watcher stop |
| DELETE | `/api/project` | 프로젝트 디렉토리 삭제 + DB 정리 (base_dir 내부만 허용) |
| GET | `/api/project/snapshots` | 스냅샷 목록 반환 |
| POST | `/api/project/snapshot/restore` | `{ step }` 으로 이전 단계 코드 복원 |
| GET | `/api/quiz` | quiz.json 반환 |
| GET | `/api/run` | 코드 실행 SSE (stdout/stderr 스트리밍, 30초 타임아웃) |
| GET | `/api/test` | 테스트 실행 SSE (언어별 커맨드 자동 선택, 60초 타임아웃) |
| POST | `/api/explain` | 오답 AI 설명 SSE |
| POST | `/api/chat` | AI 채팅 |
| POST | `/api/goto` | 정의 이동 |
| WS | `/ws` | 피드백 스트리밍 |
| WS | `/ws/terminal` | 터미널 WebSocket |
| WS | `/ws/lsp?lang=go&root=/abs/path&token=<jwt>` | LSP 프록시 — WS↔언어서버 stdio 브리지. lang 미설치 시 503 반환 |

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
## 최종 결과물         ← 모든 단계 완료 시 완성되는 프로그램 설명
## 커리큘럼 단계       ← 주제 복잡도에 따라 2~8단계 유동 (각 30분~1시간)
## 현재 단계
## 이 단계에서 추가하는 것  ← 이번 단계에서 새로 만드는 함수/파일 목록
## 현재 과제           ← 이번 단계 새 코드의 HOLE/BUG만 (이전 단계 완성된 것 제외)
## 개념 설명           ← 이번 단계 핵심 개념 (300자 이상)
## 파일 구성
## 의도된 구멍 (HOLE)
## 의도된 버그 (BUG)
## 진행 기록
```

코드 파일 내 마커:
- `[TUTOR:HOLE]` — 구현해야 할 부분 (Monaco에서 노란 하이라이트)
- `[TUTOR:BUG]` — 의도된 버그 (Monaco에서 빨간 하이라이트)

AI 응답에 `[STEP_COMPLETE]` 포함 시 → stepComplete=true. 단, testResult?.passed=true일 때만 "다음 단계로 →" 버튼 활성화.

## 학습 수준 (skillLevel)

| 값 | 표시 | 동작 |
|----|------|------|
| `newbie` | 뉴비/재활 | HOLE 블러 처리 + 인라인 3지선다 퀴즈 + 단계적 힌트 3단계 + 오답 AI 설명. HOLE은 순서대로 잠금(첫 번째만 활성). 상세한 AI 피드백. |
| `normal` | 보통 | 기존 HOLE/BUG 하이라이트. 힌트+가이드 포함 피드백. |
| `experienced` | 숙련자 | 기존 하이라이트. 간결한 피드백 (오류 위치만). |

### 뉴비 전용 기능

- **quiz.json**: `ConfirmProject`/`ConfirmDaily`/`AdvanceToNextStep` 시 Gemini가 각 HOLE/BUG에 대해 3지선다 + 힌트 3개 생성
  - 키 형식: `filename:hole:0`, `filename:bug:0` (0-based index)
- **QuizOverlay**: HOLE/BUG 라인의 glyph 마진(라인 번호 왼쪽)에 14px 컬러 아이콘 표시
  - 클릭 시 해당 라인 아래에 view zone 생성 + 콘텐츠 카드 렌더링 (코드 라인을 밀어냄)
  - ResizeObserver → layoutZone으로 높이 자동 동기화. 닫으면 zone 제거
  - 힌트 순차 공개, 오답 클릭 시 shake + AI 설명 스트리밍 (`/api/explain` SSE, auth 헤더 포함)
- **HOLE 순차 잠금**: 첫 번째 미해결 HOLE만 활성, 나머지는 🔒 pill. 다음 단계 진행 시 solvedHoles 초기화
- **개념 패널**: 📖 개념 버튼 → TUTORSYS.md의 개념/과제 섹션을 슬라이드 패널로 표시
- **코드 실행**: ▶ 실행 버튼 → SSE로 stdout/stderr 스트리밍, 하단 출력 패널 (`/api/run` SSE, auth 헤더 포함)

### 직접 fetch 시 인증

`useProject.ts`를 거치지 않는 직접 fetch (Editor의 `/api/run`, QuizOverlay의 `/api/explain`)는 각 컴포넌트에서 `supabase.auth.getSession()`으로 토큰을 가져와 `Record<string, string>` 타입의 headers 객체에 `Authorization` 헤더를 직접 설정한다.

## 스냅샷 시스템

단계 전환 시 롤백 지점을 자동 저장:
- 저장 위치: `{projectDir}/.snapshots/{stepLabel}/`
- 복사 대상: .go/.ts/.py/.rs 등 소스 파일 + TUTORSYS.md + quiz.json
- FeedbackPanel의 스냅샷 드롭다운에서 이전 단계로 복원 가능
- `GET /api/project/snapshots` → 목록, `POST /api/project/snapshot/restore` → 복원

## 빌드

```bash
make build-fe   # frontend/dist/ 생성
make build      # build-fe + Go 바이너리 (bin/coding-tutor)
```

프로덕션: Go 서버가 `frontend/dist/` 정적 파일 직접 서빙 (`:8080` 단일 포트)

## 주요 의존성

**Go**: `gin`, `gorilla/websocket`, `fsnotify`, `go-diff`, `google.golang.org/genai`, `go-toml`, `golang-jwt/jwt/v4`

**Frontend**: `@monaco-editor/react`, `@supabase/supabase-js`, `zustand`, `react-markdown`, `remark-gfm`, `@xterm/xterm`

## LSP 설정

`/ws/lsp` 엔드포인트는 언어 서버가 로컬에 설치되어 있어야 동작. 미설치 시 503 → 프론트엔드가 정규식 fallback 사용.

```bash
go install golang.org/x/tools/gopls@latest           # Go
npm install -g typescript-language-server typescript  # TypeScript/JS
pip install python-lsp-server                         # Python
npm install -g @nomicfoundation/solidity-language-server  # Solidity
rustup component add rust-analyzer                    # Rust
```

- `lspClient.connect(lang, rootPath, token)` — App.tsx에서 프로젝트 로드 후 호출 (lang은 소문자)
- `lspClient.notifyOpen(filePath, content, langId)` — FileTree에서 파일 클릭 시 호출
- `lspClient.notifyChange(filePath, content)` — handleChange에서 매 입력마다 호출
- `lspClient.notifySave(filePath)` — Cmd+S 저장 후 호출 → gopls diagnostics 즉시 갱신
- `lspClient.notifyClose(filePath)` — 탭 닫기 시 호출 → gopls 메모리 해제, openedUris/fileVersions 정리
- `lspClient.hover(filePath, line, char)` — hover provider에서 2000ms timeout으로 호출
- `lspClient.signatureHelp(filePath, line, char)` — `(`, `,` 입력 시 파라미터 힌트 팝업
- `lspClient.codeAction(filePath, range, diagnostics)` — 💡 전구 아이콘 → quickfix/organizeImports 메뉴
- `lspClient.rename(filePath, line, char, newName)` — F2 → 심볼 일괄 rename
- completion provider: `provideCompletionItems`에서 `model.getValue()`로 gopls에 최신 내용 전달 (React state ref stale 방지)
- definition provider: LSP → 정규식 fallback(Go only) 순서. 크로스 파일 이동 시 `/api/fs/read`로 내용 로드, 프로젝트 외부(stdlib 등)는 `readOnly: true`
- formatting provider: `registerDocumentFormattingEditProvider` → LSP `textDocument/formatting` → Cmd+S 시 실행
- hover provider: `registerHoverProvider` → LSP `textDocument/hover` → 타입 정보/문서 표시
- `openFileReadOnly` store 상태로 Monaco `readOnly` 옵션 제어

## 알려진 주의사항

- **daily_missions 테이블**: `UNIQUE(user_id, date)` 제약이 없어야 하루에 여러 미션 추가 가능. 기존 DB라면 `ALTER TABLE daily_missions DROP CONSTRAINT daily_missions_user_id_date_key;` 실행 필요
- **Go 프로젝트 go.mod**: ConfirmProject/ConfirmDaily/AdvanceToNextStep 시 `ensureGoMod()`가 자동으로 go mod init+tidy 실행. 실패해도 비치명적(로그만)
- **간호사 채팅 [TOPICS] 파싱**: 응답 스트림에서 `[TOPICS]`~`[/TOPICS]` 블록을 감지해 각 줄을 JSON으로 파싱. 블록은 응답 맨 끝에 한 번만 포함됨
- **DeleteProject 보안**: `project_dir`이 user의 `base_dir` 하위가 아니면 403 반환
- **gopls 경로**: 서버 프로세스의 `$PATH`에 `$GOPATH/bin`이 없으면 503. `resolveBin()`이 `~/go/bin`, `~/.local/bin`, `/opt/homebrew/bin` 순으로 탐색
- **LSP completion stale 이슈**: completion provider 내부에서 `openFileContentRef.current` 대신 `model.getValue()` 사용. React state 업데이트보다 Monaco onChange가 먼저 gopls에 notifyChange를 보내더라도, ref가 stale하면 notifyOpen이 내용을 되돌릴 수 있음
- **[STEP_COMPLETE] 하드 게이팅**: watcher가 AI 응답 전체를 수집한 후 allTestsPassed()=false면 토큰 제거. 스트리밍 중간에 토큰이 나와도 안전
- **changedFiles 절대경로**: FileTree와 Editor 모두 절대경로(fullPath)로 통일. 상대경로 사용 시 unsaved 표시가 안 될 수 있음
- **.snapshots/ diff 오염**: watcher의 `snapshot()`·`triggerSync()` 두 Walk 모두 `strings.Contains(path, "/.snapshots/")` 가드 필요. 한 곳만 추가하면 diff에 스냅샷 파일이 포함될 수 있음
- **LSP notifyClose 누락 시 메모리 누수**: 탭 닫기 시 반드시 `lspClient.notifyClose()` 호출. 미호출 시 gopls가 파일을 계속 추적해 메모리 점진 증가
- **Go 탭 설정**: Go 파일은 `model.updateOptions({ tabSize: 4, insertSpaces: false })` — MonacoEditor props의 전역 옵션(tabSize:2)은 그대로 두고 모델 레벨에서 덮어씀
