package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coding-tutor/internal/ai"
	"github.com/coding-tutor/internal/config"
	"github.com/gin-gonic/gin"
)

func TestValidateDailyRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     ConfirmDailyReq
		wantErr bool
	}{
		{name: "valid and normalized", req: ConfirmDailyReq{Topic: "  Go 동시성  ", Slug: "  GoConcurrency  "}},
		{name: "empty topic", req: ConfirmDailyReq{Slug: "GoBasics"}, wantErr: true},
		{name: "path traversal", req: ConfirmDailyReq{Topic: "Go", Slug: "../../outside"}, wantErr: true},
		{name: "hyphenated slug", req: ConfirmDailyReq{Topic: "Go", Slug: "Go-Basics"}, wantErr: true},
		{name: "long topic", req: ConfirmDailyReq{Topic: strings.Repeat("가", 121), Slug: "GoBasics"}, wantErr: true},
		{name: "control character in topic", req: ConfirmDailyReq{Topic: "Go\nBasics", Slug: "GoBasics"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.req
			err := validateDailyRequest(&req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.name == "valid and normalized" && (req.Topic != "Go 동시성" || req.Slug != "GoConcurrency") {
				t.Fatalf("request was not normalized: %#v", req)
			}
		})
	}
}

func TestDailyProjectPreflightRejectsExistingDirectoryBeforeGeneration(t *testing.T) {
	original := *config.Global
	t.Cleanup(func() { *config.Global = original })
	config.Global.BaseDir = t.TempDir()

	const suffix = "260817-GoBasics"
	if err := ensureDailyProjectTargetAvailable(suffix); err != nil {
		t.Fatalf("missing target rejected: %v", err)
	}
	projectDir := filepath.Join(config.Global.BaseDir, suffix)
	if err := os.Mkdir(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "main.go"), []byte("learner work"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureDailyProjectTargetAvailable(suffix); !errors.Is(err, errDailyProjectExists) {
		t.Fatalf("existing target error = %v, want errDailyProjectExists", err)
	}
	data, err := os.ReadFile(filepath.Join(projectDir, "main.go"))
	if err != nil || string(data) != "learner work" {
		t.Fatalf("preflight changed existing work: %q, %v", data, err)
	}
}

func TestNewDailySetupTokenIsOpaqueAndUnique(t *testing.T) {
	first, err := newDailySetupToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newDailySetupToken()
	if err != nil {
		t.Fatal(err)
	}
	if !dailySetupTokenPattern.MatchString(first) || !dailySetupTokenPattern.MatchString(second) {
		t.Fatalf("invalid token format: %q %q", first, second)
	}
	if first == second {
		t.Fatal("setup tokens unexpectedly match")
	}
}

func TestGetDailyLoadsLobbyWithoutGeneratingTopics(t *testing.T) {
	oldGet := getDailyRecords
	defer func() { getDailyRecords = oldGet }()

	calls := 0
	getDailyRecords = func(path string, result interface{}) error {
		calls++
		missions, ok := result.(*[]DailyMission)
		if !ok {
			t.Fatalf("unexpected daily result target %T", result)
		}
		*missions = []DailyMission{{ID: "mission-1", UserID: "user-1", Topic: "Go basics"}}
		return nil
	}

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("user_id", "user-1")
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/daily", nil)

	GetDaily(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if calls != 1 {
		t.Fatalf("Supabase reads = %d, want exactly one lobby mission read", calls)
	}
	var response struct {
		Missions []DailyMission       `json:"missions"`
		Topics   []ai.TopicSuggestion `json:"topics"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Missions) != 1 || len(response.Topics) != 0 {
		t.Fatalf("response = %#v, want missions and no generated topics", response)
	}
}

func TestBuildRequiredNewbieQuizFile(t *testing.T) {
	quiz := map[string]ai.QuizItem{"main.go:hole:0": {
		Key: "main.go:hole:0", Filename: "main.go", MarkerType: "hole",
		MarkerIndex: 0, Question: "빈칸을 채우세요", Hints: []string{"개념", "구조", "API"},
	}}
	generationErr := errors.New("model failed")
	marshalErr := errors.New("marshal failed")

	tests := []struct {
		name        string
		quiz        map[string]ai.QuizItem
		generate    error
		marshal     func(interface{}) ([]byte, error)
		wantErr     bool
		wantPayload bool
	}{
		{
			name: "generation error", quiz: quiz, generate: generationErr,
			marshal: json.Marshal, wantErr: true,
		},
		{
			name: "empty quiz", quiz: nil,
			marshal: json.Marshal, wantErr: true,
		},
		{
			name: "marshal error", quiz: quiz,
			marshal: func(interface{}) ([]byte, error) { return nil, marshalErr }, wantErr: true,
		},
		{
			name: "valid quiz", quiz: quiz,
			marshal: json.Marshal, wantPayload: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := buildRequiredNewbieQuizFile(tt.quiz, tt.generate, tt.marshal)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantPayload {
				var decoded map[string]ai.QuizItem
				if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
					t.Fatalf("invalid quiz JSON: %v", err)
				}
				if len(decoded) != 1 || decoded["main.go:hole:0"].Key != quiz["main.go:hole:0"].Key {
					t.Fatalf("unexpected quiz payload: %#v", decoded)
				}
			}
		})
	}
}

func TestValidateFinalizeDailyRequest(t *testing.T) {
	now := time.Date(2026, time.August, 17, 0, 5, 0, 0, time.Local)
	const setupToken = "0123456789abcdef0123456789abcdef"
	tests := []struct {
		name     string
		req      FinalizeDailyReq
		wantDate string
		wantErr  bool
	}{
		{
			name:     "server suffix from before midnight remains valid",
			req:      FinalizeDailyReq{Topic: "Go 동시성", Slug: "GoConcurrency", DirSuffix: "260816-GoConcurrency", SetupToken: setupToken},
			wantDate: "2026-08-16",
		},
		{
			name:     "server suffix from today",
			req:      FinalizeDailyReq{Topic: "Go", Slug: "GoBasics", DirSuffix: "260817-GoBasics", SetupToken: setupToken},
			wantDate: "2026-08-17",
		},
		{
			name:    "slug mismatch",
			req:     FinalizeDailyReq{Topic: "Go", Slug: "GoBasics", DirSuffix: "260817-OtherSlug", SetupToken: setupToken},
			wantErr: true,
		},
		{
			name:     "older pending setup remains recoverable",
			req:      FinalizeDailyReq{Topic: "Go", Slug: "GoBasics", DirSuffix: "240229-GoBasics", SetupToken: setupToken},
			wantDate: "2024-02-29",
		},
		{
			name:    "future date",
			req:     FinalizeDailyReq{Topic: "Go", Slug: "GoBasics", DirSuffix: "260818-GoBasics", SetupToken: setupToken},
			wantErr: true,
		},
		{
			name:    "path traversal",
			req:     FinalizeDailyReq{Topic: "Go", Slug: "GoBasics", DirSuffix: "260817-../GoBasics", SetupToken: setupToken},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.req
			got, err := validateFinalizeDailyRequestAt(&req, now)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.wantDate {
				t.Fatalf("date = %q, want %q", got, tt.wantDate)
			}
		})
	}
}

func TestVerifyFinalizedProjectSetupRequiresRegularTutorFile(t *testing.T) {
	original := *config.Global
	t.Cleanup(func() { *config.Global = original })
	config.Global.BaseDir = t.TempDir()

	const setupToken = "0123456789abcdef0123456789abcdef"
	if err := verifyFinalizedProjectSetup("MissingProject", setupToken); err == nil {
		t.Fatal("missing project directory was accepted")
	}

	noTutorDir := filepath.Join(config.Global.BaseDir, "NoTutor")
	if err := os.MkdirAll(noTutorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalizedProjectSetup("NoTutor", setupToken); err == nil {
		t.Fatal("project without TUTORSYS.md was accepted")
	}

	tutorDir := filepath.Join(config.Global.BaseDir, "TutorDirectory", "TUTORSYS.md")
	if err := os.MkdirAll(tutorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalizedProjectSetup("TutorDirectory", setupToken); err == nil {
		t.Fatal("directory named TUTORSYS.md was accepted as a regular file")
	}

	validDir := filepath.Join(config.Global.BaseDir, "ValidProject")
	if err := os.MkdirAll(validDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(validDir, "TUTORSYS.md"), []byte("# tutor"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(validDir, ".clinic-setup-token"), []byte(setupToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalizedProjectSetup("ValidProject", setupToken); err != nil {
		t.Fatalf("valid setup rejected: %v", err)
	}
}

type dailyMissionStore struct {
	mu       sync.Mutex
	rows     []DailyMission
	requests int
}

type dailyRoundTripFunc func(*http.Request) (*http.Response, error)

func (f dailyRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonHTTPResponse(status int, value interface{}) *http.Response {
	var body bytes.Buffer
	if value != nil {
		_ = json.NewEncoder(&body).Encode(value)
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body.String())),
	}
}

func (s *dailyMissionStore) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path != "/rest/v1/daily_missions" {
		return jsonHTTPResponse(http.StatusNotFound, map[string]string{"error": "not found"}), nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests++
	switch r.Method {
	case http.MethodGet:
		userID := strings.TrimPrefix(r.URL.Query().Get("user_id"), "eq.")
		projectDir := strings.TrimPrefix(r.URL.Query().Get("project_dir"), "eq.")
		rows := make([]DailyMission, 0, len(s.rows))
		for _, row := range s.rows {
			if row.UserID == userID && row.ProjectDir == projectDir {
				rows = append(rows, row)
			}
		}
		return jsonHTTPResponse(http.StatusOK, rows), nil
	case http.MethodPost:
		var mission DailyMission
		if err := json.NewDecoder(r.Body).Decode(&mission); err != nil {
			return jsonHTTPResponse(http.StatusBadRequest, map[string]string{"error": err.Error()}), nil
		}
		for _, row := range s.rows {
			if row.UserID == mission.UserID && row.ProjectDir == mission.ProjectDir {
				return jsonHTTPResponse(http.StatusConflict, map[string]string{"message": "duplicate key"}), nil
			}
		}
		s.rows = append(s.rows, mission)
		return jsonHTTPResponse(http.StatusCreated, nil), nil
	default:
		return jsonHTTPResponse(http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"}), nil
	}
}

func (s *dailyMissionStore) snapshot() []DailyMission {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]DailyMission(nil), s.rows...)
}

func (s *dailyMissionStore) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

func TestConfirmDailyStreamRejectsExistingMissionBeforeAIGeneration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dirSuffix := time.Now().In(time.Local).Format("060102") + "-GoBasics"
	store := &dailyMissionStore{rows: []DailyMission{{
		UserID: "user-1", Topic: "Go", Slug: "GoBasics", ProjectDir: dirSuffix, Status: "active",
	}}}

	original := *config.Global
	t.Cleanup(func() { *config.Global = original })
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	http.DefaultTransport = dailyRoundTripFunc(store.RoundTrip)
	config.Global.BaseDir = t.TempDir()
	config.Global.Supabase.URL = "https://test.supabase.invalid"
	config.Global.Supabase.ServiceRoleKey = "test-service-role"

	body, err := json.Marshal(ConfirmDailyReq{Topic: "Go", Slug: "GoBasics"})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Set("user_id", "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/daily/confirm-stream", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	ConfirmDailyStream(c)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", response.Code, response.Body.String())
	}
	if calls := store.requestCount(); calls != 1 {
		t.Fatalf("database requests = %d, want one preflight read", calls)
	}
}

func finalizeRequest(t *testing.T, userID string, request FinalizeDailyReq) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Set("user_id", userID)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/daily/finalize", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	FinalizeDailyMission(c)
	return response
}

func TestFinalizeDailyMissionCreatesOnlyOnFinalizeAndIsIdempotent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &dailyMissionStore{}

	original := *config.Global
	t.Cleanup(func() { *config.Global = original })
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	http.DefaultTransport = dailyRoundTripFunc(store.RoundTrip)

	config.Global.BaseDir = t.TempDir()
	config.Global.Supabase.URL = "https://test.supabase.invalid"
	config.Global.Supabase.ServiceRoleKey = "test-service-role"

	if rows := store.snapshot(); len(rows) != 0 {
		t.Fatalf("mission existed before finalize: %#v", rows)
	}

	missionDate := time.Now().In(time.Local)
	request := FinalizeDailyReq{
		Topic:      "Go 동시성",
		Slug:       "GoConcurrency",
		DirSuffix:  missionDate.Format("060102") + "-GoConcurrency",
		SetupToken: "0123456789abcdef0123456789abcdef",
	}
	response := finalizeRequest(t, "user-1", request)
	if response.Code != http.StatusConflict {
		t.Fatalf("finalize without setup status = %d, want 409; body=%s", response.Code, response.Body.String())
	}
	if rows := store.snapshot(); len(rows) != 0 {
		t.Fatalf("finalize without setup inserted rows: %#v", rows)
	}
	if calls := store.requestCount(); calls != 0 {
		t.Fatalf("finalize without setup made %d database requests, want 0", calls)
	}

	projectDir := filepath.Join(config.Global.BaseDir, request.DirSuffix)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "TUTORSYS.md"), []byte("# tutor"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".clinic-setup-token"), []byte(request.SetupToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	response = finalizeRequest(t, "user-1", request)
	if response.Code != http.StatusOK {
		t.Fatalf("first finalize status = %d, body=%s", response.Code, response.Body.String())
	}
	rows := store.snapshot()
	if len(rows) != 1 || rows[0].UserID != "user-1" || rows[0].Date != missionDate.Format("2006-01-02") || rows[0].Status != "active" {
		t.Fatalf("unexpected finalized rows: %#v", rows)
	}

	response = finalizeRequest(t, "user-1", request)
	if response.Code != http.StatusOK {
		t.Fatalf("idempotent finalize status = %d, body=%s", response.Code, response.Body.String())
	}
	if rows := store.snapshot(); len(rows) != 1 {
		t.Fatalf("idempotent finalize inserted %d rows", len(rows))
	}

	store.mu.Lock()
	store.rows[0].Status = "completed"
	store.mu.Unlock()
	response = finalizeRequest(t, "user-1", request)
	if response.Code != http.StatusConflict {
		t.Fatalf("completed-row finalize status = %d, want 409; body=%s", response.Code, response.Body.String())
	}
	rows = store.snapshot()
	if len(rows) != 1 || rows[0].Status != "completed" {
		t.Fatalf("completed row was overwritten: %#v", rows)
	}

	duplicate := rows[0]
	duplicate.Status = "active"
	store.mu.Lock()
	store.rows = []DailyMission{duplicate, duplicate}
	store.mu.Unlock()
	response = finalizeRequest(t, "user-1", request)
	if response.Code != http.StatusConflict {
		t.Fatalf("duplicate-row finalize status = %d, want 409; body=%s", response.Code, response.Body.String())
	}
}

func TestWriteNurseReplyEventsPreservesLiteralTopicsMarker(t *testing.T) {
	message := "문장 안의 [TOPICS] 표시는 그대로 남아야 합니다."
	reply := ai.NurseReply{
		Message: message,
		Topics: []ai.TopicSuggestion{{
			Name: "배열 연습", Slug: "ArrayPractice", Difficulty: "하",
		}},
	}
	var output strings.Builder
	if err := writeNurseReplyEvents(&output, reply); err != nil {
		t.Fatal(err)
	}

	blocks := strings.Split(strings.TrimSpace(output.String()), "\n\n")
	if len(blocks) != 3 {
		t.Fatalf("SSE blocks = %d, want 3: %q", len(blocks), output.String())
	}
	if !strings.HasPrefix(blocks[0], "event: message\n") ||
		!strings.HasPrefix(blocks[1], "event: topics\n") ||
		!strings.HasPrefix(blocks[2], "event: done\n") {
		t.Fatalf("unexpected SSE event order: %q", output.String())
	}

	dataLine := strings.TrimPrefix(strings.SplitN(blocks[0], "\n", 2)[1], "data: ")
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(dataLine), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Text != message {
		t.Fatalf("message = %q, want %q", payload.Text, message)
	}
}
