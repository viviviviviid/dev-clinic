package diff

import (
	"crypto/sha256"
	"fmt"
	"go/scanner"
	"go/token"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SemanticFingerprint returns a conservative, language-aware fingerprint of
// source code. It intentionally ignores ordinary comments and formatting, but
// preserves constructs where whitespace is part of the program (for example
// Python indentation and JavaScript automatic semicolon insertion).
//
// When the lightweight lexer is uncertain it keeps more information. A false
// positive merely offers a review; a false negative could hide a real edit.
func SemanticFingerprint(path, content string) string {
	ext := strings.ToLower(filepath.Ext(path))
	var canonical string
	switch ext {
	case ".go":
		canonical = canonicalGo(path, content)
	case ".py":
		canonical = canonicalPython(content)
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts":
		canonical = canonicalJavaScript(content, ext == ".jsx" || ext == ".tsx")
	default:
		canonical = canonicalCLike(content, ext)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(canonical)))
}

func canonicalGo(path, content string) string {
	fset := token.NewFileSet()
	file := fset.AddFile(filepath.Base(path), fset.Base(), len(content))
	var s scanner.Scanner
	s.Init(file, []byte(content), func(token.Position, string) {}, scanner.ScanComments)

	tokens := make([]semanticToken, 0, len(content)/4)
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.COMMENT {
			kind := "comment"
			if isDirectiveComment(lit, ".go") {
				kind = "directive"
			}
			tokens = append(tokens, semanticToken{kind: kind, text: strings.TrimSpace(lit)})
			continue
		}
		// The Go scanner inserts a semicolon with literal "\n". An explicit
		// semicolon is equivalent for fingerprinting purposes.
		if tok == token.SEMICOLON {
			lit = ";"
		} else if lit == "" {
			lit = tok.String()
		}
		tokens = append(tokens, semanticToken{kind: tok.String(), text: lit})
	}
	cgoSource := hasCgoImport(tokens)
	var out strings.Builder
	for i, tok := range tokens {
		if tok.kind == "comment" && !cgoSource {
			continue
		}
		if tok.text == ";" && nextCodeToken(tokens, i+1) != nil && nextCodeToken(tokens, i+1).text == "}" {
			// A newline before } inserts an optional semicolon; keeping it
			// would make ordinary gofmt line wrapping appear semantic.
			continue
		}
		writeToken(&out, tok.kind, tok.text)
	}
	return out.String()
}

func hasCgoImport(tokens []semanticToken) bool {
	for i, tok := range tokens {
		if tok.text != "import" {
			continue
		}
		for j := i + 1; j < len(tokens); j++ {
			if tokens[j].kind == "comment" || tokens[j].kind == "directive" {
				continue
			}
			if tokens[j].text == `"C"` {
				return true
			}
			if tokens[j].text != "(" {
				break
			}
			for j++; j < len(tokens) && tokens[j].text != ")"; j++ {
				if tokens[j].text == `"C"` {
					return true
				}
			}
			break
		}
	}
	return false
}

type semanticToken struct {
	kind string
	text string
	line int
	col  int
}

func canonicalCLike(content, language string) string {
	tokens := lexSemantic(content, language)
	var out strings.Builder
	for _, tok := range tokens {
		if tok.kind == "newline" || tok.kind == "comment" {
			continue
		}
		if tok.kind == "directive" {
			writeToken(&out, tok.kind, strings.TrimSpace(tok.text))
			continue
		}
		writeToken(&out, tok.kind, tok.text)
	}
	return out.String()
}

func canonicalJavaScript(content string, preserveJSXText bool) string {
	tokens := lexSemantic(content, "js")
	var out strings.Builder
	for i := 0; i < len(tokens); {
		tok := tokens[i]
		if tok.kind == "comment" {
			i++
			continue
		}
		if tok.kind == "newline" {
			j := i + 1
			for j < len(tokens) && (tokens[j].kind == "newline" || tokens[j].kind == "comment") {
				j++
			}
			prev := previousCodeToken(tokens, i-1)
			next := nextCodeToken(tokens, j)
			if jsNewlineMatters(prev, next) {
				writeToken(&out, "boundary", ";")
			}
			i = j
			continue
		}
		if tok.kind == "directive" {
			writeToken(&out, tok.kind, strings.TrimSpace(tok.text))
		} else if tok.text == ";" {
			next := nextCodeToken(tokens, i+1)
			if next != nil && next.text != "}" || !jsCanDropTrailingSemicolon(tokens, i) {
				writeToken(&out, "boundary", ";")
			}
		} else if tok.text == "," && isIgnorableTrailingComma(tokens, i) {
			// JavaScript/TypeScript formatters commonly add or remove one
			// trailing comma. An array elision such as [,] or [,,] is not a
			// trailing comma: it changes array length and must remain semantic.
		} else {
			writeToken(&out, tok.kind, tok.text)
		}
		i++
	}
	if preserveJSXText {
		writeToken(&out, "jsx-text", canonicalJSXText(content))
	}
	return out.String()
}

func jsCanDropTrailingSemicolon(tokens []semanticToken, semicolonIndex int) bool {
	previousIndex := previousCodeTokenIndex(tokens, semicolonIndex-1)
	if previousIndex < 0 {
		return false
	}
	previous := tokens[previousIndex]
	if previous.text != ")" {
		if previous.kind == "identifier" && previous.text == "else" {
			// `else;` is a valid empty statement. Removing its semicolon before
			// a closing brace makes the program invalid rather than relying on
			// automatic semicolon insertion.
			return false
		}
		return jsCanEndExpression(previous)
	}
	depth := 0
	for i := previousIndex; i >= 0; i-- {
		switch tokens[i].text {
		case ")":
			depth++
		case "(":
			depth--
			if depth == 0 {
				before := previousCodeTokenIndex(tokens, i-1)
				if before >= 0 {
					keyword := tokens[before].text
					if keyword == "await" {
						before = previousCodeTokenIndex(tokens, before-1)
						if before >= 0 {
							keyword = tokens[before].text
						}
					}
					switch keyword {
					case "if", "for", "while", "with", "switch":
						return false
					}
				}
				return true
			}
		}
	}
	return false
}

func previousCodeTokenIndex(tokens []semanticToken, i int) int {
	for ; i >= 0; i-- {
		if tokens[i].kind != "newline" && tokens[i].kind != "comment" {
			return i
		}
	}
	return -1
}

func isClosingDelimiter(tok *semanticToken) bool {
	return tok != nil && (tok.text == ")" || tok.text == "]" || tok.text == "}")
}

func isIgnorableTrailingComma(tokens []semanticToken, commaIndex int) bool {
	previous := previousCodeToken(tokens, commaIndex-1)
	next := nextCodeToken(tokens, commaIndex+1)
	if !isClosingDelimiter(next) {
		return false
	}
	if trailingCommaFollowsRest(tokens, commaIndex) {
		// A comma after a rest parameter/element is a syntax error, so it is
		// never formatter-only even when it appears before a closing delimiter.
		return false
	}
	if next.text != "]" {
		return true
	}
	return previous != nil && previous.text != "[" && previous.text != ","
}

func trailingCommaFollowsRest(tokens []semanticToken, commaIndex int) bool {
	depth := 0
	for i := previousCodeTokenIndex(tokens, commaIndex-1); i >= 0; i = previousCodeTokenIndex(tokens, i-1) {
		switch tokens[i].text {
		case ")", "]", "}":
			depth++
		case "(", "[", "{":
			if depth == 0 {
				return false
			}
			depth--
		case ",":
			if depth == 0 {
				return false
			}
		case "...":
			if depth == 0 {
				return true
			}
		}
	}
	return false
}

func canonicalPython(content string) string {
	tokens := lexSemantic(content, "py")
	var out strings.Builder
	bracketDepth := 0
	atLogicalLineStart := true
	lineHasCode := false
	continued := false

	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok.kind == "comment" {
			continue
		}
		if tok.kind == "newline" {
			if bracketDepth == 0 && lineHasCode && !continued {
				writeToken(&out, "newline", "\n")
				atLogicalLineStart = true
			}
			lineHasCode = false
			continued = false
			continue
		}

		if atLogicalLineStart && bracketDepth == 0 {
			writeToken(&out, "indent", fmt.Sprintf("%d", tok.col))
			atLogicalLineStart = false
		}
		if tok.kind == "directive" {
			writeToken(&out, tok.kind, strings.TrimSpace(tok.text))
		} else {
			writeToken(&out, tok.kind, tok.text)
		}
		lineHasCode = true

		if tok.kind == "op" {
			switch tok.text {
			case "(", "[", "{":
				bracketDepth++
			case ")", "]", "}":
				if bracketDepth > 0 {
					bracketDepth--
				}
			case "\\":
				continued = true
			}
		}
	}
	return out.String()
}

func writeToken(out *strings.Builder, kind, text string) {
	fmt.Fprintf(out, "%s:%d:%s;", kind, len(text), text)
}

func previousCodeToken(tokens []semanticToken, i int) *semanticToken {
	for ; i >= 0; i-- {
		if tokens[i].kind != "newline" && tokens[i].kind != "comment" {
			return &tokens[i]
		}
	}
	return nil
}

func nextCodeToken(tokens []semanticToken, i int) *semanticToken {
	for ; i < len(tokens); i++ {
		if tokens[i].kind != "newline" && tokens[i].kind != "comment" {
			return &tokens[i]
		}
	}
	return nil
}

func jsNewlineMatters(prev, next *semanticToken) bool {
	if prev == nil || next == nil {
		return false
	}
	if prev.kind == "directive" || next.kind == "directive" {
		return true
	}
	if next.text == "=>" {
		// LineTerminator is forbidden before an arrow. Keeping this boundary
		// distinguishes a valid arrow function from the corresponding syntax
		// error, including the single-identifier parameter form.
		return true
	}
	if prev.kind == "identifier" {
		switch prev.text {
		case "return", "throw", "break", "continue", "yield", "async":
			return true
		}
	}
	if prev.text == "++" || prev.text == "--" || next.text == "++" || next.text == "--" {
		return true
	}
	return jsCanEndExpression(*prev) && jsCanStartExpression(*next)
}

// canonicalJSXText extracts only JSX text nodes. The JavaScript lexer can
// safely discard formatting whitespace in expressions and tag attributes,
// while text-node whitespace is rendered output and therefore semantic.
func canonicalJSXText(content string) string {
	var out strings.Builder
	for i := 0; i < len(content); {
		if content[i] == '/' && i+1 < len(content) {
			switch content[i+1] {
			case '/':
				if end := strings.IndexByte(content[i+2:], '\n'); end >= 0 {
					i += end + 2
					continue
				}
				return out.String()
			case '*':
				if end := strings.Index(content[i+2:], "*/"); end >= 0 {
					i += end + 4
					continue
				}
				return out.String()
			}
		}
		if content[i] == '\'' || content[i] == '"' || content[i] == '`' {
			i = scanQuotedJavaScript(content, i)
			continue
		}
		tag, ok := scanJSXTag(content, i)
		if !ok || tag.closing || tag.selfClosing {
			i++
			continue
		}
		elementStart := i
		var element strings.Builder
		depth := 1
		i = tag.end
		for i < len(content) && depth > 0 {
			switch content[i] {
			case '<':
				nested, nestedOK := scanJSXTag(content, i)
				if !nestedOK {
					i++
					continue
				}
				if nested.closing {
					depth--
				} else if !nested.selfClosing {
					depth++
				}
				i = nested.end
			case '{':
				end := scanJSXExpression(content, i)
				if end > i+1 {
					// JSX can itself appear inside an expression (`ok && <span>…`).
					// Recurse only over the balanced expression body; ordinary
					// JavaScript remains covered by the primary lexer.
					if nested := canonicalJSXText(content[i+1 : end-1]); nested != "" {
						writeToken(&element, "nested-jsx", nested)
					}
				}
				i = end
			default:
				start := i
				for i < len(content) && content[i] != '<' && content[i] != '{' {
					i++
				}
				if text := normalizeJSXText(content[start:i]); text != "" {
					writeToken(&element, "text", text)
				}
			}
		}
		if depth == 0 {
			// A TS generic such as `<T>(x: T) => x` has no closing tag.
			// Commit supplemental text only for a balanced JSX element.
			writeToken(&out, "element", element.String())
		} else {
			// The candidate was a TS generic/type assertion or malformed tag.
			// Resume immediately after its '<' so a later real JSX element is
			// still discovered instead of being swallowed through EOF.
			i = elementStart + 1
		}
	}
	return out.String()
}

func scanQuotedJavaScript(content string, start int) int {
	quote := content[start]
	escaped := false
	for i := start + 1; i < len(content); i++ {
		if content[i] == quote && !escaped {
			return i + 1
		}
		escaped = content[i] == '\\' && !escaped
		if content[i] != '\\' {
			escaped = false
		}
	}
	return len(content)
}

type jsxTag struct {
	end         int
	closing     bool
	selfClosing bool
}

func scanJSXTag(content string, start int) (jsxTag, bool) {
	if start >= len(content) || content[start] != '<' {
		return jsxTag{}, false
	}
	i := start + 1
	closing := false
	if i < len(content) && content[i] == '/' {
		closing = true
		i++
	}
	if i >= len(content) {
		return jsxTag{}, false
	}
	if content[i] == '>' { // fragment: <> or </>
		return jsxTag{end: i + 1, closing: closing}, true
	}
	r, size := utf8.DecodeRuneInString(content[i:])
	if !isIdentifierStart(r) {
		return jsxTag{}, false
	}
	i += size
	for i < len(content) {
		r, size = utf8.DecodeRuneInString(content[i:])
		if !(isIdentifierPart(r) || r == '-' || r == ':' || r == '.') {
			break
		}
		i += size
	}
	quote := byte(0)
	braceDepth := 0
	escaped := false
	for ; i < len(content); i++ {
		current := content[i]
		if quote != 0 {
			if current == quote && !escaped {
				quote = 0
			}
			escaped = current == '\\' && !escaped
			if current != '\\' {
				escaped = false
			}
			continue
		}
		switch current {
		case '\'', '"':
			quote = current
		case '{':
			braceDepth++
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
		case '>':
			if braceDepth == 0 {
				previous := i - 1
				for previous > start && unicode.IsSpace(rune(content[previous])) {
					previous--
				}
				return jsxTag{end: i + 1, closing: closing, selfClosing: !closing && content[previous] == '/'}, true
			}
		}
	}
	return jsxTag{}, false
}

func scanJSXExpression(content string, start int) int {
	depth := 1
	quote := byte(0)
	escaped := false
	for i := start + 1; i < len(content); i++ {
		current := content[i]
		if quote != 0 {
			if current == quote && !escaped {
				quote = 0
			}
			escaped = current == '\\' && !escaped
			if current != '\\' {
				escaped = false
			}
			continue
		}
		if current == '\'' || current == '"' || current == '`' {
			quote = current
			continue
		}
		if current == '/' && i+1 < len(content) {
			if content[i+1] == '/' {
				if end := strings.IndexByte(content[i+2:], '\n'); end >= 0 {
					i += end + 1
					continue
				}
				return len(content)
			}
			if content[i+1] == '*' {
				if end := strings.Index(content[i+2:], "*/"); end >= 0 {
					i += end + 3
					continue
				}
				return len(content)
			}
			if javascriptRegexAllowedBefore(content, start, i) {
				if end := scanJavaScriptRegex(content, i); end > i+1 {
					i = end - 1
					continue
				}
			}
		}
		switch current {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(content)
}

func javascriptRegexAllowedBefore(content string, expressionStart, slashIndex int) bool {
	i := slashIndex - 1
	for i > expressionStart && unicode.IsSpace(rune(content[i])) {
		i--
	}
	if i <= expressionStart {
		return true
	}
	switch content[i] {
	case '{', '(', '[', ',', ':', ';', '=', '!', '?', '&', '|', '+', '-', '*', '%', '^', '~', '<':
		return true
	case '>':
		return i > expressionStart && content[i-1] == '=' // arrow body
	}
	if isASCIIIdentifierPart(content[i]) {
		end := i + 1
		for i > expressionStart && isASCIIIdentifierPart(content[i-1]) {
			i--
		}
		switch content[i:end] {
		case "return", "throw", "case", "delete", "void", "typeof", "instanceof", "in", "of", "yield", "await", "new":
			return true
		}
	}
	return false
}

func isASCIIIdentifierPart(value byte) bool {
	return value == '_' || value == '$' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func normalizeJSXText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if !strings.ContainsRune(text, '\n') {
		return text
	}
	lines := strings.Split(text, "\n")
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Trim(line, " \t")
		if line != "" {
			normalized = append(normalized, line)
		}
	}
	return strings.Join(normalized, " ")
}

func jsCanEndExpression(tok semanticToken) bool {
	if tok.kind == "identifier" || tok.kind == "number" || tok.kind == "string" {
		return true
	}
	switch tok.text {
	case ")", "]", "}", "++", "--":
		return true
	default:
		return false
	}
}

func jsCanStartExpression(tok semanticToken) bool {
	if tok.kind == "identifier" || tok.kind == "number" || tok.kind == "string" {
		return true
	}
	switch tok.text {
	case "{", "function", "class":
		return true
	default:
		return false
	}
}

var multiOperators = []string{
	">>>=", "===", "!==", "**=", "&&=", "||=", "??=", "<<=", ">>=", "...",
	">>>", "=>", "==", "!=", "<=", ">=", "++", "--", "&&", "||", "??", "?.",
	"+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "<<", ">>", "**", "::", ":=", "<-", "..",
}

func lexSemantic(content, language string) []semanticToken {
	tokens := make([]semanticToken, 0, len(content)/3)
	python := language == "py"
	line, col := 1, 0
	for i := 0; i < len(content); {
		startLine, startCol := line, col
		r, size := utf8.DecodeRuneInString(content[i:])
		if r == utf8.RuneError && size == 0 {
			break
		}

		if r == '\r' || r == '\n' {
			if r == '\r' && i+size < len(content) && content[i+size] == '\n' {
				i += size + 1
			} else {
				i += size
			}
			tokens = append(tokens, semanticToken{kind: "newline", text: "\n", line: line, col: col})
			line++
			col = 0
			continue
		}
		if unicode.IsSpace(r) {
			i += size
			if r == '\t' {
				col += 8 - col%8
			} else {
				col++
			}
			continue
		}

		if python && r == '#' {
			end := strings.IndexByte(content[i:], '\n')
			if end < 0 {
				end = len(content) - i
			}
			text := content[i : i+end]
			kind := "comment"
			if isDirectiveComment(text, ".py") {
				kind = "directive"
			}
			tokens = append(tokens, semanticToken{kind: kind, text: text, line: startLine, col: startCol})
			i += end
			col += utf8.RuneCountInString(text)
			continue
		}
		if !python && r == '/' && i+1 < len(content) && content[i+1] == '/' {
			end := strings.IndexByte(content[i:], '\n')
			if end < 0 {
				end = len(content) - i
			}
			text := content[i : i+end]
			kind := "comment"
			if isDirectiveComment(text, language) {
				kind = "directive"
			}
			tokens = append(tokens, semanticToken{kind: kind, text: text, line: startLine, col: startCol})
			i += end
			col += utf8.RuneCountInString(text)
			continue
		}
		if !python && r == '/' && i+1 < len(content) && content[i+1] == '*' {
			end := strings.Index(content[i+2:], "*/")
			if end < 0 {
				end = len(content) - i
			} else {
				end += 4
			}
			text := content[i : i+end]
			kind := "comment"
			if isDirectiveComment(text, language) {
				kind = "directive"
			}
			tokens = append(tokens, semanticToken{kind: kind, text: text, line: startLine, col: startCol})
			for _, commentRune := range text {
				if commentRune == '\n' {
					tokens = append(tokens, semanticToken{kind: "newline", text: "\n", line: line, col: col})
					line++
					col = 0
				} else {
					col++
				}
			}
			i += end
			continue
		}
		if language == "js" && r == '/' {
			end := scanJavaScriptRegex(content, i)
			if end > i+1 {
				text := content[i:end]
				tokens = append(tokens, semanticToken{kind: "string", text: text, line: startLine, col: startCol})
				i = end
				col += utf8.RuneCountInString(text)
				continue
			}
		}
		if language == ".rs" {
			if end := scanRustRawString(content, i); end > i {
				text := content[i:end]
				tokens = append(tokens, semanticToken{kind: "string", text: text, line: startLine, col: startCol})
				for _, rawRune := range text {
					if rawRune == '\n' {
						line++
						col = 0
					} else {
						col++
					}
				}
				i = end
				continue
			}
		}

		if r == '\'' || r == '"' || (!python && r == '`') {
			quote := r
			triple := python && i+2 < len(content) && content[i] == byte(quote) && content[i+1] == byte(quote) && content[i+2] == byte(quote)
			end := i + size
			if triple {
				end = i + 3
			}
			escaped := false
			for end < len(content) {
				current, currentSize := utf8.DecodeRuneInString(content[end:])
				if !escaped {
					if triple && end+2 < len(content) && content[end] == byte(quote) && content[end+1] == byte(quote) && content[end+2] == byte(quote) {
						end += 3
						break
					}
					if !triple && current == quote {
						end += currentSize
						break
					}
				}
				if current == '\\' && !escaped {
					escaped = true
				} else {
					escaped = false
				}
				end += currentSize
			}
			text := content[i:end]
			tokens = append(tokens, semanticToken{kind: "string", text: text, line: startLine, col: startCol})
			for _, stringRune := range text {
				if stringRune == '\n' {
					line++
					col = 0
				} else {
					col++
				}
			}
			i = end
			continue
		}

		if isIdentifierStart(r) {
			end := i + size
			for end < len(content) {
				next, nextSize := utf8.DecodeRuneInString(content[end:])
				if !isIdentifierPart(next) {
					break
				}
				end += nextSize
			}
			text := content[i:end]
			tokens = append(tokens, semanticToken{kind: "identifier", text: text, line: startLine, col: startCol})
			i = end
			col += utf8.RuneCountInString(text)
			continue
		}
		if unicode.IsDigit(r) {
			end := i + size
			for end < len(content) {
				next, nextSize := utf8.DecodeRuneInString(content[end:])
				if !(unicode.IsDigit(next) || unicode.IsLetter(next) || next == '_' || next == '.') {
					break
				}
				end += nextSize
			}
			text := content[i:end]
			tokens = append(tokens, semanticToken{kind: "number", text: text, line: startLine, col: startCol})
			i = end
			col += utf8.RuneCountInString(text)
			continue
		}

		op := string(r)
		for _, candidate := range multiOperators {
			if strings.HasPrefix(content[i:], candidate) {
				op = candidate
				break
			}
		}
		tokens = append(tokens, semanticToken{kind: "op", text: op, line: startLine, col: startCol})
		i += len(op)
		col += utf8.RuneCountInString(op)
	}
	return tokens
}

func scanJavaScriptRegex(content string, start int) int {
	if start >= len(content) || content[start] != '/' {
		return start
	}
	escaped := false
	inClass := false
	for i := start + 1; i < len(content); i++ {
		current := content[i]
		if current == '\n' || current == '\r' {
			return start
		}
		if escaped {
			escaped = false
			continue
		}
		if current == '\\' {
			escaped = true
			continue
		}
		if current == '[' {
			inClass = true
			continue
		}
		if current == ']' {
			inClass = false
			continue
		}
		if current == '/' && !inClass {
			i++
			for i < len(content) {
				r, size := utf8.DecodeRuneInString(content[i:])
				if !unicode.IsLetter(r) {
					break
				}
				i += size
			}
			return i
		}
	}
	return start
}

func scanRustRawString(content string, start int) int {
	i := start
	if strings.HasPrefix(content[i:], "br") {
		i += 2
	} else if strings.HasPrefix(content[i:], "r") {
		i++
	} else {
		return start
	}
	hashes := 0
	for i < len(content) && content[i] == '#' {
		hashes++
		i++
	}
	if i >= len(content) || content[i] != '"' {
		return start
	}
	i++
	closing := "\"" + strings.Repeat("#", hashes)
	end := strings.Index(content[i:], closing)
	if end < 0 {
		// An unterminated raw string is still safest as one opaque token.
		return len(content)
	}
	return i + end + len(closing)
}

func isIdentifierStart(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || r >= utf8.RuneSelf
}

func isIdentifierPart(r rune) bool {
	return isIdentifierStart(r) || unicode.IsDigit(r)
}

func isDirectiveComment(comment, ext string) bool {
	normalized := strings.ToLower(strings.TrimSpace(comment))
	if ext == ".rs" {
		// Rust doc comments desugar to attributes and participate in linting
		// and compilation (for example with deny(missing_docs)).
		return strings.HasPrefix(normalized, "///") && !strings.HasPrefix(normalized, "////") ||
			strings.HasPrefix(normalized, "//!") ||
			strings.HasPrefix(normalized, "/**") && !strings.HasPrefix(normalized, "/***") ||
			strings.HasPrefix(normalized, "/*!")
	}
	if ext == ".py" {
		return strings.HasPrefix(normalized, "#!") ||
			strings.Contains(normalized, "coding:") ||
			strings.Contains(normalized, "coding=") ||
			strings.HasPrefix(normalized, "# type:") ||
			strings.HasPrefix(normalized, "# noqa") ||
			strings.HasPrefix(normalized, "# pyright:") ||
			strings.HasPrefix(normalized, "# mypy:") ||
			strings.HasPrefix(normalized, "# pylint:") ||
			strings.HasPrefix(normalized, "# fmt:") ||
			strings.HasPrefix(normalized, "# isort:") ||
			strings.HasPrefix(normalized, "# ruff:")
	}
	markers := []string{
		"//go:", "// +build", "//line ", "/*line ", "@ts-", "/// <reference", "/// <amd-", "@jsx", "@vite", "webpack", "sourceurl=", "sourcemappingurl=",
		"eslint-", "prettier-ignore", "istanbul ignore", "c8 ignore", "@__pure__", "@__no_side_effects__", "nolint",
	}
	for _, marker := range markers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}
