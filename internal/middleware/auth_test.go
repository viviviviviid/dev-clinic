package middleware

import (
	"net/http"
	"net/http/httptest"
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
	config.Global.Supabase.JWTSecret = testJWTSecret
	t.Setenv("ALLOWED_USER_ID", "")
	t.Setenv("ALLOWED_USER_IDS", "")
	t.Cleanup(func() { *config.Global = previous })
}

func signedTestToken(t *testing.T, mutate func(jwt.MapClaims)) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":  "https://project.supabase.co/auth/v1",
		"aud":  "authenticated",
		"role": "authenticated",
		"sub":  "user-123",
		"exp":  time.Now().Add(time.Hour).Unix(),
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
