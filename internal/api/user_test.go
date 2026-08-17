package api

import "testing"

func TestCanonicalUserLanguageMatchesSupportedRuntime(t *testing.T) {
	tests := map[string]string{
		"go": "Go", "TypeScript": "TypeScript", "javascript": "JavaScript",
		" RUST ": "Rust", "Python": "Python",
	}
	for input, want := range tests {
		got, ok := canonicalUserLanguage(input)
		if !ok || got != want {
			t.Errorf("canonicalUserLanguage(%q) = %q, %v; want %q", input, got, ok, want)
		}
	}
	for _, input := range []string{"Solidity", "sol", "", "Ruby"} {
		if got, ok := canonicalUserLanguage(input); ok {
			t.Errorf("unsupported language %q was accepted as %q", input, got)
		}
	}
}
