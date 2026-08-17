package watcher

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/coding-tutor/internal/ai"
	diffpkg "github.com/coding-tutor/internal/diff"
	"github.com/coding-tutor/internal/project"
)

const (
	reviewCooldown              = 15 * time.Second
	reviewWindow                = time.Minute
	reviewMaxBurst              = 4
	maxReviewTestContextBytes   = 64 << 10
	maxReviewSourceContextBytes = 256 << 10
	maxStoredFeedbackBytes      = 128 << 10
	maxCancelledReviewRequests  = 64
)

var (
	ErrWatcherInactive = errors.New("watcher is not active")
	ErrProjectMismatch = errors.New("watcher project does not match the loaded project")
)

type ReviewState struct {
	Status         string             `json:"status"`
	ProjectDir     string             `json:"project_dir"`
	Revision       uint64             `json:"revision"`
	SemanticHash   string             `json:"semantic_hash"`
	Files          []string           `json:"files"`
	SessionID      uint64             `json:"session_id"`
	RequestID      string             `json:"request_id,omitempty"`
	LastFeedback   *CompletedFeedback `json:"last_feedback,omitempty"`
	RequestPending bool               `json:"request_pending,omitempty"`
	DiskSynced     bool               `json:"disk_synced,omitempty"`
	LastSync       string             `json:"last_sync,omitempty"`
}

type CompletedFeedback struct {
	Revision     uint64   `json:"revision"`
	SemanticHash string   `json:"semantic_hash"`
	Files        []string `json:"files"`
	SessionID    uint64   `json:"session_id"`
	RequestID    string   `json:"request_id,omitempty"`
	Content      string   `json:"content"`
	CompletedAt  string   `json:"completed_at"`
}

type TestContext struct {
	Passed    *bool  `json:"passed,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Output    string `json:"output,omitempty"`
	InputHash string `json:"input_hash,omitempty"`
}

func (c TestContext) Present() bool {
	return c.Passed != nil || strings.TrimSpace(c.Summary) != "" || strings.TrimSpace(c.Output) != ""
}

type ReviewRequest struct {
	RequestID    string
	Revision     *uint64
	SemanticHash string
	TestContext  TestContext
}

type ReviewRequestError struct {
	Code       string
	Message    string
	RetryAfter time.Duration
	State      ReviewState
}

func (e *ReviewRequestError) Error() string { return e.Message }

type reviewCandidate struct {
	ReviewState
	DiffContent    string
	ChangedCode    string
	Snapshots      map[string]FileSnapshot
	AutoTestOutput string
	ContextOnly    bool
}

type reviewRun struct {
	candidate       reviewCandidate
	cancel          context.CancelFunc
	client          *ai.Client
	stream          feedbackStreamFunc
	tutorContent    string
	skillLevel      string
	testContextHash string
	contextOnly     bool
	testInputHash   string
	hasTestEvidence bool
}

func (w *Watcher) initializeReviewState() {
	w.mu.RLock()
	snapshots := cloneSnapshots(w.snapshots)
	w.mu.RUnlock()
	semanticHash := computeSemanticCombinedHash(snapshots)
	w.reviewMu.Lock()
	w.reviewBaseline = cloneSnapshots(snapshots)
	w.reviewBaselineHash = semanticHash
	w.semanticHash = semanticHash
	w.reviewCandidate = nil
	w.reviewRunning = nil
	w.reviewStarts = nil
	w.reviewRequestGeneration = 0
	w.pendingReviewGeneration = 0
	w.pendingReviewRequestID = ""
	w.cancelledReviewRequests = make(map[string]struct{})
	w.cancelledReviewRequestOrder = nil
	w.lastReviewedTestContextHash = ""
	w.lastFeedback = nil
	w.revision = 0
	w.reviewMu.Unlock()
}

func cloneSnapshots(source map[string]FileSnapshot) map[string]FileSnapshot {
	result := make(map[string]FileSnapshot, len(source))
	for path, snapshot := range source {
		result[path] = snapshot
	}
	return result
}

func (w *Watcher) GetReviewStatus() ReviewState {
	if w == nil {
		return ReviewState{Status: "idle", Files: []string{}}
	}
	w.reviewMu.Lock()
	defer w.reviewMu.Unlock()
	return w.reviewStateLocked()
}

func (w *Watcher) reviewStateLocked() ReviewState {
	state := ReviewState{
		Status:       "idle",
		ProjectDir:   w.dir,
		SessionID:    w.sessionID,
		Revision:     w.revision,
		SemanticHash: w.semanticHash,
		Files:        []string{},
	}
	if w.reviewCandidate != nil {
		state = w.reviewCandidate.ReviewState
		state.Files = append([]string(nil), state.Files...)
		state.Status = "ready"
	}
	if w.reviewRunning != nil {
		state = w.reviewRunning.candidate.ReviewState
		state.Files = append([]string(nil), state.Files...)
		state.Status = "reviewing"
	}
	state.RequestPending = w.pendingReviewGeneration != 0
	if state.RequestPending {
		state.RequestID = w.pendingReviewRequestID
	}
	state.LastFeedback = cloneCompletedFeedback(w.lastFeedback)
	return state
}

func cloneCompletedFeedback(feedback *CompletedFeedback) *CompletedFeedback {
	if feedback == nil {
		return nil
	}
	copy := *feedback
	copy.Files = append([]string(nil), feedback.Files...)
	return &copy
}

func (w *Watcher) noteSemanticChange(ctx context.Context, snapshots map[string]FileSnapshot, semanticHash string) uint64 {
	w.cancelAutomaticTests()
	w.reviewMu.Lock()
	if semanticHash == w.semanticHash {
		revision := w.revision
		w.reviewMu.Unlock()
		return revision
	}
	w.revision++
	w.semanticHash = semanticHash
	candidate := buildReviewCandidate(w.dir, w.sessionID, w.revision, semanticHash, w.reviewBaseline, snapshots)
	hadReadyCandidate := w.reviewCandidate != nil
	var cancelled *reviewRun
	if w.reviewRunning != nil {
		cancelled = w.reviewRunning
		cancelled.cancel()
		w.reviewRunning = nil
	}
	if len(candidate.Files) == 0 {
		w.reviewCandidate = nil
		w.reviewBaseline = cloneSnapshots(snapshots)
		w.reviewBaselineHash = semanticHash
	} else {
		w.reviewCandidate = candidate
	}
	state := w.reviewStateLocked()
	revision := w.revision
	w.reviewMu.Unlock()

	if cancelled != nil {
		cancelState := cancelled.candidate.ReviewState
		cancelState.Status = state.Status
		w.emitIfActive(ctx, func() {
			broadcastReviewEvent("review_cancelled", cancelState, "source_changed", "")
			broadcastFeedbackEnd(cancelState)
		})
	}
	if candidate != nil && len(candidate.Files) > 0 {
		ready := candidate.ReviewState
		ready.Status = "ready"
		w.emitIfActive(ctx, func() { broadcastReviewEvent("review_ready", ready, "", "") })
	} else if hadReadyCandidate {
		idle := state
		idle.Status = "idle"
		w.emitIfActive(ctx, func() { broadcastReviewEvent("review_cancelled", idle, "reverted", "") })
	}
	return revision
}

func (w *Watcher) refreshCandidateContent(snapshots map[string]FileSnapshot) {
	w.reviewMu.Lock()
	defer w.reviewMu.Unlock()
	if w.reviewCandidate == nil {
		if w.reviewRunning == nil && w.semanticHash == w.reviewBaselineHash {
			// Formatting-only edits become the new raw baseline. A later
			// meaningful diff therefore does not resurrect stale whitespace.
			w.reviewBaseline = cloneSnapshots(snapshots)
		}
		return
	}
	if w.reviewCandidate.SemanticHash != w.semanticHash {
		return
	}
	autoTestOutput := w.reviewCandidate.AutoTestOutput
	contextOnly := w.reviewCandidate.ContextOnly
	updated := buildReviewCandidate(w.dir, w.sessionID, w.revision, w.semanticHash, w.reviewBaseline, snapshots)
	updated.AutoTestOutput = autoTestOutput
	updated.ContextOnly = contextOnly
	w.reviewCandidate = updated
}

func buildReviewCandidate(dir string, sessionID, revision uint64, semanticHash string, baseline, current map[string]FileSnapshot) *reviewCandidate {
	allPaths := make(map[string]struct{}, len(baseline)+len(current))
	for path := range baseline {
		allPaths[path] = struct{}{}
	}
	for path := range current {
		allPaths[path] = struct{}{}
	}
	paths := make([]string, 0, len(allPaths))
	for path := range allPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var diffs []string
	var files []string
	var changedCode strings.Builder
	for _, path := range paths {
		if isTestFile(path) || isTestConfigurationFile(path) {
			continue
		}
		oldSnapshot, oldExists := baseline[path]
		newSnapshot, newExists := current[path]
		if oldExists && newExists && oldSnapshot.SemanticHash == newSnapshot.SemanticHash {
			continue
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			rel = filepath.Base(path)
		}
		rel = filepath.ToSlash(rel)
		files = append(files, rel)
		diffText := diffpkg.Unified(oldSnapshot.Content, newSnapshot.Content, rel)
		if diffText != "" {
			diffs = append(diffs, diffText)
		}
		if newExists {
			fmt.Fprintf(&changedCode, "\n### %s\n```\n%s\n```\n", rel, newSnapshot.Content)
		} else {
			fmt.Fprintf(&changedCode, "\n### %s\n(deleted)\n", rel)
		}
	}
	return &reviewCandidate{
		ReviewState: ReviewState{
			Status:       "ready",
			ProjectDir:   dir,
			SessionID:    sessionID,
			Revision:     revision,
			SemanticHash: semanticHash,
			Files:        files,
		},
		DiffContent: clipUTF8WithMarker(strings.Join(diffs, "\n---\n"), maxReviewSourceContextBytes, "\n[diff truncated]"),
		ChangedCode: clipUTF8WithMarker(changedCode.String(), maxReviewSourceContextBytes, "\n[changed files truncated]"),
		Snapshots:   cloneSnapshots(current),
	}
}

func (w *Watcher) isCurrentRevision(revision uint64, semanticHash string) bool {
	w.reviewMu.Lock()
	defer w.reviewMu.Unlock()
	return w.revision == revision && w.semanticHash == semanticHash
}

func (w *Watcher) setAutomaticTestOutput(revision uint64, semanticHash, output string) {
	w.reviewMu.Lock()
	defer w.reviewMu.Unlock()
	if w.revision == revision && w.semanticHash == semanticHash && w.reviewCandidate != nil {
		w.reviewCandidate.AutoTestOutput = output
	}
}

func (w *Watcher) RequestReview(request ReviewRequest) (ReviewState, error) {
	if w == nil || !w.isActive() {
		return ReviewState{}, ErrWatcherInactive
	}
	w.reviewMu.Lock()
	initialState := w.reviewStateLocked()
	request.RequestID = strings.TrimSpace(request.RequestID)
	if request.RequestID != "" {
		if _, cancelled := w.cancelledReviewRequests[request.RequestID]; cancelled {
			w.reviewMu.Unlock()
			return initialState, &ReviewRequestError{Code: "review_cancelled", Message: "AI 검토 요청이 취소되었습니다", State: initialState}
		}
	}
	if w.reviewRunning != nil || w.pendingReviewGeneration != 0 {
		w.reviewMu.Unlock()
		return initialState, &ReviewRequestError{Code: "review_in_progress", Message: "이미 최신 코드를 검토하고 있습니다", State: initialState}
	}
	w.reviewRequestGeneration++
	requestGeneration := w.reviewRequestGeneration
	w.pendingReviewGeneration = requestGeneration
	w.pendingReviewRequestID = request.RequestID
	w.reviewMu.Unlock()

	w.syncMu.Lock()
	defer w.syncMu.Unlock()
	if err := w.refreshLocked(w.ctx, w.dir); err != nil {
		w.clearPendingReview(requestGeneration)
		return ReviewState{}, err
	}

	w.reviewMu.Lock()
	state := w.reviewStateLocked()
	if w.pendingReviewGeneration != requestGeneration {
		w.reviewMu.Unlock()
		return state, &ReviewRequestError{Code: "review_cancelled", Message: "AI 검토 요청이 취소되었습니다", State: state}
	}
	if !w.isActive() {
		w.pendingReviewGeneration = 0
		w.pendingReviewRequestID = ""
		state = w.reviewStateLocked()
		w.reviewMu.Unlock()
		return state, ErrWatcherInactive
	}
	if request.TestContext.Present() {
		w.mu.RLock()
		currentTestInputHash := computeAutoTestInputHash(w.snapshots)
		w.mu.RUnlock()
		if strings.TrimSpace(request.TestContext.InputHash) == "" || request.TestContext.InputHash != currentTestInputHash {
			w.pendingReviewGeneration = 0
			w.pendingReviewRequestID = ""
			state = w.reviewStateLocked()
			w.reviewMu.Unlock()
			return state, &ReviewRequestError{Code: "review_test_context_stale", Message: "테스트 결과가 현재 코드 또는 테스트 입력과 일치하지 않습니다", State: state}
		}
	}
	if ai.Global == nil {
		w.pendingReviewGeneration = 0
		w.pendingReviewRequestID = ""
		state = w.reviewStateLocked()
		w.reviewMu.Unlock()
		return state, &ReviewRequestError{Code: "review_ai_unavailable", Message: "AI가 초기화되지 않았습니다", State: state}
	}
	if w.reviewRunning != nil {
		w.pendingReviewGeneration = 0
		w.pendingReviewRequestID = ""
		state = w.reviewStateLocked()
		w.reviewMu.Unlock()
		return state, &ReviewRequestError{Code: "review_in_progress", Message: "이미 최신 코드를 검토하고 있습니다", State: state}
	}
	if request.Revision != nil && *request.Revision != w.revision || request.SemanticHash != "" && request.SemanticHash != w.semanticHash {
		w.pendingReviewGeneration = 0
		w.pendingReviewRequestID = ""
		state = w.reviewStateLocked()
		w.reviewMu.Unlock()
		return state, &ReviewRequestError{Code: "review_stale", Message: "코드가 변경되었습니다. 최신 revision으로 다시 요청하세요", State: state}
	}
	contextHash := testContextFingerprint(w.semanticHash, request.TestContext)
	candidate := w.reviewCandidate
	contextOnly := candidate != nil && candidate.ContextOnly
	if candidate == nil {
		if contextHash == "" {
			w.pendingReviewGeneration = 0
			w.pendingReviewRequestID = ""
			state = w.reviewStateLocked()
			w.reviewMu.Unlock()
			return state, &ReviewRequestError{Code: "review_not_ready", Message: "검토할 의미 있는 코드 변경이 없습니다", State: state}
		}
		if contextHash == w.lastReviewedTestContextHash {
			w.pendingReviewGeneration = 0
			w.pendingReviewRequestID = ""
			state = w.reviewStateLocked()
			w.reviewMu.Unlock()
			return state, &ReviewRequestError{Code: "review_duplicate", Message: "같은 테스트 결과는 이미 검토했습니다", State: state}
		}
		w.mu.RLock()
		current := cloneSnapshots(w.snapshots)
		w.mu.RUnlock()
		candidate = buildReviewCandidate(w.dir, w.sessionID, w.revision, w.semanticHash, current, current)
		candidate.ContextOnly = true
		w.reviewCandidate = candidate
		contextOnly = true
	}
	state = w.reviewStateLocked()
	if contextOnly && contextHash == "" {
		w.pendingReviewGeneration = 0
		w.pendingReviewRequestID = ""
		state = w.reviewStateLocked()
		w.reviewMu.Unlock()
		return state, &ReviewRequestError{Code: "review_not_ready", Message: "테스트 결과를 함께 보내야 다시 검토할 수 있습니다", State: state}
	}

	now := time.Now()
	if w.now != nil {
		now = w.now()
	}
	retryAfter := w.retryAfterLocked(now)
	if retryAfter > 0 {
		w.pendingReviewGeneration = 0
		w.pendingReviewRequestID = ""
		state = w.reviewStateLocked()
		w.reviewMu.Unlock()
		return state, &ReviewRequestError{
			Code:       "review_rate_limited",
			Message:    "AI 검토 요청이 너무 잦습니다",
			RetryAfter: retryAfter,
			State:      state,
		}
	}

	runCandidate := *candidate
	runCandidate.Files = append([]string(nil), candidate.Files...)
	runCandidate.Snapshots = cloneSnapshots(candidate.Snapshots)
	runCandidate.ReviewState.Status = "reviewing"
	runCandidate.ReviewState.RequestID = request.RequestID
	runCtx, cancel := context.WithTimeout(w.ctx, feedbackTimeout)
	hasTestEvidence := strings.TrimSpace(runCandidate.AutoTestOutput) != "" || request.TestContext.Present()
	testInputHash := ""
	if hasTestEvidence {
		w.mu.RLock()
		testInputHash = computeAutoTestInputHash(w.snapshots)
		w.mu.RUnlock()
	}
	run := &reviewRun{
		candidate:       runCandidate,
		cancel:          cancel,
		client:          ai.Global,
		stream:          streamFeedback,
		tutorContent:    project.Global.GetContent(),
		skillLevel:      project.Global.GetSkillLevel(),
		testContextHash: contextHash,
		contextOnly:     contextOnly,
		testInputHash:   testInputHash,
		hasTestEvidence: hasTestEvidence,
	}
	w.reviewRunning = run
	w.pendingReviewGeneration = 0
	w.pendingReviewRequestID = ""
	w.reviewStarts = append(w.reviewStarts, now)
	state = runCandidate.ReviewState
	w.reviewMu.Unlock()

	w.emitIfActive(w.ctx, func() {
		broadcastReviewEvent("review_started", state, "", "")
		broadcastFeedbackStart(state)
	})
	go w.executeReview(runCtx, run, request.TestContext)
	return state, nil
}

func (w *Watcher) clearPendingReview(generation uint64) ReviewState {
	w.reviewMu.Lock()
	if w.pendingReviewGeneration == generation {
		w.pendingReviewGeneration = 0
		w.pendingReviewRequestID = ""
	}
	state := w.reviewStateLocked()
	w.reviewMu.Unlock()
	return state
}

func (w *Watcher) retryAfterLocked(now time.Time) time.Duration {
	cutoff := now.Add(-reviewWindow)
	kept := w.reviewStarts[:0]
	for _, started := range w.reviewStarts {
		if started.After(cutoff) {
			kept = append(kept, started)
		}
	}
	w.reviewStarts = kept
	var allowedAt time.Time
	if len(kept) > 0 {
		allowedAt = kept[len(kept)-1].Add(reviewCooldown)
	}
	if len(kept) >= reviewMaxBurst {
		windowAllowed := kept[0].Add(reviewWindow)
		if windowAllowed.After(allowedAt) {
			allowedAt = windowAllowed
		}
	}
	if allowedAt.After(now) {
		return allowedAt.Sub(now)
	}
	return 0
}

func (w *Watcher) executeReview(ctx context.Context, run *reviewRun, testContext TestContext) {
	defer run.cancel()
	testOutput := joinTestContext(run.candidate.AutoTestOutput, testContext)
	history := GetFeedbackHistory()
	var response strings.Builder
	err := run.stream(
		run.client,
		ctx,
		run.tutorContent,
		run.candidate.DiffContent,
		run.candidate.ChangedCode,
		testOutput,
		history,
		run.skillLevel,
		func(chunk string) {
			w.reviewMu.Lock()
			current := w.reviewRunning == run
			w.reviewMu.Unlock()
			if current && ctx.Err() == nil {
				response.WriteString(chunk)
			}
		},
	)

	w.reviewMu.Lock()
	if w.reviewRunning != run {
		w.reviewMu.Unlock()
		return
	}
	if err != nil {
		w.reviewRunning = nil
		state := w.reviewStateLocked()
		state.RequestID = run.candidate.RequestID
		w.reviewMu.Unlock()
		if ctx.Err() == nil || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			w.emitIfActive(w.ctx, func() {
				broadcastFeedbackEnd(run.candidate.ReviewState)
				broadcastReviewEvent("review_error", state, "", err.Error())
			})
		}
		return
	}

	text := stripStepCompleteMarker(response.String())
	if strings.TrimSpace(text) == "" {
		w.reviewRunning = nil
		state := w.reviewStateLocked()
		state.RequestID = run.candidate.RequestID
		w.reviewMu.Unlock()
		const message = "AI가 빈 피드백을 반환했습니다"
		w.emitIfActive(w.ctx, func() {
			broadcastFeedbackEnd(run.candidate.ReviewState)
			broadcastReviewEvent("review_error", state, "", message)
		})
		return
	}
	// Reconcile disk synchronously before committing feedback. fsnotify only
	// marks a pending mutation and normally waits for the debounce ticker; a
	// fast model response must not commit an older revision in that window.
	w.reviewMu.Unlock()
	w.syncMu.Lock()
	if refreshErr := w.refreshLocked(w.ctx, w.dir); refreshErr != nil {
		w.reviewMu.Lock()
		if w.reviewRunning != run {
			w.reviewMu.Unlock()
			w.syncMu.Unlock()
			return
		}
		w.reviewRunning = nil
		state := w.reviewStateLocked()
		state.RequestID = run.candidate.RequestID
		w.reviewMu.Unlock()
		w.syncMu.Unlock()
		w.emitIfActive(w.ctx, func() {
			broadcastFeedbackEnd(run.candidate.ReviewState)
			broadcastReviewEvent("review_error", state, "", refreshErr.Error())
		})
		return
	}
	w.reviewMu.Lock()
	if w.reviewRunning != run {
		w.reviewMu.Unlock()
		w.syncMu.Unlock()
		return
	}
	w.mu.RLock()
	latestSnapshots := cloneSnapshots(w.snapshots)
	w.mu.RUnlock()
	latestSemanticHash := computeSemanticCombinedHash(latestSnapshots)
	if latestSemanticHash != run.candidate.SemanticHash {
		// A refresh may have published newer raw snapshots while waiting
		// for reviewMu. Reconcile only while this exact run and semantic hash
		// are still current. Calling the global transition after unlocking can
		// otherwise regress H3 back to a captured H2 and cancel a fresh run.
		candidate, state, cancelledState, hadReadyCandidate, reconciled := w.reconcileSemanticChangeForRunLocked(run, latestSnapshots, latestSemanticHash)
		w.reviewMu.Unlock()
		w.syncMu.Unlock()
		if !reconciled {
			return
		}
		w.emitIfActive(w.ctx, func() {
			broadcastReviewEvent("review_cancelled", cancelledState, "source_changed", "")
			broadcastFeedbackEnd(cancelledState)
		})
		if len(candidate.Files) > 0 {
			ready := candidate.ReviewState
			ready.Status = "ready"
			w.emitIfActive(w.ctx, func() { broadcastReviewEvent("review_ready", ready, "", "") })
		} else if hadReadyCandidate {
			idle := state
			idle.Status = "idle"
			w.emitIfActive(w.ctx, func() { broadcastReviewEvent("review_cancelled", idle, "reverted", "") })
		}
		return
	}
	if run.hasTestEvidence && computeAutoTestInputHash(latestSnapshots) != run.testInputHash {
		_, cancelledState, invalidated := w.invalidateTestEvidenceForRunLocked(run)
		w.reviewMu.Unlock()
		w.syncMu.Unlock()
		if !invalidated {
			return
		}
		w.emitIfActive(w.ctx, func() {
			broadcastReviewEvent("review_cancelled", cancelledState, "tests_changed", "")
			broadcastFeedbackEnd(cancelledState)
		})
		return
	}
	if !run.contextOnly {
		w.reviewBaseline = latestSnapshots
		w.reviewBaselineHash = run.candidate.SemanticHash
	}
	w.reviewCandidate = nil
	if run.testContextHash != "" {
		w.lastReviewedTestContextHash = run.testContextHash
	}
	completedAt := time.Now()
	if w.now != nil {
		completedAt = w.now()
	}
	w.lastFeedback = &CompletedFeedback{
		Revision:     run.candidate.Revision,
		SemanticHash: run.candidate.SemanticHash,
		Files:        append([]string(nil), run.candidate.Files...),
		SessionID:    run.candidate.SessionID,
		RequestID:    run.candidate.RequestID,
		Content:      clipUTF8WithMarker(text, maxStoredFeedbackBytes, "\n[feedback truncated]"),
		CompletedAt:  completedAt.UTC().Format(time.RFC3339Nano),
	}
	w.reviewRunning = nil
	state := w.reviewStateLocked()
	endState := run.candidate.ReviewState
	endState.Status = state.Status
	w.reviewMu.Unlock()
	w.syncMu.Unlock()

	w.emitIfActive(w.ctx, func() {
		if text != "" {
			broadcastFeedbackChunk(run.candidate.ReviewState, text)
			AddFeedback(text)
		}
		broadcastFeedbackEnd(run.candidate.ReviewState)
		broadcastReviewEvent("review_end", endState, "", "")
	})
}

func (w *Watcher) reconcileSemanticChangeForRunLocked(run *reviewRun, latestSnapshots map[string]FileSnapshot, latestSemanticHash string) (*reviewCandidate, ReviewState, ReviewState, bool, bool) {
	if w.reviewRunning != run || w.semanticHash != run.candidate.SemanticHash {
		return nil, w.reviewStateLocked(), ReviewState{}, false, false
	}
	w.revision++
	w.semanticHash = latestSemanticHash
	candidate := buildReviewCandidate(w.dir, w.sessionID, w.revision, latestSemanticHash, w.reviewBaseline, latestSnapshots)
	hadReadyCandidate := w.reviewCandidate != nil
	w.reviewRunning = nil
	if len(candidate.Files) == 0 {
		w.reviewCandidate = nil
		w.reviewBaseline = cloneSnapshots(latestSnapshots)
		w.reviewBaselineHash = latestSemanticHash
	} else {
		w.reviewCandidate = candidate
	}
	state := w.reviewStateLocked()
	cancelledState := run.candidate.ReviewState
	cancelledState.Status = state.Status
	return candidate, state, cancelledState, hadReadyCandidate, true
}

func (w *Watcher) invalidateTestEvidenceForRunLocked(run *reviewRun) (ReviewState, ReviewState, bool) {
	if w.reviewRunning != run {
		return w.reviewStateLocked(), ReviewState{}, false
	}
	w.reviewRunning = nil
	if w.reviewCandidate != nil {
		if w.reviewCandidate.ContextOnly {
			w.reviewCandidate = nil
		} else {
			w.reviewCandidate.AutoTestOutput = ""
		}
	}
	state := w.reviewStateLocked()
	cancelledState := run.candidate.ReviewState
	cancelledState.Status = state.Status
	return state, cancelledState, true
}

func (w *Watcher) invalidateTestEvidence(ctx context.Context) {
	w.reviewMu.Lock()
	if w.reviewCandidate != nil {
		if w.reviewCandidate.ContextOnly {
			w.reviewCandidate = nil
		} else {
			w.reviewCandidate.AutoTestOutput = ""
		}
	}
	var cancelled *reviewRun
	if w.reviewRunning != nil && w.reviewRunning.hasTestEvidence {
		cancelled = w.reviewRunning
		cancelled.cancel()
		w.reviewRunning = nil
	}
	state := w.reviewStateLocked()
	w.reviewMu.Unlock()
	if cancelled != nil {
		cancelledState := cancelled.candidate.ReviewState
		cancelledState.Status = state.Status
		w.emitIfActive(ctx, func() {
			broadcastReviewEvent("review_cancelled", cancelledState, "tests_changed", "")
			broadcastFeedbackEnd(cancelledState)
		})
	}
}

func (w *Watcher) CancelReview(reason string) (ReviewState, bool) {
	return w.CancelReviewRequest("", reason)
}

// CancelReviewRequest records a bounded request-id tombstone before looking
// for an active run. This closes the cancel-before-handler race: a delayed
// POST with the same id is rejected before refresh, AI start, or rate quota.
func (w *Watcher) CancelReviewRequest(requestID, reason string) (ReviewState, bool) {
	if w == nil {
		return ReviewState{Status: "idle", Files: []string{}}, false
	}
	requestID = strings.TrimSpace(requestID)
	if reason == "" {
		reason = "user"
	}
	w.reviewMu.Lock()
	if requestID != "" {
		w.rememberCancelledReviewRequestLocked(requestID)
		if (w.reviewRunning != nil && requestID != w.reviewRunning.candidate.RequestID) ||
			(w.pendingReviewGeneration != 0 && requestID != w.pendingReviewRequestID) {
			// The tombstone still prevents a delayed start for this id, but it
			// did not cancel the authoritative running/pending request.
			state := w.reviewStateLocked()
			w.reviewMu.Unlock()
			return state, false
		}
	}
	if w.reviewRunning == nil && w.pendingReviewGeneration != 0 && (requestID == "" || requestID == w.pendingReviewRequestID) {
		cancelledRequestID := w.pendingReviewRequestID
		w.pendingReviewGeneration = 0
		w.pendingReviewRequestID = ""
		w.reviewRequestGeneration++
		state := w.reviewStateLocked()
		state.RequestID = cancelledRequestID
		w.reviewMu.Unlock()
		w.emitIfActive(w.ctx, func() { broadcastReviewEvent("review_cancelled", state, reason, "") })
		return state, true
	}
	if w.reviewRunning == nil {
		state := w.reviewStateLocked()
		if requestID != "" {
			state.RequestID = requestID
		}
		w.reviewMu.Unlock()
		return state, requestID != ""
	}
	run := w.reviewRunning
	run.cancel()
	w.reviewRunning = nil
	state := w.reviewStateLocked()
	state.RequestID = run.candidate.RequestID
	w.reviewMu.Unlock()
	w.emitIfActive(w.ctx, func() {
		cancelled := run.candidate.ReviewState
		cancelled.Status = state.Status
		broadcastReviewEvent("review_cancelled", cancelled, reason, "")
		broadcastFeedbackEnd(cancelled)
	})
	return state, true
}

func (w *Watcher) rememberCancelledReviewRequestLocked(requestID string) {
	if w.cancelledReviewRequests == nil {
		w.cancelledReviewRequests = make(map[string]struct{})
	}
	if _, exists := w.cancelledReviewRequests[requestID]; exists {
		return
	}
	if len(w.cancelledReviewRequestOrder) >= maxCancelledReviewRequests {
		oldest := w.cancelledReviewRequestOrder[0]
		delete(w.cancelledReviewRequests, oldest)
		copy(w.cancelledReviewRequestOrder, w.cancelledReviewRequestOrder[1:])
		w.cancelledReviewRequestOrder = w.cancelledReviewRequestOrder[:len(w.cancelledReviewRequestOrder)-1]
	}
	w.cancelledReviewRequests[requestID] = struct{}{}
	w.cancelledReviewRequestOrder = append(w.cancelledReviewRequestOrder, requestID)
}

func joinTestContext(automatic string, explicit TestContext) string {
	parts := make([]string, 0, 2)
	if explicit.Present() {
		var out strings.Builder
		out.WriteString("[사용자가 전달한 테스트 컨텍스트]\n")
		if explicit.Passed != nil {
			fmt.Fprintf(&out, "passed: %t\n", *explicit.Passed)
		}
		if strings.TrimSpace(explicit.Summary) != "" {
			out.WriteString("summary: " + explicit.Summary + "\n")
		}
		if strings.TrimSpace(explicit.Output) != "" {
			out.WriteString("output:\n" + explicit.Output)
		}
		parts = append(parts, out.String())
	}
	if strings.TrimSpace(automatic) != "" {
		parts = append(parts, "[자동 테스트]\n"+automatic)
	}
	return clipReviewContext(strings.Join(parts, "\n\n"), maxReviewTestContextBytes)
}

func clipReviewContext(value string, limit int) string {
	const marker = "\n[테스트 출력 생략]"
	return clipUTF8WithMarker(value, limit, marker)
}

func clipUTF8WithMarker(value string, limit int, marker string) string {
	if len(value) <= limit {
		return value
	}
	end := limit - len(marker)
	if end < 0 {
		return ""
	}
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end] + marker
}

func testContextFingerprint(semanticHash string, testContext TestContext) string {
	if !testContext.Present() {
		return ""
	}
	passed := "unset"
	if testContext.Passed != nil {
		passed = fmt.Sprintf("%t", *testContext.Passed)
	}
	value := semanticHash + "\x00" + strings.TrimSpace(testContext.InputHash) + "\x00" + passed + "\x00" + strings.TrimSpace(testContext.Summary) + "\x00" + strings.TrimSpace(testContext.Output)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}
