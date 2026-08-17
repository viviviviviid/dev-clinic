package config

import "testing"

func TestAIDefaults(t *testing.T) {
	if Global.AIProvider != "codex" {
		t.Fatalf("AIProvider = %q, want codex", Global.AIProvider)
	}
	if Global.Gemini.Model != "gemini-3.6-flash" {
		t.Fatalf("Gemini model = %q, want gemini-3.6-flash", Global.Gemini.Model)
	}
}

func TestAIEnvironmentOverrides(t *testing.T) {
	original := *Global
	t.Cleanup(func() { *Global = original })
	t.Setenv("AI_PROVIDER", "  GEMINI ")
	t.Setenv("CODEX_MODEL", "  gpt-codex-test ")
	t.Setenv("CODEX_BIN", "  /opt/codex/bin/codex ")

	applyEnv()

	if Global.AIProvider != "gemini" {
		t.Fatalf("AIProvider = %q, want gemini", Global.AIProvider)
	}
	if Global.Codex.Model != "gpt-codex-test" {
		t.Fatalf("Codex model = %q, want gpt-codex-test", Global.Codex.Model)
	}
	if Global.Codex.Executable != "/opt/codex/bin/codex" {
		t.Fatalf("Codex executable = %q, want /opt/codex/bin/codex", Global.Codex.Executable)
	}
}
