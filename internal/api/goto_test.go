package api

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolveGotoFileConfinesCurrentProjectAndRejectsSymlinks(t *testing.T) {
	base := t.TempDir()
	projectDir := filepath.Join(base, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mainFile := filepath.Join(projectDir, "main.go")
	if err := os.WriteFile(mainFile, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resolvedProject, resolvedFile, err := resolveGotoFile(base, projectDir, "main.go")
	if err != nil {
		t.Fatalf("resolveGotoFile(valid) error = %v", err)
	}
	canonicalProject, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	canonicalFile := filepath.Join(canonicalProject, filepath.Base(mainFile))
	if resolvedProject != canonicalProject || resolvedFile != canonicalFile {
		t.Fatalf("resolveGotoFile(valid) = (%q, %q), want (%q, %q)", resolvedProject, resolvedFile, canonicalProject, canonicalFile)
	}

	outsideFile := filepath.Join(base, "outside.go")
	if err := os.WriteFile(outsideFile, []byte("package outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolveGotoFile(base, projectDir, outsideFile); err == nil {
		t.Fatal("resolveGotoFile accepted a file outside the current project")
	}

	linkedFile := filepath.Join(projectDir, "linked.go")
	if err := os.Symlink(mainFile, linkedFile); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, _, err := resolveGotoFile(base, projectDir, linkedFile); err == nil {
		t.Fatal("resolveGotoFile accepted a symbolic link")
	}
}

func TestRunGoplsDefinitionUsesProjectDirAndSanitizedEnv(t *testing.T) {
	projectDir := t.TempDir()
	file := filepath.Join(projectDir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	canonicalProject, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	canonicalFile := filepath.Join(canonicalProject, filepath.Base(file))
	binDir := t.TempDir()
	writeFakeGopls(t, binDir, `#!/bin/sh
if [ -n "${SUPABASE_SERVICE_ROLE_KEY:-}" ]; then
  exit 41
fi
if [ "$PWD" != "$EXPECTED_CWD" ]; then
  exit 42
fi
printf '%s:2:3\n' "$EXPECTED_FILE"
`)

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SUPABASE_SERVICE_ROLE_KEY", "must-not-reach-gopls")
	t.Setenv("EXPECTED_CWD", canonicalProject)
	t.Setenv("EXPECTED_FILE", canonicalFile)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := runGoplsDefinition(ctx, canonicalProject, canonicalFile, 1, 1)
	if err != nil {
		t.Fatalf("runGoplsDefinition() error = %v, output = %q", err, out)
	}
	if strings.TrimSpace(out) != canonicalFile+":2:3" {
		t.Fatalf("runGoplsDefinition() output = %q", out)
	}
}

func TestRunGoplsDefinitionKillsTimedOutProcessGroup(t *testing.T) {
	projectDir := t.TempDir()
	file := filepath.Join(projectDir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	writeFakeGopls(t, binDir, "#!/bin/sh\nsleep 10\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := runGoplsDefinition(ctx, projectDir, file, 1, 1)
	if err == nil {
		t.Fatal("runGoplsDefinition() succeeded after its deadline")
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("context error = %v, want deadline exceeded", ctx.Err())
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("timed-out process group took %v to stop", elapsed)
	}
}

func TestCappedCombinedOutputCapsAggregateBytes(t *testing.T) {
	output := &cappedCombinedOutput{}
	first := strings.Repeat("a", maxGotoOutputBytes-10)
	second := strings.Repeat("b", 100)
	if n, err := output.Write([]byte(first)); err != nil || n != len(first) {
		t.Fatalf("first Write() = (%d, %v)", n, err)
	}
	if n, err := output.Write([]byte(second)); err != nil || n != len(second) {
		t.Fatalf("second Write() = (%d, %v)", n, err)
	}
	if got := len(output.String()); got != maxGotoOutputBytes {
		t.Fatalf("capped output length = %d, want %d", got, maxGotoOutputBytes)
	}
	if !output.truncated {
		t.Fatal("capped output did not record truncation")
	}
}

func writeFakeGopls(t *testing.T, dir, body string) {
	t.Helper()
	path := filepath.Join(dir, "gopls")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}
