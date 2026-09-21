//go:build clinicdesktop && darwin

package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coding-tutor/internal/ai"
	"github.com/coding-tutor/internal/config"
)

func TestDesktopBootOwnsAuthenticatedServerAndShutdown(t *testing.T) {
	previous, previousAI := *config.Global, ai.Global
	t.Cleanup(func() { *config.Global, ai.Global = previous, previousAI })
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", os.Getenv("PATH"))
	t.Setenv("ALLOWED_ORIGINS", "")
	t.Setenv("SUPABASE_URL", "https://project.supabase.co")
	t.Setenv("SUPABASE_ANON_KEY", "sb_publishable_test")
	t.Setenv("CODEX_BIN", "/bin/sh")
	t.Setenv("AI_PROVIDER", "codex")
	t.Setenv("BASE_DIR", filepath.Join(home, "learning with spaces"))
	ctx, cancel := context.WithCancel(context.Background())
	d := &Desktop{ctx: ctx, cancel: cancel}
	// PATH discovery has separate real-shell coverage; do not run user profiles here.
	d.setupOnce.Do(func() {})
	t.Cleanup(func() { d.shutdown(context.Background()) })
	boot, err := d.Boot()
	if err != nil {
		t.Fatal(err)
	}
	if boot.BaseDir != filepath.Join(home, "learning with spaces") {
		t.Fatalf("workspace = %s", boot.BaseDir)
	}
	if _, err := os.Stat(boot.BaseDir); err != nil {
		t.Fatal(err)
	}
	again, err := d.Boot()
	if err != nil || again != boot {
		t.Fatalf("repeat boot should reuse the same server: %+v %v", again, err)
	}
	client := &http.Client{Timeout: time.Second}
	for _, entry := range []struct {
		path, origin string
		status       int
	}{
		{"/health", "wails://wails", http.StatusOK},
		{"/api/user/settings", "wails://wails", http.StatusUnauthorized},
		{"/health", "null", http.StatusForbidden},
		{"/health", "https://evil.example", http.StatusForbidden},
	} {
		req, _ := http.NewRequest(http.MethodGet, boot.LocalURL+entry.path, nil)
		req.Header.Set("Origin", entry.origin)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != entry.status {
			t.Errorf("%s (%s) status=%d want=%d", entry.path, entry.origin, resp.StatusCode, entry.status)
		}
	}
	d.shutdown(context.Background())
	if resp, err := client.Get(boot.LocalURL + "/health"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("server remained alive after app shutdown")
	}
}
