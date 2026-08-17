package watcher

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/coding-tutor/internal/ai"
	"github.com/coding-tutor/internal/completion"
	"github.com/coding-tutor/internal/config"
	diffpkg "github.com/coding-tutor/internal/diff"
	"github.com/coding-tutor/internal/project"
	"github.com/coding-tutor/internal/toolchain"
	"github.com/coding-tutor/internal/ws"
	"github.com/fsnotify/fsnotify"
)

const (
	maxTestOutputBytes = 64 << 10
	feedbackTimeout    = 90 * time.Second
)

type FileSnapshot struct {
	Path         string
	Content      string
	Hash         string
	SemanticHash string
}

// TestInputVersion identifies the exact source semantics, dedicated tests,
// and watcher epoch against which a manual test command started.
type TestInputVersion struct {
	ProjectDir string
	SessionID  uint64
	Hash       string
	Generation uint64
	watcher    *Watcher
}

type Watcher struct {
	mu             sync.RWMutex
	syncMu         sync.Mutex
	eventsMu       sync.RWMutex
	snapshots      map[string]FileSnapshot
	fsw            *fsnotify.Watcher
	dir            string
	lastChangeTime time.Time
	ctx            context.Context
	cancel         context.CancelFunc
	sessionID      uint64

	reviewMu                    sync.Mutex
	reviewBaseline              map[string]FileSnapshot
	reviewBaselineHash          string
	semanticHash                string
	revision                    uint64
	reviewCandidate             *reviewCandidate
	reviewRunning               *reviewRun
	reviewStarts                []time.Time
	reviewRequestGeneration     uint64
	pendingReviewGeneration     uint64
	pendingReviewRequestID      string
	cancelledReviewRequests     map[string]struct{}
	cancelledReviewRequestOrder []string
	lastReviewedTestContextHash string
	lastFeedback                *CompletedFeedback
	now                         func() time.Time
	autoTestMu                  sync.Mutex
	autoTestCancel              context.CancelFunc
	autoTestGeneration          uint64
	testInputGeneration         atomic.Uint64
}

var watcherSessionCounter atomic.Uint64
var activeWatcherSession atomic.Uint64

// 최근 피드백 히스토리 (AI 컨텍스트용)
var feedbackHistory []string
var feedbackMu sync.Mutex

func AddFeedback(msg string) {
	feedbackMu.Lock()
	defer feedbackMu.Unlock()
	feedbackHistory = append(feedbackHistory, msg)
	if len(feedbackHistory) > 3 {
		feedbackHistory = feedbackHistory[len(feedbackHistory)-3:]
	}
}

func GetFeedbackHistory() []string {
	feedbackMu.Lock()
	defer feedbackMu.Unlock()
	result := make([]string, len(feedbackHistory))
	copy(result, feedbackHistory)
	return result
}

func clearFeedbackHistory() {
	feedbackMu.Lock()
	feedbackHistory = nil
	feedbackMu.Unlock()
}

var Global *Watcher

var watchedExts = map[string]bool{
	".go":  true,
	".ts":  true,
	".tsx": true,
	".mts": true,
	".cts": true,
	".js":  true,
	".jsx": true,
	".mjs": true,
	".cjs": true,
	".rs":  true,
	".sol": true,
	".py":  true,
}

var ignoredDirs = map[string]bool{
	".git": true, ".snapshots": true, "node_modules": true, "vendor": true,
	"dist": true, "build": true, "target": true, ".venv": true, "venv": true,
	"coverage": true,
}

func autoTestEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CODING_TUTOR_AUTO_TEST"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func isIgnoredDir(name string) bool {
	return ignoredDirs[name] || strings.HasPrefix(name, ".")
}

// The Rust toolchain reads project-local .cargo/config{,.toml}. Keep that
// narrowly scoped directory observable while continuing to skip every other
// hidden directory (and every other file inside .cargo).
func isAllowedHiddenDir(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && filepath.ToSlash(rel) == ".cargo"
}

func shouldSkipWatchedDir(root, candidate string) bool {
	if candidate == root {
		return false
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return true
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if filepath.Base(root) == ".cargo" || len(parts) > 1 && parts[0] == ".cargo" {
		return true
	}
	return isIgnoredDir(filepath.Base(candidate)) && !isAllowedHiddenDir(root, candidate)
}

func containsIgnoredPath(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return false
	}
	slashRel := filepath.ToSlash(rel)
	if slashRel == ".cargo" || slashRel == ".cargo/config" || slashRel == ".cargo/config.toml" {
		return false
	}
	if strings.HasPrefix(slashRel, ".cargo/") {
		return true
	}
	parts := strings.Split(slashRel, "/")
	for _, part := range parts {
		if isIgnoredDir(part) {
			return true
		}
	}
	return false
}

func hash(content string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
}

func computeCombinedHash(snapshots map[string]FileSnapshot) string {
	keys := make([]string, 0, len(snapshots))
	for k := range snapshots {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(snapshots[k].Hash))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func computeSemanticCombinedHash(snapshots map[string]FileSnapshot) string {
	keys := make([]string, 0, len(snapshots))
	for path := range snapshots {
		if !isTestFile(path) && !isTestConfigurationFile(path) {
			keys = append(keys, path)
		}
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, path := range keys {
		h.Write([]byte(path))
		h.Write([]byte{0})
		h.Write([]byte(snapshots[path].SemanticHash))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func computeAutoTestInputHash(snapshots map[string]FileSnapshot) string {
	h := sha256.New()
	h.Write([]byte(computeSemanticCombinedHash(snapshots)))
	h.Write([]byte(computeCompletionMarkerHash(snapshots)))
	paths := make([]string, 0, len(snapshots))
	for path := range snapshots {
		if isTestFile(path) || isTestConfigurationFile(path) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		h.Write([]byte(path))
		h.Write([]byte{0})
		h.Write([]byte(snapshots[path].Hash))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func computeCompletionMarkerHash(snapshots map[string]FileSnapshot) string {
	paths := make([]string, 0, len(snapshots))
	for path := range snapshots {
		if !isTestFile(path) && !isTestConfigurationFile(path) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, path := range paths {
		content := snapshots[path].Content
		fmt.Fprintf(h, "%s\x00%d:%d:%d\x00", path,
			strings.Count(content, "[TUTOR:HOLE]"),
			strings.Count(content, "[TUTOR:BUG]"),
			strings.Count(content, "[TUTOR:END]"),
		)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// CaptureTestInputVersion reads the on-disk test inputs immediately. A manual
// test endpoint uses the returned guard to prevent an old command from
// publishing completion after the project or its inputs have changed.
func CaptureTestInputVersion(dir string) (TestInputVersion, error) {
	canonicalDir, err := canonicalWatcherDir(dir)
	if err != nil {
		return TestInputVersion{}, err
	}
	var current *Watcher
	if candidate := Global; candidate != nil && candidate.isActive() && sameWatcherDir(candidate.dir, canonicalDir) {
		current = candidate
		current.syncMu.Lock()
		defer current.syncMu.Unlock()
		if !current.isActive() || !sameWatcherDir(current.dir, canonicalDir) {
			return TestInputVersion{}, ErrWatcherInactive
		}
	}
	snapshots, err := readProjectSnapshots(canonicalDir)
	if err != nil {
		return TestInputVersion{}, err
	}
	version := TestInputVersion{ProjectDir: canonicalDir, Hash: computeAutoTestInputHash(snapshots)}
	if current != nil {
		version.SessionID = current.sessionID
		version.Generation = current.testInputGeneration.Load()
		version.watcher = current
	}
	return version, nil
}

// PublishIfCurrent serializes a manual-test result with watcher refreshes. A
// result can be emitted before a newer sync (which then invalidates it), or be
// rejected after that sync, but can never appear stale after invalidation.
func (version TestInputVersion) PublishIfCurrent(publish func()) bool {
	if publish == nil {
		return false
	}
	if version.watcher == nil {
		current, err := CaptureTestInputVersion(version.ProjectDir)
		if err != nil || current.Hash != version.Hash || current.SessionID != version.SessionID {
			return false
		}
		status := project.Global.GetStatus()
		if !status.Loaded || !sameWatcherDir(status.Dir, version.ProjectDir) {
			return false
		}
		publish()
		return true
	}

	w := version.watcher
	w.syncMu.Lock()
	defer w.syncMu.Unlock()
	if !w.isActive() || w.sessionID != version.SessionID || !sameWatcherDir(w.dir, version.ProjectDir) ||
		w.testInputGeneration.Load() != version.Generation {
		return false
	}
	snapshots, err := readProjectSnapshots(version.ProjectDir)
	if err != nil || computeAutoTestInputHash(snapshots) != version.Hash || w.testInputGeneration.Load() != version.Generation {
		return false
	}
	return w.emitIfActive(w.ctx, publish)
}

// markTestInputMutation serializes a filesystem mutation epoch with manual
// test publication. Even an A→B→A edit is observable although its final hash
// matches the input captured by a long-running process.
func (w *Watcher) markTestInputMutation() {
	w.syncMu.Lock()
	w.testInputGeneration.Add(1)
	w.syncMu.Unlock()
}

// isTestFile reports whether the language registry classifies the path as a
// dedicated test file that should be excluded from learner diff feedback.
func isTestFile(path string) bool {
	return toolchain.IsDedicatedTestFile(path)
}

func isTestConfigurationFile(path string) bool {
	return toolchain.IsTestConfigurationFile(path)
}

// runTests executes the appropriate test command and returns both its bounded
// output and the actual process success status.
func runTests(dir, language string) (string, bool) {
	return runTestsContext(context.Background(), dir, language)
}

func runTestsContext(parent context.Context, dir, language string) (string, bool) {
	spec, ok := toolchain.Lookup(language)
	if !ok {
		return "", false
	}
	args := spec.TestCommand("")
	if len(args) == 0 {
		return "", false
	}

	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = ws.SanitizedEnv(os.Environ())
	prepareTestCommand(cmd)
	output := &limitedOutput{max: maxTestOutputBytes}
	cmd.Stdout = output
	cmd.Stderr = output
	runErr := cmd.Run()
	text := output.buffer.String()
	if output.truncated {
		text += "\n[output truncated]"
	}
	if ctx.Err() == context.DeadlineExceeded {
		text += "\n[test timed out after 30s]"
	}
	return strings.TrimSpace(text), runErr == nil && ctx.Err() == nil
}

type limitedOutput struct {
	buffer    bytes.Buffer
	max       int
	truncated bool
}

func (w *limitedOutput) Write(p []byte) (int, error) {
	written := len(p)
	remaining := w.max - w.buffer.Len()
	if len(p) > remaining {
		w.truncated = true
	}
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = w.buffer.Write(p)
	}
	return written, nil
}

func prepareTestCommand(cmd *exec.Cmd) {
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

func stripStepCompleteMarker(response string) string {
	return strings.ReplaceAll(response, "[STEP_COMPLETE]", "")
}

func Start(dir string) error {
	canonicalDir, err := canonicalWatcherDir(dir)
	if err != nil {
		return err
	}
	if Global != nil {
		Global.Stop()
		Global = nil
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	w := &Watcher{
		snapshots: make(map[string]FileSnapshot),
		fsw:       fsw,
		dir:       canonicalDir,
		ctx:       ctx,
		cancel:    cancel,
		sessionID: watcherSessionCounter.Add(1),
		now:       time.Now,
	}

	if err := w.snapshot(canonicalDir); err != nil {
		cancel()
		_ = fsw.Close()
		return err
	}
	w.initializeReviewState()

	if err := watchDir(fsw, canonicalDir); err != nil {
		cancel()
		_ = fsw.Close()
		return err
	}

	clearFeedbackHistory()
	ws.Global.SetProjectSession(canonicalDir, w.sessionID)
	activeWatcherSession.Store(w.sessionID)
	Global = w
	broadcastSessionChanged(canonicalDir)
	go w.run(ctx, canonicalDir)
	log.Printf("watcher: started watching %s", canonicalDir)
	return nil
}

// EnsureStarted preserves the review baseline and pending candidate when a
// load request re-enters the same canonical project. Mutating workflows such
// as apply-step and restore continue to call Start to establish a new baseline.
func EnsureStarted(dir string) error {
	canonicalDir, err := canonicalWatcherDir(dir)
	if err != nil {
		return err
	}
	if Global != nil && Global.isActive() && sameWatcherDir(Global.dir, canonicalDir) {
		return Global.Refresh()
	}
	return Start(canonicalDir)
}

func canonicalWatcherDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func sameWatcherDir(left, right string) bool {
	if filepath.Clean(left) == filepath.Clean(right) {
		return true
	}
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}

func watchDir(fsw *fsnotify.Watcher, dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			return nil
		}
		if shouldSkipWatchedDir(dir, path) {
			return filepath.SkipDir
		}
		return fsw.Add(path)
	})
}

func (w *Watcher) snapshot(dir string) error {
	snapshots, err := readProjectSnapshots(dir)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.snapshots = snapshots
	w.mu.Unlock()
	return nil
}

func readProjectSnapshots(dir string) (map[string]FileSnapshot, error) {
	snapshots := make(map[string]FileSnapshot)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if shouldSkipWatchedDir(dir, path) {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !watchedExts[ext] && !isTestConfigurationFile(path) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		s := string(content)
		snapshots[path] = FileSnapshot{
			Path:         path,
			Content:      s,
			Hash:         hash(s),
			SemanticHash: diffpkg.SemanticFingerprint(path, s),
		}
		return nil
	})
	return snapshots, err
}

func (w *Watcher) run(ctx context.Context, dir string) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			if containsIgnoredPath(dir, event.Name) {
				continue
			}
			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err := watchDir(w.fsw, event.Name); err != nil {
						log.Printf("watcher: add directory %s: %v", event.Name, err)
					}
					w.mu.Lock()
					w.lastChangeTime = time.Now()
					w.mu.Unlock()
					continue
				}
			}
			affectsWatchedFile := watchedExts[strings.ToLower(filepath.Ext(event.Name))] || isTestConfigurationFile(event.Name)
			if !affectsWatchedFile && event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				prefix := filepath.Clean(event.Name) + string(os.PathSeparator)
				w.mu.RLock()
				for snapshotPath := range w.snapshots {
					if strings.HasPrefix(snapshotPath, prefix) {
						affectsWatchedFile = true
						break
					}
				}
				w.mu.RUnlock()
			}
			if !affectsWatchedFile {
				continue
			}
			w.markTestInputMutation()
			w.mu.Lock()
			w.lastChangeTime = time.Now()
			w.mu.Unlock()

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			log.Printf("watcher error: %v", err)

		case <-ticker.C:
			w.mu.RLock()
			lastChange := w.lastChangeTime
			w.mu.RUnlock()

			if lastChange.IsZero() {
				continue
			}
			if time.Since(lastChange) < 500*time.Millisecond {
				continue
			}
			w.triggerSync(ctx, dir)
		}
	}
}

func (w *Watcher) isActive() bool {
	return w != nil && w.ctx != nil && w.ctx.Err() == nil && activeWatcherSession.Load() == w.sessionID
}

func (w *Watcher) IsActive() bool { return w.isActive() }

func (w *Watcher) emitIfActive(ctx context.Context, emit func()) bool {
	w.eventsMu.RLock()
	defer w.eventsMu.RUnlock()
	if !w.isActive() || ctx.Err() != nil {
		return false
	}
	emit()
	return true
}

func (w *Watcher) triggerSync(ctx context.Context, dir string) {
	if !w.isActive() || ctx.Err() != nil {
		return
	}
	if err := w.refresh(ctx, dir); err != nil && ctx.Err() == nil {
		log.Printf("watcher: refresh %s: %v", dir, err)
	}
}

// Refresh immediately reconciles the watcher with disk. The explicit review
// endpoint uses it after the editor flushes autosave so it never has to wait
// for the filesystem debounce window.
func (w *Watcher) Refresh() error {
	if w == nil || !w.isActive() {
		return ErrWatcherInactive
	}
	return w.refresh(w.ctx, w.dir)
}

func (w *Watcher) refresh(ctx context.Context, dir string) error {
	w.syncMu.Lock()
	defer w.syncMu.Unlock()
	return w.refreshLocked(ctx, dir)
}

// refreshLocked reconciles disk while the caller holds syncMu. Review start
// keeps that lock through run reservation so a newer published snapshot cannot
// slip between synchronous refresh and AI-start validation.
func (w *Watcher) refreshLocked(ctx context.Context, dir string) error {
	refreshStarted := time.Now()
	if !w.isActive() || ctx.Err() != nil {
		return ErrWatcherInactive
	}

	w.mu.RLock()
	oldSnapshots := cloneSnapshots(w.snapshots)
	w.mu.RUnlock()
	newSnapshots, err := readProjectSnapshots(dir)
	if err != nil {
		return err
	}

	combinedHash := computeSemanticCombinedHash(newSnapshots)
	autoTestInputHash := computeAutoTestInputHash(newSnapshots)
	testsChanged := dedicatedTestsChanged(oldSnapshots, newSnapshots)
	markersChanged := computeCompletionMarkerHash(oldSnapshots) != computeCompletionMarkerHash(newSnapshots)
	testInputsChanged := testsChanged || markersChanged
	w.reviewMu.Lock()
	previousSemanticHash := w.semanticHash
	w.reviewMu.Unlock()
	semanticChanged := combinedHash != previousSemanticHash
	if semanticChanged || testInputsChanged {
		w.testInputGeneration.Add(1)
		// Invalidate the prior automatic-test generation before publishing a
		// newer snapshot. A completed old process can never repopulate a pass
		// after the sync_status invalidation.
		w.cancelAutomaticTests()
	}

	w.mu.Lock()
	w.snapshots = newSnapshots
	if !w.lastChangeTime.After(refreshStarted) {
		w.lastChangeTime = time.Time{}
	}
	w.mu.Unlock()
	if testInputsChanged && !semanticChanged {
		w.invalidateTestEvidence(ctx)
	}

	if !w.emitIfActive(ctx, func() { broadcastSyncStatus(dir, semanticChanged || testInputsChanged) }) {
		return ErrWatcherInactive
	}

	if !semanticChanged {
		w.refreshCandidateContent(newSnapshots)
		if testInputsChanged {
			status := project.Global.GetStatus()
			if autoTestEnabled() && status.Loaded && filepath.Clean(status.Dir) == filepath.Clean(dir) && status.Language != "" {
				w.reviewMu.Lock()
				revision := w.revision
				semanticHash := w.semanticHash
				w.reviewMu.Unlock()
				w.startAutomaticTests(ctx, dir, status.Language, revision, semanticHash, autoTestInputHash)
			}
		}
		return nil
	}

	status := project.Global.GetStatus()
	if !status.Loaded || filepath.Clean(status.Dir) != filepath.Clean(dir) {
		return ErrProjectMismatch
	}
	revision := w.noteSemanticChange(ctx, newSnapshots, combinedHash)

	if autoTestEnabled() && status.Language != "" {
		w.startAutomaticTests(ctx, dir, status.Language, revision, combinedHash, autoTestInputHash)
	}
	return nil
}

func dedicatedTestsChanged(oldSnapshots, newSnapshots map[string]FileSnapshot) bool {
	paths := make(map[string]struct{}, len(oldSnapshots)+len(newSnapshots))
	for path := range oldSnapshots {
		if isTestFile(path) || isTestConfigurationFile(path) {
			paths[path] = struct{}{}
		}
	}
	for path := range newSnapshots {
		if isTestFile(path) || isTestConfigurationFile(path) {
			paths[path] = struct{}{}
		}
	}
	for path := range paths {
		oldSnapshot, oldExists := oldSnapshots[path]
		newSnapshot, newExists := newSnapshots[path]
		if oldExists != newExists || oldSnapshot.Hash != newSnapshot.Hash {
			return true
		}
	}
	return false
}

func (w *Watcher) runAutomaticTests(ctx context.Context, dir, language string, revision uint64, semanticHash, inputHash string, generation uint64) {
	testOutput, testsPassed := runAutomaticTestCommand(ctx, dir, language)
	if testOutput != "" {
		log.Printf("watcher: test output (%d bytes)", len(testOutput))
	}
	summary := "테스트 실행 결과를 확인할 수 없습니다."
	if lines := strings.Split(testOutput, "\n"); len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		summary = lines[len(lines)-1]
	}
	completionResult := completion.Evaluate(config.Global.BaseDir, dir, testsPassed)

	w.autoTestMu.Lock()
	defer w.autoTestMu.Unlock()
	if w.autoTestGeneration != generation || ctx.Err() != nil || !w.isActive() {
		return
	}
	w.mu.RLock()
	currentInputHash := computeAutoTestInputHash(w.snapshots)
	w.mu.RUnlock()
	if currentInputHash != inputHash || !w.isCurrentRevision(revision, semanticHash) {
		return
	}
	w.setAutomaticTestOutput(revision, semanticHash, testOutput)
	if !w.emitIfActive(ctx, func() {
		broadcastAutomaticTestResult(dir, testsPassed, completion.AppendSummary(summary, completionResult), inputHash)
	}) {
		return
	}
	w.emitIfActive(ctx, func() {
		broadcastAutomaticStepComplete(dir, completionResult.Complete)
	})
}

func (w *Watcher) startAutomaticTests(parent context.Context, dir, language string, revision uint64, semanticHash, inputHash string) {
	w.autoTestMu.Lock()
	if w.autoTestCancel != nil {
		w.autoTestCancel()
	}
	ctx, cancel := context.WithCancel(parent)
	w.autoTestCancel = cancel
	w.autoTestGeneration++
	generation := w.autoTestGeneration
	w.autoTestMu.Unlock()
	go func() {
		w.runAutomaticTests(ctx, dir, language, revision, semanticHash, inputHash, generation)
		w.autoTestMu.Lock()
		if w.autoTestGeneration == generation {
			w.autoTestCancel = nil
		}
		w.autoTestMu.Unlock()
	}()
}

func (w *Watcher) cancelAutomaticTests() {
	w.autoTestMu.Lock()
	if w.autoTestCancel != nil {
		w.autoTestCancel()
		w.autoTestCancel = nil
	}
	w.autoTestGeneration++
	w.autoTestMu.Unlock()
}

func (w *Watcher) Stop() {
	if w == nil {
		return
	}
	if w.cancel != nil {
		w.cancel()
	}
	w.cancelAutomaticTests()
	w.eventsMu.Lock()
	defer w.eventsMu.Unlock()
	if activeWatcherSession.CompareAndSwap(w.sessionID, 0) {
		ws.Global.ClearProjectSession(w.dir, w.sessionID)
		clearFeedbackHistory()
	}
	if w.fsw != nil {
		_ = w.fsw.Close()
	}
}

type feedbackStreamFunc func(
	client *ai.Client,
	ctx context.Context,
	tutorContent, diffContent, changedFilesCode, testOutput string,
	history []string,
	skillLevel string,
	cb ai.StreamCallback,
) error

var streamFeedback feedbackStreamFunc = func(
	client *ai.Client,
	ctx context.Context,
	tutorContent, diffContent, changedFilesCode, testOutput string,
	history []string,
	skillLevel string,
	cb ai.StreamCallback,
) error {
	return client.StreamFeedback(ctx, tutorContent, diffContent, changedFilesCode, testOutput, history, skillLevel, cb)
}

var runAutomaticTestCommand = runTestsContext

var (
	broadcastSessionChanged = func(projectDir string) {
		ws.Global.BroadcastSessionChangedForProject(projectDir)
	}
	broadcastSyncStatus = func(projectDir string, changed bool) {
		ws.Global.BroadcastSyncStatusForProject(projectDir, changed)
	}
	broadcastAutomaticTestResult = func(projectDir string, passed bool, summary, inputHash string) {
		ws.Global.BroadcastTestResultForProjectWithInputHash(projectDir, passed, summary, inputHash)
	}
	broadcastAutomaticStepComplete = func(projectDir string, passed bool) {
		ws.Global.BroadcastStepCompleteForProject(projectDir, passed)
	}
	broadcastFeedbackStart = func(state ReviewState) {
		ws.Global.BroadcastFeedbackStartForReview(state.ProjectDir, state.Revision, state.SemanticHash, state.Files, state.RequestID)
	}
	broadcastFeedbackChunk = func(state ReviewState, chunk string) {
		ws.Global.BroadcastFeedbackChunkForReview(state.ProjectDir, state.Revision, state.SemanticHash, state.Files, state.RequestID, chunk)
	}
	broadcastFeedbackEnd = func(state ReviewState) {
		ws.Global.BroadcastFeedbackEndForReview(state.ProjectDir, state.Revision, state.SemanticHash, state.Files, state.RequestID)
	}
	broadcastFeedbackError = func(state ReviewState, message string) {
		ws.Global.BroadcastFeedbackErrorForReview(state.ProjectDir, state.Revision, state.SemanticHash, state.Files, state.RequestID, message)
	}
	broadcastReviewEvent = func(eventType string, state ReviewState, reason, message string) {
		ws.Global.BroadcastReviewEvent(eventType, state.ProjectDir, state.Revision, state.SemanticHash, state.Files, state.Status, reason, message, state.RequestID)
	}
)
