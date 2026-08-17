package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndRestore(t *testing.T) {
	projectDir := t.TempDir()
	file := filepath.Join(projectDir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(projectDir, "Step 1"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := os.WriteFile(file, []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Restore(projectDir, "Step 1"); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package main\n" {
		t.Fatalf("restored content = %q", got)
	}
}

func TestRestoreRejectsTraversalLabel(t *testing.T) {
	projectDir := t.TempDir()
	file := filepath.Join(projectDir, "main.go")
	if err := os.WriteFile(file, []byte("must remain"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Restore(projectDir, ".."); err == nil {
		t.Fatal("Restore() accepted a traversal label")
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "must remain" {
		t.Fatalf("file changed after rejected restore: %q", got)
	}
}

func TestSaveRejectsSourceSymlink(t *testing.T) {
	projectDir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(projectDir, "linked.go")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if err := Save(projectDir, "Step 1"); err == nil {
		t.Fatal("Save() accepted a source symlink")
	}
}

func TestRestoreRejectsSnapshotSymlink(t *testing.T) {
	projectDir := t.TempDir()
	dir, err := snapshotDir(projectDir, "Step 1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linked.go")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if err := Restore(projectDir, "Step 1"); err == nil {
		t.Fatal("Restore() accepted a snapshot symlink")
	}
}

func TestRestoreMakesManagedFilesExactAndPreservesUnmanagedFiles(t *testing.T) {
	projectDir := t.TempDir()
	initial := map[string]string{
		"main.go":      "package main\n",
		"go.mod":       "module example\n",
		"package.json": `{"name":"before"}`,
		"TUTORSYS.md":  "before curriculum",
		"notes.md":     "user notes",
		"data.json":    `{"user":"data"}`,
		".env":         "SECRET=preserve",
	}
	writeSnapshotTestFiles(t, projectDir, initial)
	if err := Save(projectDir, "Step 1"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	writeSnapshotTestFiles(t, projectDir, map[string]string{
		"main.go":              "changed\n",
		"later.go":             "package main\n",
		"src/later.ts":         "export const later = true\n",
		"go.sum":               "later checksum\n",
		"package-lock.json":    `{"lockfileVersion":3}`,
		"requirements-dev.txt": "pytest\n",
		"quiz.json":            `{"later":true}`,
		"notes.md":             "updated user notes",
		"data.json":            `{"user":"updated"}`,
		".env":                 "SECRET=still-preserved",
	})

	if err := Restore(projectDir, "Step 1"); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	for name, want := range map[string]string{
		"main.go":      initial["main.go"],
		"go.mod":       initial["go.mod"],
		"package.json": initial["package.json"],
		"TUTORSYS.md":  initial["TUTORSYS.md"],
		"notes.md":     "updated user notes",
		"data.json":    `{"user":"updated"}`,
		".env":         "SECRET=still-preserved",
	} {
		data, err := os.ReadFile(filepath.Join(projectDir, name))
		if err != nil || string(data) != want {
			t.Fatalf("restored %s = %q, %v; want %q", name, data, err, want)
		}
	}
	for _, name := range []string{"later.go", "src/later.ts", "go.sum", "package-lock.json", "requirements-dev.txt", "quiz.json"} {
		if _, err := os.Lstat(filepath.Join(projectDir, name)); !os.IsNotExist(err) {
			t.Fatalf("managed file absent from snapshot was not removed: %s (%v)", name, err)
		}
	}
}

func TestRestorePreflightsCurrentSymlinkBeforeChangingFiles(t *testing.T) {
	projectDir := t.TempDir()
	mainFile := filepath.Join(projectDir, "main.go")
	if err := os.WriteFile(mainFile, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(projectDir, "Step 1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainFile, []byte("must remain after failed restore\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(projectDir, "target.txt")
	if err := os.WriteFile(target, []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(projectDir, "linked.go")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if err := Restore(projectDir, "Step 1"); err == nil {
		t.Fatal("Restore() accepted a current managed symlink")
	}
	data, err := os.ReadFile(mainFile)
	if err != nil || string(data) != "must remain after failed restore\n" {
		t.Fatalf("preflight failure partially restored main.go: %q, %v", data, err)
	}
}

func TestSaveReplacesSameLabelWithoutStaleManagedFiles(t *testing.T) {
	projectDir := t.TempDir()
	mainFile := filepath.Join(projectDir, "main.go")
	staleFile := filepath.Join(projectDir, "stale.go")
	writeSnapshotTestFiles(t, projectDir, map[string]string{
		"main.go":  "before\n",
		"stale.go": "stale\n",
	})
	if err := Save(projectDir, "Step 1"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(staleFile); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainFile, []byte("second snapshot\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(projectDir, "Step 1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainFile, []byte("changed later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Restore(projectDir, "Step 1"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(mainFile)
	if err != nil || string(data) != "second snapshot\n" {
		t.Fatalf("main.go = %q, %v", data, err)
	}
	if _, err := os.Lstat(staleFile); !os.IsNotExist(err) {
		t.Fatalf("stale.go reappeared from reused snapshot: %v", err)
	}
}

func TestManagedSnapshotFileAllowlist(t *testing.T) {
	for _, name := range []string{
		"main.go", "src/index.ts", "go.mod", "Cargo.toml", "package-lock.json",
		"tsconfig.test.json", "jest.config.json", "pyproject.toml", "requirements-dev.txt",
	} {
		if !isManagedSnapshotFile(name) {
			t.Errorf("expected managed snapshot file: %s", name)
		}
	}
	for _, name := range []string{".env", "notes.md", "data.json", "credentials.json", "private.pem"} {
		if isManagedSnapshotFile(name) {
			t.Errorf("unexpected managed snapshot file: %s", name)
		}
	}
}

func writeSnapshotTestFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
