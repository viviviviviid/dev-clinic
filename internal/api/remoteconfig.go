package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/coding-tutor/internal/ai"
	"github.com/coding-tutor/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
)

// AIProxy proxies AI requests from local coding-tutor servers to Gemini.
// Protected by Supabase JWT — same auth as all other API endpoints.
// POST /api/ai/proxy
// Authorization: Bearer <supabase_jwt>
// Body: {"system": "...", "prompt": "...", "stream": true/false}
func AIProxy(c *gin.Context) {
	if ai.Global == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not available"})
		return
	}
	tokenStr := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	token, err := jwt.Parse(tokenStr, middleware.JWTKeyFunc)
	if err != nil || !token.Valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req struct {
		System string `json:"system"`
		Prompt string `json:"prompt"`
		Stream bool   `json:"stream"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Stream {
		flusher, ok := c.Writer.(http.Flusher)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
			return
		}
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")

		err := ai.Global.RawStream(c.Request.Context(), req.System, req.Prompt, func(chunk string) {
			data, _ := json.Marshal(map[string]string{"text": chunk})
			fmt.Fprintf(c.Writer, "data: %s\n\n", data)
			flusher.Flush()
		})
		if err != nil {
			data, _ := json.Marshal(map[string]string{"error": err.Error()})
			fmt.Fprintf(c.Writer, "data: %s\n\n", data)
			flusher.Flush()
			return
		}
		fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	text, err := ai.Global.RawGenerate(c.Request.Context(), req.Prompt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"text": text})
}
