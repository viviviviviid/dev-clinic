package ai

import (
	"fmt"
	"path"
	"strings"
	"unicode/utf8"
)

// Answer mode is an explicit user action, scoped to one marker snapshot.
// Its fields remain reference data, never additional system instructions.
type ChatAnswerRequest struct {
	MarkerType string `json:"markerType"`
	Question   string `json:"question"`
	Reference  struct {
		Path        string `json:"path"`
		StartLine   int    `json:"startLine"`
		StartColumn int    `json:"startColumn"`
		EndLine     int    `json:"endLine"`
		EndColumn   int    `json:"endColumn"`
		Code        string `json:"code"`
	} `json:"reference"`
}

func (a *ChatAnswerRequest) Validate(fileContent string) error {
	if a == nil {
		return nil
	}
	if a.MarkerType != "hole" && a.MarkerType != "bug" {
		return fmt.Errorf("답지를 생성할 문제 유형이 올바르지 않습니다")
	}
	if strings.TrimSpace(a.Question) == "" || utf8.RuneCountInString(a.Question) > maxQuizQuestionRunes {
		return fmt.Errorf("답지를 생성할 문제 설명이 비어 있거나 너무 깁니다")
	}
	r := a.Reference
	if r.Path == "" || len(r.Path) > maxGeneratedPathBytes || path.IsAbs(r.Path) || path.Clean(r.Path) != r.Path || strings.HasPrefix(r.Path, "../") || r.Path == ".." {
		return fmt.Errorf("답지를 생성할 파일 경로가 올바르지 않습니다")
	}
	if err := requireTextLimit("answer code", r.Code, 32<<10); err != nil {
		return err
	}
	// Reject large answer inputs rather than clip away the selected problem.
	if err := requireTextLimit("answer file", fileContent, maxChatFileBytes); err != nil {
		return err
	}
	lines := strings.Split(fileContent, "\n")
	if r.StartLine < 1 || r.EndLine < r.StartLine || r.EndLine > len(lines) ||
		r.Code != strings.Join(lines[r.StartLine-1:r.EndLine], "\n") {
		return fmt.Errorf("답지를 생성할 코드와 줄 범위가 일치하지 않습니다")
	}
	if !strings.Contains(lines[r.StartLine-1], "[TUTOR:"+strings.ToUpper(a.MarkerType)+"]") ||
		!strings.Contains(lines[r.EndLine-1], "[TUTOR:END]") {
		return fmt.Errorf("답지를 생성할 과제 범위를 찾지 못했습니다")
	}
	return nil
}

func buildChatPrompt(tutorContent, fileContent string, feedbackHistory []string, chatHistory []ChatMessage, userMessage, skillLevel string, answer *ChatAnswerRequest) (string, string, error) {
	if err := requireTextLimit("user message", userMessage, maxChatMessageBytes); err != nil {
		return "", "", err
	}
	if err := requireTextLimit("TUTORSYS.md", tutorContent, maxTutorContentBytes); err != nil {
		return "", "", err
	}
	if err := answer.Validate(fileContent); err != nil {
		return "", "", err
	}
	values := map[string]any{
		"tutorSystem": tutorContent, "openFile": clipUTF8(fileContent, maxChatFileBytes),
		"priorFeedback": recentStrings(feedbackHistory, 3, 32<<10),
		"chatHistory":   recentChatMessages(chatHistory), "question": userMessage,
	}
	system := chatSystemPrompt(skillLevel)
	instruction := "학습자의 질문에 필요한 범위만 답하세요."
	if answer != nil {
		values["answerRequest"] = answer
		system = answerSystemPrompt
		instruction = "선택한 과제의 답안과 해설을 작성하세요."
	}
	data, err := marshalUntrustedData(values)
	if err != nil {
		return "", "", err
	}
	return system, instruction + "\n\n" + data, nil
}
