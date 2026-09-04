package ai

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxGeneratedFileCount  = 48
	maxInputFileCount      = 64
	maxGeneratedPathBytes  = 240
	maxGeneratedFileBytes  = 256 << 10
	maxGeneratedTotalBytes = 2 << 20
	maxQuizItemCount       = 256
	maxQuizQuestionRunes   = 1000
	maxQuizHintRunes       = 2000
	maxTopicNameRunes      = 120
	maxNurseMessageRunes   = 2000
	maxUntrustedDataBytes  = 2 << 20
	maxTutorContentBytes   = 256 << 10
	maxChatMessageBytes    = 16 << 10
	maxChatFileBytes       = 128 << 10
	maxChatHistoryTurns    = 12
)

var (
	allowedGeneratedExtensions = map[string]struct{}{
		".css": {}, ".go": {}, ".html": {}, ".js": {}, ".jsx": {},
		".json": {}, ".md": {}, ".mod": {}, ".proto": {}, ".py": {}, ".rs": {},
		".scss": {}, ".sql": {}, ".toml": {}, ".ts": {},
		".tsx": {}, ".txt": {}, ".yaml": {}, ".yml": {},
	}
	slugPattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9]{0,63}$`)
)

type generatedFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type codeFilesResponse struct {
	Files []generatedFile `json:"files"`
}

type quizResponse struct {
	Items []QuizItem `json:"items"`
}

type topicsResponse struct {
	Topics []TopicSuggestion `json:"topics"`
}

// NurseReply separates conversational text from machine-readable suggestions.
// Topics is empty until the conversation has enough information; when present,
// it contains exactly one lower, medium, and upper difficulty suggestion.
type NurseReply struct {
	Message string            `json:"message"`
	Topics  []TopicSuggestion `json:"topics"`
}

type quizMarker struct {
	Key        string `json:"key"`
	Filename   string `json:"filename"`
	MarkerType string `json:"markerType"`
	Index      int    `json:"markerIndex"`
	Context    string `json:"context"`
}

type namedContent struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func codeFilesSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"files"},
		"properties": map[string]any{
			"files": map[string]any{
				"type": "array", "minItems": 1, "maxItems": maxGeneratedFileCount,
				"items": map[string]any{
					"type": "object", "additionalProperties": false,
					"required": []string{"path", "content"},
					"properties": map[string]any{
						"path":    map[string]any{"type": "string", "minLength": 1, "maxLength": maxGeneratedPathBytes},
						"content": map[string]any{"type": "string", "maxLength": maxGeneratedFileBytes},
					},
				},
			},
		},
	}
}

func quizSchema() map[string]any {
	return map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"items"},
		"properties": map[string]any{
			"items": map[string]any{
				"type": "array", "minItems": 1, "maxItems": maxQuizItemCount,
				"items": map[string]any{
					"type": "object", "additionalProperties": false,
					"required": []string{"key", "filename", "markerType", "markerIndex", "question", "hints"},
					"properties": map[string]any{
						"key":         map[string]any{"type": "string", "minLength": 1},
						"filename":    map[string]any{"type": "string", "minLength": 1},
						"markerType":  map[string]any{"type": "string", "enum": []string{"hole", "bug"}},
						"markerIndex": map[string]any{"type": "integer", "minimum": 0},
						"question":    map[string]any{"type": "string", "minLength": 1, "maxLength": maxQuizQuestionRunes},
						"hints": map[string]any{
							"type": "array", "minItems": 3, "maxItems": 3,
							"items": map[string]any{"type": "string", "minLength": 1, "maxLength": maxQuizHintRunes},
						},
					},
				},
			},
		},
	}
}

func topicsSchema() map[string]any {
	return map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"topics"},
		"properties": map[string]any{
			"topics": map[string]any{
				"type": "array", "minItems": 3, "maxItems": 3,
				"items": topicItemSchema(),
			},
		},
	}
}

func nurseReplySchema() map[string]any {
	return map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"message", "topics"},
		"properties": map[string]any{
			"message": map[string]any{"type": "string", "minLength": 1, "maxLength": maxNurseMessageRunes},
			"topics":  map[string]any{"type": "array", "minItems": 0, "maxItems": 3, "items": topicItemSchema()},
		},
	}
}

func topicItemSchema() map[string]any {
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"name", "slug", "difficulty", "style"},
		"properties": map[string]any{
			"name":       map[string]any{"type": "string", "minLength": 1, "maxLength": maxTopicNameRunes},
			"slug":       map[string]any{"type": "string", "pattern": slugPattern.String()},
			"difficulty": map[string]any{"type": "string", "enum": []string{"하", "중", "상"}},
			"style":      map[string]any{"type": "string", "enum": []string{"구조실험", "현실사례", "테마형"}},
		},
	}
}

func decodeStrictJSON(raw string, dst any) error {
	dec := json.NewDecoder(strings.NewReader(extractJSON(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return fmt.Errorf("unexpected trailing JSON content: %w", err)
	}
	return nil
}

func validateRelativeFilePath(name string) error {
	if name == "" || len(name) > maxGeneratedPathBytes || strings.TrimSpace(name) != name || !utf8.ValidString(name) {
		return fmt.Errorf("invalid file path %q", name)
	}
	if strings.ContainsAny(name, "\\\x00:") || path.IsAbs(name) {
		return fmt.Errorf("file path must be relative and slash-separated: %q", name)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("file path contains a control character: %q", name)
		}
	}
	clean := path.Clean(name)
	if clean == "." || clean != name || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("file path escapes or is not canonical: %q", name)
	}
	for _, part := range strings.Split(clean, "/") {
		if part == "" || part == "." || part == ".." || strings.TrimSpace(part) != part || strings.HasPrefix(part, ".") {
			return fmt.Errorf("hidden or invalid path component in %q", name)
		}
	}
	ext := strings.ToLower(path.Ext(clean))
	if _, ok := allowedGeneratedExtensions[ext]; !ok {
		return fmt.Errorf("file extension %q is not allowed", ext)
	}
	return nil
}

func validateGeneratedFiles(resp codeFilesResponse) (map[string]string, error) {
	if len(resp.Files) == 0 || len(resp.Files) > maxGeneratedFileCount {
		return nil, fmt.Errorf("generated file count must be between 1 and %d", maxGeneratedFileCount)
	}
	result := make(map[string]string, len(resp.Files))
	seen := make(map[string]struct{}, len(resp.Files))
	total := 0
	for _, file := range resp.Files {
		if err := validateRelativeFilePath(file.Path); err != nil {
			return nil, err
		}
		if isReservedGeneratedPath(file.Path) {
			return nil, fmt.Errorf("generated file path %q is reserved by the clinic", file.Path)
		}
		key := strings.ToLower(file.Path)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("duplicate generated file path %q", file.Path)
		}
		seen[key] = struct{}{}
		if len(file.Content) > maxGeneratedFileBytes {
			return nil, fmt.Errorf("generated file %q exceeds %d bytes", file.Path, maxGeneratedFileBytes)
		}
		total += len(file.Content)
		if total > maxGeneratedTotalBytes {
			return nil, fmt.Errorf("generated files exceed %d total bytes", maxGeneratedTotalBytes)
		}
		result[file.Path] = file.Content
	}
	return result, nil
}

func validateQuiz(resp quizResponse, expected []quizMarker) (map[string]QuizItem, error) {
	if len(resp.Items) != len(expected) {
		return nil, fmt.Errorf("quiz item count %d does not match marker count %d", len(resp.Items), len(expected))
	}
	expectedByKey := make(map[string]quizMarker, len(expected))
	for _, marker := range expected {
		expectedByKey[marker.Key] = marker
	}
	result := make(map[string]QuizItem, len(resp.Items))
	for _, item := range resp.Items {
		marker, ok := expectedByKey[item.Key]
		if !ok {
			return nil, fmt.Errorf("quiz item has unknown key %q", item.Key)
		}
		if _, duplicate := result[item.Key]; duplicate {
			return nil, fmt.Errorf("duplicate quiz key %q", item.Key)
		}
		if item.Filename != marker.Filename || item.MarkerType != marker.MarkerType || item.MarkerIndex != marker.Index {
			return nil, fmt.Errorf("quiz metadata mismatch for %q", item.Key)
		}
		if strings.TrimSpace(item.Question) == "" || utf8.RuneCountInString(item.Question) > maxQuizQuestionRunes || len(item.Hints) != 3 {
			return nil, fmt.Errorf("quiz item %q requires a question and exactly three hints", item.Key)
		}
		for _, hint := range item.Hints {
			if strings.TrimSpace(hint) == "" || utf8.RuneCountInString(hint) > maxQuizHintRunes {
				return nil, fmt.Errorf("quiz item %q contains an empty hint", item.Key)
			}
		}
		result[item.Key] = item
	}
	return result, nil
}

func validateTopics(topics []TopicSuggestion) ([]TopicSuggestion, error) {
	if len(topics) != 3 {
		return nil, fmt.Errorf("topics must contain exactly three items")
	}
	byDifficulty := make(map[string]TopicSuggestion, 3)
	seenStyles := make(map[string]struct{}, 3)
	seenNames := make(map[string]struct{}, 3)
	seenSlugs := make(map[string]struct{}, 3)
	for _, topic := range topics {
		topic.Name = strings.TrimSpace(topic.Name)
		topic.Slug = strings.TrimSpace(topic.Slug)
		if topic.Name == "" || !utf8.ValidString(topic.Name) || utf8.RuneCountInString(topic.Name) > maxTopicNameRunes {
			return nil, fmt.Errorf("topic name is empty or too long")
		}
		if !slugPattern.MatchString(topic.Slug) {
			return nil, fmt.Errorf("invalid topic slug %q", topic.Slug)
		}
		if topic.Difficulty != "하" && topic.Difficulty != "중" && topic.Difficulty != "상" {
			return nil, fmt.Errorf("invalid topic difficulty %q", topic.Difficulty)
		}
		if _, exists := byDifficulty[topic.Difficulty]; exists {
			return nil, fmt.Errorf("duplicate topic difficulty %q", topic.Difficulty)
		}
		if topic.Style != "구조실험" && topic.Style != "현실사례" && topic.Style != "테마형" {
			return nil, fmt.Errorf("invalid topic style %q", topic.Style)
		}
		if _, exists := seenStyles[topic.Style]; exists {
			return nil, fmt.Errorf("duplicate topic style %q", topic.Style)
		}
		nameKey, slugKey := strings.ToLower(topic.Name), strings.ToLower(topic.Slug)
		if _, exists := seenNames[nameKey]; exists {
			return nil, fmt.Errorf("duplicate topic name %q", topic.Name)
		}
		if _, exists := seenSlugs[slugKey]; exists {
			return nil, fmt.Errorf("duplicate topic slug %q", topic.Slug)
		}
		seenNames[nameKey], seenSlugs[slugKey] = struct{}{}, struct{}{}
		seenStyles[topic.Style] = struct{}{}
		byDifficulty[topic.Difficulty] = topic
	}
	if len(seenStyles) != 3 {
		return nil, fmt.Errorf("topics must include structure, real-world, and themed styles")
	}
	return []TopicSuggestion{byDifficulty["하"], byDifficulty["중"], byDifficulty["상"]}, nil
}

func marshalUntrustedData(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	if len(b) > maxUntrustedDataBytes {
		return "", fmt.Errorf("untrusted input exceeds %d bytes", maxUntrustedDataBytes)
	}
	return "<UNTRUSTED_DATA_JSON>\n" + string(b) + "\n</UNTRUSTED_DATA_JSON>", nil
}

func sortedNamedContents(files map[string]string) ([]namedContent, error) {
	if len(files) > maxInputFileCount {
		return nil, fmt.Errorf("input file count exceeds %d", maxInputFileCount)
	}
	keys := make([]string, 0, len(files))
	for name := range files {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	result := make([]namedContent, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	total := 0
	for _, name := range keys {
		if err := validateRelativeFilePath(name); err != nil {
			return nil, fmt.Errorf("input file: %w", err)
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("duplicate input file path %q", name)
		}
		seen[key] = struct{}{}
		content := files[name]
		if len(content) > maxGeneratedFileBytes {
			return nil, fmt.Errorf("input file %q exceeds %d bytes", name, maxGeneratedFileBytes)
		}
		total += len(content)
		if total > maxGeneratedTotalBytes {
			return nil, fmt.Errorf("input files exceed %d total bytes", maxGeneratedTotalBytes)
		}
		result = append(result, namedContent{Path: name, Content: content})
	}
	return result, nil
}

func requireTextLimit(label, value string, max int) error {
	if len(value) > max {
		return fmt.Errorf("%s exceeds %d bytes", label, max)
	}
	return nil
}

func recentChatMessages(history []ChatMessage) []ChatMessage {
	if len(history) > maxChatHistoryTurns {
		history = history[len(history)-maxChatHistoryTurns:]
	}
	result := make([]ChatMessage, 0, len(history))
	for _, message := range history {
		if message.Role != "user" && message.Role != "ai" {
			continue
		}
		if len(message.Content) > maxChatMessageBytes {
			message.Content = clipUTF8(message.Content, maxChatMessageBytes)
		}
		result = append(result, message)
	}
	return result
}

func recentNurseMessages(history []NurseChatMessage) []NurseChatMessage {
	if len(history) > maxChatHistoryTurns {
		history = history[len(history)-maxChatHistoryTurns:]
	}
	result := make([]NurseChatMessage, 0, len(history))
	for _, message := range history {
		if message.Role != "user" && message.Role != "nurse" {
			continue
		}
		if len(message.Content) > maxChatMessageBytes {
			message.Content = clipUTF8(message.Content, maxChatMessageBytes)
		}
		result = append(result, message)
	}
	return result
}

func recentStrings(values []string, count, maxBytes int) []string {
	if len(values) > count {
		values = values[len(values)-count:]
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, clipUTF8(value, maxBytes))
	}
	return result
}

func normalizedPastTopics(topics []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(topics))
	for _, topic := range topics {
		topic = strings.TrimSpace(clipUTF8(topic, 200))
		if topic == "" {
			continue
		}
		key := strings.ToLower(topic)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, topic)
	}
	sort.Strings(result)
	if len(result) > 100 {
		result = result[len(result)-100:]
	}
	return result
}

func clipUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.ValidString(s[:cut]) {
		cut--
	}
	return s[:cut]
}
