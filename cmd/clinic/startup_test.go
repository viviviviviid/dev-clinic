package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClinicStartupHelper(t *testing.T) {
	if os.Getenv("CLINIC_STARTUP_TEST") != "1" {
		return
	}
	os.Args = []string{"clinic", os.Args[len(os.Args)-1]}
	main()
}

func TestRunScriptStartsClinicWithoutConfigFiles(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make unavailable")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "download with spaces")
	if err := os.MkdirAll(filepath.Join(repo, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("../../run.sh")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"run.sh": string(script),
		// The test binary already contains the actual main implementation.
		"Makefile":   "build-be:\n\t@test -x bin/clinic\n",
		"bin/clinic": "#!/bin/sh\nexec \"$CLINIC_TEST_BINARY\" -test.run=^TestClinicStartupHelper$ -- \"$@\"\n",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(contents), 0755); err != nil {
			t.Fatal(err)
		}
	}
	bootstrap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/clinic-config.json" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"version": 1, "supabaseUrl": "https://project.supabase.co", "supabaseAnonKey": "sb_publishable_test"})
	}))
	defer bootstrap.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	_ = listener.Close()
	logFile, err := os.Create(filepath.Join(root, "clinic.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	cmd := exec.Command("bash", filepath.Join(repo, "run.sh"), "my learning")
	cmd.Dir = root // outside the downloaded repo; relative user paths stay here.
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + root,
		"CLINIC_STARTUP_TEST=1", "CLINIC_TEST_BINARY=" + executable, "GIN_MODE=release",
		"CLINIC_SITE_URL=" + bootstrap.URL, "PORT=" + port, "CODEX_BIN=/bin/sh",
		"BASE_DIR=" + filepath.Join(root, "ignored-env-directory"),
	}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	client := &http.Client{Timeout: time.Second}
	baseURL := "http://127.0.0.1:" + port
	ready := false
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		resp, err := client.Get(baseURL + "/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	logs, _ := os.ReadFile(logFile.Name())
	if !ready {
		t.Fatalf("clinic did not start:\n%s", logs)
	}
	wantDir := filepath.Join(root, "my learning")
	if _, err := os.Stat(wantDir); err != nil {
		t.Fatal("CLI workspace was not created")
	}
	if !strings.Contains(string(logs), "base_dir="+wantDir) {
		t.Fatalf("CLI directory was not used:\n%s", logs)
	}
	if _, err := os.Stat(filepath.Join(root, "ignored-env-directory")); !os.IsNotExist(err) {
		t.Fatal("BASE_DIR overrode explicit CLI path")
	}
	resp, err := client.Get(baseURL + "/api/user/settings")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated API status = %d", resp.StatusCode)
	}
}
