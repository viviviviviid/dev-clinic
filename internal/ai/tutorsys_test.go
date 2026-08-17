package ai

import (
	"strings"
	"testing"
)

func tutorSystemFixture(currentStep int) string {
	step1 := "- [ ] Step 1: 기초"
	step2 := "- [ ] Step 2: 확장"
	step3 := "- [ ] Step 3: 완성"
	current := "Step 1: 기초"
	concept := "기초 개념"
	addition := "기초 함수"
	if currentStep >= 2 {
		step1 = "- [x] Step 1: 기초"
		current = "Step 2: 확장"
		concept = "확장 개념"
		addition = "확장 함수"
	}
	if currentStep >= 3 {
		step2 = "- [x] Step 2: 확장"
		current = "Step 3: 완성"
		concept = "완성 개념"
		addition = "완성 함수"
	}
	return `# TUTORSYS

## 학습자 목표
작은 프로그램을 단계별로 완성한다.

## 언어 & 환경
go

## 학습 수준
newbie

## 최종 결과물
명령줄 프로그램

## 개념 설명
` + concept + `

## 커리큘럼 단계
` + step1 + "\n" + step2 + "\n" + step3 + `

## 현재 단계
` + current + `

## 이 단계에서 추가하는 것
` + addition + `

## 현재 과제
HOLE과 BUG를 해결한다.

## 파일 구성
main.go와 main_test.go

## 진행 기록
[]`
}

func TestValidateInitialTutorSystem(t *testing.T) {
	doc, err := validateInitialTutorSystem("---\n"+tutorSystemFixture(1)+"\n---", "go", "newbie")
	if err != nil {
		t.Fatalf("valid initial TUTORSYS.md: %v", err)
	}
	if doc.CurrentStep != 1 || strings.HasPrefix(doc.Raw, "---") {
		t.Fatalf("canonical document = %#v", doc)
	}

	if _, err := validateInitialTutorSystem(tutorSystemFixture(1), "solidity", "newbie"); err == nil {
		t.Fatal("unsupported Solidity curriculum was accepted")
	}
	if _, err := validateInitialTutorSystem(tutorSystemFixture(1), "go", "expert"); err == nil {
		t.Fatal("mismatched skill level was accepted")
	}
}

func TestParseTutorSystemRejectsMalformedStructure(t *testing.T) {
	valid := tutorSystemFixture(1)
	tests := map[string]string{
		"missing section":   strings.Replace(valid, "## 파일 구성\nmain.go와 main_test.go\n\n", "", 1),
		"empty section":     strings.Replace(valid, "## 개념 설명\n기초 개념", "## 개념 설명\n", 1),
		"unknown section":   strings.Replace(valid, "## 진행 기록", "## 임의 섹션\n값\n\n## 진행 기록", 1),
		"malformed heading": strings.Replace(valid, "기초 개념", "기초 개념\n##삽입된 제목", 1),
		"code fence":        strings.Replace(valid, "기초 개념", "```go\npackage main\n```", 1),
		"tilde fence":       strings.Replace(valid, "기초 개념", "~~~go\npackage main\n~~~", 1),
		"step gap":          strings.Replace(valid, "Step 2: 확장", "Step 4: 확장", 1),
		"five steps":        strings.Replace(valid, "- [ ] Step 3: 완성", "- [ ] Step 3: 완성\n- [ ] Step 4: 검증\n- [ ] Step 5: 배포", 1),
		"wrong current":     strings.Replace(valid, "Step 1: 기초\n\n## 이 단계", "Step 2: 확장\n\n## 이 단계", 1),
		"non-prefix done":   strings.Replace(valid, "- [ ] Step 2: 확장", "- [x] Step 2: 확장", 1),
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parseTutorSystem(content); err == nil {
				t.Fatalf("malformed document was accepted:\n%s", content)
			}
		})
	}

	if _, err := parseTutorSystem(string([]byte{0xff, 0xfe})); err == nil {
		t.Fatal("invalid UTF-8 was accepted")
	}
	if _, err := parseTutorSystem(strings.Repeat("x", maxTutorContentBytes+1)); err == nil {
		t.Fatal("oversized TUTORSYS.md was accepted")
	}
}

func TestValidateTutorSystemTransition(t *testing.T) {
	previous := tutorSystemFixture(1)
	next := tutorSystemFixture(2)
	doc, err := validateTutorSystemTransition(previous, next, "Step 2: 확장")
	if err != nil {
		t.Fatalf("valid transition: %v", err)
	}
	if doc.CurrentStep != 2 {
		t.Fatalf("current step = %d", doc.CurrentStep)
	}

	tests := map[string]struct {
		next      string
		requested string
	}{
		"skip requested":        {next: next, requested: "Step 3: 완성"},
		"request without title": {next: next, requested: "Step 2"},
		"renamed step":          {next: strings.Replace(next, "Step 3: 완성", "Step 3: 변경", 1), requested: "Step 2: 확장"},
		"mutated goal":          {next: strings.Replace(next, "작은 프로그램을 단계별로 완성한다.", "다른 목표", 1), requested: "Step 2: 확장"},
		"did not advance":       {next: previous, requested: "Step 2: 확장"},
		"current missing title": {next: strings.Replace(next, "Step 2: 확장\n\n## 이 단계", "Step 2\n\n## 이 단계", 1), requested: "Step 2: 확장"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := validateTutorSystemTransition(previous, tt.next, tt.requested); err == nil {
				t.Fatal("invalid transition was accepted")
			}
		})
	}
}
