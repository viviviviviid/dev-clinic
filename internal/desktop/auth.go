package desktop

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const CallbackURL = "coding-tutor://auth/callback"

var challengePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

type loginResult struct {
	code string
	err  error
}

// Auth transfers only a short-lived authorization code from the OS URL handler.
// The PKCE verifier stays in the app's Supabase client, never in the URL.
type Auth struct {
	mu      sync.Mutex
	pending chan loginResult
}

func ValidateAuthorizationURL(raw, supabaseURL string) error {
	u, err := url.Parse(raw)
	base, baseErr := url.Parse(supabaseURL)
	if err != nil || baseErr != nil || len(raw) > 8192 || u.User != nil || u.Fragment != "" ||
		u.Scheme != base.Scheme || u.Host != base.Host || u.Path != "/auth/v1/authorize" {
		return errors.New("허용되지 않은 로그인 주소입니다")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return errors.New("로그인 요청이 올바르지 않습니다")
	}
	for _, key := range []string{"provider", "redirect_to", "code_challenge", "code_challenge_method"} {
		if len(q[key]) != 1 {
			return errors.New("로그인 요청이 올바르지 않습니다")
		}
	}
	if q.Get("provider") != "google" || q.Get("redirect_to") != CallbackURL ||
		!strings.EqualFold(q.Get("code_challenge_method"), "s256") || !challengePattern.MatchString(q.Get("code_challenge")) {
		return errors.New("Google 로그인에 안전한 PKCE 요청이 필요합니다")
	}
	return nil
}

func (a *Auth) Login(ctx context.Context, raw, supabaseURL string, open func(string) error) (string, error) {
	if err := ValidateAuthorizationURL(raw, supabaseURL); err != nil {
		return "", err
	}
	a.mu.Lock()
	if a.pending != nil {
		a.mu.Unlock()
		return "", errors.New("이미 로그인이 진행 중입니다")
	}
	result := make(chan loginResult, 1)
	a.pending = result
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		if a.pending == result {
			a.pending = nil
		}
		a.mu.Unlock()
	}()
	if err := open(raw); err != nil {
		return "", fmt.Errorf("브라우저를 열지 못했습니다: %w", err)
	}
	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()
	select {
	case value := <-result:
		return value.code, value.err
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return "", errors.New("로그인 시간이 초과되었습니다. 다시 시도해 주세요. 앱으로 돌아오지 않으면 Supabase Redirect URLs에 coding-tutor://auth/callback 등록을 확인하세요")
	}
}

func (a *Auth) HandleURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 8192 || u.Scheme != "coding-tutor" || u.Host != "auth" ||
		u.Path != "/callback" || u.User != nil || u.Fragment != "" {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	result := loginResult{}
	if len(q["error"]) == 1 && q.Get("error") != "" {
		// Never surface arbitrary callback text as trusted application instructions.
		result.err = errors.New("Google 로그인이 취소되었거나 거부되었습니다. 다시 시도해 주세요")
	} else if len(q["code"]) == 1 && len(q.Get("code")) > 0 && len(q.Get("code")) <= 2048 {
		result.code = q.Get("code")
	} else {
		return false
	}
	return a.deliver(result)
}

func (a *Auth) Cancel() {
	a.deliver(loginResult{err: errors.New("로그인을 취소했습니다")})
}

func (a *Auth) deliver(result loginResult) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pending == nil {
		return false
	}
	select {
	case a.pending <- result:
		return true
	default:
		return false
	}
}
