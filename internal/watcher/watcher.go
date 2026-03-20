package watcher

import (
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
	"time"

	"github.com/coding-tutor/internal/ai"
	diffpkg "github.com/coding-tutor/internal/diff"
	"github.com/coding-tutor/internal/project"
	"github.com/coding-tutor/internal/ws"
	"github.com/fsnotify/fsnotify"
)

type FileSnapshot struct {
	Path    string
	Content string
	Hash    string
}

type Watcher struct {
	mu             sync.RWMutex
	snapshots      map[string]FileSnapshot
	fsw            *fsnotify.Watcher
	lastChangeTime time.Time
	lastSyncedHash string
	cancel         context.CancelFunc
}

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

var Global *Watcher

var watchedExts = map[string]bool{
	".go":  true,
	".ts":  true,
	".tsx": true,
	".js":  true,
	".jsx": true,
	".rs":  true,
	".sol": true,
	".py":  true,
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
		h.Write([]byte(snapshots[k].Hash))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// isTestFile reports whether the given path is a test file that should be
// excluded from diff calculation (학습자가 수정하는 파일이 아님).
func isTestFile(path string) bool {
	base := filepath.Base(path)
	if strings.HasSuffix(base, "_test.go") {
		return true
	}
	if strings.HasPrefix(base, "test_") && (strings.HasSuffix(base, ".py")) {
		return true
	}
	if strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
		return true
	}
	return false
}

// runTests executes the appropriate test command for the given language and
// returns the combined output (stdout + stderr). Returns empty string if the
// language is unsupported or no test files exist.
func runTests(dir, language string) string {
	var args []string
	switch strings.ToLower(language) {
	case "go":
		args = []string{"go", "test", "-v", "./..."}
	case "python":
		args = []string{"python", "-m", "pytest", "--tb=short", "-q"}
	case "rust":
		args = []string{"cargo", "test"}
	case "typescript", "javascript":
		args = []string{"npm", "test", "--", "--watchAll=false", "--passWithNoTests"}
	default:
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput()
	return strings.TrimSpace(string(out))
}

func allTestsPassed(language, testOutput string) bool {
	if testOutput == "" {
		return false
	}
	switch strings.ToLower(language) {
	case "go":
		// "ok  module/pkg 0.001s" 형태의 라인이 있어야 함 (단순 "ok" 포함 판단 금지)
		hasPkg := false
		for _, l := range strings.Split(testOutput, "\n") {
			trimmed := strings.TrimSpace(l)
			if strings.HasPrefix(trimmed, "ok ") || strings.HasPrefix(trimmed, "ok\t") {
				hasPkg = true
			}
			if strings.HasPrefix(trimmed, "FAIL") {
				return false
			}
		}
		return hasPkg
	case "python":
		// pytest -q 출력: "1 passed in 0.01s", "2 passed, 1 warning in ..."
		return strings.Contains(testOutput, " passed") && !strings.Contains(testOutput, " failed")
	case "rust":
		return strings.Contains(testOutput, "test result: ok")
	case "typescript", "javascript":
		return strings.Contains(testOutput, "Tests:") && !strings.Contains(testOutput, "failed")
	}
	return false
}

func Start(dir string) error {
	if Global != nil {
		Global.Stop()
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	w := &Watcher{
		snapshots: make(map[string]FileSnapshot),
		fsw:       fsw,
		cancel:    cancel,
	}

	if err := w.snapshot(dir); err != nil {
		return err
	}

	if err := watchDir(fsw, dir); err != nil {
		return err
	}

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
		if info.IsDir() && !strings.HasPrefix(info.Name(), ".") {
			return fsw.Add(path)
		}
		return nil
	})
}

func (w *Watcher) snapshot(dir string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.Contains(path, "/.snapshots/") {
			return nil
		}
		ext := filepath.Ext(path)
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
			if !watchedExts[filepath.Ext(event.Name)] {
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
			w.triggerSync(dir)
		}
	}
}

func (w *Watcher) triggerSync(dir string) {
	w.mu.Lock()
	oldSnapshots := make(map[string]FileSnapshot, len(w.snapshots))
	for k, v := range w.snapshots {
		oldSnapshots[k] = v
	}
	w.mu.Unlock()

	// Read current state
	newSnapshots := make(map[string]FileSnapshot)
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.Contains(path, "/.snapshots/") {
			return nil
		}
		ext := filepath.Ext(path)
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

	for path, newSnap := range newSnapshots {
		if isTestFile(path) {
			continue
		}
		oldSnap, exists := oldSnapshots[path]
		rel, _ := filepath.Rel(dir, path)

		if !exists || diffpkg.HasChanges(oldSnap.Content, newSnap.Content) {
			changed = true
			diffStr := diffpkg.Unified(oldSnap.Content, newSnap.Content, rel)
			if diffStr != "" {
				diffs = append(diffs, diffStr)
			}
			changedCode.WriteString(fmt.Sprintf("\n### %s\n```\n%s\n```\n", rel, newSnap.Content))
		}
	}

	combinedHash := computeCombinedHash(newSnapshots)

	w.mu.Lock()
	w.snapshots = newSnapshots
	w.lastChangeTime = time.Time{}
	w.mu.Unlock()

	ws.Global.BroadcastSyncStatus(changed)

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

	if !project.Global.IsLoaded() {
		return
	}

	if ai.Global == nil {
		log.Println("watcher: AI client not initialized")
		return
	}

	// Run tests and capture output
	status := project.Global.GetStatus()
	testOutput := ""
	testsPassed := false
	if status.Language != "" {
		testOutput = runTests(dir, status.Language)
		if testOutput != "" {
			log.Printf("watcher: test output (%d bytes)", len(testOutput))
			testsPassed = allTestsPassed(status.Language, testOutput)
			// 테스트 결과 요약 추출 (마지막 줄 또는 전체)
			summary := testOutput
			if lines := strings.Split(testOutput, "\n"); len(lines) > 0 {
				summary = lines[len(lines)-1]
			}
			ws.Global.BroadcastTestResult(testsPassed, summary)
		}
	}

	// Stream feedback
	ws.Global.BroadcastFeedbackStart()

	tutorContent := project.Global.GetContent()
	diffContent := strings.Join(diffs, "\n---\n")
	history := GetFeedbackHistory()
	skillLevel := project.Global.GetSkillLevel()

	var fullResponse strings.Builder
	ctx := context.Background()
	err := ai.Global.StreamFeedback(ctx, tutorContent, diffContent, changedCode.String(), testOutput, history, skillLevel, func(chunk string) {
		fullResponse.WriteString(chunk)
	})
	if err != nil {
		log.Printf("watcher: AI feedback error: %v", err)
		ws.Global.BroadcastError(err.Error())
	} else {
		response := fullResponse.String()
		// [STEP_COMPLETE] 하드 게이팅 — 테스트 미통과 시 제거
		if strings.Contains(response, "[STEP_COMPLETE]") && !testsPassed {
			log.Println("watcher: [STEP_COMPLETE] stripped — tests not passing")
			response = strings.ReplaceAll(response, "[STEP_COMPLETE]", "")
		}
		ws.Global.BroadcastFeedbackChunk(response)
		AddFeedback(response)
	}

	ws.Global.BroadcastFeedbackEnd()
}

func (w *Watcher) Stop() {
	w.cancel()
	w.fsw.Close()
}
