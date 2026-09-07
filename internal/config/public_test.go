package config

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWithoutFilesBootstrapsPublicSettings(t *testing.T) {
	previous := *Global
	t.Cleanup(func() { *Global = previous })
	for _, name := range []string{"SUPABASE_URL", "SUPABASE_ANON_KEY", "CLINIC_SITE_URL"} {
		t.Setenv(name, "")
	}
	// Retired administrative variables must have no effect on a user's runtime.
	t.Setenv("SUPABASE_SERVICE_ROLE_KEY", "private")
	t.Setenv("SUPABASE_JWT_SECRET", "private")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/clinic-config.json" || r.Header.Get("Authorization") != "" {
			t.Error("unexpected bootstrap request")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"version": 1, "supabaseUrl": "https://project.supabase.co/", "supabaseAnonKey": "sb_publishable_test"})
	}))
	defer server.Close()
	Global.Supabase = SupabaseConfig{}
	Global.SiteURL = server.URL
	Load(filepath.Join(t.TempDir(), "no-config.toml"))
	if err := LoadPublic(context.Background()); err != nil {
		t.Fatal(err)
	}
	if Global.Supabase.URL != "https://project.supabase.co" || Global.Supabase.AnonKey != "sb_publishable_test" {
		t.Fatal("incorrect public settings")
	}
}

func TestPublicConfigRejectsUnsafeOrMissingDeploymentSettings(t *testing.T) {
	previous := *Global
	t.Cleanup(func() { *Global = previous })
	for _, body := range []string{
		`<html>SPA fallback</html>`,
		`{"version":2,"supabaseUrl":"https://project.supabase.co","supabaseAnonKey":"sb_publishable_test"}`,
		`{"version":1,"supabaseUrl":"http://project.supabase.co","supabaseAnonKey":"sb_publishable_test"}`,
		`{"version":1,"supabaseUrl":"https://evil.example","supabaseAnonKey":"sb_publishable_test"}`,
		`{"version":1,"supabaseUrl":"https://project.supabase.co","supabaseAnonKey":"sb_secret_private"}`,
		strings.Repeat("x", maxPublicConfigBytes+1),
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		Global.Supabase = SupabaseConfig{}
		Global.SiteURL = server.URL
		if err := LoadPublic(context.Background()); err == nil {
			t.Error("unsafe bootstrap accepted")
		}
		server.Close()
	}
}

func TestPublicConfigDoesNotFollowRedirects(t *testing.T) {
	previous := *Global
	t.Cleanup(func() { *Global = previous })
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect followed") }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer server.Close()
	Global.Supabase = SupabaseConfig{}
	Global.SiteURL = server.URL
	if err := LoadPublic(context.Background()); err == nil {
		t.Fatal("redirect accepted")
	}
}

func TestPublicKeysAndOriginValidation(t *testing.T) {
	for _, role := range []string{"anon", "service_role", "authenticated"} {
		key := "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"role":"`+role+`"}`)) + ".signature"
		if err := ValidatePublicKey(key); (err == nil) != (role == "anon") {
			t.Errorf("unexpected key result for %s", role)
		}
	}
	for _, origin := range []string{"http://remote.example", "https://user:pass@site.example", "https://site.example/path", "https://site.example?", "https://site.example#fragment"} {
		if _, err := normalizeOrigin(origin, false); err == nil {
			t.Errorf("accepted origin %q", origin)
		}
	}
}
