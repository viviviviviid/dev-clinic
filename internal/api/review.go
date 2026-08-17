package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/coding-tutor/internal/config"
	"github.com/coding-tutor/internal/pathguard"
	"github.com/coding-tutor/internal/project"
	"github.com/coding-tutor/internal/watcher"
	"github.com/gin-gonic/gin"
)

const (
	maxReviewRequestBytes = 80 << 10
	maxReviewCancelBytes  = 4 << 10
	maxTestSummaryBytes   = 8 << 10
	maxTestOutputBytes    = 64 << 10
)

type reviewRequestBody struct {
	RequestID    string               `json:"request_id,omitempty"`
	ProjectDir   string               `json:"project_dir,omitempty"`
	SessionID    *uint64              `json:"session_id,omitempty"`
	Revision     *uint64              `json:"revision,omitempty"`
	SemanticHash string               `json:"semantic_hash,omitempty"`
	TestContext  *watcher.TestContext `json:"test_context,omitempty"`
}

// GetReviewStatus returns the current project's resumable review state.
// GET /api/review/status
func GetReviewStatus(c *gin.Context) {
	w, ok := currentProjectWatcher(c)
	if !ok {
		return
	}
	state := w.GetReviewStatus()
	if c.Query("refresh") == "1" {
		if err := w.Refresh(); err != nil {
			writeReviewError(c, w.GetReviewStatus(), err)
			return
		}
		state = w.GetReviewStatus()
		state.DiskSynced = true
		state.LastSync = time.Now().UTC().Format(time.RFC3339Nano)
	}
	writeReviewStatus(c, state)
}

func writeReviewStatus(c *gin.Context, state watcher.ReviewState) {
	c.JSON(http.StatusOK, state)
}

// StartReview refreshes the latest autosaved files and starts exactly one
// review for the latest semantic revision.
// POST /api/review
func StartReview(c *gin.Context) {
	w, ok := currentProjectWatcher(c)
	if !ok {
		return
	}
	body, err := decodeReviewRequest(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "review_invalid_request", "error": err.Error()})
		return
	}
	if err := validateReviewRequest(body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "review_invalid_request", "error": err.Error()})
		return
	}
	if !validateReviewTarget(c, w, body.ProjectDir, body.SessionID, body.RequestID) {
		return
	}
	testContext := watcher.TestContext{}
	if body.TestContext != nil {
		testContext = *body.TestContext
	}
	state, err := w.RequestReview(watcher.ReviewRequest{
		RequestID:    strings.TrimSpace(body.RequestID),
		Revision:     body.Revision,
		SemanticHash: strings.TrimSpace(body.SemanticHash),
		TestContext:  testContext,
	})
	if err != nil {
		writeReviewErrorForRequest(c, state, err, strings.TrimSpace(body.RequestID))
		return
	}
	c.JSON(http.StatusAccepted, state)
}

// CancelReview cancels only the current project's active AI review. The
// semantic candidate remains ready so the user can explicitly retry it.
// POST /api/review/cancel
func CancelReview(c *gin.Context) {
	w, ok := currentProjectWatcher(c)
	if !ok {
		return
	}
	body, err := decodeReviewCancelRequest(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "review_invalid_request", "error": err.Error()})
		return
	}
	if !validateReviewTarget(c, w, body.ProjectDir, body.SessionID, body.RequestID) {
		return
	}
	state, cancelled := w.CancelReviewRequest(strings.TrimSpace(body.RequestID), "user")
	c.JSON(http.StatusOK, gin.H{
		"status":        state.Status,
		"cancelled":     cancelled,
		"reason":        "user",
		"project_dir":   state.ProjectDir,
		"revision":      state.Revision,
		"semantic_hash": state.SemanticHash,
		"files":         state.Files,
		"session_id":    state.SessionID,
		"request_id":    state.RequestID,
	})
}

type reviewCancelRequestBody struct {
	RequestID  string  `json:"request_id,omitempty"`
	ProjectDir string  `json:"project_dir,omitempty"`
	SessionID  *uint64 `json:"session_id,omitempty"`
}

func decodeReviewCancelRequest(reader io.Reader) (reviewCancelRequestBody, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxReviewCancelBytes+1))
	if err != nil {
		return reviewCancelRequestBody{}, errors.New("요청 본문을 읽을 수 없습니다")
	}
	if len(data) > maxReviewCancelBytes {
		return reviewCancelRequestBody{}, errors.New("요청 본문이 너무 큽니다")
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return reviewCancelRequestBody{}, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var body reviewCancelRequestBody
	if err := decoder.Decode(&body); err != nil {
		return reviewCancelRequestBody{}, errors.New("유효한 JSON 요청이 아닙니다")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return reviewCancelRequestBody{}, errors.New("JSON 값은 하나만 보낼 수 있습니다")
	}
	if err := validateReviewRequestID(body.RequestID); err != nil {
		return reviewCancelRequestBody{}, err
	}
	if strings.TrimSpace(body.RequestID) == "" {
		return reviewCancelRequestBody{}, errors.New("request_id가 필요합니다")
	}
	if strings.TrimSpace(body.ProjectDir) == "" {
		return reviewCancelRequestBody{}, errors.New("project_dir가 필요합니다")
	}
	if body.SessionID == nil || *body.SessionID == 0 {
		return reviewCancelRequestBody{}, errors.New("session_id가 필요합니다")
	}
	if len(body.ProjectDir) > 1024 {
		return reviewCancelRequestBody{}, errors.New("project_dir가 너무 깁니다")
	}
	return body, nil
}

func validateReviewTarget(c *gin.Context, w *watcher.Watcher, expectedProjectDir string, expectedSessionID *uint64, requestID string) bool {
	state := w.GetReviewStatus()
	matches := true
	if strings.TrimSpace(expectedProjectDir) != "" {
		expected, err := pathguard.Resolve(config.Global.BaseDir, expectedProjectDir)
		actual, actualErr := pathguard.Resolve(config.Global.BaseDir, state.ProjectDir)
		matches = err == nil && actualErr == nil && filepath.Clean(expected) == filepath.Clean(actual)
	}
	if expectedSessionID != nil && *expectedSessionID != state.SessionID {
		matches = false
	}
	if matches {
		return true
	}
	writeReviewErrorForRequest(c, state, &watcher.ReviewRequestError{
		Code:    "review_session_stale",
		Message: "프로젝트 검토 세션이 변경되었습니다. 최신 상태를 다시 불러오세요",
		State:   state,
	}, strings.TrimSpace(requestID))
	return false
}

func currentProjectWatcher(c *gin.Context) (*watcher.Watcher, bool) {
	status := project.Global.GetStatus()
	if !status.Loaded {
		c.JSON(http.StatusBadRequest, gin.H{"code": "project_not_loaded", "error": "프로젝트가 로드되지 않았습니다"})
		return nil, false
	}
	projectDir, err := pathguard.Resolve(config.Global.BaseDir, status.Dir)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"code": "project_outside_base", "error": "프로젝트가 기본 디렉터리 밖에 있습니다"})
		return nil, false
	}
	w := watcher.Global
	if w == nil || !w.IsActive() {
		c.JSON(http.StatusConflict, gin.H{"code": "review_watcher_inactive", "error": "프로젝트 watcher가 실행 중이 아닙니다"})
		return nil, false
	}
	watcherState := w.GetReviewStatus()
	watcherDir, err := pathguard.Resolve(config.Global.BaseDir, watcherState.ProjectDir)
	if err != nil || filepath.Clean(watcherDir) != filepath.Clean(projectDir) {
		c.JSON(http.StatusConflict, gin.H{"code": "review_project_mismatch", "error": "현재 프로젝트 watcher와 로드된 프로젝트가 일치하지 않습니다"})
		return nil, false
	}
	return w, true
}

func decodeReviewRequest(reader io.Reader) (reviewRequestBody, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxReviewRequestBytes+1))
	if err != nil {
		return reviewRequestBody{}, errors.New("요청 본문을 읽을 수 없습니다")
	}
	if len(data) > maxReviewRequestBytes {
		return reviewRequestBody{}, errors.New("요청 본문이 너무 큽니다")
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return reviewRequestBody{}, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var body reviewRequestBody
	if err := decoder.Decode(&body); err != nil {
		return reviewRequestBody{}, errors.New("유효한 JSON 요청이 아닙니다")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return reviewRequestBody{}, errors.New("JSON 값은 하나만 보낼 수 있습니다")
	}
	return body, nil
}

func validateReviewRequest(body reviewRequestBody) error {
	if err := validateReviewRequestID(body.RequestID); err != nil {
		return err
	}
	if strings.TrimSpace(body.RequestID) == "" {
		return errors.New("request_id가 필요합니다")
	}
	if strings.TrimSpace(body.ProjectDir) == "" {
		return errors.New("project_dir가 필요합니다")
	}
	if body.SessionID == nil || *body.SessionID == 0 {
		return errors.New("session_id가 필요합니다")
	}
	if len(body.SemanticHash) > 128 {
		return errors.New("semantic_hash가 너무 깁니다")
	}
	if len(body.ProjectDir) > 1024 {
		return errors.New("project_dir가 너무 깁니다")
	}
	if body.TestContext == nil {
		return nil
	}
	if len(body.TestContext.Summary) > maxTestSummaryBytes {
		return errors.New("test_context.summary가 너무 깁니다")
	}
	if len(body.TestContext.Output) > maxTestOutputBytes {
		return errors.New("test_context.output이 너무 깁니다")
	}
	if len(body.TestContext.InputHash) > 128 {
		return errors.New("test_context.input_hash가 너무 깁니다")
	}
	if body.TestContext.Present() && strings.TrimSpace(body.TestContext.InputHash) == "" {
		return errors.New("test_context.input_hash가 필요합니다")
	}
	return nil
}

func validateReviewRequestID(requestID string) error {
	if len(requestID) > 128 {
		return errors.New("request_id가 너무 깁니다")
	}
	if requestID != "" && strings.TrimSpace(requestID) != requestID {
		return errors.New("request_id 형식이 올바르지 않습니다")
	}
	return nil
}

func writeReviewError(c *gin.Context, state watcher.ReviewState, err error) {
	writeReviewErrorForRequest(c, state, err, "")
}

func writeReviewErrorForRequest(c *gin.Context, state watcher.ReviewState, err error, attemptedRequestID string) {
	status := http.StatusConflict
	code := "review_unavailable"
	message := err.Error()
	retrySeconds := 0
	var requestErr *watcher.ReviewRequestError
	if errors.As(err, &requestErr) {
		code = requestErr.Code
		message = requestErr.Message
		state = requestErr.State
		switch requestErr.Code {
		case "review_rate_limited":
			status = http.StatusTooManyRequests
			retrySeconds = int(math.Ceil(requestErr.RetryAfter.Seconds()))
			if retrySeconds < 1 {
				retrySeconds = 1
			}
			c.Header("Retry-After", fmt.Sprintf("%d", retrySeconds))
		case "review_ai_unavailable":
			status = http.StatusServiceUnavailable
		}
	} else if errors.Is(err, watcher.ErrWatcherInactive) {
		code = "review_watcher_inactive"
	} else if errors.Is(err, watcher.ErrProjectMismatch) {
		code = "review_project_mismatch"
	}
	body := gin.H{
		"code":                code,
		"error":               message,
		"status":              state.Status,
		"project_dir":         state.ProjectDir,
		"revision":            state.Revision,
		"semantic_hash":       state.SemanticHash,
		"files":               state.Files,
		"session_id":          state.SessionID,
		"request_id":          state.RequestID,
		"retry_after_seconds": retrySeconds,
	}
	if attemptedRequestID != "" {
		body["attempted_request_id"] = attemptedRequestID
	}
	c.JSON(status, body)
}
