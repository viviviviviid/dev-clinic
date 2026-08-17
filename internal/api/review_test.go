package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coding-tutor/internal/ai"
	"github.com/coding-tutor/internal/config"
	"github.com/coding-tutor/internal/project"
	"github.com/coding-tutor/internal/watcher"
	"github.com/gin-gonic/gin"
)

func setupReviewHandlerTest(t *testing.T) (http.Handler, string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	base := t.TempDir()
	dir := filepath.Join(base, "project")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nvar value = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldBase := config.Global.BaseDir
	oldProject := project.Global
	oldWatcher := watcher.Global
	oldAI := ai.Global
	config.Global.BaseDir = base
	project.Global = &project.Manager{}
	project.Global.Set(dir, "## 언어 & 환경\ngo\n\n## 학습 수준\nnormal\n")
	if err := watcher.Start(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if watcher.Global != nil {
			watcher.Global.Stop()
		}
		watcher.Global = oldWatcher
		project.Global = oldProject
		config.Global.BaseDir = oldBase
		ai.Global = oldAI
	})
	router := gin.New()
	router.GET("/review/status", GetReviewStatus)
	router.POST("/review", StartReview)
	router.POST("/review/cancel", CancelReview)
	return router, dir, path
}

func TestReviewStatusAndCancelUseAuthoritativeState(t *testing.T) {
	router, _, _ := setupReviewHandlerTest(t)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/review/status", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"idle"`) {
		t.Fatalf("status response = %d %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	state := watcher.Global.GetReviewStatus()
	cancelBody := mustJSON(t, map[string]any{"request_id": "cancel-idle", "project_dir": state.ProjectDir, "session_id": state.SessionID})
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/review/cancel", bytes.NewReader(cancelBody)))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"idle"`) || !strings.Contains(response.Body.String(), `"cancelled":true`) {
		t.Fatalf("cancel response = %d %s", response.Code, response.Body.String())
	}
}

func TestReviewCancelRequestIDTombstonesLateStart(t *testing.T) {
	router, _, path := setupReviewHandlerTest(t)
	state := watcher.Global.GetReviewStatus()
	if err := os.WriteFile(path, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cancelResponse := httptest.NewRecorder()
	target := map[string]any{"request_id": "request-before-handler", "project_dir": state.ProjectDir, "session_id": state.SessionID}
	cancelRequest := httptest.NewRequest(http.MethodPost, "/review/cancel", bytes.NewReader(mustJSON(t, target)))
	cancelRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(cancelResponse, cancelRequest)
	if cancelResponse.Code != http.StatusOK || !strings.Contains(cancelResponse.Body.String(), `"cancelled":true`) {
		t.Fatalf("early cancel response = %d %s", cancelResponse.Code, cancelResponse.Body.String())
	}

	startResponse := httptest.NewRecorder()
	startRequest := httptest.NewRequest(http.MethodPost, "/review", bytes.NewReader(mustJSON(t, target)))
	startRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(startResponse, startRequest)
	if startResponse.Code != http.StatusConflict || !strings.Contains(startResponse.Body.String(), `"code":"review_cancelled"`) {
		t.Fatalf("late start response = %d %s", startResponse.Code, startResponse.Body.String())
	}
}

func TestDelayedOldSessionReviewAndCancelCannotAffectNewWatcher(t *testing.T) {
	router, oldDir, _ := setupReviewHandlerTest(t)
	oldState := watcher.Global.GetReviewStatus()
	base := filepath.Dir(oldDir)
	newDir := filepath.Join(base, "project-new")
	if err := os.Mkdir(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(newDir, "main.go")
	if err := os.WriteFile(newPath, []byte("package main\nvar value = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	project.Global.Set(newDir, "## 언어 & 환경\ngo\n")
	if err := watcher.Start(newDir); err != nil {
		t.Fatal(err)
	}
	newState := watcher.Global.GetReviewStatus()
	if newState.SessionID == oldState.SessionID {
		t.Fatal("watcher restart reused its session id")
	}
	if err := os.WriteFile(newPath, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ai.Global = nil

	oldTarget := map[string]any{
		"request_id":  "delayed-old-request",
		"project_dir": oldState.ProjectDir,
		"session_id":  oldState.SessionID,
	}
	for _, endpoint := range []string{"/review", "/review/cancel"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(mustJSON(t, oldTarget)))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"review_session_stale"`) {
			t.Fatalf("delayed %s response = %d %s", endpoint, response.Code, response.Body.String())
		}
	}
	if state := watcher.Global.GetReviewStatus(); state.Revision != 0 || state.Status != "idle" {
		t.Fatalf("old-session requests refreshed or mutated new watcher: %#v", state)
	}

	currentTarget := map[string]any{
		"request_id":  "delayed-old-request",
		"project_dir": newState.ProjectDir,
		"session_id":  newState.SessionID,
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/review", bytes.NewReader(mustJSON(t, currentTarget)))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"code":"review_ai_unavailable"`) {
		t.Fatalf("new-session request inherited old cancel tombstone: %d %s", response.Code, response.Body.String())
	}
}

func TestReviewStatusSerializesLastFeedbackReplay(t *testing.T) {
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	writeReviewStatus(ctx, watcher.ReviewState{
		Status:       "idle",
		Revision:     4,
		SemanticHash: "hash",
		Files:        []string{},
		LastFeedback: &watcher.CompletedFeedback{
			Revision:     4,
			SemanticHash: "hash",
			Files:        []string{"main.go"},
			Content:      "saved feedback",
			CompletedAt:  "2026-08-17T12:00:00Z",
		},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	for _, field := range []string{`"last_feedback"`, `"content":"saved feedback"`, `"completed_at":"2026-08-17T12:00:00Z"`} {
		if !strings.Contains(response.Body.String(), field) {
			t.Errorf("replay field %s missing: %s", field, response.Body.String())
		}
	}
}

func TestReviewRequestRejectsUnknownPathsAndOversizedContext(t *testing.T) {
	router, _, _ := setupReviewHandlerTest(t)
	for _, body := range [][]byte{
		[]byte(`{"dir":"/tmp/other"}`),
		mustJSON(t, map[string]any{"test_context": map[string]string{"output": strings.Repeat("x", maxTestOutputBytes+1)}}),
		bytes.Repeat([]byte("x"), maxReviewRequestBytes+1),
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/review", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body bytes=%d status=%d body=%s", len(body), response.Code, response.Body.String())
		}
	}
}

func TestReviewRequestRequiresTestInputProvenance(t *testing.T) {
	router, _, _ := setupReviewHandlerTest(t)
	state := watcher.Global.GetReviewStatus()
	body := mustJSON(t, map[string]any{
		"request_id":  "missing-test-input",
		"project_dir": state.ProjectDir,
		"session_id":  state.SessionID,
		"test_context": map[string]any{
			"passed":  false,
			"summary": "old failure",
		},
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/review", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "input_hash") {
		t.Fatalf("missing provenance response = %d %s", response.Code, response.Body.String())
	}
}

func TestReviewRequestRefreshesDiskBeforeResponding(t *testing.T) {
	router, _, path := setupReviewHandlerTest(t)
	ai.Global = nil
	if err := os.WriteFile(path, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	state := watcher.Global.GetReviewStatus()
	body := mustJSON(t, map[string]any{"request_id": "refresh-latest", "project_dir": state.ProjectDir, "session_id": state.SessionID})
	request := httptest.NewRequest(http.MethodPost, "/review", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"revision":1`) || !strings.Contains(response.Body.String(), `"status":"ready"`) {
		t.Fatalf("POST did not synchronously refresh latest source: %s", response.Body.String())
	}
}

func TestReviewStatusRefreshReturnsAuthoritativeDiskAck(t *testing.T) {
	router, _, path := setupReviewHandlerTest(t)
	if err := os.WriteFile(path, []byte("package main\nvar value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/review/status?refresh=1", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	for _, field := range []string{`"disk_synced":true`, `"last_sync":`, `"status":"ready"`, `"revision":1`} {
		if !strings.Contains(response.Body.String(), field) {
			t.Errorf("refresh ack field %s missing: %s", field, response.Body.String())
		}
	}
}

func TestReviewRequestConfinesLoadedProjectToBase(t *testing.T) {
	router, _, _ := setupReviewHandlerTest(t)
	outside := t.TempDir()
	project.Global.Set(outside, "## 언어 & 환경\ngo\n")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/review/status", nil))
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "project_outside_base") {
		t.Fatalf("outside project response = %d %s", response.Code, response.Body.String())
	}
}

func TestReviewRateLimitResponseIncludesRetryMetadata(t *testing.T) {
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	state := watcher.ReviewState{Status: "ready", Revision: 3, SemanticHash: "hash", Files: []string{"main.go"}}
	writeReviewError(ctx, state, &watcher.ReviewRequestError{
		Code:       "review_rate_limited",
		Message:    "slow down",
		RetryAfter: 1200 * time.Millisecond,
		State:      state,
	})
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "2" {
		t.Fatalf("rate response = %d Retry-After=%q body=%s", response.Code, response.Header().Get("Retry-After"), response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"retry_after_seconds":2`) || !strings.Contains(response.Body.String(), `"code":"review_rate_limited"`) {
		t.Fatalf("rate metadata missing: %s", response.Body.String())
	}
}

func TestReviewInProgressErrorPreservesAuthoritativeRunningRequestID(t *testing.T) {
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	state := watcher.ReviewState{Status: "reviewing", RequestID: "running-request", SessionID: 4, Revision: 3, SemanticHash: "hash", Files: []string{"main.go"}}
	writeReviewErrorForRequest(ctx, state, &watcher.ReviewRequestError{
		Code:    "review_in_progress",
		Message: "already running",
		State:   state,
	}, "competing-request")
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	for _, field := range []string{`"request_id":"running-request"`, `"attempted_request_id":"competing-request"`} {
		if !strings.Contains(response.Body.String(), field) {
			t.Errorf("field %s missing from %s", field, response.Body.String())
		}
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
