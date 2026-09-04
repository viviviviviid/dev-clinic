package completion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEvaluateRequiresPassingTestsAndResolvedMarkers(t *testing.T) {
	base := t.TempDir()
	projectDir := filepath.Join(base, "project")
	writeTestFile(t, filepath.Join(projectDir, "main.go"), "package main\n")

	result := Evaluate(base, projectDir, true)
	if !result.Complete || result.ScannedFiles != 1 {
		t.Fatalf("clean passing result = %+v", result)
	}

	result = Evaluate(base, projectDir, false)
	if result.Complete || !strings.Contains(result.Summary(), "전체 테스트 미통과") {
		t.Fatalf("failing-test result = %+v, summary = %q", result, result.Summary())
	}

	writeTestFile(t, filepath.Join(projectDir, "main.go"), "package main\n// [TUTOR:HOLE]\n// [TUTOR:BUG]\n")
	result = Evaluate(base, projectDir, true)
	if !result.Complete || result.HoleCount != 1 || result.BugCount != 1 {
		t.Fatalf("marker-annotation result = %+v", result)
	}
	if !strings.Contains(result.Summary(), "학습 범위 marker 2개") {
		t.Fatalf("marker summary = %q", result.Summary())
	}

	writeTestFile(t, filepath.Join(projectDir, "main.go"), "package main\n// [TUTOR:END]\n")
	result = Evaluate(base, projectDir, true)
	if !result.Complete || result.HoleCount != 0 || result.BugCount != 0 || result.EndCount != 1 {
		t.Fatalf("marker-annotation result = %+v", result)
	}
	if !strings.Contains(result.Summary(), "END/범위 종료 1") {
		t.Fatalf("orphan-end summary = %q", result.Summary())
	}
}

func TestEvaluateIgnoresDependenciesAndNonSourceFiles(t *testing.T) {
	base := t.TempDir()
	projectDir := filepath.Join(base, "project")
	writeTestFile(t, filepath.Join(projectDir, "main.ts"), "export const ok = true\n")
	writeTestFile(t, filepath.Join(projectDir, "README.md"), "[TUTOR:HOLE]\n")
	writeTestFile(t, filepath.Join(projectDir, "node_modules", "dependency.ts"), "[TUTOR:BUG]\n")
	writeTestFile(t, filepath.Join(projectDir, ".cache", "generated.go"), "[TUTOR:HOLE]\n")

	result := Evaluate(base, projectDir, true)
	if !result.Complete || result.ScannedFiles != 1 || result.HoleCount != 0 || result.BugCount != 0 || result.EndCount != 0 {
		t.Fatalf("ignored-path result = %+v", result)
	}
}

func TestEvaluateFailsClosedForUnsafeOrAmbiguousScans(t *testing.T) {
	t.Run("outside base", func(t *testing.T) {
		base := t.TempDir()
		outside := t.TempDir()
		writeTestFile(t, filepath.Join(outside, "main.go"), "package main\n")
		result := Evaluate(base, outside, true)
		if result.Complete || result.ScanErr == nil {
			t.Fatalf("outside-base result = %+v", result)
		}
	})

	t.Run("source symlink", func(t *testing.T) {
		base := t.TempDir()
		projectDir := filepath.Join(base, "project")
		writeTestFile(t, filepath.Join(projectDir, "main.go"), "package main\n")
		outside := filepath.Join(t.TempDir(), "outside.go")
		writeTestFile(t, outside, "// [TUTOR:HOLE]\n")
		if err := os.Symlink(outside, filepath.Join(projectDir, "linked.go")); err != nil {
			t.Fatal(err)
		}
		result := Evaluate(base, projectDir, true)
		if result.Complete || result.ScanErr == nil || !strings.Contains(result.ScanErr.Error(), "symbolic link") {
			t.Fatalf("symlink result = %+v", result)
		}
	})

	t.Run("no source files", func(t *testing.T) {
		base := t.TempDir()
		projectDir := filepath.Join(base, "project")
		writeTestFile(t, filepath.Join(projectDir, "README.md"), "docs\n")
		result := Evaluate(base, projectDir, true)
		if result.Complete || result.ScanErr == nil {
			t.Fatalf("empty-source result = %+v", result)
		}
	})
}

func TestEvaluateFailsClosedAtFileAndByteLimits(t *testing.T) {
	base := t.TempDir()
	projectDir := filepath.Join(base, "project")
	writeTestFile(t, filepath.Join(projectDir, "one.go"), "1234")
	writeTestFile(t, filepath.Join(projectDir, "two.go"), "5678")

	result := evaluate(base, projectDir, true, limits{files: 1, totalBytes: 100, bytesPerFile: 100})
	if result.Complete || result.ScanErr == nil || !strings.Contains(result.ScanErr.Error(), "file limit") {
		t.Fatalf("file-limit result = %+v", result)
	}

	result = evaluate(base, projectDir, true, limits{files: 10, totalBytes: 6, bytesPerFile: 100})
	if result.Complete || result.ScanErr == nil || !strings.Contains(result.ScanErr.Error(), "total source byte limit") {
		t.Fatalf("total-byte-limit result = %+v", result)
	}

	result = evaluate(base, projectDir, true, limits{files: 10, totalBytes: 100, bytesPerFile: 3})
	if result.Complete || result.ScanErr == nil || !strings.Contains(result.ScanErr.Error(), "file byte limit") {
		t.Fatalf("per-file-limit result = %+v", result)
	}
}
