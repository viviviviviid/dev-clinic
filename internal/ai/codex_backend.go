package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const maxCodexOutputBytes = 4 << 20

// Codex runs are intentionally serialized. The CLI uses the user's stored
// ChatGPT/Codex authentication, and a single in-flight generation avoids
// competing interactive-account jobs and keeps local resource use bounded.
var codexRunSlot = make(chan struct{}, 1)

type codexBackend struct {
	executable string
	model      string
}

func newCodexBackend(executable, model string) (*codexBackend, error) {
	resolved, err := resolveCodexExecutable(executable)
	if err != nil {
		return nil, err
	}
	return &codexBackend{executable: resolved, model: strings.TrimSpace(model)}, nil
}

func resolveCodexExecutable(configured string) (string, error) {
	requested := strings.TrimSpace(configured)
	if requested == "" {
		requested = "codex"
	}
	if resolved, err := exec.LookPath(requested); err == nil {
		if filepath.IsAbs(resolved) {
			return resolved, nil
		}
		absolute, absErr := filepath.Abs(resolved)
		if absErr == nil {
			return absolute, nil
		}
		return "", fmt.Errorf("codex: resolve executable %q: %w", requested, absErr)
	}
	if requested != "codex" {
		return "", fmt.Errorf("codex executable %q was not found or is not executable; set CODEX_BIN or [codex].executable", requested)
	}

	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		fallback := filepath.Join(home, ".local", "bin", "codex")
		if resolved, fallbackErr := exec.LookPath(fallback); fallbackErr == nil {
			return resolved, nil
		}
		return "", fmt.Errorf("codex executable was not found in PATH or at %q; install Codex or set CODEX_BIN/[codex].executable", fallback)
	}
	return "", fmt.Errorf("codex executable was not found in PATH and the home-directory fallback could not be resolved; install Codex or set CODEX_BIN/[codex].executable")
}

// codexDisabledFeatures prevents the generation-only backend from acquiring
// tools. The prompt and optional JSON Schema are its complete inputs.
var codexDisabledFeatures = []string{
	"shell_tool",
	"unified_exec",
	"code_mode",
	"code_mode_host",
	"browser_use",
	"browser_use_external",
	"in_app_browser",
	"apps",
	"enable_mcp_apps",
	"auth_elicitation",
	"tool_call_mcp_elicitation",
	"request_permissions_tool",
	"exec_permission_approvals",
	"guardian_approval",
	"computer_use",
	"multi_agent",
	"multi_agent_v2",
	"plugins",
	"remote_plugin",
	"hooks",
	"image_generation",
	"view_image",
	"workspace_dependencies",
	"skill_search",
	"skill_mcp_dependency_install",
	"tool_suggest",
	"browser_use_full_cdp_access",
	"standalone_web_search",
	"shell_snapshot",
}

func codexExecArgs(workDir, outputPath, schemaPath, model string) []string {
	args := []string{
		"exec",
		"--ephemeral",
		"--skip-git-repo-check",
		"--ignore-user-config",
		"--ignore-rules",
		"--config", `approval_policy="never"`,
		"--sandbox", "read-only",
		"--cd", workDir,
		"--color", "never",
	}
	for _, feature := range codexDisabledFeatures {
		args = append(args, "--disable", feature)
	}
	if model = strings.TrimSpace(model); model != "" {
		args = append(args, "--model", model)
	}
	if schemaPath != "" {
		args = append(args, "--output-schema", schemaPath)
	}
	args = append(args, "-o", outputPath, "-")
	return args
}

func (b *codexBackend) generate(ctx context.Context, system, prompt string, schema map[string]any) (string, error) {
	select {
	case codexRunSlot <- struct{}{}:
		defer func() { <-codexRunSlot }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	tempRoot, err := os.MkdirTemp("", "coding-tutor-codex-")
	if err != nil {
		return "", fmt.Errorf("codex: create temporary workspace: %w", err)
	}
	defer os.RemoveAll(tempRoot)
	workDir := filepath.Join(tempRoot, "workspace")
	if err := os.Mkdir(workDir, 0700); err != nil {
		return "", fmt.Errorf("codex: create empty working directory: %w", err)
	}

	outputPath := filepath.Join(tempRoot, "last-message.txt")
	schemaPath := ""
	if schema != nil {
		schemaBytes, marshalErr := json.Marshal(schema)
		if marshalErr != nil {
			return "", fmt.Errorf("codex: encode output schema: %w", marshalErr)
		}
		schemaPath = filepath.Join(tempRoot, "output-schema.json")
		if writeErr := os.WriteFile(schemaPath, schemaBytes, 0600); writeErr != nil {
			return "", fmt.Errorf("codex: write output schema: %w", writeErr)
		}
	}

	cmd := exec.CommandContext(ctx, b.executable, codexExecArgs(workDir, outputPath, schemaPath, b.model)...)
	cmd.Dir = workDir
	cmd.Env = sanitizedCodexEnv(os.Environ())
	cmd.Stdin = strings.NewReader(composeCodexPrompt(system, prompt))
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	prepareCodexCommand(cmd)

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", fmt.Errorf("codex exec: %w: %s", err, clipForError(stderr.String(), 4096))
	}

	output, err := readFileLimited(outputPath, maxCodexOutputBytes)
	if err != nil {
		return "", fmt.Errorf("codex: read final response: %w", err)
	}
	text := strings.TrimSpace(string(output))
	if text == "" {
		return "", fmt.Errorf("codex: empty final response")
	}
	return text, nil
}

func prepareCodexCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if err == syscall.ESRCH {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
}

func sanitizedCodexEnv(environ []string) []string {
	result := make([]string, 0, len(environ))
	for _, entry := range environ {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key == "" || isSensitiveCodexEnvKey(key) {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func isSensitiveCodexEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, fragment := range []string{
		"API_KEY", "TOKEN", "SECRET", "PASSWORD", "PASSWD", "PRIVATE_KEY",
		"CREDENTIAL", "SERVICE_ROLE", "COOKIE",
	} {
		if strings.Contains(upper, fragment) {
			return true
		}
	}
	if upper == "DATABASE_URL" || upper == "REDIS_URL" || upper == "SSH_AUTH_SOCK" || upper == "GPG_AGENT_INFO" || strings.HasSuffix(upper, "_DSN") {
		return true
	}
	for _, prefix := range []string{"SUPABASE_", "GEMINI_", "OPENAI_", "ANTHROPIC_", "AWS_", "AZURE_", "GITHUB_", "GITLAB_"} {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

func composeCodexPrompt(system, prompt string) string {
	return "<SYSTEM_INSTRUCTION>\n" + strings.TrimSpace(system) +
		"\n</SYSTEM_INSTRUCTION>\n\n<USER_TASK>\n" + strings.TrimSpace(prompt) +
		"\n</USER_TASK>\n"
}

func readFileLimited(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return b, nil
}

func clipForError(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
