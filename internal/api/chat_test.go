package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestChatRejectsInvalidAnswerBeforeStartingStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/chat", Chat)
	request := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(`{"message":"answer","fileContent":"code","answerRequest":{"markerType":"bug","question":"fix","reference":{"path":"main.go","startLine":5,"endLine":8,"code":"other"}}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("invalid answer response: %d %s", response.Code, response.Body.String())
	}
}
