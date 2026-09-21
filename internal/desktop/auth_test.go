package desktop

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

func authorizationURL() string {
	q := url.Values{"provider": {"google"}, "redirect_to": {CallbackURL},
		"code_challenge": {strings.Repeat("a", 43)}, "code_challenge_method": {"s256"}}
	return "https://project.supabase.co/auth/v1/authorize?" + q.Encode()
}

func TestAuthorizationURLRequiresExactProviderAndPKCE(t *testing.T) {
	valid := authorizationURL()
	if err := ValidateAuthorizationURL(valid, "https://project.supabase.co"); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		strings.Replace(valid, "project.supabase.co", "evil.example", 1),
		strings.Replace(valid, "https:", "http:", 1),
		strings.Replace(valid, "project.supabase.co", "user@project.supabase.co", 1),
		strings.Replace(valid, "/authorize", "/logout", 1),
		strings.Replace(valid, "provider=google", "provider=github", 1),
		strings.Replace(valid, "code_challenge_method=s256", "code_challenge_method=plain", 1),
		strings.Replace(valid, url.QueryEscape(CallbackURL), url.QueryEscape("https://evil.example"), 1),
		valid + "&redirect_to=https://evil.example", valid + "#fragment",
		strings.Replace(valid, strings.Repeat("a", 43), "short", 1),
	} {
		if err := ValidateAuthorizationURL(raw, "https://project.supabase.co"); err == nil {
			t.Errorf("accepted unsafe URL %q", raw)
		}
	}
}

func TestBrowserCallbackTransfersCodeOnce(t *testing.T) {
	var auth Auth
	if auth.HandleURL(CallbackURL + "?code=unsolicited") {
		t.Fatal("accepted unsolicited callback")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	opened := make(chan struct{})
	finished := make(chan loginResult, 1)
	go func() {
		code, err := auth.Login(ctx, authorizationURL(), "https://project.supabase.co", func(raw string) error {
			close(opened)
			return nil
		})
		finished <- loginResult{code, err}
	}()
	select {
	case <-opened:
	case <-ctx.Done():
		t.Fatal("browser was not opened")
	}
	if _, err := auth.Login(ctx, authorizationURL(), "https://project.supabase.co", nil); err == nil {
		t.Fatal("allowed overlapping logins")
	}
	for _, raw := range []string{"https://auth/callback?code=x", "coding-tutor://evil/callback?code=x",
		CallbackURL + "?code=a&code=b", CallbackURL + "#access_token=secret", CallbackURL + "?code="} {
		if auth.HandleURL(raw) {
			t.Errorf("accepted malformed callback %q", raw)
		}
	}
	if !auth.HandleURL(CallbackURL + "?code=valid-pkce-code") {
		t.Fatal("callback was not delivered")
	}
	result := <-finished
	if result.err != nil || result.code != "valid-pkce-code" {
		t.Fatalf("login result: %+v", result)
	}
	if auth.HandleURL(CallbackURL + "?code=replay") {
		t.Fatal("callback was replayed")
	}
}

func TestLoginCancellationAndProviderError(t *testing.T) {
	for _, mode := range []string{"cancel", "context", "provider"} {
		t.Run(mode, func(t *testing.T) {
			var auth Auth
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			code, err := auth.Login(ctx, authorizationURL(), "https://project.supabase.co", func(string) error {
				switch mode {
				case "cancel":
					auth.Cancel()
				case "context":
					cancel()
				case "provider":
					auth.HandleURL(CallbackURL + "?error=access_denied&error_description=untrusted")
				}
				return nil
			})
			if err == nil || code != "" || strings.Contains(err.Error(), "untrusted") {
				t.Fatalf("unexpected cancellation: code=%q error=%v", code, err)
			}
			if auth.HandleURL(CallbackURL + "?code=expired") {
				t.Fatal("pending login was not cleared")
			}
		})
	}
}
