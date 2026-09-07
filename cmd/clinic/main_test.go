package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coding-tutor/internal/config"
	"github.com/gin-gonic/gin"
)

func TestLocalAccessMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("ALLOWED_ORIGINS", "https://personal.example")

	tests := []struct {
		name   string
		host   string
		origin string
		want   int
	}{
		{name: "production origin", host: "127.0.0.1:47291", origin: "https://tutor.abcfe.net", want: http.StatusNoContent},
		{name: "configured origin", host: "localhost:47291", origin: "https://personal.example", want: http.StatusNoContent},
		{name: "loopback development", host: "127.0.0.1:47291", origin: "http://localhost:5173", want: http.StatusNoContent},
		{name: "reject foreign origin", host: "127.0.0.1:47291", origin: "https://evil.example", want: http.StatusForbidden},
		{name: "reject dns rebinding host", host: "evil.example:47291", origin: "https://tutor.abcfe.net", want: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(localAccessMiddleware())
			r.OPTIONS("/health", func(c *gin.Context) { c.Status(http.StatusNoContent) })

			req := httptest.NewRequest(http.MethodOptions, "http://127.0.0.1:47291/health", nil)
			req.Host = tt.host
			req.Header.Set("Origin", tt.origin)
			resp := httptest.NewRecorder()
			r.ServeHTTP(resp, req)

			if resp.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", resp.Code, tt.want, resp.Body.String())
			}
		})
	}
}

func TestIsAllowedOriginRejectsURLWithPath(t *testing.T) {
	if isAllowedOrigin("http://localhost:5173/evil", nil) {
		t.Fatal("origin with path was accepted")
	}
}

func TestValidateRuntimeConfig(t *testing.T) {
	original := *config.Global
	t.Cleanup(func() { *config.Global = original })

	config.Global.Server.Port = "47291"
	config.Global.Supabase.URL = "https://example.supabase.co"
	config.Global.Supabase.AnonKey = "sb_publishable_test"
	if err := validateRuntimeConfig(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	config.Global.Server.Port = "70000"
	if err := validateRuntimeConfig(); err == nil {
		t.Fatal("invalid port accepted")
	}
	config.Global.Server.Port = "47291"
	config.Global.Supabase.URL = ""
	if err := validateRuntimeConfig(); err == nil {
		t.Fatal("missing Supabase URL accepted")
	}
	config.Global.Supabase.URL = "https://example.supabase.co"
	config.Global.Supabase.AnonKey = ""
	if err := validateRuntimeConfig(); err == nil {
		t.Fatal("missing public key accepted")
	}
}

func TestReviewRoutesAreRegisteredBehindGroupMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	authenticated := func(c *gin.Context) {
		if c.GetHeader("X-Test-Auth") != "ok" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	}
	registerAPIRoutes(router.Group("/api", authenticated))

	for _, route := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/review/status"},
		{http.MethodPost, "/api/review"},
		{http.MethodPost, "/api/review/cancel"},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(route.method, route.path, nil))
		if response.Code != http.StatusUnauthorized {
			t.Errorf("%s %s status=%d, want authenticated 401", route.method, route.path, response.Code)
		}
	}
}
