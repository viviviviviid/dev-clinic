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
	Path    string
	Content string
	Hash    string
}

type Watcher struct {
	mu             sync.RWMutex
	eventsMu       sync.RWMutex
	snapshots      map[string]FileSnapshot
	fsw            *fsnotify.Watcher
	lastChangeTime time.Time
	lastSyncedHash string
	ctx            context.Context
	cancel         context.CancelFunc
	sessionID      uint64
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

func containsIgnoredPath(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
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

// isTestFile reports whether the language registry classifies the path as a
// dedicated test file that should be excluded from learner diff feedback.
func isTestFile(path string) bool {
	return toolchain.IsDedicatedTestFile(path)
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
		ctx:       ctx,
		cancel:    cancel,
		sessionID: watcherSessionCounter.Add(1),
	}

	if err := w.snapshot(dir); err != nil {
		cancel()
		_ = fsw.Close()
		return err
	}

	if err := watchDir(fsw, dir); err != nil {
		cancel()
		_ = fsw.Close()
		return err
	}

	clearFeedbackHistory()
	activeWatcherSession.Store(w.sessionID)
	Global = w
	go w.run(ctx, dir)
	log.Printf("watcher: started watching %s", dir)
	return nil
}

func watchDir(fsw *fsnotify.Watcher, dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			return nil
		}
		if path != dir && isIgnoredDir(info.Name()) {
			return filepath.SkipDir
		}
		return fsw.Add(path)
	})
}

func (w *Watcher) snapshot(dir string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if path != dir && isIgnoredDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !watchedExts[ext] {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		s := string(content)
		w.snapshots[path] = FileSnapshot{
			Path:    path,
			Content: s,
			Hash:    hash(s),
		}
		return nil
	})
}

func (w *Watcher) run(ctx context.Context, dir string) {
	ticker := time.NewTicker(3 * time.Second)
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
			affectsWatchedFile := watchedExts[strings.ToLower(filepath.Ext(event.Name))]
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
			if time.Since(lastChange) < 2*time.Second {
				continue
			}
			w.triggerSync(ctx, dir)
		}
	}
}

func (w *Watcher) isActive() bool {
	return w != nil && w.ctx != nil && w.ctx.Err() == nil && activeWatcherSession.Load() == w.sessionID
}

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

	w.mu.Lock()
	oldSnapshots := make(map[string]FileSnapshot, len(w.snapshots))
	for k, v := range w.snapshots {
		oldSnapshots[k] = v
	}
	w.mu.Unlock()

	// Read current state
	newSnapshots := make(map[string]FileSnapshot)
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if path != dir && isIgnoredDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !watchedExts[ext] {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		s := string(content)
		newSnapshots[path] = FileSnapshot{
			Path:    path,
			Content: s,
			Hash:    hash(s),
		}
		return nil
	})

	// Compute diffs — 테스트 파일 제외, 학습자가 수정한 파일만
	var diffs []string
	var changedCode strings.Builder
	changed := false

	allPaths := make(map[string]struct{}, len(oldSnapshots)+len(newSnapshots))
	for path := range oldSnapshots {
		allPaths[path] = struct{}{}
	}
	for path := range newSnapshots {
		allPaths[path] = struct{}{}
	}
	paths := make([]string, 0, len(allPaths))
	for path := range allPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		if isTestFile(path) {
			continue
		}
		oldSnap, oldExists := oldSnapshots[path]
		newSnap, newExists := newSnapshots[path]
		rel, _ := filepath.Rel(dir, path)

		if !oldExists || !newExists || diffpkg.HasChanges(oldSnap.Content, newSnap.Content) {
			changed = true
			diffStr := diffpkg.Unified(oldSnap.Content, newSnap.Content, rel)
			if diffStr != "" {
				diffs = append(diffs, diffStr)
			}
			if newExists {
				changedCode.WriteString(fmt.Sprintf("\n### %s\n```\n%s\n```\n", rel, newSnap.Content))
			} else {
				changedCode.WriteString(fmt.Sprintf("\n### %s\n(deleted)\n", rel))
			}
		}
	}

	combinedHash := computeCombinedHash(newSnapshots)

	w.mu.Lock()
	w.snapshots = newSnapshots
	w.lastChangeTime = time.Time{}
	w.mu.Unlock()

	if !w.emitIfActive(ctx, func() { ws.Global.BroadcastSyncStatusForProject(dir, changed) }) {
		return
	}

	if !changed || len(diffs) == 0 {
		return
	}

	w.mu.RLock()
	lastHash := w.lastSyncedHash
	w.mu.RUnlock()
	if combinedHash == lastHash {
		log.Println("watcher: content unchanged since last sync, skipping AI")
		return
	}
	w.mu.Lock()
	w.lastSyncedHash = combinedHash
	w.mu.Unlock()

	status := project.Global.GetStatus()
	if !status.Loaded || filepath.Clean(status.Dir) != filepath.Clean(dir) {
		return
	}

	// Run tests and capture output
	testOutput := ""
	if autoTestEnabled() && status.Language != "" {
		var testsPassed bool
		testOutput, testsPassed = runTestsContext(ctx, dir, status.Language)
		if !w.isActive() || ctx.Err() != nil {
			return
		}
		if testOutput != "" {
			log.Printf("watcher: test output (%d bytes)", len(testOutput))
		}
		// 테스트 결과 요약 추출 (마지막 줄 또는 실행 실패 메시지)
		summary := "테스트 실행 결과를 확인할 수 없습니다."
		if lines := strings.Split(testOutput, "\n"); len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			summary = lines[len(lines)-1]
		}
		completionResult := completion.Evaluate(config.Global.BaseDir, dir, testsPassed)
		if !w.emitIfActive(ctx, func() {
			ws.Global.BroadcastTestResultForProject(dir, testsPassed, completion.AppendSummary(summary, completionResult))
		}) {
			return
		}
		if !w.emitIfActive(ctx, func() {
			ws.Global.BroadcastStepCompleteForProject(dir, completionResult.Complete)
		}) {
			return
		}
	}

	if ai.Global == nil {
		log.Println("watcher: AI client not initialized")
		return
	}

	// Stream feedback
	if !w.emitIfActive(ctx, func() { broadcastFeedbackStart(dir) }) {
		return
	}

	tutorContent := project.Global.GetContent()
	diffContent := strings.Join(diffs, "\n---\n")
	history := GetFeedbackHistory()
	skillLevel := project.Global.GetSkillLevel()

	var fullResponse strings.Builder
	feedbackCtx, cancel := context.WithTimeout(ctx, feedbackTimeout)
	defer cancel()
	err := streamFeedback(ai.Global, feedbackCtx, tutorContent, diffContent, changedCode.String(), testOutput, history, skillLevel, func(chunk string) {
		if w.isActive() && ctx.Err() == nil {
			fullResponse.WriteString(chunk)
		}
	})
	if !w.isActive() || ctx.Err() != nil {
		return
	}
	if err != nil {
		log.Printf("watcher: AI feedback error: %v", err)
		if !w.emitIfActive(ctx, func() { broadcastFeedbackError(dir, err.Error()) }) {
			return
		}
	} else {
		response := fullResponse.String()
		// 단계 완료는 서버의 테스트+마커 게이트만 결정한다.
		strippedResponse := stripStepCompleteMarker(response)
		if response != strippedResponse {
			log.Println("watcher: removed model-supplied [STEP_COMPLETE]")
		}
		response = strippedResponse
		if !w.emitIfActive(ctx, func() {
			broadcastFeedbackChunk(dir, response)
			AddFeedback(response)
		}) {
			return
		}
	}

	w.emitIfActive(ctx, func() { broadcastFeedbackEnd(dir) })
}

func (w *Watcher) Stop() {
	if w == nil {
		return
	}
	if w.cancel != nil {
		w.cancel()
	}
	w.eventsMu.Lock()
	defer w.eventsMu.Unlock()
	if activeWatcherSession.CompareAndSwap(w.sessionID, 0) {
		clearFeedbackHistory()
	}
	if w.fsw != nil {
		_ = w.fsw.Close()
	}
}

var streamFeedback = func(
	client *ai.Client,
	ctx context.Context,
	tutorContent, diffContent, changedFilesCode, testOutput string,
	history []string,
	skillLevel string,
	cb ai.StreamCallback,
) error {
	return client.StreamFeedback(ctx, tutorContent, diffContent, changedFilesCode, testOutput, history, skillLevel, cb)
}

var (
	broadcastFeedbackStart = func(projectDir string) { ws.Global.BroadcastFeedbackStartForProject(projectDir) }
	broadcastFeedbackChunk = func(projectDir, chunk string) { ws.Global.BroadcastFeedbackChunkForProject(projectDir, chunk) }
	broadcastFeedbackEnd   = func(projectDir string) { ws.Global.BroadcastFeedbackEndForProject(projectDir) }
	broadcastFeedbackError = func(projectDir, message string) { ws.Global.BroadcastErrorForProject(projectDir, message) }
)
