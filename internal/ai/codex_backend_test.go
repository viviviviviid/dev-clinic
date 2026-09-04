package ai

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCodexExecArgsAreLockedDown(t *testing.T) {
	args := codexExecArgs("/tmp/empty-workspace", "/tmp/last.txt", "/tmp/schema.json", "gpt-test")

	if len(args) == 0 || args[0] != "exec" {
		t.Fatalf("first argument = %v, want exec", args)
	}
	for _, required := range []string{
		"--ephemeral", "--skip-git-repo-check", "--ignore-user-config", "--ignore-rules",
		"--config", `approval_policy="never"`,
		"--sandbox", "read-only", "--cd", "/tmp/empty-workspace",
		"--output-schema", "/tmp/schema.json", "-o", "/tmp/last.txt", "--model", "gpt-test",
	} {
		if !slices.Contains(args, required) {
			t.Errorf("args missing %q: %v", required, args)
		}
	}
	if got := args[len(args)-1]; got != "-" {
		t.Fatalf("last argument = %q, want stdin marker '-'", got)
	}

	disabled := disabledFeaturesFromArgs(args)
	for _, feature := range codexDisabledFeatures {
		if !slices.Contains(disabled, feature) {
			t.Errorf("feature %q was not disabled: %v", feature, disabled)
		}
	}
}

func TestResolveCodexExecutableUsesHomeLocalFallback(t *testing.T) {
	home := t.TempDir()
	fallback := filepath.Join(home, ".local", "bin", "codex")
	if err := os.MkdirAll(filepath.Dir(fallback), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fallback, []byte("test executable"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")

	got, err := resolveCodexExecutable("codex")
	if err != nil {
		t.Fatalf("resolve fallback: %v", err)
	}
	if got != fallback {
		t.Fatalf("resolved executable = %q, want %q", got, fallback)
	}
}

func TestResolveCodexExecutableReportsMissingConfiguredBinary(t *testing.T) {
	t.Setenv("PATH", "")
	if _, err := resolveCodexExecutable("missing-codex-test-binary"); err == nil {
		t.Fatal("missing configured executable was accepted")
	}
}

func TestSanitizedCodexEnvRemovesApplicationSecrets(t *testing.T) {
	input := []string{
		"HOME=/home/test", "CODEX_HOME=/home/test/.codex", "PATH=/usr/bin", "LANG=ko_KR.UTF-8",
		"GEMINI_API_KEY=gemini-secret", "SUPABASE_URL=https://example.invalid", "SUPABASE_SERVICE_ROLE_KEY=role-secret",
		"SUPABASE_JWT_SECRET=jwt-secret", "SERVICE_ROLE=role-secret", "OPENAI_API_KEY=openai-secret",
		"MY_TOKEN=token-secret", "DATABASE_URL=postgres://secret", "SSH_AUTH_SOCK=/tmp/agent.sock",
	}
	got := sanitizedCodexEnv(input)
	for _, preserved := range []string{"HOME=/home/test", "CODEX_HOME=/home/test/.codex", "PATH=/usr/bin", "LANG=ko_KR.UTF-8"} {
		if !slices.Contains(got, preserved) {
			t.Errorf("required runtime environment %q was removed: %v", preserved, got)
		}
	}
	for _, entry := range got {
		for _, secret := range []string{"gemini-secret", "role-secret", "jwt-secret", "openai-secret", "token-secret", "postgres://secret", "agent.sock"} {
			if strings.Contains(entry, secret) {
				t.Errorf("secret %q survived sanitization in %q", secret, entry)
			}
		}
	}
}

func TestCodexSemaphoreHonorsCanceledContext(t *testing.T) {
	codexRunSlot <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := (&codexBackend{}).generate(ctx, "system", "prompt", nil)
	<-codexRunSlot
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("generate error = %v, want context.Canceled", err)
	}
}

func TestCodexDeadlinesStopRunningAndWaitingRequests(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Codex process-group handling targets macOS and Unix")
	}

	executable, _ := writeHungCodexExecutable(t)

	client := &Client{codex: &codexBackend{executable: executable}}
	firstCtx, firstCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer firstCancel()
	firstDone := make(chan error, 1)
	go func() {
		_, err := client.generateWithSystem(firstCtx, "system", "first")
		firstDone <- err
	}()

	waitForCodexSlot(t, time.Second)
	secondCtx, secondCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	started := time.Now()
	_, secondErr := client.generateWithSystem(secondCtx, "system", "second")
	secondCancel()
	if !errors.Is(secondErr, context.DeadlineExceeded) {
		t.Fatalf("waiting request error = %v, want deadline exceeded", secondErr)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("waiting request ignored shorter deadline: %v", elapsed)
	}

	select {
	case firstErr := <-firstDone:
		if !errors.Is(firstErr, context.DeadlineExceeded) {
			t.Fatalf("running request error = %v, want deadline exceeded", firstErr)
		}
	case <-time.After(time.Second):
		t.Fatal("running Codex request did not stop after its deadline")
	}

	select {
	case codexRunSlot <- struct{}{}:
		<-codexRunSlot
	default:
		t.Fatal("Codex semaphore slot leaked after cancellation")
	}
}

func TestCodexCancellationKillsChildProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Codex process-group handling targets macOS and Unix")
	}
	executable, pidFile := writeHungCodexExecutable(t)
	client := &Client{codex: &codexBackend{executable: executable}}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	done := make(chan error, 1)
	go func() {
		_, err := client.generateWithSystem(ctx, "system", "process group")
		done <- err
	}()

	waitForCodexSlot(t, 2*time.Second)
	childPID := waitForPIDFile(t, pidFile, done, 5*time.Second)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled Codex error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Codex process did not return after cancellation")
	}
	waitForProcessExit(t, childPID, time.Second)
}

func TestCodexExecArgsOmitOptionalFlags(t *testing.T) {
	args := codexExecArgs("/tmp/work", "/tmp/out", "", "")
	if slices.Contains(args, "--model") {
		t.Fatalf("blank model should not emit --model: %v", args)
	}
	if slices.Contains(args, "--output-schema") {
		t.Fatalf("blank schema should not emit --output-schema: %v", args)
	}
}

func TestComposeCodexPromptSeparatesSystemAndTask(t *testing.T) {
	got := composeCodexPrompt("system rule", "user task")
	for _, want := range []string{"<SYSTEM_INSTRUCTION>", "system rule", "</SYSTEM_INSTRUCTION>", "<USER_TASK>", "user task", "</USER_TASK>"} {
		if !slices.Contains(splitPromptTokens(got), want) {
			t.Fatalf("prompt missing %q: %s", want, got)
		}
	}
}

func TestCodexLiveStructuredOutput(t *testing.T) {
	if os.Getenv("CODEX_LIVE_TEST") != "1" {
		t.Skip("set CODEX_LIVE_TEST=1 to use the saved Codex CLI login")
	}

	backend, err := newCodexBackend(os.Getenv("CODEX_BIN"), os.Getenv("CODEX_MODEL"))
	if err != nil {
		t.Fatalf("initialize Codex CLI backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"ok": map[string]any{"type": "boolean"},
		},
		"required": []string{"ok"},
	}
	output, err := backend.generate(
		ctx,
		"This is a health check. Return only output that satisfies the supplied JSON schema.",
		"Set ok to true.",
		schema,
	)
	if err != nil {
		t.Fatalf("Codex CLI live smoke failed: %v", err)
	}

	var response struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		t.Fatalf("Codex CLI returned invalid structured output: %v", err)
	}
	if !response.OK {
		t.Fatal("Codex CLI structured output did not confirm the health check")
	}
}

func TestCodexLiveTutorContract(t *testing.T) {
	if os.Getenv("CODEX_LIVE_CONTRACT_TEST") != "1" {
		t.Skip("set CODEX_LIVE_CONTRACT_TEST=1 to verify curriculum and code prompts with the saved Codex login")
	}

	backend, err := newCodexBackend(os.Getenv("CODEX_BIN"), os.Getenv("CODEX_MODEL"))
	if err != nil {
		t.Fatalf("initialize Codex CLI backend: %v", err)
	}
	client := &Client{codex: backend}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	curriculum, err := client.GenerateCurriculum(ctx, "go", "문자열에서 각 문자의 빈도를 세는 작은 CLI", "normal")
	if err != nil {
		t.Fatalf("live curriculum contract: %v", err)
	}
	files, err := client.GenerateCodeFiles(ctx, curriculum, nil)
	if err != nil {
		t.Fatalf("live code contract: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("live code contract returned no files")
	}
}

func TestCodexLiveTopicContract(t *testing.T) {
	if os.Getenv("CODEX_LIVE_TOPIC_TEST") != "1" {
		t.Skip("set CODEX_LIVE_TOPIC_TEST=1 to verify topic recommendations with the saved Codex CLI login")
	}

	backend, err := newCodexBackend(os.Getenv("CODEX_BIN"), os.Getenv("CODEX_MODEL"))
	if err != nil {
		t.Fatalf("initialize Codex CLI backend: %v", err)
	}
	client := &Client{codex: backend}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topics, err := client.GenerateDailyTopics(ctx, "go", "normal", []string{
		"JSON 설정 파일 검증기",
		"파일 업로드 제한 미들웨어",
		"TTL 인메모리 캐시",
	})
	if err != nil {
		t.Fatalf("live topic contract: %v", err)
	}
	for _, topic := range topics {
		t.Logf("%s/%s: %s", topic.Difficulty, topic.Style, topic.Name)
	}
}

func disabledFeaturesFromArgs(args []string) []string {
	var result []string
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--disable" {
			result = append(result, args[i+1])
		}
	}
	return result
}

func splitPromptTokens(prompt string) []string {
	var result []string
	start := 0
	for i, r := range prompt {
		if r == '\n' {
			result = append(result, prompt[start:i])
			start = i + 1
		}
	}
	if start < len(prompt) {
		result = append(result, prompt[start:])
	}
	return result
}

func waitForPIDFile(t *testing.T, path string, done <-chan error, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("fake Codex executable exited before writing its child PID: %v", err)
		default:
		}
		data, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr != nil {
				t.Fatalf("parse child PID: %v", parseErr)
			}
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("fake Codex executable did not start its child process")
	return 0
}

func writeHungCodexExecutable(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	executable := filepath.Join(dir, "hung-codex")
	pidFile := executable + ".pid"
	script := "#!/bin/sh\n/bin/sleep 30 &\nchild_pid=$!\nprintf '%s\\n' \"$child_pid\" > \"$0.pid\"\nwait \"$child_pid\"\n"
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return executable, pidFile
}

func waitForCodexSlot(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(codexRunSlot) == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("Codex request did not acquire the generation slot")
}

func waitForProcessExit(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, 0)
		if err == syscall.ESRCH {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("child process %d survived Codex cancellation", pid)
}
