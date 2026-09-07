package ws

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/coding-tutor/internal/config"
	"github.com/coding-tutor/internal/middleware"
	"github.com/gorilla/websocket"
)

const applicationSubprotocol = "coding-tutor"

var (
	errOriginNotAllowed = errors.New("websocket origin is not allowed")
	errInvalidProtocol  = errors.New("invalid websocket subprotocol")
)

var upgrader = websocket.Upgrader{
	CheckOrigin: allowedOrigin,
}

// AuthenticateRequest validates the exact browser origin and the two-part
// WebSocket protocol list: ["coding-tutor", <Supabase JWT>].
func AuthenticateRequest(r *http.Request) (string, error) {
	if !allowedOrigin(r) {
		return "", errOriginNotAllowed
	}
	protocols := websocket.Subprotocols(r)
	if len(protocols) != 2 || protocols[0] != applicationSubprotocol {
		return "", errInvalidProtocol
	}
	return middleware.ValidateToken(protocols[1])
}

// Upgrade authenticates before sending the HTTP 101 response. Only the public
// application protocol is echoed; the JWT is never returned in response headers.
func Upgrade(w http.ResponseWriter, r *http.Request) (*websocket.Conn, string, error) {
	userID, err := AuthenticateRequest(r)
	if err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, errOriginNotAllowed) {
			status = http.StatusForbidden
		}
		http.Error(w, http.StatusText(status), status)
		return nil, "", err
	}

	responseHeader := upgradeResponseHeader()
	conn, err := upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		return nil, "", err
	}
	return conn, userID, nil
}

func upgradeResponseHeader() http.Header {
	header := http.Header{}
	header.Set("Sec-WebSocket-Protocol", applicationSubprotocol)
	return header
}

func allowedOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	allowed := map[string]struct{}{
		"https://tutor.abcfe.net":  {},
		"https://clinic.abcfe.net": {},
		"http://localhost:5173":    {},
		"http://127.0.0.1:5173":    {},
	}
	if config.Global.SiteURL != "" {
		allowed[config.Global.SiteURL] = struct{}{}
	}
	for _, configured := range strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",") {
		if configured = strings.TrimSpace(configured); configured != "" {
			allowed[configured] = struct{}{}
		}
	}
	_, ok := allowed[origin]
	return ok
}

// SanitizedEnv removes credentials before launching a shell or language
// server. Non-secret development variables such as PATH, HOME and GOPATH remain.
func SanitizedEnv(environ []string) []string {
	result := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || isSensitiveEnvName(strings.ToUpper(name)) {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func isSensitiveEnvName(name string) bool {
	switch name {
	case "ALLOWED_USER_ID", "ALLOWED_USER_IDS", "ALLOWED_USER_EMAIL", "ALLOWED_USER_EMAILS",
		"SUPABASE_ANON_KEY", "SUPABASE_SERVICE_ROLE_KEY", "SUPABASE_JWT_SECRET",
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "GOOGLE_APPLICATION_CREDENTIALS",
		"DATABASE_URL", "REDIS_URL", "SSH_AUTH_SOCK", "GPG_AGENT_INFO":
		return true
	}
	for _, fragment := range []string{"API_KEY", "TOKEN", "SECRET", "PASSWORD", "PASSWD", "PRIVATE_KEY", "CREDENTIAL", "SERVICE_ROLE", "COOKIE"} {
		if strings.Contains(name, fragment) {
			return true
		}
	}
	if strings.HasSuffix(name, "_DSN") {
		return true
	}
	for _, prefix := range []string{"SUPABASE_", "GEMINI_", "OPENAI_", "ANTHROPIC_", "AWS_", "AZURE_", "GITHUB_", "GITLAB_"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
