package ai

import (
	"fmt"
	"path"
	"strings"

	"github.com/coding-tutor/internal/toolchain"
)

type tutoringLanguageContract struct {
	name         string
	comment      string
	instructions string
}

type tutorMarkerRange struct {
	Kind      string
	StartLine int
	EndLine   int
}

const maxTutorMarkerBodyLines = 4

func languageContract(language string) (tutoringLanguageContract, error) {
	normalized := strings.ToLower(strings.TrimSpace(language))
	if normalized == "solidity" {
		return tutoringLanguageContract{}, fmt.Errorf("Solidity is not supported because the local clinic has no Solidity run/test executor")
	}
	spec, ok := toolchain.Lookup(language)
	if !ok {
		return tutoringLanguageContract{}, fmt.Errorf("language %q is not supported by the local run/test executor", language)
	}
	return tutoringLanguageContract{
		name:         spec.Name(),
		comment:      spec.Comment(),
		instructions: spec.Instructions(),
	}, nil
}

func validateGeneratedProject(contract tutoringLanguageContract, generated, existing map[string]string) error {
	merged := make(map[string]string, len(existing)+len(generated))
	for name, content := range existing {
		if isReservedGeneratedPath(name) {
			continue
		}
		merged[filepathKey(name)] = content
	}
	for name, content := range generated {
		merged[filepathKey(name)] = content
	}

	if !hasRequiredEntrypoint(contract.name, merged) {
		return fmt.Errorf("generated %s project does not satisfy its required entrypoint contract", contract.name)
	}
	if !hasGeneratedTests(contract.name, generated) {
		return fmt.Errorf("generated %s files must include tests for the current HOLE/BUG tasks", contract.name)
	}

	holes, bugs := 0, 0
	for name, content := range generated {
		isTest := isDedicatedTestFile(contract.name, name)
		fileHoles, fileBugs, err := validateMarkerLayout(contract, name, content, isTest)
		if err != nil {
			return err
		}
		holes += fileHoles
		bugs += fileBugs
	}
	if holes == 0 || bugs == 0 {
		return fmt.Errorf("generated files must contain at least one [TUTOR:HOLE] and one [TUTOR:BUG]")
	}
	if holes+bugs > maxQuizItemCount {
		return fmt.Errorf("generated marker count exceeds %d", maxQuizItemCount)
	}
	return nil
}

func validateMarkerLayout(contract tutoringLanguageContract, filename, content string, testFile bool) (int, int, error) {
	if !isContractSourceFile(contract.name, filename) && !testFile {
		if containsTutorMarker(content) {
			return 0, 0, fmt.Errorf("markers are only allowed in supported source files: %q", filename)
		}
		return 0, 0, nil
	}

	ranges, err := parseTutorMarkerRanges(contract.comment, filename, content)
	if err != nil {
		return 0, 0, err
	}
	if testFile && len(ranges) > 0 {
		return 0, 0, fmt.Errorf("test file %q must not contain tutor markers", filename)
	}

	holes, bugs := 0, 0
	for _, markerRange := range ranges {
		switch markerRange.Kind {
		case "hole":
			holes++
		case "bug":
			bugs++
		}
	}
	return holes, bugs, nil
}

func parseTutorMarkerRanges(comment, filename, content string) ([]tutorMarkerRange, error) {
	lines := strings.Split(content, "\n")
	var ranges []tutorMarkerRange
	openKind := ""
	openLine := -1
	hasBody := false
	bodyLines := 0
	bodyContent := make([]string, 0, maxTutorMarkerBodyLines)

	for i, line := range lines {
		kind, present, err := parseTutorMarkerLine(comment, line)
		if err != nil {
			return nil, fmt.Errorf("marker %s:%d: %w", filename, i+1, err)
		}
		if !present {
			if openKind != "" {
				trimmed := strings.TrimSpace(line)
				if trimmed != "" && !strings.HasPrefix(trimmed, comment) {
					hasBody = true
					bodyLines++
					bodyContent = append(bodyContent, trimmed)
				}
			}
			continue
		}

		switch kind {
		case "hole", "bug":
			if openKind != "" {
				return nil, fmt.Errorf("marker %s:%d starts before the %s range at line %d is closed", filename, i+1, openKind, openLine+1)
			}
			openKind = kind
			openLine = i
			hasBody = false
			bodyLines = 0
			bodyContent = bodyContent[:0]
		case "end":
			if openKind == "" {
				return nil, fmt.Errorf("marker %s:%d has no matching HOLE or BUG start", filename, i+1)
			}
			if !hasBody {
				return nil, fmt.Errorf("marker range %s:%d-%d must contain at least one non-comment code line", filename, openLine+1, i+1)
			}
			if bodyLines > maxTutorMarkerBodyLines {
				return nil, fmt.Errorf("marker range %s:%d-%d has %d code lines; maximum is %d", filename, openLine+1, i+1, bodyLines, maxTutorMarkerBodyLines)
			}
			if markerWrapsFunctionBody(bodyContent) {
				return nil, fmt.Errorf("marker range %s:%d-%d must be inside a function or block body, not wrap its declaration", filename, openLine+1, i+1)
			}
			ranges = append(ranges, tutorMarkerRange{Kind: openKind, StartLine: openLine, EndLine: i})
			openKind = ""
			openLine = -1
			hasBody = false
			bodyLines = 0
			bodyContent = bodyContent[:0]
		}
	}
	if openKind != "" {
		return nil, fmt.Errorf("marker %s:%d is missing a matching %s [TUTOR:END] line", filename, openLine+1, comment)
	}
	return ranges, nil
}

func markerWrapsFunctionBody(lines []string) bool {
	if len(lines) < 2 || strings.TrimSpace(lines[len(lines)-1]) != "}" {
		return false
	}
	first := strings.TrimSpace(lines[0])
	openBrace := strings.Index(first, "{")
	if openBrace < 0 {
		return false
	}
	declaration := strings.TrimSpace(first[:openBrace])
	return strings.HasPrefix(declaration, "func ") ||
		strings.HasPrefix(declaration, "fn ") ||
		strings.HasPrefix(declaration, "pub fn ") ||
		strings.HasPrefix(declaration, "function ") ||
		strings.HasPrefix(declaration, "def ") ||
		strings.Contains(declaration, "=>")
}

func parseTutorMarkerLine(comment, line string) (string, bool, error) {
	tokenCount := strings.Count(line, "[TUTOR:HOLE]") + strings.Count(line, "[TUTOR:BUG]") + strings.Count(line, "[TUTOR:END]")
	if tokenCount == 0 {
		return "", false, nil
	}
	if tokenCount != 1 {
		return "", true, fmt.Errorf("must contain exactly one tutor marker token")
	}
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, comment) {
		return "", true, fmt.Errorf("must be a standalone %s comment", comment)
	}
	body := strings.TrimSpace(strings.TrimPrefix(trimmed, comment))
	switch {
	case body == "[TUTOR:HOLE]" || strings.HasPrefix(body, "[TUTOR:HOLE] "):
		return "hole", true, nil
	case body == "[TUTOR:BUG]" || strings.HasPrefix(body, "[TUTOR:BUG] "):
		return "bug", true, nil
	case body == "[TUTOR:END]":
		return "end", true, nil
	default:
		return "", true, fmt.Errorf("must start the standalone comment; [TUTOR:END] cannot have trailing text")
	}
}

func containsTutorMarker(content string) bool {
	return strings.Contains(content, "[TUTOR:HOLE]") || strings.Contains(content, "[TUTOR:BUG]") || strings.Contains(content, "[TUTOR:END]")
}

func hasRequiredEntrypoint(language string, files map[string]string) bool {
	spec, ok := toolchain.Lookup(language)
	return ok && spec.HasRequiredEntrypoint(files)
}

func hasGeneratedTests(language string, files map[string]string) bool {
	for name, content := range files {
		if isContractTestFile(language, name, content) && hasTestDeclaration(language, content) {
			return true
		}
	}
	return false
}

func hasTestDeclaration(language, content string) bool {
	spec, ok := toolchain.Lookup(language)
	return ok && spec.HasTestDeclaration(content)
}

func isContractTestFile(language, filename, content string) bool {
	spec, ok := toolchain.Lookup(language)
	return ok && spec.IsTestFile(filename, content)
}

func isDedicatedTestFile(language, filename string) bool {
	spec, ok := toolchain.Lookup(language)
	return ok && spec.IsDedicatedTestFile(filename)
}

func isContractSourceFile(language, filename string) bool {
	spec, ok := toolchain.Lookup(language)
	return ok && spec.IsSourceFile(filename)
}

func tutorCommentForFilename(filename string) string {
	switch strings.ToLower(path.Ext(filename)) {
	case ".py":
		return "#"
	case ".go", ".rs", ".ts", ".tsx", ".js", ".jsx":
		return "//"
	default:
		return ""
	}
}

func isReservedGeneratedPath(filename string) bool {
	base := strings.ToLower(path.Base(filename))
	return base == "tutorsys.md" || base == "quiz.json"
}

func filepathKey(filename string) string {
	return strings.ToLower(strings.ReplaceAll(filename, "\\", "/"))
}
