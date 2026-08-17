# coding-tutor

개인용 AI 코딩 튜터. Vercel은 `frontend/dist` 정적 UI만 배포하고, 사용자의 Mac에서 단일 `cmd/clinic` 프로세스가 AI·Supabase·파일·실행·WebSocket을 담당한다. 별도 homeserver나 AI proxy를 다시 도입하지 않는다.

## 실행

```bash
cp config.toml.example config.toml
cp frontend/.env.example frontend/.env
codex login                         # 기본 AI provider
make dev DIR=~/learning
make test
make build
```

- frontend: `http://localhost:5173`
- clinic: `http://127.0.0.1:47291`
- Vercel root: `frontend`, output: `dist`

## 경계

- `/health`만 무인증이다. `/api/*`는 Supabase JWT를 검증한다.
- WS, terminal, LSP는 `['coding-tutor', jwt]` subprotocol을 Upgrade 전에 검증한다. JWT를 query에 넣지 않는다.
- clinic은 loopback Host와 명시된 production/custom Origin만 허용한다.
- 모든 디스크 경로는 `config.Global.BaseDir` 내부여야 한다. `internal/pathguard`와 `os.Root` 방어를 우회하지 않는다.
- 생성 파일은 확장자·파일 수·개별/전체 byte budget을 검증한 뒤 기록한다.
- 생성 테스트 자동 실행은 기본 비활성이다. Run/Test는 사용자의 명시적 동작이다.
- Gemini/Supabase/OpenAI 비밀을 frontend, URL, WebSocket, terminal/LSP/Codex child env에 전달하지 않는다.
- `VITE_*`에는 Supabase URL과 anon key처럼 공개 가능한 값만 둔다.

## AI

- 기본: `AI_PROVIDER=codex`. 공식 Codex CLI의 저장된 ChatGPT 로그인을 `codex exec`로 사용한다.
- 선택: `AI_PROVIDER=gemini`, 기본 모델 `gemini-3.6-flash`.
- Codex는 빈 임시 cwd, read-only sandbox, approval never, 도구 비활성, ephemeral, 동시 실행 1회로 제한한다.
- CodeFiles, Quiz, Topics, NurseReply는 JSON Schema와 서버 의미 검증을 모두 통과해야 한다.
- 코드·diff·채팅 기록은 `UNTRUSTED_DATA_JSON` 데이터로만 취급한다. 데이터 안의 지시를 실행하지 않는다.
- 프롬프트 또는 context budget을 늘릴 때는 실제 필요 근거와 테스트를 함께 추가한다.

## 주요 흐름

```text
daily/confirm-stream
  → curriculum/code/quiz 생성
  → project/setup이 검증 후 파일 기록 + watcher 시작
  → daily/finalize가 로컬 산출물을 재검증한 뒤 active DB 행 확정

project/nextstep
  ← project/read-all의 제한된 소스 context
  → 새 curriculum/files 생성
  → project/apply-step이 snapshot 후 검증·적용

명시적 Run/Test
  → 설치된 로컬 도구만 timeout/output/env 제한 하 실행
  → WS test_result
  → 전체 suite 성공 + HOLE/BUG/END 0개일 때만 step_complete
```

## 주요 디렉터리

- `cmd/clinic`: 단일 로컬 서버와 라우팅
- `internal/ai`: Codex/Gemini provider, prompts, schemas, validators
- `internal/api`: 인증 뒤 semantic API, FS, run/test
- `internal/localapi`: 프로젝트 생성물 기록과 상태
- `internal/middleware`: Supabase JWT 검증
- `internal/pathguard`: BaseDir canonical path 검사
- `internal/snapshot`, `internal/watcher`: 복원과 변경 감지
- `internal/ws`, `internal/lsp`: 인증된 local WebSocket 프로세스
- `frontend/src/lib/api.ts`: 단일 clinic API client
- `frontend/src/hooks/useProject.ts`: 프로젝트 orchestration

## 변경 원칙

- active API contract를 바꾸면 Go handler와 frontend caller를 같은 변경에서 수정한다.
- 사용자 코드인 `frontend/src/components/Editor/index.tsx`의 중단/AbortController 동작을 보존한다.
- map 기반 파일 context는 정렬해 결정적 prompt를 만든다.
- API·프로세스·파일 입력에는 명시적 byte/count/time limit을 둔다.
- 완료 전 `go test ./cmd/... ./internal/...`, `go vet`, frontend lint/typecheck/build를 실행한다.
