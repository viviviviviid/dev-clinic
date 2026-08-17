package pathguard

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAllowsMissingChild(t *testing.T) {
	base := t.TempDir()
	canonicalBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(canonicalBase, "src", "main.go")
	got, err := Resolve(base, "src/main.go")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != want {
		t.Fatalf("Resolve() = %q, want %q", got, want)
	}
}

func TestResolveRejectsPrefixCollision(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "project")
	outside := filepath.Join(root, "project-backup", "secret.txt")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(base, outside); !errors.Is(err, ErrOutsideBase) {
		t.Fatalf("Resolve() error = %v, want ErrOutsideBase", err)
	}
}

func TestResolveRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "project")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Resolve(base, "linked/secret.txt"); !errors.Is(err, ErrOutsideBase) {
		t.Fatalf("Resolve() error = %v, want ErrOutsideBase", err)
	}
}

func TestResolveRelativeRejectsUnsafeNames(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"../secret", "src/../../secret", filepath.Join(base, "absolute.go"), "."} {
		t.Run(name, func(t *testing.T) {
			if _, err := ResolveRelative(base, name); err == nil {
				t.Fatalf("ResolveRelative(%q) succeeded", name)
			}
		})
	}
}

func TestResolveChildRejectsBase(t *testing.T) {
	base := t.TempDir()
	if _, err := ResolveChild(base, base); err == nil {
		t.Fatal("ResolveChild(base, base) succeeded")
	}
}

func TestResolveChildNoSymlinksAllowsMissingChild(t *testing.T) {
	base := t.TempDir()
	canonicalBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(canonicalBase, "missing", "file.go")
	got, err := ResolveChildNoSymlinks(base, "missing/file.go")
	if err != nil {
		t.Fatalf("ResolveChildNoSymlinks() error = %v", err)
	}
	if got != want {
		t.Fatalf("ResolveChildNoSymlinks() = %q, want %q", got, want)
	}
}

func TestResolveChildNoSymlinksRejectsLeafAndParentLinks(t *testing.T) {
	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	realFile := filepath.Join(realDir, "main.go")
	if err := os.WriteFile(realFile, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realFile, filepath.Join(base, "linked.go")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.Symlink(realDir, filepath.Join(base, "linked-dir")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	for _, candidate := range []string{"linked.go", "linked-dir/main.go"} {
		if _, err := ResolveChildNoSymlinks(base, candidate); err == nil {
			t.Fatalf("ResolveChildNoSymlinks(%q) accepted a symlink", candidate)
		}
	}
}

func TestResolveChildNoSymlinksRejectsBaseAndOutside(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "base")
	if err := os.Mkdir(base, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{base, root, "../outside"} {
		if _, err := ResolveChildNoSymlinks(base, candidate); err == nil {
			t.Fatalf("ResolveChildNoSymlinks(%q) succeeded", candidate)
		}
	}
}
