package watcher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coding-tutor/internal/ai"
	"github.com/coding-tutor/internal/project"
	"github.com/fsnotify/fsnotify"
)

func TestAutoTestDisabledByDefault(t *testing.T) {
	t.Setenv("CODING_TUTOR_AUTO_TEST", "")
	if autoTestEnabled() {
		t.Fatal("auto tests are enabled without an explicit opt-in")
	}
	t.Setenv("CODING_TUTOR_AUTO_TEST", "true")
	if !autoTestEnabled() {
		t.Fatal("auto tests were not enabled by explicit opt-in")
	}
}

func TestWatchDirSkipsIgnoredDirectories(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"src/nested", "node_modules/pkg", ".git/objects", "dist/assets"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()
	if err := watchDir(watcher, root); err != nil {
		t.Fatalf("watchDir() error = %v", err)
	}

	got := map[string]bool{}
	for _, path := range watcher.WatchList() {
		got[filepath.Clean(path)] = true
	}
	for _, expected := range []string{root, filepath.Join(root, "src"), filepath.Join(root, "src", "nested")} {
		if !got[filepath.Clean(expected)] {
			t.Errorf("missing watch for %s; got %#v", expected, got)
		}
	}
	for path := range got {
		if containsIgnoredPath(root, path) {
			t.Errorf("ignored directory was watched: %s", path)
		}
	}
}

func TestLimitedOutputCapsBytes(t *testing.T) {
	output := &limitedOutput{max: 4}
	if n, err := output.Write([]byte("abcdef")); err != nil || n != 6 {
		t.Fatalf("Write() = (%d, %v)", n, err)
	}
	if got := output.buffer.String(); got != "abcd" {
		t.Fatalf("buffer = %q, want abcd", got)
	}
	if !output.truncated {
		t.Fatal("truncation was not recorded")
	}
}

func TestRunTestsUsesExitStatusAndNormalizesLanguage(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("SUPABASE_SERVICE_ROLE_KEY", "must-not-reach-tests")
	goBin := filepath.Join(binDir, "go")

	if err := os.WriteFile(goBin, []byte("#!/bin/sh\nif [ -n \"${SUPABASE_SERVICE_ROLE_KEY:-}\" ]; then exit 97; fi\nprintf 'no parser-specific success text\\n'\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	output, passed := runTests(dir, "Go (v1.25)")
	if !passed || !strings.Contains(output, "no parser-specific success text") {
		t.Fatalf("passing runTests() = (%q, %v)", output, passed)
	}

	if err := os.WriteFile(goBin, []byte("#!/bin/sh\nprintf 'ok misleading-output\\n'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	output, passed = runTests(dir, "go")
	if passed || !strings.Contains(output, "ok misleading-output") {
		t.Fatalf("failing runTests() = (%q, %v)", output, passed)
	}
}

func TestRunTestsUsesToolchainTypeScriptContract(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	npxBin := filepath.Join(binDir, "npx")
	script := "#!/bin/sh\n" +
		"if [ \"$1 $2 $3\" != \"--no-install jest --no-coverage\" ]; then exit 98; fi\n" +
		"printf 'typescript tests passed\\n'\n"
	if err := os.WriteFile(npxBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	output, passed := runTests(dir, "TypeScript (Node 22)")
	if !passed || !strings.Contains(output, "typescript tests passed") {
		t.Fatalf("runTests() = (%q, %v)", output, passed)
	}
}

func TestCombinedHashIncludesPath(t *testing.T) {
	a := map[string]FileSnapshot{"a.go": {Hash: "same"}}
	b := map[string]FileSnapshot{"b.go": {Hash: "same"}}
	if computeCombinedHash(a) == computeCombinedHash(b) {
		t.Fatal("combined hash did not include the file path")
	}
}

func TestIsTestFileUsesToolchainRegistry(t *testing.T) {
	for _, path := range []string{
		"/project/main_test.go",
		"/project/test_main.py",
		"/project/tests/integration.rs",
		"/project/src/index.test.ts",
		"/project/src/index.spec.js",
	} {
		if !isTestFile(path) {
			t.Errorf("registered test file was not recognized: %s", path)
		}
	}
	for _, path := range []string{
		"/project/main.go",
		"/project/src/index.ts",
		"/project/src/contest.py",
	} {
		if isTestFile(path) {
			t.Errorf("source file was classified as a test: %s", path)
		}
	}
}

func TestStripStepCompleteMarkerAlwaysRemovesModelClaim(t *testing.T) {
	response := "feedback\n[STEP_COMPLETE]\n[STEP_COMPLETE]"
	got := stripStepCompleteMarker(response)
	if strings.Contains(got, "[STEP_COMPLETE]") {
		t.Fatalf("stripStepCompleteMarker() = %q", got)
	}
}

func TestStopCancelsInFlightFeedbackAndDropsStaleResult(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &Watcher{
		snapshots: make(map[string]FileSnapshot),
		fsw:       fsw,
		ctx:       ctx,
		cancel:    cancel,
		sessionID: watcherSessionCounter.Add(1),
	}
	if err := w.snapshot(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldAI := ai.Global
	oldProject := project.Global
	oldGlobal := Global
	oldActiveSession := activeWatcherSession.Load()
	oldStreamFeedback := streamFeedback
	oldBroadcastStart := broadcastFeedbackStart
	oldBroadcastChunk := broadcastFeedbackChunk
	oldBroadcastEnd := broadcastFeedbackEnd
	oldBroadcastError := broadcastFeedbackError
	t.Cleanup(func() {
		cancel()
		_ = fsw.Close()
		ai.Global = oldAI
		project.Global = oldProject
		Global = oldGlobal
		activeWatcherSession.Store(oldActiveSession)
		streamFeedback = oldStreamFeedback
		broadcastFeedbackStart = oldBroadcastStart
		broadcastFeedbackChunk = oldBroadcastChunk
		broadcastFeedbackEnd = oldBroadcastEnd
		broadcastFeedbackError = oldBroadcastError
		clearFeedbackHistory()
	})

	clearFeedbackHistory()
	activeWatcherSession.Store(w.sessionID)
	Global = w
	project.Global = &project.Manager{}
	project.Global.Set(dir, "# TUTORSYS\n\n## 학습 수준\nnormal\n")
	ai.Global = &ai.Client{}

	started := make(chan struct{})
	streamFeedback = func(
		_ *ai.Client,
		feedbackCtx context.Context,
		_, _, _, _ string,
		_ []string,
		_ string,
		cb ai.StreamCallback,
	) error {
		close(started)
		<-feedbackCtx.Done()
		// Simulate a backend that invokes its callback once more while unwinding.
		cb("stale feedback")
		return feedbackCtx.Err()
	}

	var starts, chunks, ends, broadcastErrors int
	broadcastFeedbackStart = func(projectDir string) {
		if projectDir != dir {
			t.Errorf("feedback start project = %q, want %q", projectDir, dir)
		}
		starts++
	}
	broadcastFeedbackChunk = func(string, string) { chunks++ }
	broadcastFeedbackEnd = func(string) { ends++ }
	broadcastFeedbackError = func(string, string) { broadcastErrors++ }

	done := make(chan struct{})
	go func() {
		defer close(done)
		w.triggerSync(ctx, dir)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("feedback did not start")
	}
	w.Stop()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("feedback did not stop after watcher cancellation")
	}

	if ctx.Err() != context.Canceled {
		t.Fatalf("watcher context error = %v, want context.Canceled", ctx.Err())
	}
	if starts != 1 {
		t.Fatalf("feedback starts = %d, want 1", starts)
	}
	if chunks != 0 || ends != 0 || broadcastErrors != 0 {
		t.Fatalf("stale broadcasts = chunks:%d ends:%d errors:%d", chunks, ends, broadcastErrors)
	}
	if history := GetFeedbackHistory(); len(history) != 0 {
		t.Fatalf("stale feedback entered history: %#v", history)
	}
}
