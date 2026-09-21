package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coding-tutor/internal/config"
	"github.com/golang-jwt/jwt/v4"
	"github.com/gorilla/websocket"
)

func configureWSTest(t *testing.T) string {
	t.Helper()
	previous := *config.Global
	config.Global.Supabase.URL = "https://project.supabase.co"
	config.Global.Supabase.AnonKey = "sb_publishable_test"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := jwt.MapClaims{}
		token, err := jwt.ParseWithClaims(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), claims, func(token *jwt.Token) (interface{}, error) { return []byte("websocket-test-secret"), nil }, jwt.WithValidMethods([]string{"HS256"}))
		if err != nil || !token.Valid {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": claims["sub"]})
	}))
	t.Cleanup(server.Close)
	config.Global.Supabase.URL = server.URL
	t.Setenv("ALLOWED_USER_ID", "")
	t.Setenv("ALLOWED_USER_IDS", "")
	t.Setenv("ALLOWED_USER_EMAIL", "")
	t.Setenv("ALLOWED_USER_EMAILS", "")
	t.Setenv("ALLOWED_ORIGINS", "")
	t.Cleanup(func() { *config.Global = previous })

	claims := jwt.MapClaims{
		"iss":  config.Global.Supabase.URL + "/auth/v1",
		"aud":  "authenticated",
		"role": "authenticated",
		"sub":  "user-123",
		"exp":  time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte("websocket-test-secret"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func websocketRequest(origin, token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/ws", nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Sec-WebSocket-Protocol", applicationSubprotocol+", "+token)
	return req
}

func TestAuthenticateRequest(t *testing.T) {
	token := configureWSTest(t)

	userID, err := AuthenticateRequest(websocketRequest("https://tutor.abcfe.net", token))
	if err != nil {
		t.Fatalf("AuthenticateRequest() error = %v", err)
	}
	if userID != "user-123" {
		t.Fatalf("AuthenticateRequest() user = %q", userID)
	}
}

func TestDesktopWebSocketStillRequiresJWT(t *testing.T) {
	token := configureWSTest(t)
	t.Setenv("ALLOWED_ORIGINS", "wails://wails")
	if _, err := AuthenticateRequest(websocketRequest("wails://wails", token)); err != nil {
		t.Fatal(err)
	}
	for _, req := range []*http.Request{
		websocketRequest("wails://wails", "invalid"),
		websocketRequest("null", token),
		websocketRequest("wails://evil", token),
	} {
		if _, err := AuthenticateRequest(req); err == nil {
			t.Fatal("desktop request bypassed authentication/origin validation")
		}
	}
}

func TestServerCancellationClosesUpgradedWebSocket(t *testing.T) {
	token := configureWSTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		conn, _, err := Upgrade(w, r.WithContext(ctx))
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()
	dialer := websocket.Dialer{Subprotocols: []string{applicationSubprotocol, token}}
	header := http.Header{"Origin": {"https://tutor.abcfe.net"}}
	conn, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cancel()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("WebSocket remained open after cancellation")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("WebSocket handler did not terminate")
	}
}

func TestAuthenticateRequestRejectsOriginProtocolAndToken(t *testing.T) {
	token := configureWSTest(t)

	tests := []struct {
		name string
		req  *http.Request
	}{
		{"arbitrary localhost port", websocketRequest("http://localhost:9999", token)},
		{"missing origin", websocketRequest("", token)},
		{"wrong protocol", websocketRequest("https://tutor.abcfe.net", "not-a-jwt")},
		{"invalid token", websocketRequest("https://tutor.abcfe.net", "one.two.three")},
	}
	tests[2].req.Header.Set("Sec-WebSocket-Protocol", "other, "+token)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := AuthenticateRequest(tt.req); err == nil {
				t.Fatal("AuthenticateRequest() accepted an invalid request")
			}
		})
	}
}

func TestUpgradeHeaderDoesNotEchoJWT(t *testing.T) {
	header := upgradeResponseHeader()
	if got := header.Get("Sec-WebSocket-Protocol"); got != applicationSubprotocol {
		t.Fatalf("selected protocol = %q, want %q", got, applicationSubprotocol)
	}
}

func TestSanitizedEnv(t *testing.T) {
	got := SanitizedEnv([]string{
		"PATH=/usr/bin",
		"HOME=/tmp/home",
		"GEMINI_API_KEY=secret",
		"SUPABASE_JWT_SECRET=secret",
		"ALLOWED_USER_IDS=user-123",
		"ALLOWED_USER_EMAILS=user@example.com",
		"SOME_TOKEN=secret",
		"DATABASE_URL=postgres://secret",
		"REDIS_URL=redis://secret",
		"SENTRY_DSN=https://secret",
		"GITHUB_PAT=secret",
		"SESSION_COOKIE=secret",
		"SSH_AUTH_SOCK=/tmp/agent.sock",
	})
	want := []string{"PATH=/usr/bin", "HOME=/tmp/home"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("SanitizedEnv() = %#v, want %#v", got, want)
	}
}
