package ai

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	tutorStepPattern    = regexp.MustCompile(`^- \[([ x])\] Step ([1-9][0-9]*): (.+)$`)
	tutorCurrentPattern = regexp.MustCompile(`^Step ([1-9][0-9]*)(: (.+))?$`)
)

var requiredTutorSections = []string{
	"학습자 목표",
	"언어 & 환경",
	"학습 수준",
	"최종 결과물",
	"개념 설명",
	"커리큘럼 단계",
	"현재 단계",
	"이 단계에서 추가하는 것",
	"현재 과제",
	"파일 구성",
	"진행 기록",
}

type tutorStep struct {
	Number    int
	Title     string
	Full      string
	Completed bool
}

type tutorSystemDocument struct {
	Raw         string
	Sections    map[string]string
	Steps       []tutorStep
	CurrentStep int
}

func parseTutorSystem(raw string) (tutorSystemDocument, error) {
	if raw == "" || len(raw) > maxTutorContentBytes || !utf8.ValidString(raw) {
		return tutorSystemDocument{}, fmt.Errorf("TUTORSYS.md must be valid UTF-8 between 1 and %d bytes", maxTutorContentBytes)
	}

	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(raw), "\r\n", "\n"), "\n")
	lines = trimBlankLines(lines)
	for _, line := range lines {
		if strings.Contains(line, "```") || strings.Contains(line, "~~~") {
			return tutorSystemDocument{}, fmt.Errorf("TUTORSYS.md must not contain Markdown code fences; use inline code instead")
		}
	}
	if len(lines) > 0 && lines[0] == "---" {
		lines = trimBlankLines(lines[1:])
	}
	if len(lines) > 0 && lines[len(lines)-1] == "---" {
		lines = trimBlankLines(lines[:len(lines)-1])
	}
	if len(lines) == 0 || lines[0] != "# TUTORSYS" {
		return tutorSystemDocument{}, fmt.Errorf("TUTORSYS.md must start with the exact heading # TUTORSYS")
	}

	sectionIndex := make(map[string]int, len(requiredTutorSections))
	for i, name := range requiredTutorSections {
		sectionIndex[name] = i
	}
	sections := make(map[string]string, len(requiredTutorSections))
	sectionLines := make(map[string][]string, len(requiredTutorSections))
	currentSection := ""
	nextSection := 0
	rootCount := 0

	for i, line := range lines {
		if line == "# TUTORSYS" {
			rootCount++
			if i != 0 {
				return tutorSystemDocument{}, fmt.Errorf("duplicate # TUTORSYS heading")
			}
			continue
		}
		if strings.HasPrefix(line, "## ") {
			name := strings.TrimPrefix(line, "## ")
			idx, ok := sectionIndex[name]
			if !ok || line != "## "+name {
				return tutorSystemDocument{}, fmt.Errorf("unknown or malformed TUTORSYS.md section %q", line)
			}
			if idx != nextSection {
				if _, duplicate := sections[name]; duplicate || currentSection == name {
					return tutorSystemDocument{}, fmt.Errorf("duplicate TUTORSYS.md section %q", name)
				}
				expected := "<none>"
				if nextSection < len(requiredTutorSections) {
					expected = requiredTutorSections[nextSection]
				}
				return tutorSystemDocument{}, fmt.Errorf("TUTORSYS.md section %q is out of order; expected %q", name, expected)
			}
			if currentSection != "" {
				sections[currentSection] = strings.TrimSpace(strings.Join(sectionLines[currentSection], "\n"))
			}
			currentSection = name
			nextSection++
			continue
		}
		if strings.HasPrefix(line, "##") && !strings.HasPrefix(line, "###") {
			return tutorSystemDocument{}, fmt.Errorf("malformed TUTORSYS.md level-two heading %q", line)
		}
		if currentSection == "" {
			if strings.TrimSpace(line) != "" {
				return tutorSystemDocument{}, fmt.Errorf("unexpected content before the first TUTORSYS.md section")
			}
			continue
		}
		sectionLines[currentSection] = append(sectionLines[currentSection], line)
	}
	if currentSection != "" {
		sections[currentSection] = strings.TrimSpace(strings.Join(sectionLines[currentSection], "\n"))
	}
	if rootCount != 1 || nextSection != len(requiredTutorSections) {
		missing := "<none>"
		if nextSection < len(requiredTutorSections) {
			missing = requiredTutorSections[nextSection]
		}
		return tutorSystemDocument{}, fmt.Errorf("TUTORSYS.md is missing required section %q", missing)
	}
	for _, name := range requiredTutorSections {
		if strings.TrimSpace(sections[name]) == "" {
			return tutorSystemDocument{}, fmt.Errorf("TUTORSYS.md section %q must not be empty", name)
		}
	}

	steps, currentStep, err := validateTutorProgress(sections["커리큘럼 단계"], sections["현재 단계"])
	if err != nil {
		return tutorSystemDocument{}, err
	}

	return tutorSystemDocument{
		Raw:         strings.Join(lines, "\n"),
		Sections:    sections,
		Steps:       steps,
		CurrentStep: currentStep,
	}, nil
}

func validateTutorProgress(curriculum, current string) ([]tutorStep, int, error) {
	var steps []tutorStep
	seenUnchecked := false
	for _, line := range strings.Split(curriculum, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line != strings.TrimSpace(line) {
			return nil, 0, fmt.Errorf("curriculum step lines must not be indented")
		}
		match := tutorStepPattern.FindStringSubmatch(line)
		if match == nil {
			return nil, 0, fmt.Errorf("invalid curriculum step line %q", line)
		}
		number, _ := strconv.Atoi(match[2])
		if number != len(steps)+1 {
			return nil, 0, fmt.Errorf("curriculum steps must be contiguous from Step 1")
		}
		title := strings.TrimSpace(match[3])
		if title == "" {
			return nil, 0, fmt.Errorf("Step %d title must not be empty", number)
		}
		completed := match[1] == "x"
		if completed && seenUnchecked {
			return nil, 0, fmt.Errorf("completed curriculum steps must form a contiguous prefix")
		}
		if !completed {
			seenUnchecked = true
		}
		steps = append(steps, tutorStep{
			Number: number, Title: title, Full: fmt.Sprintf("Step %d: %s", number, title), Completed: completed,
		})
	}
	if len(steps) < 2 || len(steps) > 4 {
		return nil, 0, fmt.Errorf("curriculum must contain between 2 and 4 steps")
	}

	current = strings.TrimSpace(current)
	match := tutorCurrentPattern.FindStringSubmatch(current)
	if match == nil {
		return nil, 0, fmt.Errorf("current step must use Step N or Step N: title format")
	}
	currentNumber, _ := strconv.Atoi(match[1])
	if currentNumber < 1 || currentNumber > len(steps) {
		return nil, 0, fmt.Errorf("current step %d is outside the curriculum", currentNumber)
	}
	if match[3] != "" && current != steps[currentNumber-1].Full {
		return nil, 0, fmt.Errorf("current step title does not match the curriculum")
	}
	firstUnchecked := 0
	for _, step := range steps {
		if !step.Completed {
			firstUnchecked = step.Number
			break
		}
	}
	if firstUnchecked == 0 {
		return nil, 0, fmt.Errorf("curriculum must retain a current unchecked step")
	}
	if currentNumber != firstUnchecked {
		return nil, 0, fmt.Errorf("current step %d must equal the first unchecked step %d", currentNumber, firstUnchecked)
	}
	return steps, currentNumber, nil
}

func validateInitialTutorSystem(raw, language, skillLevel string) (tutorSystemDocument, error) {
	requestedContract, err := languageContract(language)
	if err != nil {
		return tutorSystemDocument{}, err
	}
	if err := validateSkillLevel(skillLevel); err != nil {
		return tutorSystemDocument{}, err
	}
	doc, err := parseTutorSystem(raw)
	if err != nil {
		return tutorSystemDocument{}, err
	}
	documentContract, err := languageContract(doc.Sections["언어 & 환경"])
	if err != nil {
		return tutorSystemDocument{}, err
	}
	if doc.Sections["언어 & 환경"] != language {
		return tutorSystemDocument{}, fmt.Errorf("language section %q does not match requested language %q", doc.Sections["언어 & 환경"], language)
	}
	if documentContract.name != requestedContract.name {
		return tutorSystemDocument{}, fmt.Errorf("language section does not match the requested runtime contract")
	}
	if doc.Sections["학습 수준"] != skillLevel {
		return tutorSystemDocument{}, fmt.Errorf("skill level section %q does not match requested level %q", doc.Sections["학습 수준"], skillLevel)
	}
	if doc.CurrentStep != 1 {
		return tutorSystemDocument{}, fmt.Errorf("initial curriculum must start at Step 1")
	}
	return doc, nil
}

func validateSkillLevel(skillLevel string) error {
	switch skillLevel {
	case "newbie", "normal", "experienced":
		return nil
	default:
		return fmt.Errorf("unsupported skill level %q", skillLevel)
	}
}

func validateTutorSystemTransition(previousRaw, nextRaw, requestedNext string) (tutorSystemDocument, error) {
	previous, err := parseTutorSystem(previousRaw)
	if err != nil {
		return tutorSystemDocument{}, fmt.Errorf("previous TUTORSYS.md: %w", err)
	}
	next, err := parseTutorSystem(nextRaw)
	if err != nil {
		return tutorSystemDocument{}, fmt.Errorf("generated TUTORSYS.md: %w", err)
	}
	if previous.CurrentStep >= len(previous.Steps) {
		return tutorSystemDocument{}, fmt.Errorf("current step is already the final curriculum step")
	}
	expectedNumber := previous.CurrentStep + 1
	requestedNumber, requestedFull, err := parseRequestedStep(requestedNext)
	if err != nil {
		return tutorSystemDocument{}, err
	}
	if requestedNumber != expectedNumber || requestedFull != previous.Steps[expectedNumber-1].Full {
		return tutorSystemDocument{}, fmt.Errorf("requested next step does not match the immediate curriculum successor")
	}
	if next.CurrentStep != expectedNumber {
		return tutorSystemDocument{}, fmt.Errorf("generated curriculum advanced to Step %d; expected Step %d", next.CurrentStep, expectedNumber)
	}
	if strings.TrimSpace(next.Sections["현재 단계"]) != requestedFull {
		return tutorSystemDocument{}, fmt.Errorf("generated current step must exactly match requested %q", requestedFull)
	}
	if len(previous.Steps) != len(next.Steps) {
		return tutorSystemDocument{}, fmt.Errorf("generated curriculum changed the number of steps")
	}
	for i := range previous.Steps {
		if previous.Steps[i].Full != next.Steps[i].Full {
			return tutorSystemDocument{}, fmt.Errorf("generated curriculum changed Step %d identity", i+1)
		}
	}
	for _, name := range []string{"학습자 목표", "언어 & 환경", "학습 수준", "최종 결과물", "진행 기록"} {
		if previous.Sections[name] != next.Sections[name] {
			return tutorSystemDocument{}, fmt.Errorf("generated curriculum changed immutable section %q", name)
		}
	}
	return next, nil
}

// normalizeTutorSystemTransition keeps model-authored, step-specific teaching
// content while rebuilding all progression and immutable sections from the
// trusted previous document. Those fields are application state, not creative
// model output; harmless wording or whitespace changes must not block a learner.
func normalizeTutorSystemTransition(previousRaw, generatedRaw, requestedNext string) (tutorSystemDocument, error) {
	previous, err := parseTutorSystem(previousRaw)
	if err != nil {
		return tutorSystemDocument{}, fmt.Errorf("previous TUTORSYS.md: %w", err)
	}
	generated, err := parseTutorSystem(generatedRaw)
	if err != nil {
		return tutorSystemDocument{}, fmt.Errorf("generated TUTORSYS.md: %w", err)
	}

	requestedNumber, requestedFull, err := parseRequestedStep(requestedNext)
	if err != nil {
		return tutorSystemDocument{}, err
	}
	if requestedNumber != previous.CurrentStep+1 || requestedNumber > len(previous.Steps) || previous.Steps[requestedNumber-1].Full != requestedFull {
		return tutorSystemDocument{}, fmt.Errorf("requested next step does not match the immediate curriculum successor")
	}

	for _, name := range []string{"학습자 목표", "언어 & 환경", "학습 수준", "최종 결과물", "진행 기록"} {
		generated.Sections[name] = previous.Sections[name]
	}
	stepLines := make([]string, 0, len(previous.Steps))
	for _, step := range previous.Steps {
		mark := " "
		if step.Number < requestedNumber {
			mark = "x"
		}
		stepLines = append(stepLines, fmt.Sprintf("- [%s] %s", mark, step.Full))
	}
	generated.Sections["커리큘럼 단계"] = strings.Join(stepLines, "\n")
	generated.Sections["현재 단계"] = requestedFull

	normalized := renderTutorSystem(generated.Sections)
	return validateTutorSystemTransition(previousRaw, normalized, requestedNext)
}

func renderTutorSystem(sections map[string]string) string {
	var builder strings.Builder
	builder.WriteString("# TUTORSYS\n")
	for _, name := range requiredTutorSections {
		builder.WriteString("\n## ")
		builder.WriteString(name)
		builder.WriteByte('\n')
		builder.WriteString(strings.TrimSpace(sections[name]))
		builder.WriteByte('\n')
	}
	return strings.TrimSpace(builder.String())
}

func parseRequestedStep(value string) (int, string, error) {
	value = strings.TrimSpace(value)
	match := tutorCurrentPattern.FindStringSubmatch(value)
	if match == nil || match[3] == "" {
		return 0, "", fmt.Errorf("requested next step must use Step N: title format")
	}
	number, _ := strconv.Atoi(match[1])
	return number, value, nil
}

func trimBlankLines(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
