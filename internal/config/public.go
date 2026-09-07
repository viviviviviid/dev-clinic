package config

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxPublicConfigBytes = 32 << 10

// PublicHTTPClient does not follow redirects: credentials must stay on the
// configured service, and bootstrap must not change the trusted site silently.
func PublicHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

func normalizeOrigin(raw string, supabase bool) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("invalid service origin")
	}
	loopback := (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1") && u.Port() != ""
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return "", fmt.Errorf("service origin requires HTTPS (or loopback HTTP for development)")
	}
	if supabase && !loopback && !strings.HasSuffix(u.Hostname(), ".supabase.co") {
		return "", fmt.Errorf("Supabase URL must use a hosted .supabase.co project")
	}
	return u.Scheme + "://" + u.Host, nil
}

// ValidatePublicKey accepts only publishable keys or legacy anon JWTs, never
// administrative keys. Decoding here checks the key type, not authentication.
func ValidatePublicKey(key string) error {
	if strings.HasPrefix(key, "sb_publishable_") && len(key) > len("sb_publishable_") {
		return nil
	}
	parts := strings.Split(key, ".")
	if len(parts) == 3 {
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		var claims struct {
			Role string `json:"role"`
		}
		if err == nil && json.Unmarshal(payload, &claims) == nil && claims.Role == "anon" {
			return nil
		}
	}
	return fmt.Errorf("Supabase requires a public publishable/anon key; administrator keys are not supported")
}

func ValidateSupabase() error {
	origin, err := normalizeOrigin(Global.Supabase.URL, true)
	if err != nil {
		return fmt.Errorf("Supabase URL: %w", err)
	}
	Global.Supabase.URL = origin
	Global.Supabase.AnonKey = strings.TrimSpace(Global.Supabase.AnonKey)
	return ValidatePublicKey(Global.Supabase.AnonKey)
}

// LoadPublic obtains the same public settings used by the deployed browser.
// Explicit local settings are supported for developers and private installs.
func LoadPublic(ctx context.Context) error {
	origin, err := normalizeOrigin(Global.SiteURL, false)
	if err != nil {
		return fmt.Errorf("CLINIC_SITE_URL: %w", err)
	}
	Global.SiteURL = origin
	if Global.Supabase.URL != "" || Global.Supabase.AnonKey != "" {
		return ValidateSupabase()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/clinic-config.json", nil)
	if err != nil {
		return err
	}
	resp, err := PublicHTTPClient(10 * time.Second).Do(req)
	if err != nil {
		return fmt.Errorf("배포 사이트의 공개 설정을 가져오지 못했습니다: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("배포 사이트의 clinic-config.json 응답이 HTTP %d입니다. 운영자가 최신 frontend를 배포해야 합니다", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPublicConfigBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxPublicConfigBytes {
		return fmt.Errorf("public configuration exceeds size limit")
	}
	var public struct {
		Version int    `json:"version"`
		URL     string `json:"supabaseUrl"`
		AnonKey string `json:"supabaseAnonKey"`
	}
	if err := json.Unmarshal(body, &public); err != nil || public.Version != 1 {
		return fmt.Errorf("배포 사이트의 공개 설정이 올바르지 않습니다. 운영자가 최신 frontend를 배포해야 합니다")
	}
	Global.Supabase = SupabaseConfig{URL: public.URL, AnonKey: public.AnonKey}
	return ValidateSupabase()
}
