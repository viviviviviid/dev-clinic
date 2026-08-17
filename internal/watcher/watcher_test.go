package watcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

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

func TestDedicatedTestsChangedUsesToolchainRegistry(t *testing.T) {
	for _, path := range []string{
		"/project/main_test.go",
		"/project/src/index.test.ts",
		"/project/test_main.py",
		"/project/tests/integration.rs",
	} {
		oldSnapshots := map[string]FileSnapshot{path: {Hash: "old"}}
		newSnapshots := map[string]FileSnapshot{path: {Hash: "new"}}
		if !dedicatedTestsChanged(oldSnapshots, newSnapshots) {
			t.Errorf("test change was not detected: %s", path)
		}
	}
	if dedicatedTestsChanged(
		map[string]FileSnapshot{"/project/main.go": {Hash: "old"}},
		map[string]FileSnapshot{"/project/main.go": {Hash: "new"}},
	) {
		t.Fatal("source change was classified as a dedicated test change")
	}
}

func TestCompletionMarkersParticipateInTestInputHash(t *testing.T) {
	plain := map[string]FileSnapshot{
		"main.go": {Content: "package main\n// ordinary note\n", SemanticHash: "same"},
	}
	withMarker := map[string]FileSnapshot{
		"main.go": {Content: "package main\n// [TUTOR:HOLE]\n", SemanticHash: "same"},
	}
	if computeAutoTestInputHash(plain) == computeAutoTestInputHash(withMarker) {
		t.Fatal("completion marker change did not alter test input hash")
	}
}

func TestRunnerConfigurationsParticipateInRawTestInputHash(t *testing.T) {
	for _, filename := range []string{"go.mod", "Cargo.toml", "package.json", "tsconfig.test.json", "pyproject.toml"} {
		t.Run(filename, func(t *testing.T) {
			path := filepath.Join("project", filename)
			before := map[string]FileSnapshot{path: {Content: "before", Hash: hash("before")}}
			after := map[string]FileSnapshot{path: {Content: "after", Hash: hash("after")}}
			if computeAutoTestInputHash(before) == computeAutoTestInputHash(after) {
				t.Fatalf("runner configuration change was not hashed: %s", filename)
			}
		})
	}
}

func TestProjectCargoConfigurationIsWatchedWithoutOpeningOtherDotDirectories(t *testing.T) {
	dir := t.TempDir()
	cargoDir := filepath.Join(dir, ".cargo")
	secretDir := filepath.Join(dir, ".secret")
	cargoCacheDir := filepath.Join(cargoDir, "registry")
	if err := os.Mkdir(cargoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(secretDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(cargoCacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cargoConfig := filepath.Join(cargoDir, "config.toml")
	secretConfig := filepath.Join(secretDir, "config.toml")
	cargoCacheSource := filepath.Join(cargoCacheDir, "dependency.rs")
	if err := os.WriteFile(cargoConfig, []byte("[build]\nrustflags=[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretConfig, []byte("do not watch"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cargoCacheSource, []byte("do not watch"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshots, err := readProjectSnapshots(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snapshots[cargoConfig]; !ok {
		t.Fatal("project .cargo/config.toml was not included in test inputs")
	}
	if _, ok := snapshots[secretConfig]; ok {
		t.Fatal("unrelated hidden directory was traversed")
	}
	if _, ok := snapshots[cargoCacheSource]; ok {
		t.Fatal(".cargo subdirectories were traversed")
	}
	if containsIgnoredPath(dir, cargoConfig) {
		t.Fatal("project .cargo configuration was filtered from fsnotify events")
	}
	if !containsIgnoredPath(dir, cargoCacheSource) {
		t.Fatal(".cargo subdirectory event was not filtered")
	}
}

func TestStripStepCompleteMarkerAlwaysRemovesModelClaim(t *testing.T) {
	response := "feedback\n[STEP_COMPLETE]\n[STEP_COMPLETE]"
	got := stripStepCompleteMarker(response)
	if strings.Contains(got, "[STEP_COMPLETE]") {
		t.Fatalf("stripStepCompleteMarker() = %q", got)
	}
}

func newTestReviewWatcher(t *testing.T, initial string) (*Watcher, string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
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
		dir:       dir,
		ctx:       ctx,
		cancel:    cancel,
		sessionID: watcherSessionCounter.Add(1),
		now:       time.Now,
	}
	if err := w.snapshot(dir); err != nil {
		t.Fatal(err)
	}
	w.initializeReviewState()

	oldAI := ai.Global
	oldProject := project.Global
	oldGlobal := Global
	oldActiveSession := activeWatcherSession.Load()
	oldStreamFeedback := streamFeedback
	oldAutomaticTest := runAutomaticTestCommand
	oldSessionChanged := broadcastSessionChanged
	oldBroadcastSync := broadcastSyncStatus
	oldAutomaticResult := broadcastAutomaticTestResult
	oldAutomaticComplete := broadcastAutomaticStepComplete
	oldBroadcastStart := broadcastFeedbackStart
	oldBroadcastChunk := broadcastFeedbackChunk
	oldBroadcastEnd := broadcastFeedbackEnd
	oldBroadcastError := broadcastFeedbackError
	oldBroadcastReview := broadcastReviewEvent
	t.Cleanup(func() {
		w.Stop()
		_ = fsw.Close()
		ai.Global = oldAI
		project.Global = oldProject
		Global = oldGlobal
		activeWatcherSession.Store(oldActiveSession)
		streamFeedback = oldStreamFeedback
		runAutomaticTestCommand = oldAutomaticTest
		broadcastSessionChanged = oldSessionChanged
		broadcastSyncStatus = oldBroadcastSync
		broadcastAutomaticTestResult = oldAutomaticResult
		broadcastAutomaticStepComplete = oldAutomaticComplete
		broadcastFeedbackStart = oldBroadcastStart
		broadcastFeedbackChunk = oldBroadcastChunk
		broadcastFeedbackEnd = oldBroadcastEnd
		broadcastFeedbackError = oldBroadcastError
		broadcastReviewEvent = oldBroadcastReview
		clearFeedbackHistory()
	})

	clearFeedbackHistory()
	activeWatcherSession.Store(w.sessionID)
	Global = w
	project.Global = &project.Manager{}
	project.Global.Set(dir, "# TUTORSYS\n\n## 언어 & 환경\ngo\n\n## 학습 수준\nnormal\n")
	ai.Global = &ai.Client{}
	broadcastFeedbackStart = func(ReviewState) {}
	broadcastSessionChanged = func(string) {}
	broadcastSyncStatus = func(string, bool) {}
	broadcastAutomaticTestResult = func(string, bool, string, string) {}
	broadcastAutomaticStepComplete = func(string, bool) {}
	broadcastFeedbackChunk = func(ReviewState, string) {}
	broadcastFeedbackEnd = func(ReviewState) {}
	broadcastFeedbackError = func(ReviewState, string) {}
	broadcastReviewEvent = func(string, ReviewState, string, string) {}
	return w, dir, path
}

func TestRefreshCreatesReadyOnlyForSemanticChanges(t *testing.T) {
	w, _, path := newTestReviewWatcher(t, "package main\nfunc equal(a, b int) bool { return a == b }\n")
	var syncChanges []bool
	broadcastSyncStatus = func(_ string, changed bool) { syncChanges = append(syncChanges, changed) }
	formatted := "// explanation\npackage main\n\nfunc equal(a, b int) bool {\n\treturn a == b\n}\n"
	if err := os.WriteFile(path, []byte(formatted), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	if state := w.GetReviewStatus(); state.Status != "idle" || state.Revision != 0 {
		t.Fatalf("format/comment-only state = %#v", state)
	}

	meaningful := strings.Replace(formatted, "a == b", "a != b", 1)
	if err := os.WriteFile(path, []byte(meaningful), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	state := w.GetReviewStatus()
	if state.Status != "ready" || state.Revision != 1 || len(state.Files) != 1 || state.Files[0] != "main.go" {
		t.Fatalf("one-character operator state = %#v", state)
	}
	w.reviewMu.Lock()
	diffText := w.reviewCandidate.DiffContent
	w.reviewMu.Unlock()
	if strings.Contains(diffText, "func equal(a, b int) bool { return") {
		t.Fatalf("stale pre-format baseline leaked into semantic diff:\n%s", diffText)
	}
	if len(syncChanges) != 2 || syncChanges[0] || !syncChanges[1] {
		t.Fatalf("semantic sync flags = %#v", syncChanges)
	}
}

func TestEnsureStartedSameProjectPreservesReadyCandidateAndSession(t *testing.T) {
	w, dir, path := newTestReviewWatcher(t, "package main\nvar value = 1\n")
	if err := os.WriteFile(path, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	before := w.GetReviewStatus()
	if before.Status != "ready" {
		t.Fatalf("state before reload = %#v", before)
	}
	if err := EnsureStarted(dir); err != nil {
		t.Fatal(err)
	}
	after := Global.GetReviewStatus()
	if Global != w || after.Status != "ready" || after.Revision != before.Revision || after.SemanticHash != before.SemanticHash || after.SessionID != before.SessionID {
		t.Fatalf("same-project reload replaced review state:\nbefore=%#v\nafter=%#v", before, after)
	}
	Global.reviewMu.Lock()
	diffText := Global.reviewCandidate.DiffContent
	changedCode := Global.reviewCandidate.ChangedCode
	Global.reviewMu.Unlock()
	if !strings.Contains(diffText, "+ 2") || !strings.Contains(changedCode, "value = 2") {
		t.Fatalf("same-project reload lost A-to-B diff:\n%s\n%s", diffText, changedCode)
	}
}

func TestRefreshMergesBurstAndUpdatesReadyCandidateWithoutRevisionChurn(t *testing.T) {
	w, _, path := newTestReviewWatcher(t, "package main\nvar value = 1\n")
	for _, content := range []string{"package main\nvar value = 2\n", "package main\nvar value = 3\n"} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	if state := w.GetReviewStatus(); state.Revision != 1 || state.Status != "ready" {
		t.Fatalf("burst state = %#v", state)
	}
	if err := os.WriteFile(path, []byte("package main\n\nvar value = 3 // note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	if state := w.GetReviewStatus(); state.Revision != 1 || state.Status != "ready" {
		t.Fatalf("format-after-ready state = %#v", state)
	}
	w.reviewMu.Lock()
	changedCode := w.reviewCandidate.ChangedCode
	w.reviewMu.Unlock()
	if !strings.Contains(changedCode, "var value = 3 // note") {
		t.Fatalf("candidate did not refresh latest raw content: %q", changedCode)
	}
}

func TestRevertingReadyRevisionClearsCandidate(t *testing.T) {
	initial := "package main\nvar value = 1\n"
	w, _, path := newTestReviewWatcher(t, initial)
	events := make(chan string, 4)
	broadcastReviewEvent = func(event string, _ ReviewState, reason, _ string) { events <- event + ":" + reason }
	if err := os.WriteFile(path, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	if state := w.GetReviewStatus(); state.Status != "idle" || state.Revision != 2 || len(state.Files) != 0 {
		t.Fatalf("reverted state = %#v", state)
	}
	got := []string{<-events, <-events}
	if got[0] != "review_ready:" || got[1] != "review_cancelled:reverted" {
		t.Fatalf("revert events = %#v", got)
	}
}

func TestSourceChangeCancelsReviewAndDropsStaleResponse(t *testing.T) {
	w, _, path := newTestReviewWatcher(t, "package main\nvar value = 1\n")
	if err := os.WriteFile(path, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	done := make(chan struct{})
	streamFeedback = func(_ *ai.Client, ctx context.Context, _, _, _, _ string, _ []string, _ string, cb ai.StreamCallback) error {
		close(started)
		<-ctx.Done()
		cb("stale feedback")
		close(done)
		return ctx.Err()
	}
	chunks := make(chan string, 1)
	broadcastFeedbackChunk = func(_ ReviewState, chunk string) { chunks <- chunk }
	if _, err := w.RequestReview(ReviewRequest{}); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := os.WriteFile(path, []byte("package main\nvar value = 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("superseded review context was not cancelled")
	}
	if state := w.GetReviewStatus(); state.Status != "ready" || state.Revision != 2 {
		t.Fatalf("latest state = %#v", state)
	}
	select {
	case chunk := <-chunks:
		t.Fatalf("stale chunk was broadcast: %q", chunk)
	default:
	}
	if history := GetFeedbackHistory(); len(history) != 0 {
		t.Fatalf("stale feedback entered history: %#v", history)
	}
}

func TestCancelInvalidatesReviewRequestBlockedInRefresh(t *testing.T) {
	w, _, path := newTestReviewWatcher(t, "package main\nvar value = 1\n")
	if err := os.WriteFile(path, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	refreshBlocked := make(chan struct{})
	releaseRefresh := make(chan struct{})
	broadcastSyncStatus = func(_ string, changed bool) {
		if changed {
			close(refreshBlocked)
			<-releaseRefresh
		}
	}
	var streamCalls atomic.Int32
	streamFeedback = func(_ *ai.Client, _ context.Context, _, _, _, _ string, _ []string, _ string, _ ai.StreamCallback) error {
		streamCalls.Add(1)
		return nil
	}

	requestDone := make(chan error, 1)
	go func() {
		_, err := w.RequestReview(ReviewRequest{})
		requestDone <- err
	}()
	select {
	case <-refreshBlocked:
	case <-time.After(2 * time.Second):
		t.Fatal("review request did not enter synchronous refresh")
	}
	if state := w.GetReviewStatus(); !state.RequestPending {
		t.Fatalf("blocked request was not visible as pending: %#v", state)
	}
	state, cancelled := w.CancelReview("user")
	if !cancelled || state.RequestPending {
		t.Fatalf("pending cancel = (%#v, %t)", state, cancelled)
	}
	close(releaseRefresh)
	select {
	case err := <-requestDone:
		if reviewErrorCode(err) != "review_cancelled" {
			t.Fatalf("request after cancel error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled request remained blocked")
	}
	if calls := streamCalls.Load(); calls != 0 {
		t.Fatalf("cancelled pending request started AI %d times", calls)
	}
	w.reviewMu.Lock()
	starts := len(w.reviewStarts)
	w.reviewMu.Unlock()
	if starts != 0 {
		t.Fatalf("cancelled pending request consumed %d rate-limit starts", starts)
	}
	if state := w.GetReviewStatus(); state.Status != "ready" || state.Revision != 1 || state.RequestPending {
		t.Fatalf("state after pending cancel = %#v", state)
	}
}

func TestCancelRequestIDBeforeHandlerPreventsLateAIStart(t *testing.T) {
	w, _, path := newTestReviewWatcher(t, "package main\nvar value = 1\n")
	if err := os.WriteFile(path, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	var streamCalls atomic.Int32
	streamFeedback = func(_ *ai.Client, _ context.Context, _, _, _, _ string, _ []string, _ string, _ ai.StreamCallback) error {
		streamCalls.Add(1)
		return nil
	}
	state, cancelled := w.CancelReviewRequest("request-before-handler", "user")
	if !cancelled || state.RequestID != "request-before-handler" {
		t.Fatalf("early cancel = (%#v, %t)", state, cancelled)
	}
	if _, err := w.RequestReview(ReviewRequest{RequestID: "request-before-handler"}); reviewErrorCode(err) != "review_cancelled" {
		t.Fatalf("late request error = %v", err)
	}
	if calls := streamCalls.Load(); calls != 0 {
		t.Fatalf("tombstoned request started AI %d times", calls)
	}
	w.reviewMu.Lock()
	starts := len(w.reviewStarts)
	w.reviewMu.Unlock()
	if starts != 0 {
		t.Fatalf("tombstoned request consumed %d rate-limit starts", starts)
	}
	if state := w.GetReviewStatus(); state.Status != "ready" || state.Revision != 1 {
		t.Fatalf("candidate after early cancel = %#v", state)
	}
}

func TestSnapshotPublishedBeforeRevisionUpdateCannotLoseLatestDiff(t *testing.T) {
	w, _, path := newTestReviewWatcher(t, "package main\nvar value = 1\n")
	if err := os.WriteFile(path, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	finishReview := make(chan struct{})
	reviewReturned := make(chan struct{})
	streamFeedback = func(_ *ai.Client, _ context.Context, _, _, _, _ string, _ []string, _ string, cb ai.StreamCallback) error {
		<-finishReview
		cb("stale H1 response")
		close(reviewReturned)
		return nil
	}
	chunks := make(chan string, 1)
	broadcastFeedbackChunk = func(_ ReviewState, chunk string) { chunks <- chunk }
	latestReady := make(chan ReviewState, 1)
	broadcastReviewEvent = func(event string, state ReviewState, _, _ string) {
		if event == "review_ready" && state.Revision == 2 {
			latestReady <- state
		}
	}
	if _, err := w.RequestReview(ReviewRequest{}); err != nil {
		t.Fatal(err)
	}

	snapshotPublished := make(chan struct{})
	allowRefreshToContinue := make(chan struct{})
	broadcastSyncStatus = func(_ string, changed bool) {
		if changed {
			close(snapshotPublished)
			<-allowRefreshToContinue
		}
	}
	if err := os.WriteFile(path, []byte("package main\nvar value = 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refreshDone := make(chan error, 1)
	go func() { refreshDone <- w.Refresh() }()
	<-snapshotPublished
	close(finishReview)
	<-reviewReturned
	close(allowRefreshToContinue)
	if err := <-refreshDone; err != nil {
		t.Fatal(err)
	}
	select {
	case state := <-latestReady:
		if state.Status != "ready" || state.Revision != 2 {
			t.Fatalf("latest ready state = %#v", state)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("review completion did not reconcile the published H2 snapshot")
	}
	select {
	case chunk := <-chunks:
		t.Fatalf("stale H1 response was broadcast: %q", chunk)
	default:
	}
	if history := GetFeedbackHistory(); len(history) != 0 {
		t.Fatalf("stale H1 response entered history: %#v", history)
	}
	w.reviewMu.Lock()
	diffText := w.reviewCandidate.DiffContent
	changedCode := w.reviewCandidate.ChangedCode
	w.reviewMu.Unlock()
	if !strings.Contains(changedCode, "value = 3") {
		t.Fatalf("latest H2 diff was lost:\n%s\n%s", diffText, changedCode)
	}
}

func TestReviewCompletionRefreshesDiskBeforeDebounceCommit(t *testing.T) {
	w, _, path := newTestReviewWatcher(t, "package main\nvar value = 1\n")
	if err := os.WriteFile(path, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	finish := make(chan struct{})
	streamFeedback = func(_ *ai.Client, _ context.Context, _, _, _, _ string, _ []string, _ string, cb ai.StreamCallback) error {
		<-finish
		cb("feedback for old disk revision")
		return nil
	}
	cancelled := make(chan string, 1)
	chunks := make(chan string, 1)
	broadcastReviewEvent = func(event string, _ ReviewState, reason, _ string) {
		if event == "review_cancelled" {
			cancelled <- reason
		}
	}
	broadcastFeedbackChunk = func(_ ReviewState, chunk string) { chunks <- chunk }
	if _, err := w.RequestReview(ReviewRequest{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package main\nvar value = 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.markTestInputMutation()
	close(finish)
	select {
	case reason := <-cancelled:
		if reason != "source_changed" {
			t.Fatalf("cancel reason = %q", reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pre-debounce source edit did not cancel completed old review")
	}
	select {
	case chunk := <-chunks:
		t.Fatalf("stale feedback emitted before debounce: %q", chunk)
	default:
	}
	if state := w.GetReviewStatus(); state.Status != "ready" || state.Revision != 2 || state.LastFeedback != nil {
		t.Fatalf("post-refresh review state = %#v", state)
	}
}

func TestStaleRunCleanupCannotRegressOrCancelFreshRun(t *testing.T) {
	w, _, _ := newTestReviewWatcher(t, "package main\nvar value = 1\n")
	oldRun := &reviewRun{candidate: reviewCandidate{ReviewState: ReviewState{
		ProjectDir: w.dir, SessionID: w.sessionID, Revision: 1, SemanticHash: "H1", RequestID: "old",
	}}}
	freshCandidate := &reviewCandidate{
		ReviewState: ReviewState{
			Status: "ready", ProjectDir: w.dir, SessionID: w.sessionID, Revision: 3, SemanticHash: "H3", RequestID: "fresh",
		},
		AutoTestOutput: "fresh test evidence",
	}
	freshRun := &reviewRun{candidate: *freshCandidate, hasTestEvidence: true}

	w.reviewMu.Lock()
	w.revision = 3
	w.semanticHash = "H3"
	w.reviewCandidate = freshCandidate
	w.reviewRunning = freshRun
	if _, _, _, _, reconciled := w.reconcileSemanticChangeForRunLocked(oldRun, nil, "H2"); reconciled {
		w.reviewMu.Unlock()
		t.Fatal("stale H1 run reconciled captured H2 over fresh H3")
	}
	if _, _, invalidated := w.invalidateTestEvidenceForRunLocked(oldRun); invalidated {
		w.reviewMu.Unlock()
		t.Fatal("stale H1 run invalidated fresh H3 test evidence")
	}
	gotRun := w.reviewRunning
	gotHash := w.semanticHash
	gotRevision := w.revision
	gotEvidence := w.reviewCandidate.AutoTestOutput
	w.reviewMu.Unlock()
	if gotRun != freshRun || gotHash != "H3" || gotRevision != 3 || gotEvidence != "fresh test evidence" {
		t.Fatalf("fresh run mutated: run=%p hash=%q revision=%d evidence=%q", gotRun, gotHash, gotRevision, gotEvidence)
	}
}

func TestChangedTestsCancelReviewThatCapturedOldTestEvidence(t *testing.T) {
	w, dir, path := newTestReviewWatcher(t, "package main\nvar value = 1\n")
	if err := os.WriteFile(path, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	w.reviewMu.Lock()
	w.reviewCandidate.AutoTestOutput = "old failing suite"
	w.reviewMu.Unlock()
	finishReview := make(chan struct{})
	streamFeedback = func(_ *ai.Client, _ context.Context, _, _, _, testOutput string, _ []string, _ string, cb ai.StreamCallback) error {
		if !strings.Contains(testOutput, "old failing suite") {
			t.Errorf("review did not capture expected old test evidence: %q", testOutput)
		}
		<-finishReview
		cb("feedback based on old tests")
		return nil
	}
	cancelled := make(chan string, 1)
	broadcastReviewEvent = func(event string, _ ReviewState, reason, _ string) {
		if event == "review_cancelled" {
			cancelled <- reason
		}
	}
	chunks := make(chan string, 1)
	broadcastFeedbackChunk = func(_ ReviewState, chunk string) { chunks <- chunk }
	if _, err := w.RequestReview(ReviewRequest{}); err != nil {
		t.Fatal(err)
	}
	testPath := filepath.Join(dir, "main_test.go")
	if err := os.WriteFile(testPath, []byte("package main\n// changed expectation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Publish the new raw snapshot without running refresh's invalidation to
	// deterministically exercise executeReview's final evidence-hash guard.
	if err := w.snapshot(dir); err != nil {
		t.Fatal(err)
	}
	close(finishReview)
	select {
	case reason := <-cancelled:
		if reason != "tests_changed" {
			t.Fatalf("cancel reason = %q", reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("review using obsolete test evidence was not cancelled")
	}
	select {
	case chunk := <-chunks:
		t.Fatalf("obsolete test-evidence feedback was emitted: %q", chunk)
	default:
	}
	state := w.GetReviewStatus()
	if state.Status != "ready" || state.LastFeedback != nil {
		t.Fatalf("state after test evidence invalidation = %#v", state)
	}
	w.reviewMu.Lock()
	autoOutput := w.reviewCandidate.AutoTestOutput
	w.reviewMu.Unlock()
	if autoOutput != "" {
		t.Fatalf("stale candidate auto-test output survived: %q", autoOutput)
	}
}

func TestRevertDuringReviewPublishesLatestIdleRevision(t *testing.T) {
	initial := "package main\nvar value = 1\n"
	w, _, path := newTestReviewWatcher(t, initial)
	if err := os.WriteFile(path, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	streamFeedback = func(_ *ai.Client, ctx context.Context, _, _, _, _ string, _ []string, _ string, _ ai.StreamCallback) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	idleRevision := make(chan uint64, 1)
	broadcastReviewEvent = func(event string, state ReviewState, reason, _ string) {
		if event == "review_cancelled" && reason == "reverted" && state.Status == "idle" {
			idleRevision <- state.Revision
		}
	}
	if _, err := w.RequestReview(ReviewRequest{}); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	select {
	case revision := <-idleRevision:
		if revision != 2 {
			t.Fatalf("idle revision = %d, want 2", revision)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("latest idle revision was not published")
	}
}

func TestReviewFailureEmitsOneAuthoritativeErrorEvent(t *testing.T) {
	w, _, path := newTestReviewWatcher(t, "package main\nvar value = 1\n")
	if err := os.WriteFile(path, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	streamFeedback = func(_ *ai.Client, _ context.Context, _, _, _, _ string, _ []string, _ string, _ ai.StreamCallback) error {
		return errors.New("provider unavailable")
	}
	errorEvent := make(chan ReviewState, 1)
	legacyErrors := make(chan string, 1)
	feedbackEnds := make(chan struct{}, 1)
	broadcastReviewEvent = func(event string, state ReviewState, _, message string) {
		if event == "review_error" {
			if message != "provider unavailable" {
				t.Errorf("review error message = %q", message)
			}
			errorEvent <- state
		}
	}
	broadcastFeedbackError = func(_ ReviewState, message string) { legacyErrors <- message }
	broadcastFeedbackEnd = func(ReviewState) { feedbackEnds <- struct{}{} }
	if _, err := w.RequestReview(ReviewRequest{}); err != nil {
		t.Fatal(err)
	}
	select {
	case state := <-errorEvent:
		if state.Status != "ready" || state.Revision != 1 {
			t.Fatalf("review_error state = %#v", state)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("review_error was not emitted")
	}
	select {
	case message := <-legacyErrors:
		t.Fatalf("duplicate legacy error was emitted: %q", message)
	default:
	}
	select {
	case <-feedbackEnds:
	default:
		t.Fatal("feedback_end cleanup was not emitted")
	}
}

func TestReviewRateLimitAndValidationDoNotConsumeQuota(t *testing.T) {
	w, _, path := newTestReviewWatcher(t, "package main\nvar value = 1\n")
	if err := os.WriteFile(path, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	w.now = func() time.Time { return now }
	streamFeedback = func(_ *ai.Client, ctx context.Context, _, _, _, _ string, _ []string, _ string, _ ai.StreamCallback) error {
		<-ctx.Done()
		return ctx.Err()
	}
	wrong := uint64(99)
	if _, err := w.RequestReview(ReviewRequest{Revision: &wrong}); reviewErrorCode(err) != "review_stale" {
		t.Fatalf("stale request error = %v", err)
	}
	for start := 0; start < 4; start++ {
		if _, err := w.RequestReview(ReviewRequest{}); err != nil {
			t.Fatalf("start %d: %v", start+1, err)
		}
		w.CancelReview("user")
		if start < 3 {
			now = now.Add(15 * time.Second)
		}
	}
	now = now.Add(14 * time.Second)
	if _, err := w.RequestReview(ReviewRequest{}); reviewErrorCode(err) != "review_rate_limited" {
		t.Fatalf("fifth request before window error = %v", err)
	}
	now = now.Add(time.Second)
	if _, err := w.RequestReview(ReviewRequest{}); err != nil {
		t.Fatalf("request at rolling-window boundary: %v", err)
	}
}

func TestSuccessfulSemanticHashIsNotReviewedTwice(t *testing.T) {
	w, _, path := newTestReviewWatcher(t, "package main\nvar value = 1\n")
	if err := os.WriteFile(path, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	calls := 0
	ended := make(chan struct{}, 1)
	streamFeedback = func(_ *ai.Client, _ context.Context, _, _, _, _ string, _ []string, _ string, cb ai.StreamCallback) error {
		calls++
		cb("reviewed")
		return nil
	}
	broadcastReviewEvent = func(event string, _ ReviewState, _, _ string) {
		if event == "review_end" {
			ended <- struct{}{}
		}
	}
	if _, err := w.RequestReview(ReviewRequest{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("review did not finish")
	}
	if _, err := w.RequestReview(ReviewRequest{}); reviewErrorCode(err) != "review_not_ready" {
		t.Fatalf("same-hash retry error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("same semantic hash AI calls = %d, want 1", calls)
	}
	state := w.GetReviewStatus()
	if state.LastFeedback == nil || state.LastFeedback.Revision != 1 || state.LastFeedback.Content != "reviewed" || len(state.LastFeedback.Files) != 1 {
		t.Fatalf("completed feedback replay = %#v", state.LastFeedback)
	}
}

func TestEmptyFeedbackKeepsCandidateReadyAndDoesNotPersist(t *testing.T) {
	w, _, path := newTestReviewWatcher(t, "package main\nvar value = 1\n")
	if err := os.WriteFile(path, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	streamFeedback = func(_ *ai.Client, _ context.Context, _, _, _, _ string, _ []string, _ string, cb ai.StreamCallback) error {
		cb(" \n[STEP_COMPLETE]\n")
		return nil
	}
	errorsSeen := make(chan string, 1)
	broadcastReviewEvent = func(event string, _ ReviewState, _, message string) {
		if event == "review_error" {
			errorsSeen <- message
		}
	}
	if _, err := w.RequestReview(ReviewRequest{}); err != nil {
		t.Fatal(err)
	}
	select {
	case message := <-errorsSeen:
		if !strings.Contains(message, "빈 피드백") {
			t.Fatalf("empty feedback error = %q", message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("empty feedback did not emit review_error")
	}
	state := w.GetReviewStatus()
	if state.Status != "ready" || state.LastFeedback != nil {
		t.Fatalf("state after empty feedback = %#v", state)
	}
	if history := GetFeedbackHistory(); len(history) != 0 {
		t.Fatalf("empty feedback entered history: %#v", history)
	}
}

func TestTestContextReviewDeduplicatesOnlyAfterSuccessfulStart(t *testing.T) {
	w, _, _ := newTestReviewWatcher(t, "package main\n")
	ended := make(chan struct{}, 1)
	broadcastReviewEvent = func(event string, _ ReviewState, _, _ string) {
		if event == "review_end" {
			ended <- struct{}{}
		}
	}
	streamFeedback = func(_ *ai.Client, _ context.Context, _, _, _, _ string, _ []string, _ string, cb ai.StreamCallback) error {
		cb("test diagnosis")
		return nil
	}
	passed := false
	w.mu.RLock()
	inputHash := computeAutoTestInputHash(w.snapshots)
	w.mu.RUnlock()
	request := ReviewRequest{TestContext: TestContext{Passed: &passed, Summary: "suite failed", Output: "assertion", InputHash: inputHash}}
	if _, err := w.RequestReview(request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("context-only review did not finish")
	}
	if _, err := w.RequestReview(request); reviewErrorCode(err) != "review_duplicate" {
		t.Fatalf("duplicate context error = %v", err)
	}
}

func TestJoinedTestContextIsUTF8SafeAndBounded(t *testing.T) {
	passed := false
	joined := joinTestContext(strings.Repeat("auto", 20_000), TestContext{
		Passed: &passed,
		Output: strings.Repeat("한", 30_000),
	})
	if len(joined) > maxReviewTestContextBytes {
		t.Fatalf("joined test context bytes = %d", len(joined))
	}
	if !strings.Contains(joined, "passed: false") || !strings.Contains(joined, "[테스트 출력 생략]") {
		t.Fatalf("bounded context lost priority/marker: prefix=%q suffix=%q", joined[:40], joined[len(joined)-40:])
	}
	if !utf8.ValidString(joined) {
		t.Fatal("bounded test context is not valid UTF-8")
	}
}

func TestTestContextFingerprintIsScopedToSemanticHash(t *testing.T) {
	passed := false
	context := TestContext{Passed: &passed, Summary: "same failure", Output: "same assertion", InputHash: "input-v1"}
	if testContextFingerprint("hash-one", context) == testContextFingerprint("hash-two", context) {
		t.Fatal("same test failure was deduplicated across different source semantics")
	}
}

func TestExplicitTestContextRejectsChangedTestInputsBeforeAIStart(t *testing.T) {
	w, dir, _ := newTestReviewWatcher(t, "package main\n")
	w.mu.RLock()
	oldInputHash := computeAutoTestInputHash(w.snapshots)
	w.mu.RUnlock()
	if err := os.WriteFile(filepath.Join(dir, "main_test.go"), []byte("package main\nfunc TestNewInput(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	passed := false
	_, err := w.RequestReview(ReviewRequest{TestContext: TestContext{
		Passed:    &passed,
		Summary:   "failure from old tests",
		InputHash: oldInputHash,
	}})
	if reviewErrorCode(err) != "review_test_context_stale" {
		t.Fatalf("stale explicit context error = %v", err)
	}
	w.reviewMu.Lock()
	starts := len(w.reviewStarts)
	w.reviewMu.Unlock()
	if starts != 0 {
		t.Fatalf("stale explicit context consumed review quota: %d", starts)
	}
}

func TestContextOnlyCandidateSurvivesFormattingButNotChangedTests(t *testing.T) {
	w, dir, path := newTestReviewWatcher(t, "package main\n")
	failed := make(chan struct{}, 1)
	streamFeedback = func(_ *ai.Client, _ context.Context, _, _, _, _ string, _ []string, _ string, _ ai.StreamCallback) error {
		return errors.New("temporary provider failure")
	}
	broadcastReviewEvent = func(event string, _ ReviewState, _, _ string) {
		if event == "review_error" {
			failed <- struct{}{}
		}
	}
	passed := false
	w.mu.RLock()
	inputHash := computeAutoTestInputHash(w.snapshots)
	w.mu.RUnlock()
	if _, err := w.RequestReview(ReviewRequest{TestContext: TestContext{Passed: &passed, Summary: "failed", InputHash: inputHash}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-failed:
	case <-time.After(2 * time.Second):
		t.Fatal("context-only review did not fail")
	}
	if err := os.WriteFile(path, []byte("// formatting note\npackage main\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	w.reviewMu.Lock()
	contextOnly := w.reviewCandidate != nil && w.reviewCandidate.ContextOnly
	w.reviewMu.Unlock()
	if !contextOnly {
		t.Fatal("format-only refresh lost the context-only guard")
	}
	if _, err := w.RequestReview(ReviewRequest{}); reviewErrorCode(err) != "review_not_ready" {
		t.Fatalf("empty paid retry error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main_test.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	if state := w.GetReviewStatus(); state.Status != "idle" {
		t.Fatalf("changed tests retained obsolete context-only candidate: %#v", state)
	}
}

func reviewErrorCode(err error) string {
	if reviewErr, ok := err.(*ReviewRequestError); ok {
		return reviewErr.Code
	}
	return ""
}

func TestAutomaticTestsCancelPreviousRevision(t *testing.T) {
	t.Setenv("CODING_TUTOR_AUTO_TEST", "true")
	w, _, path := newTestReviewWatcher(t, "package main\nvar value = 1\n")
	started := make(chan context.Context, 2)
	runAutomaticTestCommand = func(ctx context.Context, _, _ string) (string, bool) {
		started <- ctx
		<-ctx.Done()
		return "", false
	}
	if err := os.WriteFile(path, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	first := <-started
	if err := os.WriteFile(path, []byte("package main\nvar value = 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	second := <-started
	select {
	case <-first.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("previous automatic test was not cancelled")
	}
	if second.Err() != nil {
		t.Fatalf("latest automatic test was already cancelled: %v", second.Err())
	}
}

func TestTestFileChangeRerunsLatestAutoTestWithoutReviewRevision(t *testing.T) {
	t.Setenv("CODING_TUTOR_AUTO_TEST", "true")
	w, dir, _ := newTestReviewWatcher(t, "package main\nvar value = 1\n")
	testPath := filepath.Join(dir, "main_test.go")
	started := make(chan context.Context, 2)
	syncChanges := make(chan bool, 2)
	broadcastSyncStatus = func(_ string, changed bool) { syncChanges <- changed }
	runAutomaticTestCommand = func(ctx context.Context, _, _ string) (string, bool) {
		started <- ctx
		<-ctx.Done()
		return "", false
	}
	if err := os.WriteFile(testPath, []byte("package main\n// first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	first := <-started
	if changed := <-syncChanges; !changed {
		t.Fatal("test-only edit did not invalidate test/completion state")
	}
	if state := w.GetReviewStatus(); state.Revision != 0 || state.Status != "idle" {
		t.Fatalf("test-only edit changed review state: %#v", state)
	}
	if err := os.WriteFile(testPath, []byte("package main\n// second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	second := <-started
	if changed := <-syncChanges; !changed {
		t.Fatal("second test-only edit did not invalidate test/completion state")
	}
	select {
	case <-first.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("older test-file auto test was not cancelled")
	}
	if second.Err() != nil {
		t.Fatalf("latest test-file auto test was cancelled: %v", second.Err())
	}
}

func TestCompletionMarkerOnlyChangeInvalidatesWithoutReviewRevision(t *testing.T) {
	w, _, path := newTestReviewWatcher(t, "package main\n// ordinary note\n")
	syncChanges := make(chan bool, 1)
	broadcastSyncStatus = func(_ string, changed bool) { syncChanges <- changed }
	if err := os.WriteFile(path, []byte("package main\n// [TUTOR:HOLE]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	if changed := <-syncChanges; !changed {
		t.Fatal("completion marker change did not invalidate completion state")
	}
	if state := w.GetReviewStatus(); state.Status != "idle" || state.Revision != 0 {
		t.Fatalf("completion marker-only change created an AI review: %#v", state)
	}
}

func TestRunnerConfigurationChangeInvalidatesWithoutReviewRevision(t *testing.T) {
	for _, filename := range []string{"go.mod", "Cargo.toml", "package.json", "tsconfig.json"} {
		t.Run(filename, func(t *testing.T) {
			w, dir, _ := newTestReviewWatcher(t, "package main\n")
			configPath := filepath.Join(dir, filename)
			if err := os.WriteFile(configPath, []byte("before\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := w.Refresh(); err != nil {
				t.Fatal(err)
			}
			syncChanges := make(chan bool, 1)
			broadcastSyncStatus = func(_ string, changed bool) { syncChanges <- changed }
			if err := os.WriteFile(configPath, []byte("after\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := w.Refresh(); err != nil {
				t.Fatal(err)
			}
			if changed := <-syncChanges; !changed {
				t.Fatal("runner configuration did not invalidate test/completion state")
			}
			if state := w.GetReviewStatus(); state.Status != "idle" || state.Revision != 0 {
				t.Fatalf("runner configuration created AI review state: %#v", state)
			}
		})
	}
}

func TestPublishedTestSnapshotRejectsOldAutoTestBeforeCancellation(t *testing.T) {
	t.Setenv("CODING_TUTOR_AUTO_TEST", "true")
	w, dir, _ := newTestReviewWatcher(t, "package main\nvar value = 1\n")
	testPath := filepath.Join(dir, "main_test.go")
	firstStarted := make(chan struct{})
	finishFirst := make(chan struct{})
	secondStarted := make(chan context.Context, 1)
	var calls atomic.Int32
	runAutomaticTestCommand = func(ctx context.Context, _, _ string) (string, bool) {
		if calls.Add(1) == 1 {
			close(firstStarted)
			<-finishFirst
			return "old passing result", true
		}
		secondStarted <- ctx
		<-ctx.Done()
		return "", false
	}
	results := make(chan bool, 1)
	broadcastAutomaticTestResult = func(_ string, passed bool, _, _ string) { results <- passed }
	if err := os.WriteFile(testPath, []byte("package main\n// first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	<-firstStarted

	snapshotPublished := make(chan struct{})
	allowRefresh := make(chan struct{})
	broadcastSyncStatus = func(_ string, changed bool) {
		if changed {
			close(snapshotPublished)
			<-allowRefresh
		}
	}
	if err := os.WriteFile(testPath, []byte("package main\n// second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refreshDone := make(chan error, 1)
	go func() { refreshDone <- w.Refresh() }()
	<-snapshotPublished
	close(finishFirst)
	// The old command is allowed to return while refresh is deliberately
	// blocked before cancelAutomaticTests. Its input hash must still reject it.
	select {
	case passed := <-results:
		t.Fatalf("stale auto-test result was broadcast: %t", passed)
	case <-time.After(50 * time.Millisecond):
	}
	close(allowRefresh)
	if err := <-refreshDone; err != nil {
		t.Fatal(err)
	}
	select {
	case ctx := <-secondStarted:
		if ctx.Err() != nil {
			t.Fatalf("latest auto-test context already cancelled: %v", ctx.Err())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("latest auto-test did not start")
	}
}

func TestManualTestPublicationIsOrderedBeforeNewerSync(t *testing.T) {
	w, dir, path := newTestReviewWatcher(t, "package main\nvar value = 1\n")
	version, err := CaptureTestInputVersion(dir)
	if err != nil {
		t.Fatal(err)
	}
	validated := make(chan struct{})
	releasePublication := make(chan struct{})
	events := make(chan string, 2)
	publishDone := make(chan bool, 1)
	go func() {
		publishDone <- version.PublishIfCurrent(func() {
			close(validated)
			<-releasePublication
			events <- "result"
		})
	}()
	select {
	case <-validated:
	case <-time.After(2 * time.Second):
		t.Fatal("manual test result did not reach final validation")
	}

	if err := os.WriteFile(path, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	broadcastSyncStatus = func(string, bool) { events <- "sync" }
	refreshDone := make(chan error, 1)
	go func() { refreshDone <- w.Refresh() }()
	select {
	case event := <-events:
		t.Fatalf("new sync passed a validated old result: %s", event)
	case <-time.After(50 * time.Millisecond):
	}

	close(releasePublication)
	if published := <-publishDone; !published {
		t.Fatal("result validated against the old input was unexpectedly dropped")
	}
	if err := <-refreshDone; err != nil {
		t.Fatal(err)
	}
	first, second := <-events, <-events
	if first != "result" || second != "sync" {
		t.Fatalf("manual result/sync ordering = %q, %q", first, second)
	}
}

func TestManualTestVersionRejectsABATestInputMutation(t *testing.T) {
	w, dir, path := newTestReviewWatcher(t, "package main\nvar value = 1\n")
	version, err := CaptureTestInputVersion(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.markTestInputMutation()
	if err := os.WriteFile(path, []byte("package main\nvar value = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.markTestInputMutation()
	called := false
	if version.PublishIfCurrent(func() { called = true }) || called {
		t.Fatal("A→B→A mutation published a stale manual-test result")
	}
}

func TestCancelMismatchedRequestPreservesAuthoritativeRun(t *testing.T) {
	w, _, path := newTestReviewWatcher(t, "package main\nvar value = 1\n")
	if err := os.WriteFile(path, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	streamFeedback = func(_ *ai.Client, ctx context.Context, _, _, _, _ string, _ []string, _ string, _ ai.StreamCallback) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	if _, err := w.RequestReview(ReviewRequest{RequestID: "request-b"}); err != nil {
		t.Fatal(err)
	}
	<-started
	state, cancelled := w.CancelReviewRequest("request-a", "user")
	if cancelled || state.Status != "reviewing" || state.RequestID != "request-b" {
		t.Fatalf("mismatched cancel = cancelled:%t state:%#v", cancelled, state)
	}
	if _, err := w.RequestReview(ReviewRequest{RequestID: "request-a"}); reviewErrorCode(err) != "review_cancelled" {
		t.Fatalf("mismatched cancel did not tombstone delayed request: %v", err)
	}
	if _, cancelled = w.CancelReviewRequest("request-b", "user"); !cancelled {
		t.Fatal("authoritative request was not cancelled")
	}
}
