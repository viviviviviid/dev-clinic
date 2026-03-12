package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/coding-tutor/internal/config"
	"google.golang.org/genai"
)

type Client struct {
	client *genai.Client
}

var Global *Client

func Init() {
	cfg := config.Global.Gemini
	ctx := context.Background()
	c, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  cfg.APIKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		panic("ai: failed to create Gemini client: " + err.Error())
	}
	Global = &Client{client: c}
}

type StreamCallback func(chunk string)

type QuizOption struct {
	Label     string `json:"label"`
	IsCorrect bool   `json:"isCorrect"`
}

type QuizItem struct {
	Key         string       `json:"key"`
	Filename    string       `json:"filename"`
	MarkerType  string       `json:"markerType"`  // "hole" or "bug"
	MarkerIndex int          `json:"markerIndex"` // index within its type
	Question    string       `json:"question"`
	Options     []QuizOption `json:"options"`
	CorrectCode string       `json:"correctCode"`
	Hints       []string     `json:"hints"` // 3단계 힌트: 개념 → 구조 → 거의 다
}

func (c *Client) StreamFeedback(ctx context.Context, tutorContent, diffContent, changedFilesCode, testOutput string, history []string, skillLevel string, cb StreamCallback) error {
	var systemPrompt string
	switch skillLevel {
	case "newbie":
		systemPrompt = `당신은 매우 친절하고 인내심 있는 코딩 멘토입니다. 입문자/재활 수준의 학습자를 돕고 있습니다. 한국어로 피드백을 주세요.

중요:
- "## 학습자가 수정한 부분 (diff)"가 학습자가 실제로 작성/수정한 내용입니다. 이것만 피드백하세요.
- "## 학습자가 수정한 파일 (현재 상태)"는 맥락 파악용입니다.
- diff에 없는 내용은 언급하지 마세요.
- "## 이전 피드백 히스토리"가 있다면 이미 언급한 내용은 반복하지 마세요.
- "## 테스트 실행 결과"가 있다면 FAIL 항목을 우선 설명하세요.

피드백 규칙 (입문자 맞춤):
- 매우 친절하고 격려적으로 작성하세요
- 모든 개념을 처음 배우는 사람 기준으로 상세히 설명하세요
- 왜 그렇게 해야 하는지 "이유"를 항상 설명하세요
- 단계별로 무엇을 해야 할지 구체적으로 안내하세요
- 잘한 점을 먼저 칭찬하고 개선점을 부드럽게 알려주세요
- 힌트는 매우 구체적으로 주되, 완성된 답은 직접 주지 마세요
- 마크다운 형식으로 작성하세요
- 모든 HOLE과 BUG가 해결되었다고 판단되면 응답 마지막에 [STEP_COMPLETE] 태그를 추가하세요`
	case "experienced":
		systemPrompt = `당신은 코딩 튜터입니다. 숙련된 개발자에게 간결하게 피드백하세요. 한국어로 피드백을 주세요.

중요:
- "## 학습자가 수정한 부분 (diff)"만 피드백하세요.
- diff에 없는 내용은 언급하지 마세요.
- "## 이전 피드백 히스토리"가 있다면 이미 언급한 내용은 반복하지 마세요.
- "## 테스트 실행 결과"가 있다면 FAIL 항목 위치만 간결히 지적하세요.

피드백 규칙 (숙련자 맞춤):
- 간결하게 핵심만 짚으세요
- 오류 위치와 문제점만 지적하고 힌트는 최소화하세요
- 스스로 해결할 수 있도록 여지를 남기세요
- 마크다운 형식으로 작성하세요
- 모든 HOLE과 BUG가 해결되었다고 판단되면 응답 마지막에 [STEP_COMPLETE] 태그를 추가하세요`
	default: // "normal"
		systemPrompt = `당신은 친절하고 격려하는 코딩 튜터입니다. 학습자의 코드 변경만을 보고 한국어로 피드백을 주세요.

중요:
- "## 학습자가 수정한 부분 (diff)"가 학습자가 실제로 작성/수정한 내용입니다. 이것만 피드백하세요.
- "## 학습자가 수정한 파일 (현재 상태)"는 diff 맥락 파악용입니다.
- diff에 없는 내용(기존 코드, 주석 등)은 언급하지 마세요.
- "## 이전 피드백 히스토리"가 있다면, 이미 언급한 내용은 반복하지 말고 새로운 관점에서 피드백하세요.
- "## 테스트 실행 결과"가 있다면 결과를 참고해 피드백하세요.

피드백 규칙:
- 격려하되 정확하게 피드백하세요
- 힌트는 주되 답을 직접 알려주지 마세요
- 마크다운 형식으로 작성하세요
- [TUTOR:HOLE] 부분을 학습자가 구현했다면 구체적으로 칭찬하세요
- [TUTOR:BUG] 부분의 버그를 학습자가 수정했다면 칭찬하세요
- 모든 HOLE과 BUG가 해결되었다고 판단되면 응답 마지막에 [STEP_COMPLETE] 태그를 추가하세요`
	}

	historySection := ""
	if len(history) > 0 {
		historySection = "\n\n## 이전 피드백 히스토리 (이미 한 말은 반복하지 마세요)\n"
		for i, h := range history {
			historySection += fmt.Sprintf("### 피드백 %d\n%s\n", i+1, h)
		}
	}

	testSection := ""
	if testOutput != "" {
		testSection = fmt.Sprintf("\n\n## 테스트 실행 결과\n```\n%s\n```", testOutput)
	}

	userMsg := fmt.Sprintf(`## TUTORSYS.md (프로젝트 정보 및 학습 목표)
%s%s%s

## 학습자가 수정한 부분 (diff) ← 이것이 학습자의 실제 작업입니다
%s

## 학습자가 수정한 파일 (현재 상태, 맥락 파악용)
%s

## 요청
위 diff를 기준으로, 학습자가 방금 수정한 내용에 대해서만 피드백해주세요.
diff에 없는 기존 코드는 AI가 미리 작성한 템플릿이므로 평가 대상이 아닙니다.`, tutorContent, historySection, testSection, diffContent, changedFilesCode)

	model := config.Global.Gemini.Model
	cfg := &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(systemPrompt, genai.RoleUser),
	}
	for chunk, err := range c.client.Models.GenerateContentStream(ctx, model,
		genai.Text(userMsg), cfg,
	) {
		if err != nil {
			return err
		}
		if chunk.Candidates != nil {
			for _, cand := range chunk.Candidates {
				if cand.Content != nil {
					for _, part := range cand.Content.Parts {
						if part.Text != "" {
							cb(part.Text)
						}
					}
				}
			}
		}
	}
	return nil
}

type ChatMessage struct {
	Role    string `json:"role"`    // "user" | "ai"
	Content string `json:"content"`
}

func (c *Client) StreamChat(ctx context.Context, tutorContent, fileContent string, feedbackHistory []string, chatHistory []ChatMessage, userMessage, skillLevel string, cb StreamCallback) error {
	var systemPrompt string
	switch skillLevel {
	case "newbie":
		systemPrompt = `당신은 매우 친절하고 인내심 있는 코딩 멘토입니다. 입문자/재활 수준의 학습자의 질문에 답해주세요. 한국어로 답변하세요. 처음 배우는 사람도 이해할 수 있도록 개념부터 친절하게 설명하세요.`
	case "experienced":
		systemPrompt = `당신은 코딩 튜터입니다. 숙련된 개발자의 질문에 간결하게 핵심만 답하세요. 한국어로 답변하세요.`
	default:
		systemPrompt = `당신은 친절한 코딩 튜터입니다. 학습자의 질문에 힌트와 가이드를 포함해 한국어로 답변하세요.`
	}

	historySection := ""
	if len(feedbackHistory) > 0 {
		historySection = "\n\n## 이전 AI 피드백 (최근 3개)\n"
		for i, h := range feedbackHistory {
			historySection += fmt.Sprintf("### 피드백 %d\n%s\n", i+1, h)
		}
	}

	chatSection := ""
	if len(chatHistory) > 0 {
		chatSection = "\n\n## 대화 내역\n"
		for _, msg := range chatHistory {
			role := "학습자"
			if msg.Role == "ai" {
				role = "AI 튜터"
			}
			chatSection += fmt.Sprintf("**%s**: %s\n\n", role, msg.Content)
		}
	}

	fileSection := ""
	if fileContent != "" {
		fileSection = fmt.Sprintf("\n\n## 현재 열린 파일\n```\n%s\n```", fileContent)
	}

	userMsg := fmt.Sprintf(`## TUTORSYS.md
%s%s%s%s

## 학습자 질문
%s`, tutorContent, fileSection, historySection, chatSection, userMessage)

	model := config.Global.Gemini.Model
	cfg := &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(systemPrompt, genai.RoleUser),
	}
	for chunk, err := range c.client.Models.GenerateContentStream(ctx, model, genai.Text(userMsg), cfg) {
		if err != nil {
			return err
		}
		if chunk.Candidates != nil {
			for _, cand := range chunk.Candidates {
				if cand.Content != nil {
					for _, part := range cand.Content.Parts {
						if part.Text != "" {
							cb(part.Text)
						}
					}
				}
			}
		}
	}
	return nil
}

func (c *Client) GenerateCurriculum(ctx context.Context, language, topic, skillLevel string) (string, error) {
	skillLevelKr := skillLevelToKorean(skillLevel)

	prompt := fmt.Sprintf(`학습자를 위한 코딩 튜터 커리큘럼을 설계해주세요.

언어: %s
주제/요청: %s
학습 수준: %s

아래 형식의 TUTORSYS.md를 생성하세요. 마크다운 코드블록 없이 내용만 출력하세요.

---
# TUTORSYS

## 학습자 목표
[이 프로젝트를 통해 학습자가 달성할 구체적인 목표. 1~3문장.]

## 언어 & 환경
%s

## 학습 수준
%s

## 개념 설명
[이 Step에서 다루는 핵심 개념들을 초보자도 이해할 수 있도록 설명.
각 개념마다 "왜 필요한지"와 "어떻게 동작하는지"를 포함.
코드 예시를 들어 설명해도 좋음. 300자 이상 충분히 작성.]

## 커리큘럼 단계
- [ ] Step 1: [단계명]
- [ ] Step 2: [단계명]
- [ ] Step 3: [단계명]

## 현재 단계
Step 1

## 현재 과제
### 구현할 것 (HOLE)
1. **[파일명] - [함수/구조체명]**: [무엇을 구현해야 하는지 2~3문장 명확히]
   - 왜 필요한가: [이 구현이 전체 프로그램에서 어떤 역할을 하는지]
   - 단계별 접근: [1단계 → 2단계 → 3단계 순서로 어떻게 작성해야 하는지]
   - 사용할 것: [관련 표준 라이브러리 함수, 타입, 키워드]

### 찾아서 고칠 것 (BUG)
1. **[파일명] - [함수/위치]**: [어떤 종류의 버그인지]
   - 증상: [이 버그가 있으면 어떤 문제가 발생하는지 구체적으로]
   - 힌트: [어떤 종류의 오류인지 — 반복 범위? 조건 방향? 연산 순서?]

## 파일 구성
- [파일명]: [이 파일의 역할과 구조 설명]

## 진행 기록
[]
---`, language, topic, skillLevelKr, language, skillLevel)

	model := config.Global.Gemini.Model
	resp, err := c.client.Models.GenerateContent(ctx, model, genai.Text(prompt), nil)
	if err != nil {
		return "", err
	}
	return resp.Text(), nil
}

func (c *Client) GenerateCodeFiles(ctx context.Context, tutorContent string, existingFiles map[string]string) (map[string]string, error) {
	existingSection := ""
	if len(existingFiles) > 0 {
		var sb strings.Builder
		sb.WriteString("=== 기존 코드 파일 (학습자가 이전 단계에서 작성한 코드) ===\n")
		sb.WriteString("기존 파일을 기반으로 다음 단계 코드를 생성할 때 반드시 지켜야 할 규칙:\n\n")
		sb.WriteString("1. **절대 변경 금지**: 패키지명, 임포트, 타입 선언, 함수 시그니처\n")
		sb.WriteString("2. **유지 필수**: 학습자가 이미 구현한 코드 (HOLE/BUG 마커가 없는 코드)\n")
		sb.WriteString("3. **교체 가능**: 기존 HOLE 자리 → 이미 구현됐으므로 완성 코드로 교체\n")
		sb.WriteString("4. **추가 가능**: 새 단계에 필요한 새 함수/메서드에만 새 HOLE/BUG 추가\n")
		sb.WriteString("5. **파일 이름 유지**: 기존 파일명 그대로 사용, 새 파일은 추가만 가능\n\n")
		sb.WriteString("검증: 생성된 코드가 기존 함수 시그니처를 변경하지 않았는지 재확인하세요.\n\n")
		for name, content := range existingFiles {
			sb.WriteString(fmt.Sprintf("===FILE:%s===\n%s\n===END===\n\n", name, content))
		}
		existingSection = sb.String() + "\n"
	}
	prompt := fmt.Sprintf(`다음 TUTORSYS.md를 보고 코드 파일들과 테스트 파일들을 생성하세요.

%s

%s=== 구현 파일 생성 규칙 ===

TUTORSYS.md의 "## 학습 수준" 값을 반드시 확인하고, 그에 맞게 HOLE/BUG 인라인 힌트 양을 조절하세요.

────────────────────────────────────────
[TUTOR:HOLE] — 학습자가 직접 구현해야 할 부분
────────────────────────────────────────
함수/메서드 시그니처는 완성된 상태로 두고, 바디에 아래 규칙에 따라 힌트 주석을 작성하세요.

★ 학습 수준 = newbie 일 때 (가장 풍부한 힌트):
  구조:
    // [TUTOR:HOLE] <이 함수가 해야 할 일 한 줄 요약>
    //
    // 📌 목표: <학습자가 달성해야 할 결과를 2~3문장으로. 왜 필요한지 포함>
    //
    // 💡 단계별 접근법:
    //   1. <첫 번째로 무엇을 준비/선언해야 하는지>
    //   2. <핵심 로직 — 어떤 조건/반복이 필요한지>
    //   3. <결과를 어떻게 반환/저장해야 하는지>
    //
    // 🔧 사용할 것들: <관련 표준 라이브러리 함수, 타입, 키워드를 구체적으로 언급>
    //    예시 패턴: <완전한 답은 아니지만 구조를 추론할 수 있는 코드 조각>
    <return 적절한_제로값 또는 빈 상태>

  예시 (Go, HTTP 핸들러):
    // [TUTOR:HOLE] 클라이언트 요청에서 이름을 읽어 인사말을 반환
    //
    // 📌 목표: URL 쿼리 파라미터 "name"을 읽고 "Hello, <name>!" 형태로 응답합니다.
    //          파라미터가 없으면 "Hello, World!"를 반환해야 합니다.
    //
    // 💡 단계별 접근법:
    //   1. r.URL.Query().Get("name") 으로 name 파라미터를 읽으세요
    //   2. name이 빈 문자열이면 "World"로 대체하세요 (if 문 또는 조건 연산자)
    //   3. fmt.Fprintf(w, "Hello, %s!", name) 으로 응답을 작성하세요
    //
    // 🔧 사용할 것들: r.URL.Query().Get(), fmt.Fprintf(), if 조건문
    //    패턴: if name == "" { name = "..." }

★ 학습 수준 = normal 일 때 (중간 힌트):
  구조:
    // [TUTOR:HOLE] <이 함수가 해야 할 일 한 줄 요약>
    // 구현: <1~2문장으로 핵심 접근법과 필요한 로직>
    // 힌트: <사용할 함수/패턴 이름만 언급>
    <return 적절한_제로값 또는 빈 상태>

★ 학습 수준 = experienced 일 때 (최소 힌트):
  구조:
    // [TUTOR:HOLE] <이 함수가 해야 할 일>
    <return 적절한_제로값 또는 빈 상태>

────────────────────────────────────────
[TUTOR:BUG] — 의도적으로 잘못 작성된 코드
────────────────────────────────────────
- 컴파일은 되지만 런타임에 잘못 동작하거나 논리적으로 틀린 코드여야 합니다
- BUG 마커 주석은 버그가 있는 코드 줄 바로 위에 작성하세요

★ newbie 일 때:
    // [TUTOR:BUG] <버그가 있는 함수/블록 이름>
    // 🔍 이 코드는 실행은 되지만 결과가 예상과 다릅니다.
    //    <어떤 증상이 나타나는지 — 예: "항상 0을 반환합니다", "마지막 요소가 빠집니다">
    //    <어떤 종류의 문제인지 — 예: "반복 범위", "연산 순서", "조건 방향">
    <버그 코드>

★ normal 일 때:
    // [TUTOR:BUG] <버그 증상 한 줄>
    <버그 코드>

★ experienced 일 때:
    // [TUTOR:BUG]
    <버그 코드>

────────────────────────────────────────
일반 규칙
────────────────────────────────────────
- HOLE·BUG 이외의 나머지 코드는 완전히 동작하는 코드로 작성하세요
- 파일 맨 위에 이 파일의 역할을 간단히 설명하는 주석을 달아주세요
- HOLE 주변 코드(함수 시그니처, 호출부, 타입 선언 등)는 충분히 채워서
  학습자가 주석 없이도 코드 맥락만으로 무엇을 써야 할지 추론 가능하게 하세요
- newbie 수준에서는 HOLE 하나당 최소 8줄 이상의 힌트 주석을 작성하세요

=== 테스트 파일 생성 규칙 ===

- 언어에 맞는 테스트 파일을 반드시 함께 생성하세요:
  - Go: *_test.go (testing 패키지 사용, httptest 등 표준 도구 활용)
  - Python: test_*.py (unittest 또는 pytest)
  - TypeScript/JavaScript: *.test.ts / *.test.js (jest)
  - Rust: 동일 파일 내 #[cfg(test)] 모듈 또는 tests/integration_test.rs
- 각 HOLE에 대한 테스트: HOLE이 올바르게 구현되면 PASS, 빈 상태(제로값 반환)면 FAIL
- 각 BUG에 대한 테스트: BUG가 수정되면 PASS, 그대로면 FAIL
- 테스트 함수명은 TestXxx 형식으로 명확하게 작성하세요
- 테스트 실패 시 학습자가 무엇이 잘못됐는지 알 수 있도록 메시지를 포함하세요
- 테스트 파일도 동일한 ===FILE:파일명=== 형식으로 출력하세요

응답 형식 (마크다운 코드블록 없이):
===FILE:파일명===
[파일 내용]
===END===`, tutorContent, existingSection)

	model := config.Global.Gemini.Model
	resp, err := c.client.Models.GenerateContent(ctx, model, genai.Text(prompt), nil)
	if err != nil {
		return nil, err
	}
	return parseCodeFiles(resp.Text()), nil
}

func (c *Client) GenerateQuizData(ctx context.Context, tutorContent string, codeFiles map[string]string) (map[string]QuizItem, error) {
	type markerInfo struct {
		key        string
		filename   string
		markerType string // "hole" or "bug"
		index      int
		context    string
	}

	var markers []markerInfo
	for filename, content := range codeFiles {
		// Skip test files
		if isTestFilename(filename) {
			continue
		}
		lines := strings.Split(content, "\n")
		holeIdx := 0
		bugIdx := 0
		for i, line := range lines {
			if strings.Contains(line, "[TUTOR:HOLE]") {
				start := i - 5
				if start < 0 {
					start = 0
				}
				end := i + 6
				if end > len(lines) {
					end = len(lines)
				}
				markers = append(markers, markerInfo{
					key:        fmt.Sprintf("%s:hole:%d", filename, holeIdx),
					filename:   filename,
					markerType: "hole",
					index:      holeIdx,
					context:    strings.Join(lines[start:end], "\n"),
				})
				holeIdx++
			}
			if strings.Contains(line, "[TUTOR:BUG]") {
				start := i - 3
				if start < 0 {
					start = 0
				}
				end := i + 8
				if end > len(lines) {
					end = len(lines)
				}
				markers = append(markers, markerInfo{
					key:        fmt.Sprintf("%s:bug:%d", filename, bugIdx),
					filename:   filename,
					markerType: "bug",
					index:      bugIdx,
					context:    strings.Join(lines[start:end], "\n"),
				})
				bugIdx++
			}
		}
	}

	if len(markers) == 0 {
		return map[string]QuizItem{}, nil
	}

	var markerList strings.Builder
	for _, m := range markers {
		markerList.WriteString(fmt.Sprintf(
			"=== key=%q filename=%q type=%q index=%d ===\n%s\n\n",
			m.key, m.filename, m.markerType, m.index, m.context,
		))
	}

	prompt := fmt.Sprintf(`다음 코딩 튜터 프로젝트의 HOLE(구현 위치)과 BUG(버그 위치)에 대한 3지선다 퀴즈를 생성하세요.
뉴비/재활 학습자를 위한 것이므로 퀴즈를 충분히 많이, 명확하게 만드세요.

## TUTORSYS.md
%s

## 마커 목록
%s

각 마커에 대해 3지선다 퀴즈를 생성하세요:

HOLE 퀴즈:
- question: "이 자리에 들어갈 코드는?" 형식으로 HOLE의 의도를 담은 구체적인 질문
- options: 3개 선택지 (하나만 isCorrect=true, 나머지는 그럴듯하지만 틀린 코드). 여러 줄 코드는 \n으로 구분하여 그대로 작성 (세미콜론으로 이어붙이지 말 것)
- correctCode: HOLE 줄 전체를 대체할 올바른 코드. 여러 줄이면 \n으로 구분 (인덴테이션 포함, 주석 없이 순수 코드만)
- hints: 3단계 힌트 배열 (순서대로 점점 더 구체적으로)
  - hints[0]: 개념 힌트 — 어떤 개념/함수를 써야 하는지 (코드 없이 설명만)
  - hints[1]: 구조 힌트 — 코드 패턴을 보여주되 핵심 부분은 ?로 가림 (예: "w.(?.?).???()")
  - hints[2]: 거의 다 — 정답 직전까지 알려주기 (예: "w.(http.Flusher).???()  ← 메서드명만 채우면 됩니다")

BUG 퀴즈:
- question: "이 코드의 버그를 고친 올바른 코드는?" 형식으로 버그의 증상을 설명하는 질문
- options: 3개 선택지 (하나만 isCorrect=true — 버그가 수정된 코드, 나머지는 비슷하지만 틀린 버전). 여러 줄 코드는 \n으로 구분하여 그대로 작성 (세미콜론으로 이어붙이지 말 것)
- correctCode: 버그가 있는 줄(BUG 마커 다음 줄)을 대체할 올바른 코드. 여러 줄이면 \n으로 구분 (인덴테이션 포함)
- hints: 3단계 힌트 배열
  - hints[0]: 어떤 종류의 버그인지 (논리 오류? 순서 오류? 잘못된 연산?)
  - hints[1]: 버그가 있는 줄 범위를 좁혀서 알려주기
  - hints[2]: 올바른 코드에서 달라지는 부분만 강조 (예: "Flush()가 루프 안에 있어야 합니다")

JSON만 출력하세요. 마크다운 코드블록 없이:
{
  "filename:hole:0": {
    "key": "filename:hole:0",
    "filename": "filename",
    "markerType": "hole",
    "markerIndex": 0,
    "question": "...",
    "hints": ["개념 힌트", "구조 힌트", "거의 다 힌트"],
    "options": [
      {"label": "올바른 코드", "isCorrect": true},
      {"label": "틀린 코드1", "isCorrect": false},
      {"label": "틀린 코드2", "isCorrect": false}
    ],
    "correctCode": "실제 정답 코드 한 줄"
  },
  "filename:bug:0": {
    "key": "filename:bug:0",
    "filename": "filename",
    "markerType": "bug",
    "markerIndex": 0,
    "question": "...",
    "hints": ["버그 종류 힌트", "범위 힌트", "핵심 힌트"],
    "options": [...],
    "correctCode": "버그 수정된 코드 한 줄"
  }
}`, tutorContent, markerList.String())

	model := config.Global.Gemini.Model
	resp, err := c.client.Models.GenerateContent(ctx, model, genai.Text(prompt), nil)
	if err != nil {
		return nil, err
	}

	raw := extractJSON(resp.Text())
	var result map[string]QuizItem
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("quiz JSON parse error: %w\nraw: %s", err, raw)
	}
	return result, nil
}

type TopicSuggestion struct {
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	Difficulty string `json:"difficulty"` // "상" | "중" | "하"
}

func (c *Client) GenerateDailyTopics(ctx context.Context, language, skillLevel string) ([]TopicSuggestion, error) {
	skillLevelKr := skillLevelToKorean(skillLevel)

	prompt := fmt.Sprintf(`코딩 튜터 앱에서 오늘 학습할 주제 3개를 추천해주세요. 각각 상/중/하 난이도로 하나씩 추천하세요.

언어: %s
학습 수준: %s

각 주제는:
- 1~2시간 안에 완성할 수 있는 실용적인 예제 기반
- HOLE(구현 과제)과 BUG(디버깅 과제)를 만들 수 있는 내용
- 이전에 자주 하지 않는 새로운 주제 위주로 선정

JSON 배열만 출력하세요. 마크다운 코드블록 없이:
[
  {"name": "주제 이름 (한국어)", "slug": "EnglishSlug", "difficulty": "하"},
  {"name": "...", "slug": "...", "difficulty": "중"},
  {"name": "...", "slug": "...", "difficulty": "상"}
]

slug는 영문 파스칼케이스 또는 단어 하나로, 디렉토리명에 사용됩니다.
difficulty는 반드시 "하", "중", "상" 중 하나여야 합니다.`, language, skillLevelKr)

	model := config.Global.Gemini.Model
	resp, err := c.client.Models.GenerateContent(ctx, model, genai.Text(prompt), nil)
	if err != nil {
		return nil, err
	}

	raw := extractJSON(resp.Text())
	var topics []TopicSuggestion
	if err := json.Unmarshal([]byte(raw), &topics); err != nil {
		return nil, fmt.Errorf("topics JSON parse error: %w\nraw: %s", err, raw)
	}
	return topics, nil
}

func (c *Client) ExplainWrongAnswer(ctx context.Context, question, wrongChoice, correctCode, markerType, skillLevel string, cb StreamCallback) error {
	var tone string
	switch skillLevel {
	case "newbie":
		tone = "매우 친절하고 쉽게 설명하세요. 처음 배우는 사람도 이해할 수 있도록 개념부터 설명해주세요."
	case "experienced":
		tone = "간결하게 핵심 이유만 설명하세요. 불필요한 설명은 생략하세요."
	default:
		tone = "친절하게, 왜 틀렸는지와 올바른 방향을 힌트로 설명하세요."
	}

	markerDesc := "구현(HOLE)"
	if markerType == "bug" {
		markerDesc = "버그(BUG)"
	}

	prompt := fmt.Sprintf(`학습자가 코딩 퀴즈(%s)에서 오답을 선택했습니다.

문제: %s
학습자가 선택한 답 (틀림): %s
올바른 답: %s

왜 선택한 답이 틀렸는지 설명하고, 올바른 답을 이해할 수 있도록 도와주세요.
%s

2~4문장으로 한국어로 답하세요. 마크다운 없이 평문으로.`, markerDesc, question, wrongChoice, correctCode, tone)

	model := config.Global.Gemini.Model
	cfg := &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText("당신은 친절한 코딩 튜터입니다.", genai.RoleUser),
	}
	for chunk, err := range c.client.Models.GenerateContentStream(ctx, model, genai.Text(prompt), cfg) {
		if err != nil {
			return err
		}
		if chunk.Candidates != nil {
			for _, cand := range chunk.Candidates {
				if cand.Content != nil {
					for _, part := range cand.Content.Parts {
						if part.Text != "" {
							cb(part.Text)
						}
					}
				}
			}
		}
	}
	return nil
}

func (c *Client) GenerateNextStep(ctx context.Context, tutorContent, nextStep string) (string, error) {
	prompt := fmt.Sprintf(`아래 TUTORSYS.md를 보고, 다음 학습 단계(%s)로 업데이트하세요.

기존 TUTORSYS.md:
%s

업데이트 규칙:
1. "## 현재 단계" 값을 "%s"로 변경 (Step N 레이블만, 예: Step 2)
2. "## 커리큘럼 단계"에서 이전 단계들(현재 단계 이전)을 - [x]로 표시
3. "## 현재 과제" 섹션을 %s에 맞는 새로운 HOLE과 BUG 과제로 완전히 교체
   - 구현할 것 (HOLE): 학습자가 직접 구현해야 할 함수/메서드
   - 찾아서 고칠 것 (BUG): 의도적으로 잘못 작성된 코드
4. "## 개념 설명" 섹션을 %s에서 다루는 핵심 개념으로 교체 (300자 이상)
5. "## 학습자 목표", "## 언어 & 환경", "## 학습 수준", "## 파일 구성", "## 진행 기록" 섹션은 그대로 유지

마크다운 코드블록 없이 TUTORSYS.md 전체 내용만 출력하세요.`,
		nextStep, tutorContent, nextStep, nextStep, nextStep)

	model := config.Global.Gemini.Model
	resp, err := c.client.Models.GenerateContent(ctx, model, genai.Text(prompt), nil)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Text()), nil
}

func isTestFilename(filename string) bool {
	base := filename
	if idx := strings.LastIndex(filename, "/"); idx >= 0 {
		base = filename[idx+1:]
	}
	if strings.HasSuffix(base, "_test.go") {
		return true
	}
	if strings.HasPrefix(base, "test_") {
		return true
	}
	if strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
		return true
	}
	return false
}

func skillLevelToKorean(level string) string {
	switch level {
	case "newbie":
		return "뉴비/재활 (자세한 설명 + 퀴즈)"
	case "experienced":
		return "숙련자 (간결한 피드백)"
	default:
		return "보통 (힌트와 가이드)"
	}
}

func extractJSON(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		if len(lines) > 2 {
			lines = lines[1 : len(lines)-1]
		}
		text = strings.Join(lines, "\n")
	}
	return strings.TrimSpace(text)
}

func parseCodeFiles(raw string) map[string]string {
	files := make(map[string]string)
	lines := splitLines(raw)
	var currentFile string
	var currentContent []string
	inFile := false

	for _, line := range lines {
		if len(line) > 10 && line[:9] == "===FILE:=" {
			continue
		}
		if len(line) > 8 && line[:8] == "===FILE:" {
			if inFile && currentFile != "" {
				files[currentFile] = joinLines(currentContent)
			}
			currentFile = line[8 : len(line)-3]
			if len(line) > 3 && line[len(line)-3:] == "===" {
				currentFile = line[8 : len(line)-3]
			} else {
				currentFile = line[8:]
			}
			currentContent = nil
			inFile = true
			continue
		}
		if line == "===END===" {
			if inFile && currentFile != "" {
				files[currentFile] = joinLines(currentContent)
			}
			inFile = false
			currentFile = ""
			currentContent = nil
			continue
		}
		if inFile {
			currentContent = append(currentContent, line)
		}
	}
	if inFile && currentFile != "" {
		files[currentFile] = joinLines(currentContent)
	}
	return files
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func joinLines(lines []string) string {
	result := ""
	for i, l := range lines {
		if i > 0 {
			result += "\n"
		}
		result += l
	}
	return result
}
