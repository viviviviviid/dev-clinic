package middleware

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coding-tutor/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
)

const testJWTSecret = "test-secret-that-is-long-enough-for-unit-tests"

func configureAuthTest(t *testing.T) {
	t.Helper()
	previous := *config.Global
	config.Global.Supabase.URL = "https://project.supabase.co"
	config.Global.Supabase.AnonKey = "sb_publishable_test"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/v1/user" || r.Header.Get("apikey") != "sb_publishable_test" {
			http.Error(w, "wrong auth request", http.StatusBadRequest)
			return
		}
		claims := jwt.MapClaims{}
		token, err := jwt.ParseWithClaims(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), claims, func(token *jwt.Token) (interface{}, error) { return []byte(testJWTSecret), nil }, jwt.WithValidMethods([]string{"HS256"}))
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
	t.Cleanup(func() { *config.Global = previous })
}

func signedTestToken(t *testing.T, mutate func(jwt.MapClaims)) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":   config.Global.Supabase.URL + "/auth/v1",
		"aud":   "authenticated",
		"role":  "authenticated",
		"sub":   "user-123",
		"email": "user@example.com",
		"exp":   time.Now().Add(time.Hour).Unix(),
	}
	if mutate != nil {
		mutate(claims)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func TestValidateToken(t *testing.T) {
	configureAuthTest(t)

	userID, err := ValidateToken(signedTestToken(t, nil))
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if userID != "user-123" {
		t.Fatalf("ValidateToken() user = %q, want user-123", userID)
	}
}

func TestValidateTokenRejectsInvalidClaims(t *testing.T) {
	configureAuthTest(t)

	tests := []struct {
		name   string
		mutate func(jwt.MapClaims)
	}{
		{"issuer", func(c jwt.MapClaims) { c["iss"] = "https://attacker.invalid/auth/v1" }},
		{"audience", func(c jwt.MapClaims) { c["aud"] = "anon" }},
		{"role", func(c jwt.MapClaims) { c["role"] = "service_role" }},
		{"subject", func(c jwt.MapClaims) { delete(c, "sub") }},
		{"expiration missing", func(c jwt.MapClaims) { delete(c, "exp") }},
		{"expired", func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Minute).Unix() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ValidateToken(signedTestToken(t, tt.mutate)); err == nil {
				t.Fatal("ValidateToken() accepted invalid claims")
			}
		})
	}
}

func TestValidateTokenHonorsAllowedUser(t *testing.T) {
	configureAuthTest(t)
	t.Setenv("ALLOWED_USER_ID", "another-user")

	if _, err := ValidateToken(signedTestToken(t, nil)); err == nil {
		t.Fatal("ValidateToken() accepted a different user")
	}
}

func TestValidateTokenHonorsAllowedUsers(t *testing.T) {
	configureAuthTest(t)
	t.Setenv("ALLOWED_USER_IDS", "another-user, user-123")

	if _, err := ValidateToken(signedTestToken(t, nil)); err != nil {
		t.Fatalf("ValidateToken() rejected an allowed user: %v", err)
	}

	if _, err := ValidateToken(signedTestToken(t, func(claims jwt.MapClaims) {
		claims["sub"] = "unlisted-user"
	})); err == nil {
		t.Fatal("ValidateToken() accepted an unlisted user")
	}
}

func TestValidateTokenRejectsMalformedAllowedUsers(t *testing.T) {
	configureAuthTest(t)
	t.Setenv("ALLOWED_USER_IDS", ", ;")

	if _, err := ValidateToken(signedTestToken(t, nil)); err == nil {
		t.Fatal("ValidateToken() treated a malformed non-empty allowlist as unrestricted")
	}
}

func TestValidateTokenHonorsAllowedEmails(t *testing.T) {
	configureAuthTest(t)
	t.Setenv("ALLOWED_USER_EMAILS", "another@example.com, USER@example.com")

	if _, err := ValidateToken(signedTestToken(t, nil)); err != nil {
		t.Fatalf("ValidateToken() rejected an allowed email: %v", err)
	}

	if _, err := ValidateToken(signedTestToken(t, func(claims jwt.MapClaims) {
		claims["email"] = "unlisted@example.com"
	})); err == nil {
		t.Fatal("ValidateToken() accepted an unlisted email")
	}

	if _, err := ValidateToken(signedTestToken(t, func(claims jwt.MapClaims) {
		delete(claims, "email")
	})); err == nil {
		t.Fatal("ValidateToken() accepted a token without an email")
	}
}

func TestValidateTokenAllowsIDOrEmailMatch(t *testing.T) {
	configureAuthTest(t)
	t.Setenv("ALLOWED_USER_IDS", "user-123")
	t.Setenv("ALLOWED_USER_EMAILS", "another@example.com")

	if _, err := ValidateToken(signedTestToken(t, nil)); err != nil {
		t.Fatalf("ValidateToken() rejected an allowed user ID: %v", err)
	}
}

func TestAuthMiddleware(t *testing.T) {
	configureAuthTest(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Auth())
	router.GET("/protected", func(c *gin.Context) {
		userID, _ := c.Get("user_id")
		c.JSON(http.StatusOK, gin.H{"user_id": userID})
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+signedTestToken(t, nil))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("valid request status = %d, body = %s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/protected", nil)
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestAuthDistinguishesInvalidSessionFromDeniedIdentity(t *testing.T) {
	configureAuthTest(t)
	t.Setenv("ALLOWED_USER_EMAILS", "user@example.com")
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Auth())
	router.GET("/protected", func(c *gin.Context) { c.Status(http.StatusOK) })
	tests := []struct {
		name   string
		token  string
		status int
		code   string
	}{
		{"allowed account", signedTestToken(t, nil), http.StatusOK, ""},
		{"unlisted account", signedTestToken(t, func(c jwt.MapClaims) { c["email"] = "unlisted@example.com" }), http.StatusForbidden, "account_not_allowed"},
		{"expired allowed account", signedTestToken(t, func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Minute).Unix() }), http.StatusUnauthorized, "invalid_token"},
		{"expired unlisted account", signedTestToken(t, func(c jwt.MapClaims) {
			c["email"] = "unlisted@example.com"
			c["exp"] = time.Now().Add(-time.Minute).Unix()
		}), http.StatusUnauthorized, "invalid_token"},
		{"invalid signature", signedTestToken(t, nil) + "broken", http.StatusUnauthorized, "invalid_token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("Authorization", "Bearer "+tt.token)
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != tt.status {
				t.Fatalf("status = %d, want %d", res.Code, tt.status)
			}
			if tt.code != "" {
				var body map[string]string
				if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body["code"] != tt.code {
					t.Fatalf("error code = %q, want %q", body["code"], tt.code)
				}
			}
		})
	}
}

func TestValidateES256WithPublicJWKS(t *testing.T) {
	configureAuthTest(t)
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/v1/.well-known/jwks.json" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"keys": []map[string]string{{
			"kty": "EC", "kid": "test-key", "alg": "ES256", "crv": "P-256",
			"x": base64.RawURLEncoding.EncodeToString(private.X.FillBytes(make([]byte, 32))),
			"y": base64.RawURLEncoding.EncodeToString(private.Y.FillBytes(make([]byte, 32))),
		}}})
	}))
	defer server.Close()
	config.Global.Supabase.URL = server.URL
	claims := jwt.MapClaims{"iss": server.URL + "/auth/v1", "aud": "authenticated", "role": "authenticated", "sub": "user-123", "exp": time.Now().Add(time.Hour).Unix()}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = "test-key"
	signed, err := token.SignedString(private)
	if err != nil {
		t.Fatal(err)
	}
	if user, err := ValidateToken(signed); err != nil || user != "user-123" {
		t.Fatalf("public verification failed: %s %v", user, err)
	}
	if _, err := ValidateToken(signed + "tampered"); err == nil {
		t.Fatal("invalid ES256 signature accepted")
	}
}

func TestHS256FailsClosedWhenAuthIsUnavailableOrIdentityDiffers(t *testing.T) {
	configureAuthTest(t)
	for _, status := range []int{http.StatusUnauthorized, http.StatusServiceUnavailable, http.StatusOK} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"id":"different-user"}`))
		}))
		config.Global.Supabase.URL = server.URL
		if _, err := ValidateToken(signedTestToken(t, nil)); err == nil {
			t.Errorf("accepted invalid auth response %d", status)
		}
		server.Close()
	}
}
