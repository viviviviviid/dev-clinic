package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/coding-tutor/internal/ai"
	"github.com/coding-tutor/internal/project"
	"github.com/gin-gonic/gin"
)

type ExplainReq struct {
	Question    string `json:"question"`
	WrongChoice string `json:"wrongChoice"`
	CorrectCode string `json:"correctCode"`
	MarkerType  string `json:"markerType"`
}

// ExplainWrongAnswer streams an AI explanation for a wrong quiz answer.
// POST /api/explain
func ExplainWrongAnswer(c *gin.Context) {
	var req ExplainReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if ai.Global == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI not initialized"})
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

	sendChunk := func(text string) {
		data, _ := json.Marshal(map[string]string{"text": text})
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		flusher.Flush()
	}

	skillLevel := "normal"
	if project.Global.IsLoaded() {
		skillLevel = project.Global.GetSkillLevel()
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	err := ai.Global.ExplainWrongAnswer(
		ctx,
		req.Question, req.WrongChoice, req.CorrectCode, req.MarkerType,
		skillLevel,
		func(chunk string) { sendChunk(chunk) },
	)
	if err != nil {
		sendChunk("설명을 불러오지 못했어요: " + err.Error())
	}

	fmt.Fprintf(c.Writer, "event: done\ndata: {}\n\n")
	flusher.Flush()
}
