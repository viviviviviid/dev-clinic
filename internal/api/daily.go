package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/coding-tutor/internal/ai"
	"github.com/coding-tutor/internal/config"
	"github.com/coding-tutor/internal/pathguard"
	"github.com/coding-tutor/internal/supabase"
	"github.com/gin-gonic/gin"
)

type DailyMission struct {
	ID         string    `json:"id,omitempty"`
	UserID     string    `json:"user_id"`
	Date       string    `json:"date"`
	Topic      string    `json:"topic"`
	Slug       string    `json:"slug"`
	ProjectDir string    `json:"project_dir"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
}

var getDailyRecords = supabase.Get

func GetDaily(c *gin.Context) {
	userID := c.GetString("user_id")
	today := time.Now().Format("2006-01-02")

	var missions []DailyMission
	err := getDailyRecords(c.Request.Context(),
		fmt.Sprintf("daily_missions?user_id=eq.%s&date=eq.%s&order=created_at.asc&select=*", supabase.FilterValue(userID), supabase.FilterValue(today)),
		&missions,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if missions == nil {
		missions = []DailyMission{}
	}

	// Topic generation is intentionally user initiated through nurse chat.
	// Loading the lobby must remain a read-only operation with no hidden AI cost.
	c.JSON(http.StatusOK, gin.H{"missions": missions, "topics": []ai.TopicSuggestion{}})
}

func GetDailyHistory(c *gin.Context) {
	userID := c.GetString("user_id")

	dateFilter := c.Query("date")
	var query string
	if dateFilter != "" {
		if _, err := time.Parse("2006-01-02", dateFilter); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "date must use YYYY-MM-DD"})
			return
		}
		query = fmt.Sprintf("daily_missions?user_id=eq.%s&date=eq.%s&order=created_at.asc&select=*", supabase.FilterValue(userID), supabase.FilterValue(dateFilter))
	} else {
		query = fmt.Sprintf("daily_missions?user_id=eq.%s&order=date.desc,created_at.asc&select=*", supabase.FilterValue(userID))
	}

	var missions []DailyMission
	if err := supabase.Get(c.Request.Context(), query, &missions); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, missions)
}

type ConfirmDailyReq struct {
	Topic string `json:"topic"`
	Slug  string `json:"slug"`
}

var (
	dailySlugPattern       = regexp.MustCompile(`^[A-Z][A-Za-z0-9]{0,63}$`)
	dailyDirSuffixPattern  = regexp.MustCompile(`^([0-9]{6})-([A-Z][A-Za-z0-9]{0,63})$`)
	dailySetupTokenPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
	errDailyProjectExists  = errors.New("daily project directory already exists")
)

func newDailySetupToken() (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}

func ensureDailyProjectTargetAvailable(dirSuffix string) error {
	projectDir, err := pathguard.ResolveChildNoSymlinks(config.Global.BaseDir, dirSuffix)
	if err != nil {
		return fmt.Errorf("resolve daily project directory: %w", err)
	}
	if _, err := os.Lstat(projectDir); err == nil {
		return errDailyProjectExists
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect daily project directory: %w", err)
	}
	return nil
}

func validateDailyRequest(req *ConfirmDailyReq) error {
	req.Topic = strings.TrimSpace(req.Topic)
	req.Slug = strings.TrimSpace(req.Slug)
	if req.Topic == "" || utf8.RuneCountInString(req.Topic) > 120 {
		return fmt.Errorf("topic must be between 1 and 120 characters")
	}
	if strings.IndexFunc(req.Topic, unicode.IsControl) >= 0 {
		return fmt.Errorf("topic must not contain control characters")
	}
	if !dailySlugPattern.MatchString(req.Slug) {
		return fmt.Errorf("slug must be a PascalCase identifier of at most 64 characters")
	}
	return nil
}

func buildRequiredNewbieQuizFile(
	quizData map[string]ai.QuizItem,
	generationErr error,
	marshalJSON func(interface{}) ([]byte, error),
) (string, error) {
	if generationErr != nil {
		return "", fmt.Errorf("generate quiz: %w", generationErr)
	}
	if len(quizData) == 0 {
		return "", fmt.Errorf("generated quiz is empty")
	}
	quizBytes, err := marshalJSON(quizData)
	if err != nil {
		return "", fmt.Errorf("encode quiz: %w", err)
	}
	return string(quizBytes), nil
}

func ConfirmDailyStream(c *gin.Context) {
	userID := c.GetString("user_id")

	var req ConfirmDailyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateDailyRequest(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	dirSuffix := time.Now().Format("060102") + "-" + req.Slug
	if err := ensureDailyProjectTargetAvailable(dirSuffix); err != nil {
		if errors.Is(err, errDailyProjectExists) {
			c.JSON(http.StatusConflict, gin.H{"error": "같은 이름의 오늘 미션이 이미 로컬에 있습니다. 기존 미션을 열거나 다른 주제를 선택해 주세요."})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "프로젝트 생성 경로를 확인하지 못했습니다"})
		return
	}
	existingMissions, err := getDailyMissionsByProjectDir(c.Request.Context(), userID, dirSuffix)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "기존 학습 기록을 확인하지 못해 AI 생성을 시작하지 않았습니다"})
		return
	}
	if len(existingMissions) > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "같은 이름의 오늘 미션 기록이 이미 있습니다. 기존 미션을 열거나 다른 주제를 선택해 주세요."})
		return
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	sendProgress := func(stage, msg string) {
		data, _ := json.Marshal(map[string]string{"stage": stage, "message": msg})
		fmt.Fprintf(c.Writer, "event: progress\ndata: %s\n\n", data)
		flusher.Flush()
	}
	sendError := func(msg string) {
		data, _ := json.Marshal(map[string]string{"error": msg})
		fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", data)
		flusher.Flush()
	}

	sendProgress("setup", "사용자 설정 불러오는 중...")

	var settings []UserSettings
	if err := supabase.Get(c.Request.Context(),
		fmt.Sprintf("user_settings?user_id=eq.%s&select=*", supabase.FilterValue(userID)),
		&settings,
	); err != nil || len(settings) == 0 {
		sendError("사용자 설정을 찾을 수 없습니다")
		return
	}
	s := settings[0]

	if ai.Global == nil {
		sendError("AI 클라이언트가 초기화되지 않았습니다")
		return
	}

	sendProgress("curriculum", "AI가 커리큘럼을 생성하고 있습니다...")
	curriculum, err := ai.Global.GenerateCurriculum(c.Request.Context(), s.Language, req.Topic, s.SkillLevel)
	if err != nil {
		sendError("커리큘럼 생성 실패: " + err.Error())
		return
	}

	sendProgress("code", "코드 파일을 생성하고 있습니다...")
	files, err := ai.Global.GenerateCodeFiles(c.Request.Context(), curriculum, nil)
	if err != nil {
		sendError("코드 생성 실패: " + err.Error())
		return
	}

	// Build all-files map (browser will write these via LOCAL /api/project/setup)
	allFiles := make(map[string]string, len(files)+1)
	allFiles["TUTORSYS.md"] = curriculum
	for k, v := range files {
		allFiles[k] = v
	}

	if s.SkillLevel == "newbie" {
		sendProgress("quiz", "퀴즈 데이터를 생성하고 있습니다...")
		quizData, generationErr := ai.Global.GenerateQuizData(c.Request.Context(), curriculum, files)
		quizFile, err := buildRequiredNewbieQuizFile(quizData, generationErr, json.Marshal)
		if err != nil {
			sendError("퀴즈 생성 실패: " + err.Error())
			return
		}
		allFiles["quiz.json"] = quizFile
	}

	// The browser must first persist these files through /api/project/setup. Only
	// then does it call /api/daily/finalize to create the active mission row.
	setupToken, err := newDailySetupToken()
	if err != nil {
		sendError("프로젝트 복구 토큰 생성 실패")
		return
	}
	doneData, _ := json.Marshal(map[string]interface{}{
		"dir_suffix":  dirSuffix,
		"setup_token": setupToken,
		"files":       allFiles,
		"curriculum":  curriculum,
		"skill_level": s.SkillLevel,
		"language":    s.Language,
	})
	fmt.Fprintf(c.Writer, "event: done\ndata: %s\n\n", doneData)
	flusher.Flush()
}

type FinalizeDailyReq struct {
	Topic      string `json:"topic"`
	Slug       string `json:"slug"`
	DirSuffix  string `json:"dir_suffix"`
	SetupToken string `json:"setup_token"`
}

func validateFinalizeDailyRequest(req *FinalizeDailyReq) (string, error) {
	return validateFinalizeDailyRequestAt(req, time.Now())
}

func validateFinalizeDailyRequestAt(req *FinalizeDailyReq, now time.Time) (string, error) {
	dailyReq := ConfirmDailyReq{Topic: req.Topic, Slug: req.Slug}
	if err := validateDailyRequest(&dailyReq); err != nil {
		return "", err
	}
	req.Topic = dailyReq.Topic
	req.Slug = dailyReq.Slug
	req.SetupToken = strings.TrimSpace(req.SetupToken)
	if !dailySetupTokenPattern.MatchString(req.SetupToken) {
		return "", fmt.Errorf("invalid setup_token")
	}

	if req.DirSuffix != strings.TrimSpace(req.DirSuffix) {
		return "", fmt.Errorf("dir_suffix must not contain surrounding whitespace")
	}
	matches := dailyDirSuffixPattern.FindStringSubmatch(req.DirSuffix)
	if len(matches) != 3 || matches[2] != req.Slug {
		return "", fmt.Errorf("dir_suffix must use YYMMDD-<slug> and match slug")
	}

	prefix := matches[1]
	localNow := now.In(time.Local)
	missionDate, err := time.ParseInLocation("20060102", "20"+prefix, time.Local)
	if err != nil || missionDate.Format("060102") != prefix {
		return "", fmt.Errorf("dir_suffix date is invalid")
	}
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.Local)
	if missionDate.After(today) {
		return "", fmt.Errorf("dir_suffix date must not be in the future")
	}
	// The opaque setup token and the on-disk token file prove that this is a
	// clinic-created project. Do not expire that proof: a user returning after
	// several days must be able to finalize without regenerating paid AI output.
	return missionDate.Format("2006-01-02"), nil
}

func getDailyMissionsByProjectDir(ctx context.Context, userID, dirSuffix string) ([]DailyMission, error) {
	var missions []DailyMission
	path := fmt.Sprintf(
		"daily_missions?user_id=eq.%s&project_dir=eq.%s&select=*",
		supabase.FilterValue(userID),
		supabase.FilterValue(dirSuffix),
	)
	if err := supabase.Get(ctx, path, &missions); err != nil {
		return nil, err
	}
	return missions, nil
}

func verifyFinalizedProjectSetup(dirSuffix, setupToken string) error {
	projectDir, err := pathguard.ResolveRelative(config.Global.BaseDir, dirSuffix)
	if err != nil {
		return fmt.Errorf("resolve project directory: %w", err)
	}
	projectInfo, err := os.Lstat(projectDir)
	if err != nil {
		return fmt.Errorf("stat project directory: %w", err)
	}
	if !projectInfo.IsDir() {
		return fmt.Errorf("project path is not a directory")
	}

	tutorPath := filepath.Join(projectDir, "TUTORSYS.md")
	tutorInfo, err := os.Lstat(tutorPath)
	if err != nil {
		return fmt.Errorf("stat TUTORSYS.md: %w", err)
	}
	if !tutorInfo.Mode().IsRegular() {
		return fmt.Errorf("TUTORSYS.md is not a regular file")
	}
	tokenPath := filepath.Join(projectDir, ".clinic-setup-token")
	tokenInfo, err := os.Lstat(tokenPath)
	if err != nil {
		return fmt.Errorf("stat setup token: %w", err)
	}
	if !tokenInfo.Mode().IsRegular() {
		return fmt.Errorf("setup token is not a regular file")
	}
	tokenData, err := os.ReadFile(tokenPath)
	if err != nil || strings.TrimSpace(string(tokenData)) != setupToken {
		return fmt.Errorf("setup token does not match")
	}
	return nil
}

func finalizedMissionState(missions []DailyMission, req FinalizeDailyReq, missionDate string) (idempotent, conflict bool) {
	if len(missions) == 0 {
		return false, false
	}
	if len(missions) != 1 {
		return false, true
	}
	mission := missions[0]
	if mission.Status != "active" ||
		mission.Date != missionDate ||
		mission.Topic != req.Topic ||
		mission.Slug != req.Slug ||
		mission.ProjectDir != req.DirSuffix {
		return false, true
	}
	return true, false
}

// FinalizeDailyMission records a mission only after local project setup has
// succeeded. A unique (user_id, project_dir) index makes concurrent retries
// safe; regular INSERT is intentional so completed rows are never reactivated.
func FinalizeDailyMission(c *gin.Context) {
	userID := strings.TrimSpace(c.GetString("user_id"))
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authenticated user required"})
		return
	}

	var req FinalizeDailyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid finalize request"})
		return
	}
	missionDate, err := validateFinalizeDailyRequest(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := verifyFinalizedProjectSetup(req.DirSuffix, req.SetupToken); err != nil {
		log.Printf("daily finalize: local setup is incomplete: %v", err)
		c.JSON(http.StatusConflict, gin.H{"error": "로컬 프로젝트 설정이 완료되지 않았습니다"})
		return
	}
	existing, err := getDailyMissionsByProjectDir(c.Request.Context(), userID, req.DirSuffix)
	if err != nil {
		log.Printf("daily finalize: lookup failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "학습 기록을 확인하지 못했습니다"})
		return
	}
	if idempotent, conflict := finalizedMissionState(existing, req, missionDate); idempotent {
		c.JSON(http.StatusOK, gin.H{"ok": true, "created": false})
		return
	} else if conflict {
		c.JSON(http.StatusConflict, gin.H{"error": "같은 프로젝트 경로의 학습 기록이 이미 존재합니다"})
		return
	}
	mission := DailyMission{
		UserID:     userID,
		Date:       missionDate,
		Topic:      req.Topic,
		Slug:       req.Slug,
		ProjectDir: req.DirSuffix,
		Status:     "active",
	}
	if err := supabase.Insert(c.Request.Context(), "daily_missions", mission); err == nil {
		c.JSON(http.StatusOK, gin.H{"ok": true, "created": true})
		return
	} else {
		// A concurrent identical finalize can win the unique-index race or the
		// response can be lost after commit. Re-read once before reporting failure.
		log.Printf("daily finalize: insert failed, checking retry state: %v", err)
		existing, lookupErr := getDailyMissionsByProjectDir(c.Request.Context(), userID, req.DirSuffix)
		if lookupErr == nil {
			if idempotent, conflict := finalizedMissionState(existing, req, missionDate); idempotent {
				c.JSON(http.StatusOK, gin.H{"ok": true, "created": false})
				return
			} else if conflict {
				c.JSON(http.StatusConflict, gin.H{"error": "같은 프로젝트 경로의 학습 기록이 이미 존재합니다"})
				return
			}
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "학습 기록 확정에 실패했습니다"})
	}
}

type NurseChatReq struct {
	Message             string                `json:"message"`
	History             []ai.NurseChatMessage `json:"history"`
	PastTopics          []string              `json:"pastTopics"`
	RecommendationsOnly bool                  `json:"recommendationsOnly"`
}

func isNurseRecommendationRequest(req NurseChatReq) bool {
	if req.RecommendationsOnly {
		return true
	}
	// Backward compatibility for a deployed frontend that used this greeting
	// as the recommendation-button sentinel before the explicit intent field.
	return len(req.History) == 0 && req.Message == "안녕하세요! 오늘 어떤 훈련을 할까요?"
}

func writeSSEEvent(w io.Writer, event string, data interface{}) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
	return err
}

func writeNurseReplyEvents(w io.Writer, reply ai.NurseReply) error {
	if err := writeSSEEvent(w, "message", map[string]string{"text": reply.Message}); err != nil {
		return err
	}
	topics := reply.Topics
	if topics == nil {
		topics = []ai.TopicSuggestion{}
	}
	if err := writeSSEEvent(w, "topics", map[string][]ai.TopicSuggestion{"topics": topics}); err != nil {
		return err
	}
	return writeSSEEvent(w, "done", struct{}{})
}

// NurseChatHandler streams nurse chat responses.
// POST /api/daily/nurse-chat
func NurseChatHandler(c *gin.Context) {
	userID := c.GetString("user_id")

	var req NurseChatReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" || utf8.RuneCountInString(req.Message) > 4000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message must be between 1 and 4000 characters"})
		return
	}
	if len(req.History) > 20 {
		req.History = req.History[len(req.History)-20:]
	}
	if len(req.PastTopics) > 50 {
		req.PastTopics = req.PastTopics[:50]
	}

	if ai.Global == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI not initialized"})
		return
	}

	var settings []UserSettings
	if err := supabase.Get(c.Request.Context(),
		fmt.Sprintf("user_settings?user_id=eq.%s&select=*", supabase.FilterValue(userID)),
		&settings,
	); err != nil || len(settings) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user settings not found"})
		return
	}

	// If caller didn't supply past topics, fetch from DB
	if len(req.PastTopics) == 0 {
		var allHistory []DailyMission
		if err := supabase.Get(c.Request.Context(), fmt.Sprintf("daily_missions?user_id=eq.%s&order=created_at.desc&limit=100&select=topic", supabase.FilterValue(userID)), &allHistory); err != nil {
			log.Printf("daily: nurse-chat history fetch error: %v", err)
		}
		seen := map[string]bool{}
		for _, m := range allHistory {
			if !seen[m.Topic] {
				seen[m.Topic] = true
				req.PastTopics = append(req.PastTopics, m.Topic)
				if len(req.PastTopics) == 30 {
					break
				}
			}
		}
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	var reply ai.NurseReply
	var err error
	if isNurseRecommendationRequest(req) {
		var topics []ai.TopicSuggestion
		topics, err = ai.Global.GenerateDailyTopics(
			c.Request.Context(), settings[0].Language, settings[0].SkillLevel, req.PastTopics,
		)
		if err == nil {
			reply = ai.NurseReply{
				Message: "바로 시작할 수 있는 주제 3개를 준비했어요. 난이도를 보고 하나를 골라 주세요.",
				Topics:  topics,
			}
		}
	} else {
		reply, err = ai.Global.GenerateNurseReply(
			c.Request.Context(),
			req.Message, req.History, req.PastTopics,
			settings[0].Language, settings[0].SkillLevel,
		)
	}
	if err != nil {
		log.Printf("daily: nurse reply failed: %v", err)
		reply = ai.NurseReply{Message: "죄송해요, 지금은 대화가 어려워요. 잠시 후 다시 시도해 주세요.", Topics: []ai.TopicSuggestion{}}
	}
	if err := writeNurseReplyEvents(c.Writer, reply); err != nil {
		log.Printf("daily: nurse SSE write failed: %v", err)
		return
	}
	flusher.Flush()
}
