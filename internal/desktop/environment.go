package desktop

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type limitedOutput struct{ data []byte }

func (w *limitedOutput) Write(p []byte) (int, error) {
	if len(w.data)+len(p) > 64<<10 {
		return 0, io.ErrShortBuffer
	}
	w.data = append(w.data, p...)
	return len(p), nil
}

// Finder does not inherit the terminal's PATH. Import only PATH, with a timeout,
// so Homebrew, nvm, Go and Codex remain discoverable without importing secrets.
func RestorePath(ctx context.Context) {
	home, _ := os.UserHomeDir()
	paths := []string{os.Getenv("PATH"), filepath.Join(home, ".local/bin"), filepath.Join(home, "go/bin"), "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"}
	shell := os.Getenv("SHELL")
	if shell != "/bin/bash" && shell != "/bin/zsh" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-ilc", `printf '\n__CLINIC_PATH__=%s\n' "$PATH"`)
	var output limitedOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if cmd.Run() == nil {
		for _, line := range strings.Split(string(output.data), "\n") {
			if path, ok := strings.CutPrefix(line, "__CLINIC_PATH__="); ok {
				paths = append([]string{path}, paths...)
			}
		}
	}
	seen := map[string]bool{}
	var result []string
	for _, path := range strings.Split(strings.Join(paths, ":"), ":") {
		// Do not search the app's working directory through an empty/relative entry.
		if filepath.IsAbs(path) && !seen[path] {
			seen[path] = true
			result = append(result, path)
		}
	}
	_ = os.Setenv("PATH", strings.Join(result, ":"))
}
