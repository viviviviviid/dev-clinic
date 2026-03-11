package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/coding-tutor/internal/ai"
	"github.com/coding-tutor/internal/project"
	"github.com/coding-tutor/internal/watcher"
	"github.com/gin-gonic/gin"
)

type ChatReq struct {
	Message     string           `json:"message"`
	FileContent string           `json:"fileContent"`
	ChatHistory []ai.ChatMessage `json:"chatHistory"`
}

// Chat streams an AI response to a user's question.
// POST /api/chat
func Chat(c *gin.Context) {
	var req ChatReq
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

	tutorContent := ""
	if project.Global != nil {
		tutorContent = project.Global.GetContent()
	}
	feedbackHistory := watcher.GetFeedbackHistory()
	skillLevel := "normal"
	if project.Global != nil {
		skillLevel = project.Global.GetSkillLevel()
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	err := ai.Global.StreamChat(
		ctx,
		tutorContent, req.FileContent,
		feedbackHistory, req.ChatHistory,
		req.Message, skillLevel,
		func(chunk string) { sendChunk(chunk) },
	)
	if err != nil {
		sendChunk("응답을 불러오지 못했어요: " + err.Error())
	}

	fmt.Fprintf(c.Writer, "event: done\ndata: {}\n\n")
	flusher.Flush()
}
