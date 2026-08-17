package ai

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/coding-tutor/internal/config"
	"google.golang.org/genai"
)

type Client struct {
	client *genai.Client
	codex  *codexBackend
}

var Global *Client

const generationTimeout = 4 * time.Minute

func Init() {
	cfg := config.Global.Gemini

	switch strings.ToLower(strings.TrimSpace(config.Global.AIProvider)) {
	case "", "codex":
		backend, err := newCodexBackend(config.Global.Codex.Executable, config.Global.Codex.Model)
		if err != nil {
			panic("ai: " + err.Error())
		}
		Global = &Client{codex: backend}
		log.Printf("ai: codex exec mode (model=%q)", config.Global.Codex.Model)
		return
	case "gemini":
		// Continue with the direct Gemini client below.
	default:
		panic("ai: unsupported AI_PROVIDER (expected codex or gemini): " + config.Global.AIProvider)
	}
	if cfg.APIKey == "" {
		panic("ai: GEMINI_API_KEY not set for AI_PROVIDER=gemini")
	}
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

type QuizItem struct {
	Key         string   `json:"key"`
	Filename    string   `json:"filename"`
	MarkerType  string   `json:"markerType"`  // "hole" or "bug"
	MarkerIndex int      `json:"markerIndex"` // index within its type
	Question    string   `json:"question"`
	Hints       []string `json:"hints"` // 3단계 힌트: 개념 → 구조 → 거의 다
}

// stream dispatches to Codex or direct Gemini.
func (c *Client) stream(ctx context.Context, system, prompt string, cb StreamCallback) error {
	ctx, cancel := withGenerationTimeout(ctx)
	defer cancel()
	if c.codex != nil {
		text, err := c.codex.generate(ctx, system, prompt, nil)
		if err != nil {
			return err
		}
		cb(text)
		return nil
	}
	cfg := &genai.GenerateContentConfig{}
	if system != "" {
		cfg.SystemInstruction = genai.NewContentFromText(system, genai.RoleUser)
	}
	for chunk, err := range c.client.Models.GenerateContentStream(ctx, config.Global.Gemini.Model, genai.Text(prompt), cfg) {
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

// generate dispatches to Codex or direct Gemini.
func (c *Client) generate(ctx context.Context, prompt string) (string, error) {
	return c.generateWithSystem(ctx, generationSystemPrompt, prompt)
}

func (c *Client) generateWithSystem(ctx context.Context, system, prompt string) (string, error) {
	ctx, cancel := withGenerationTimeout(ctx)
	defer cancel()
	if c.codex != nil {
		return c.codex.generate(ctx, system, prompt, nil)
	}
	cfg := &genai.GenerateContentConfig{}
	if system != "" {
		cfg.SystemInstruction = genai.NewContentFromText(system, genai.RoleUser)
	}
	resp, err := c.client.Models.GenerateContent(ctx, config.Global.Gemini.Model, genai.Text(prompt), cfg)
	if err != nil {
		return "", err
	}
	return resp.Text(), nil
}

func (c *Client) generateStructured(ctx context.Context, system, prompt string, schema map[string]any) (string, error) {
	ctx, cancel := withGenerationTimeout(ctx)
	defer cancel()
	if c.codex != nil {
		return c.codex.generate(ctx, system, prompt, schema)
	}
	cfg := &genai.GenerateContentConfig{
		ResponseMIMEType:   "application/json",
		ResponseJsonSchema: schema,
	}
	if system != "" {
		cfg.SystemInstruction = genai.NewContentFromText(system, genai.RoleUser)
	}
	resp, err := c.client.Models.GenerateContent(ctx, config.Global.Gemini.Model, genai.Text(prompt), cfg)
	if err != nil {
		return "", err
	}
	return resp.Text(), nil
}

func withGenerationTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, generationTimeout)
}

func (c *Client) StreamFeedback(ctx context.Context, tutorContent, diffContent, changedFilesCode, testOutput string, history []string, skillLevel string, cb StreamCallback) error {
	for label, value := range map[string]string{
		"TUTORSYS.md": tutorContent, "diff": diffContent, "changed files": changedFilesCode,
	} {
		if err := requireTextLimit(label, value, maxTutorContentBytes); err != nil {
			return err
		}
	}
	if err := requireTextLimit("test output", testOutput, 64<<10); err != nil {
		return err
	}
	data, err := marshalUntrustedData(map[string]any{
		"tutorSystem":   tutorContent,
		"diff":          diffContent,
		"changedFiles":  changedFilesCode,
		"testOutput":    testOutput,
		"priorFeedback": recentStrings(history, 3, 32<<10),
	})
	if err != nil {
		return err
	}
	userMsg := "학습자의 방금 변경만 평가하세요. Markdown으로 관찰 → 이유 → 다음 행동 1개를 작성하세요.\n\n" + data
	return c.stream(ctx, feedbackSystemPrompt(skillLevel), userMsg, cb)
}

type ChatMessage struct {
	Role    string `json:"role"` // "user" | "ai"
	Content string `json:"content"`
}

func (c *Client) StreamChat(ctx context.Context, tutorContent, fileContent string, feedbackHistory []string, chatHistory []ChatMessage, userMessage, skillLevel string, cb StreamCallback) error {
	if err := requireTextLimit("user message", userMessage, maxChatMessageBytes); err != nil {
		return err
	}
	if err := requireTextLimit("TUTORSYS.md", tutorContent, maxTutorContentBytes); err != nil {
		return err
	}
	fileContent = clipUTF8(fileContent, maxChatFileBytes)
	data, err := marshalUntrustedData(map[string]any{
		"tutorSystem":   tutorContent,
		"openFile":      fileContent,
		"priorFeedback": recentStrings(feedbackHistory, 3, 32<<10),
		"chatHistory":   recentChatMessages(chatHistory),
		"question":      userMessage,
	})
	if err != nil {
		return err
	}
	return c.stream(ctx, chatSystemPrompt(skillLevel), "학습자의 질문에 필요한 범위만 답하세요.\n\n"+data, cb)
}

func (c *Client) GenerateCurriculum(ctx context.Context, language, topic, skillLevel string) (string, error) {
	if _, err := languageContract(language); err != nil {
		return "", err
	}
	if err := validateSkillLevel(skillLevel); err != nil {
		return "", err
	}
	skillLevelKr := skillLevelToKorean(skillLevel)
	if err := requireTextLimit("topic", topic, 2000); err != nil {
		return "", err
	}
	data, err := marshalUntrustedData(map[string]string{
		"language": language, "topic": topic, "skillLevel": skillLevel,
		"skillLevelDescription": skillLevelKr,
	})
	if err != nil {
		return "", err
	}

	prompt := fmt.Sprintf(`학습자를 위한 코딩 튜터 커리큘럼을 설계해주세요.

%s

━━━ 단계 수 결정 기준 ━━━
전체 1~2시간 안에 끝나는 2~4개 단계를 설계하세요. 각 단계는 15~30분 분량이어야 합니다.

━━━ 설계 원칙 ━━━
이 커리큘럼은 "누적 확장" 방식입니다.
- 각 스텝은 이전 스텝의 코드 위에 새 기능을 추가합니다.
- 모든 스텝을 완료하면 하나의 완성된 프로그램이 만들어집니다.
- 스텝 1의 코드가 마지막 스텝에도 그대로 살아있어야 합니다.

먼저 "최종 완성 프로그램"을 설계한 뒤, 그것을 적절한 수의 단계로 분해하세요.
각 단계는 새로운 함수/파일을 추가하는 것이 원칙입니다.

아래 형식의 TUTORSYS.md를 생성하세요. 마크다운 코드블록 없이 내용만 출력하세요.

---
# TUTORSYS

## 학습자 목표
[이 프로젝트를 통해 학습자가 달성할 구체적인 목표. 1~3문장.]

## 언어 & 환경
[입력 language 값을 정확히 기록]

## 학습 수준
[입력 skillLevel 값을 정확히 기록]

## 최종 결과물
[모든 단계를 완료했을 때 완성되는 프로그램 설명.
- 어떤 기능을 하는 프로그램인지 구체적으로
- 어떤 함수/파일로 구성되는지 (최종 파일 목록과 각 역할)
- 어떻게 실행하고 어떤 출력이 나오는지]

## 개념 설명
[Step 1에서 다루는 핵심 개념들을 초보자도 이해할 수 있도록 설명.
각 개념마다 "왜 필요한지"와 "어떻게 동작하는지"를 포함.
코드 예시는 inline code로만 작성하고 Markdown fenced code block은 사용하지 않음. 300자 이상 충분히 작성.]

## 커리큘럼 단계
[주제 복잡도에 맞게 2~4개 단계를 작성. 형식 예시:]
- [ ] Step 1: [단계명 — 이 단계에서 새로 추가하는 것]
- [ ] Step 2: [단계명 — 이 단계에서 새로 추가하는 것]
(필요한 만큼 계속 추가)

## 현재 단계
Step 1

## 이 단계에서 추가하는 것
[Step 1에서 새로 만드는 함수/파일 목록과 각각의 역할.
"이전 단계 없음 — 프로젝트의 기초 뼈대를 만듭니다."]

## 현재 과제
### 구현할 것 (HOLE)
1. **[파일명] - [함수/구조체명]**: [무엇을 구현해야 하는지 2~3문장 명확히]
   - 왜 필요한가: [이 구현이 최종 프로그램에서 어떤 역할을 하는지]
   - 단계별 접근: [1단계 → 2단계 → 3단계 순서로 어떻게 작성해야 하는지]
   - 사용할 것: [관련 표준 라이브러리 함수, 타입, 키워드]

### 찾아서 고칠 것 (BUG)
1. **[파일명] - [함수/위치]**: [어떤 종류의 버그인지]
   - 증상: [이 버그가 있으면 어떤 문제가 발생하는지 구체적으로]
   - 힌트: [어떤 종류의 오류인지 — 반복 범위? 조건 방향? 연산 순서?]

## 파일 구성
- [파일명]: [이 파일의 역할과 구조 설명 (Step 1에서 생성되는 파일들)]

## 진행 기록
[]
---`, data)

	text, err := c.generateWithSystem(ctx, curriculumSystemPrompt, prompt)
	if err != nil {
		return "", err
	}
	doc, err := validateInitialTutorSystem(strings.TrimSpace(text), language, skillLevel)
	if err != nil {
		return "", fmt.Errorf("generated curriculum failed validation: %w", err)
	}
	return doc.Raw, nil
}

func (c *Client) GenerateCodeFiles(ctx context.Context, tutorContent string, existingFiles map[string]string) (map[string]string, error) {
	doc, err := parseTutorSystem(tutorContent)
	if err != nil {
		return nil, fmt.Errorf("TUTORSYS.md validation: %w", err)
	}
	if err := validateSkillLevel(doc.Sections["학습 수준"]); err != nil {
		return nil, err
	}
	contract, err := languageContract(doc.Sections["언어 & 환경"])
	if err != nil {
		return nil, err
	}
	sortedFiles, err := sortedNamedContents(existingFiles)
	if err != nil {
		return nil, err
	}
	data, err := marshalUntrustedData(map[string]any{
		"tutorSystem":   tutorContent,
		"existingFiles": sortedFiles,
	})
	if err != nil {
		return nil, err
	}
	prompt := fmt.Sprintf(`현재 단계에 필요한 구현 파일과 테스트 파일을 생성하세요.

- existingFiles가 비어 있으면 실행 가능한 최소 프로젝트 뼈대를 만드세요.
- 기존 파일이 있으면 이전 단계의 HOLE/BUG를 올바른 코드로 완성하되 마커 없는 학습자 코드는 보존하세요.
- 새 [TUTOR:HOLE]/[TUTOR:BUG]는 현재 단계의 새 기능에만 추가하세요.
- HOLE과 BUG를 각각 하나 이상 만드세요.
- 각 과제 범위는 독립된 %s 주석 시작 줄([TUTOR:HOLE] 또는 [TUTOR:BUG])과 독립된 %s 주석 종료 줄([TUTOR:END])로 감싸세요. 설명은 시작 marker와 같은 줄에만 쓰세요.
- 시작 marker와 [TUTOR:END] 사이에는 학습자가 통째로 교체할 컴파일 가능한 placeholder/bug 본문을 한 줄 이상 넣으세요. 여러 줄 본문도 허용되며 에디터는 시작부터 END까지 전부 입력 코드로 치환합니다.
- marker 범위는 정확히 1:1로 닫고 중첩하거나 겹치지 마세요. 범위 밖의 시그니처와 주변 코드는 marker가 남아 있는 초기 상태에서도 컴파일되어야 합니다.
- BUG는 실제 호출 경로의 컴파일 가능한 논리 오류여야 합니다.
- 각 마커에는 수정 전 실패하고 올바른 구현 후 통과하는 결정적 테스트가 있어야 합니다.
- TUTORSYS.md와 quiz.json은 생성하지 마세요. clinic이 별도로 관리합니다.
- 모든 필요한 파일을 files 배열에 담고 JSON 외 텍스트를 출력하지 마세요.

%s

%s`, contract.comment, contract.comment, contract.instructions, data)

	text, err := c.generateStructured(ctx, codeFilesSystemPrompt, prompt, codeFilesSchema())
	if err != nil {
		return nil, err
	}
	var response codeFilesResponse
	if err := decodeStrictJSON(text, &response); err != nil {
		return nil, fmt.Errorf("code files JSON parse error: %w", err)
	}
	generated, err := validateGeneratedFiles(response)
	if err != nil {
		return nil, err
	}
	if err := validateGeneratedProject(contract, generated, existingFiles); err != nil {
		return nil, fmt.Errorf("generated project contract: %w", err)
	}
	return generated, nil
}

func (c *Client) GenerateQuizData(ctx context.Context, tutorContent string, codeFiles map[string]string) (map[string]QuizItem, error) {
	if err := requireTextLimit("TUTORSYS.md", tutorContent, maxTutorContentBytes); err != nil {
		return nil, err
	}
	markers, err := collectQuizMarkers(codeFiles)
	if err != nil {
		return nil, err
	}
	if len(markers) == 0 {
		return map[string]QuizItem{}, nil
	}
	data, err := marshalUntrustedData(map[string]any{
		"tutorSystem": tutorContent,
		"markers":     markers,
	})
	if err != nil {
		return nil, err
	}
	prompt := `각 마커에 대해 question과 정확히 3단계 힌트를 생성하세요.

- HOLE: 구현 목표 질문, 개념 힌트, 구조 힌트, 구체 API/키워드 힌트
- BUG: 관찰되는 증상 질문, 오류 종류 힌트, 문제 범위 힌트, 수정 방향 힌트
- key, filename, markerType, markerIndex는 입력 값을 정확히 복사하세요.
- 완성 코드를 공개하지 말고 JSON 외 텍스트를 출력하지 마세요.

` + data
	text, err := c.generateStructured(ctx, quizSystemPrompt, prompt, quizSchema())
	if err != nil {
		return nil, err
	}
	var response quizResponse
	if err := decodeStrictJSON(text, &response); err != nil {
		return nil, fmt.Errorf("quiz JSON parse error: %w", err)
	}
	return validateQuiz(response, markers)
}

func collectQuizMarkers(codeFiles map[string]string) ([]quizMarker, error) {
	files, err := sortedNamedContents(codeFiles)
	if err != nil {
		return nil, err
	}
	var markers []quizMarker
	for _, file := range files {
		filename, content := file.Path, file.Content
		// Skip test files
		if isTestFilename(filename) {
			continue
		}
		comment := tutorCommentForFilename(filename)
		if comment == "" {
			if containsTutorMarker(content) {
				return nil, fmt.Errorf("tutor marker found in unsupported source file %q", filename)
			}
			continue
		}
		lines := strings.Split(content, "\n")
		ranges, err := parseTutorMarkerRanges(comment, filename, content)
		if err != nil {
			return nil, err
		}
		holeIdx := 0
		bugIdx := 0
		for _, markerRange := range ranges {
			start := markerRange.StartLine - 5
			if start < 0 {
				start = 0
			}
			end := markerRange.EndLine + 6
			if end > len(lines) {
				end = len(lines)
			}
			switch markerRange.Kind {
			case "hole":
				markers = append(markers, quizMarker{
					Key:        fmt.Sprintf("%s:hole:%d", filename, holeIdx),
					Filename:   filename,
					MarkerType: "hole",
					Index:      holeIdx,
					Context:    strings.Join(lines[start:end], "\n"),
				})
				holeIdx++
			case "bug":
				markers = append(markers, quizMarker{
					Key:        fmt.Sprintf("%s:bug:%d", filename, bugIdx),
					Filename:   filename,
					MarkerType: "bug",
					Index:      bugIdx,
					Context:    strings.Join(lines[start:end], "\n"),
				})
				bugIdx++
			}
		}
	}
	if len(markers) > maxQuizItemCount {
		return nil, fmt.Errorf("quiz marker count exceeds %d", maxQuizItemCount)
	}

	return markers, nil
}

type TopicSuggestion struct {
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	Difficulty string `json:"difficulty"` // "상" | "중" | "하"
}

// NurseChatMessage is a single turn in a nurse chat conversation.
type NurseChatMessage struct {
	Role    string `json:"role"` // "user" or "nurse"
	Content string `json:"content"`
}

// GenerateNurseReply returns conversational text and topic suggestions as
// separate structured fields for the handler's typed SSE events.
func (c *Client) GenerateNurseReply(ctx context.Context, message string, history []NurseChatMessage, pastTopics []string, language, skillLevel string) (NurseReply, error) {
	if err := requireTextLimit("nurse message", message, maxChatMessageBytes); err != nil {
		return NurseReply{}, err
	}
	data, err := marshalUntrustedData(map[string]any{
		"message":          message,
		"history":          recentNurseMessages(history),
		"pastTopics":       normalizedPastTopics(pastTopics),
		"language":         clipUTF8(language, 64),
		"skillLevel":       skillLevel,
		"skillDescription": skillLevelToKorean(skillLevel),
	})
	if err != nil {
		return NurseReply{}, err
	}
	prompt := "오늘 연습 주제를 파악하기 위한 다음 응답을 생성하세요. JSON 외 텍스트를 출력하지 마세요.\n\n" + data
	text, err := c.generateStructured(ctx, nurseSystemPrompt, prompt, nurseReplySchema())
	if err != nil {
		return NurseReply{}, err
	}
	var reply NurseReply
	if err := decodeStrictJSON(text, &reply); err != nil {
		return NurseReply{}, fmt.Errorf("nurse reply JSON parse error: %w", err)
	}
	reply.Message = strings.TrimSpace(reply.Message)
	if reply.Message == "" {
		return NurseReply{}, fmt.Errorf("nurse reply message is empty")
	}
	if len([]rune(reply.Message)) > maxNurseMessageRunes {
		return NurseReply{}, fmt.Errorf("nurse reply message exceeds %d characters", maxNurseMessageRunes)
	}
	if len(reply.Topics) > 0 {
		reply.Topics, err = validateTopics(reply.Topics)
		if err != nil {
			return NurseReply{}, err
		}
	}
	return reply, nil
}

func (c *Client) GenerateDailyTopics(ctx context.Context, language, skillLevel string, pastTopics []string) ([]TopicSuggestion, error) {
	data, err := marshalUntrustedData(map[string]any{
		"language": language, "skillLevel": skillLevel,
		"skillDescription": skillLevelToKorean(skillLevel),
		"pastTopics":       normalizedPastTopics(pastTopics),
	})
	if err != nil {
		return nil, err
	}
	prompt := `실용적인 예제 기반 주제 3개를 추천하세요. 각 주제는 HOLE과 BUG 과제로 평가할 수 있고 과거 주제와 겹치지 않아야 합니다. slug는 영문 파스칼케이스이며 JSON 외 텍스트를 출력하지 마세요.

` + data
	text, err := c.generateStructured(ctx, topicsSystemPrompt, prompt, topicsSchema())
	if err != nil {
		return nil, err
	}
	var response topicsResponse
	if err := decodeStrictJSON(text, &response); err != nil {
		return nil, fmt.Errorf("topics JSON parse error: %w", err)
	}
	return validateTopics(response.Topics)
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

	data, err := marshalUntrustedData(map[string]string{
		"marker": markerDesc, "question": question, "wrongChoice": wrongChoice,
		"correctCode": correctCode,
	})
	if err != nil {
		return err
	}
	prompt := fmt.Sprintf("오답을 관찰 → 틀린 이유 → 학습자가 확인할 다음 행동 1개 순서로 2~4문장 평문으로 설명하세요. %s\n\n%s", tone, data)
	return c.stream(ctx, chatSystemPrompt(skillLevel), prompt, cb)
}

func (c *Client) GenerateNextStep(ctx context.Context, tutorContent, nextStep string, currentFiles map[string]string) (string, error) {
	previous, err := parseTutorSystem(tutorContent)
	if err != nil {
		return "", fmt.Errorf("TUTORSYS.md validation: %w", err)
	}
	if _, err := languageContract(previous.Sections["언어 & 환경"]); err != nil {
		return "", err
	}
	if err := validateSkillLevel(previous.Sections["학습 수준"]); err != nil {
		return "", err
	}
	files, err := sortedNamedContents(currentFiles)
	if err != nil {
		return "", err
	}
	data, err := marshalUntrustedData(map[string]any{
		"tutorSystem": tutorContent, "nextStep": nextStep, "currentFiles": files,
	})
	if err != nil {
		return "", err
	}
	prompt := fmt.Sprintf(`다음 학습 단계로 TUTORSYS.md를 업데이트하세요.

이 커리큘럼은 "누적 확장" 방식입니다.
- 각 스텝은 이전 스텝의 코드 위에 새 기능을 추가합니다.
- 이전 단계에서 만든 함수/파일은 그대로 유지되며, 새 기능만 추가됩니다.

업데이트 규칙:
1. "## 현재 단계" 값을 입력 nextStep으로 변경
2. "## 커리큘럼 단계"에서 완료된 단계들을 - [x]로 표시
3. "## 이 단계에서 추가하는 것" 섹션을 새 단계의 함수/파일로 업데이트
   - 이전 단계에서 이어받는 것(완성된 코드)과 이번에 새로 추가하는 것을 명확히 구분
4. "## 현재 과제" 섹션을 새 코드에 대한 HOLE/BUG로만 업데이트
   - 이전 단계에서 이미 완성된 함수는 과제에 포함하지 마세요
   - 이번 단계에서 새로 만드는 함수/기능에만 HOLE/BUG를 설정하세요
5. "## 개념 설명" 섹션을 새 기능의 핵심 개념으로 교체
6. "## 파일 구성" 섹션에 이번 단계에서 추가/수정되는 파일 정보를 반영
7. "## 최종 결과물", "## 학습자 목표", "## 언어 & 환경", "## 학습 수준", "## 진행 기록" 섹션은 그대로 유지
8. 코드 예시는 inline code로만 쓰고 Markdown fenced code block은 사용하지 않기

마크다운 코드블록 없이 TUTORSYS.md 전체 내용만 출력하세요.

%s`, data)

	text, err := c.generateWithSystem(ctx, nextStepSystemPrompt, prompt)
	if err != nil {
		return "", err
	}
	doc, err := validateTutorSystemTransition(tutorContent, strings.TrimSpace(text), nextStep)
	if err != nil {
		return "", fmt.Errorf("generated next-step curriculum failed validation: %w", err)
	}
	return doc.Raw, nil
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
		return "뉴비/재활 (자세한 설명 + 단계별 힌트)"
	case "experienced":
		return "숙련자 (간결한 피드백)"
	default:
		return "보통 (힌트와 가이드)"
	}
}

func extractJSON(text string) string {
	text = strings.TrimSpace(text)
	lines := strings.Split(text, "\n")
	if len(lines) >= 3 {
		opening := strings.TrimSpace(lines[0])
		closing := strings.TrimSpace(lines[len(lines)-1])
		if (opening == "```" || strings.EqualFold(opening, "```json")) && closing == "```" {
			return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
		}
	}
	return text
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
			// 파일명 추출: 끝의 "===" 제거 후 공백 trim (AI가 ===FILE:foo.go === 같이 생성해도 안전)
			if len(line) > 11 && line[len(line)-3:] == "===" {
				currentFile = strings.TrimSpace(line[8 : len(line)-3])
			} else {
				currentFile = strings.TrimSpace(line[8:])
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
			end := i
			if end > start && s[end-1] == '\r' {
				end-- // CRLF 처리: CR 제거
			}
			lines = append(lines, s[start:end])
			start = i + 1
		}
	}
	if start < len(s) {
		line := s[start:]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		lines = append(lines, line)
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
