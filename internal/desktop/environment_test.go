package desktop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestorePathFromFinderImportsOnlyPath(t *testing.T) {
	if _, err := os.Stat("/bin/zsh"); err != nil {
		t.Skip("macOS zsh is unavailable")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", home)
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("PATH", "/usr/bin:/bin:.:relative")
	t.Setenv("OPENAI_API_KEY", "")
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("export PATH=/test/developer/bin:$PATH\nexport OPENAI_API_KEY=must-not-import\n"), 0600); err != nil {
		t.Fatal(err)
	}
	RestorePath(context.Background())
	paths := strings.Split(os.Getenv("PATH"), ":")
	if paths[0] != "/test/developer/bin" {
		t.Fatalf("login-shell PATH was not loaded: %v", paths)
	}
	if os.Getenv("OPENAI_API_KEY") != "" {
		t.Fatal("imported a secret environment variable")
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			t.Fatalf("unsafe relative PATH entry: %q", path)
		}
	}
}
