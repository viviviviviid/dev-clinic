package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coding-tutor/internal/config"
	"github.com/coding-tutor/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
)

// Exercise the real router -> authentication -> request context -> REST path.
// The HTTP fixture models Auth/PostgREST's contract, not a live database RLS test.
func TestAccountAPIsForwardOnlyTheVerifiedCallersToken(t *testing.T) {
	previous := *config.Global
	t.Cleanup(func() { *config.Global = previous })
	for _, name := range []string{"ALLOWED_USER_ID", "ALLOWED_USER_IDS", "ALLOWED_USER_EMAIL", "ALLOWED_USER_EMAILS"} {
		t.Setenv(name, "")
	}
	const secret = "test-only-auth-server-signing-secret"
	var mu sync.Mutex
	settings := map[string]map[string]interface{}{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("apikey") != "sb_publishable_test" {
			t.Error("non-public API key used")
			http.Error(w, "wrong key", 403)
			return
		}
		claims := jwt.MapClaims{}
		token, err := jwt.ParseWithClaims(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), claims, func(*jwt.Token) (interface{}, error) { return []byte(secret), nil }, jwt.WithValidMethods([]string{"HS256"}))
		if err != nil || !token.Valid {
			http.Error(w, "invalid JWT", 401)
			return
		}
		user := claims["sub"].(string)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/auth/v1/user" {
			_ = json.NewEncoder(w).Encode(map[string]string{"id": user})
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPost {
			var row map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&row); err != nil {
				t.Error(err)
			}
			if row["user_id"] != user || r.Header.Get("Prefer") != "resolution=merge-duplicates,return=minimal" {
				t.Error("write did not preserve the authenticated identity/upsert contract")
				http.Error(w, "wrong identity", 403)
				return
			}
			settings[user] = row
			w.WriteHeader(http.StatusCreated)
			return
		}
		if r.URL.Query().Get("user_id") != "eq."+user {
			t.Error("read or mutation used another user's token")
			http.Error(w, "wrong identity", 403)
			return
		}
		if r.Method == http.MethodGet {
			rows := []map[string]interface{}{}
			if r.URL.Path == "/rest/v1/user_settings" {
				rows = append(rows, settings[user])
			}
			_ = json.NewEncoder(w).Encode(rows)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	config.Global.Supabase = config.SupabaseConfig{URL: upstream.URL, AnonKey: "sb_publishable_test"}
	config.Global.BaseDir = t.TempDir()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerAPIRoutes(router.Group("/api", middleware.Auth()))

	var wg sync.WaitGroup
	for _, user := range []string{"alice", "bob"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claims := jwt.MapClaims{"iss": upstream.URL + "/auth/v1", "aud": "authenticated", "role": "authenticated", "sub": user, "exp": time.Now().Add(time.Hour).Unix()}
			token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
			if err != nil {
				t.Error(err)
				return
			}
			for _, action := range []struct{ method, path, body string }{
				{http.MethodPut, "/api/user/settings", `{"language":"Go","skill_level":"normal","user_id":"spoofed"}`},
				{http.MethodGet, "/api/user/settings", ""},
				{http.MethodGet, "/api/daily", ""},
				{http.MethodGet, "/api/daily/history", ""},
				{http.MethodPost, "/api/project/complete", `{"project_dir":"/learning/project"}`},
				{http.MethodDelete, "/api/project", `{"project_dir":"/learning/project"}`},
			} {
				req := httptest.NewRequest(action.method, action.path, strings.NewReader(action.body))
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")
				resp := httptest.NewRecorder()
				router.ServeHTTP(resp, req)
				if resp.Code != http.StatusOK {
					t.Errorf("%s %s: HTTP %d %s", user, action.path, resp.Code, resp.Body.String())
				}
				if action.path == "/api/user/settings" && !strings.Contains(resp.Body.String(), `"user_id":"`+user+`"`) {
					t.Error("response belongs to another user")
				}
			}
		}()
	}
	wg.Wait()
}
