package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coding-tutor/internal/config"
	"github.com/coding-tutor/internal/project"
	"github.com/gin-gonic/gin"
)

func TestNodeCommandsNeverInstallPackages(t *testing.T) {
	commands := [][]string{
		runCommand("typescript"),
		testCommand("typescript", ""),
		testCommand("javascript", "TestName"),
	}
	for _, command := range commands {
		joined := strings.Join(command, " ")
		if strings.Contains(joined, "--yes") {
			t.Fatalf("command allows package installation: %q", joined)
		}
		if !strings.Contains(joined, "--no-install") {
			t.Fatalf("command does not disable package installation: %q", joined)
		}
	}
}

func TestPrepareBoundedCommandConfiguresProcessGroup(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "true")
	prepareBoundedCommand(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("process group isolation was not configured")
	}
	if cmd.Cancel == nil || cmd.WaitDelay != 2*time.Second {
		t.Fatal("bounded cancellation was not configured")
	}
}

func TestStreamOutputLinesDrainsLongLines(t *testing.T) {
	input := strings.Repeat("x", streamReadBufferSize*2) + "\nlast"
	lines := make(chan string, 8)
	go func() {
		streamOutputLines(strings.NewReader(input), "", "", lines)
		close(lines)
	}()

	var got strings.Builder
	for line := range lines {
		got.WriteString(line)
	}
	if got.String() != strings.TrimSuffix(input, "\nlast")+"last" {
		t.Fatalf("streamed output length = %d, want %d", got.Len(), len(input)-1)
	}
}

func TestForwardLimitedOutputTruncates(t *testing.T) {
	lines := make(chan string, 2)
	lines <- strings.Repeat("x", maxStreamOutputBytes+1)
	lines <- "discarded"
	close(lines)

	var sent []string
	forwardLimitedOutput(lines, func(line string) { sent = append(sent, line) })
	if len(sent) != 1 || sent[0] != "[output truncated after 256 KiB]" {
		t.Fatalf("sent output = %#v", sent)
	}
}

func TestExplicitTestResult(t *testing.T) {
	tests := []struct {
		name       string
		exitCode   int
		timedOut   bool
		lastLine   string
		wantPassed bool
		wantText   string
	}{
		{name: "passed", lastLine: "ok package", wantPassed: true, wantText: "ok package"},
		{name: "failed", exitCode: 1, lastLine: "FAIL package", wantText: "FAIL package"},
		{name: "failed without output", exitCode: 2, wantText: "종료 코드 2"},
		{name: "timed out", exitCode: 1, timedOut: true, lastLine: "ignored", wantText: "60초"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			passed, summary := explicitTestResult(tt.exitCode, tt.timedOut, tt.lastLine)
			if passed != tt.wantPassed || !strings.Contains(summary, tt.wantText) {
				t.Fatalf("explicitTestResult() = (%v, %q), want passed=%v and text %q", passed, summary, tt.wantPassed, tt.wantText)
			}
		})
	}
}

func TestPublishExplicitTestResultCompletesOnlyFullSuite(t *testing.T) {
	oldBaseDir := config.Global.BaseDir
	baseDir := t.TempDir()
	projectDir := filepath.Join(baseDir, "project")
	if err := os.Mkdir(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config.Global.BaseDir = baseDir
	defer func() { config.Global.BaseDir = oldBaseDir }()

	oldTestResult := broadcastTestResult
	oldStepComplete := broadcastStepComplete
	defer func() {
		broadcastTestResult = oldTestResult
		broadcastStepComplete = oldStepComplete
	}()

	var testResults []bool
	var completions []bool
	var projectDirs []string
	broadcastTestResult = func(projectDir string, passed bool, _ string) {
		projectDirs = append(projectDirs, projectDir)
		testResults = append(testResults, passed)
	}
	broadcastStepComplete = func(projectDir string, passed bool) {
		projectDirs = append(projectDirs, projectDir)
		completions = append(completions, passed)
	}

	publishExplicitTestResult("TestOne", projectDir, true, "targeted pass")
	if len(testResults) != 1 || !testResults[0] {
		t.Fatalf("targeted test result = %#v", testResults)
	}
	if len(completions) != 0 {
		t.Fatalf("targeted test completed the step: %#v", completions)
	}

	publishExplicitTestResult("", projectDir, true, "suite pass")
	if err := os.WriteFile(filepath.Join(projectDir, "main.go"), []byte("package main\n// [TUTOR:HOLE]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	publishExplicitTestResult("", projectDir, true, "suite pass with marker")
	publishExplicitTestResult("", projectDir, false, "suite fail")
	if len(testResults) != 4 || !testResults[1] || !testResults[2] || testResults[3] {
		t.Fatalf("full-suite test results = %#v", testResults)
	}
	if len(completions) != 3 || !completions[0] || completions[1] || completions[2] {
		t.Fatalf("full-suite completions = %#v", completions)
	}
	for _, got := range projectDirs {
		if got != projectDir {
			t.Fatalf("broadcast project dir = %q, want %q", got, projectDir)
		}
	}
}

func TestRunTestBroadcastsSuccessAndFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldProject := project.Global
	project.Global = &project.Manager{}
	defer func() { project.Global = oldProject }()

	oldBaseDir := config.Global.BaseDir
	root := t.TempDir()
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	config.Global.BaseDir = root
	defer func() { config.Global.BaseDir = oldBaseDir }()
	project.Global.Set(root, "## 언어 & 환경\ngo\n")

	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	oldTestResult := broadcastTestResult
	oldStepComplete := broadcastStepComplete
	defer func() {
		broadcastTestResult = oldTestResult
		broadcastStepComplete = oldStepComplete
	}()

	for _, tt := range []struct {
		name         string
		exitCode     string
		wantPassed   bool
		marker       string
		wantComplete bool
	}{
		{name: "success", exitCode: "0", wantPassed: true, wantComplete: true},
		{name: "success with unresolved marker", exitCode: "0", wantPassed: true, marker: "// [TUTOR:BUG]\n"},
		{name: "failure", exitCode: "3"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"+tt.marker), 0o644); err != nil {
				t.Fatal(err)
			}
			script := "#!/bin/sh\nprintf 'suite " + tt.name + "\\n'\nexit " + tt.exitCode + "\n"
			if err := os.WriteFile(filepath.Join(binDir, "go"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}

			var gotResult []bool
			var gotSummary []string
			var gotComplete []bool
			var gotProjectDirs []string
			broadcastTestResult = func(projectDir string, passed bool, summary string) {
				gotProjectDirs = append(gotProjectDirs, projectDir)
				gotResult = append(gotResult, passed)
				gotSummary = append(gotSummary, summary)
			}
			broadcastStepComplete = func(projectDir string, passed bool) {
				gotProjectDirs = append(gotProjectDirs, projectDir)
				gotComplete = append(gotComplete, passed)
			}

			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/test", nil)
			RunTest(ctx)

			if response.Code != http.StatusOK {
				t.Fatalf("RunTest status = %d, body = %q", response.Code, response.Body.String())
			}
			if len(gotResult) != 1 || gotResult[0] != tt.wantPassed {
				t.Fatalf("test_result broadcasts = %#v, want %v", gotResult, tt.wantPassed)
			}
			if len(gotComplete) != 1 || gotComplete[0] != tt.wantComplete {
				t.Fatalf("step_complete broadcasts = %#v, want %v", gotComplete, tt.wantComplete)
			}
			for _, got := range gotProjectDirs {
				if got != resolvedRoot {
					t.Fatalf("broadcast project dir = %q, want %q", got, resolvedRoot)
				}
			}
			if tt.marker != "" && !strings.Contains(response.Body.String(), "미해결 학습 마커 1개") {
				t.Fatalf("completion summary missing from SSE: %q", response.Body.String())
			}
			if tt.marker != "" && (len(gotSummary) != 1 || !strings.Contains(gotSummary[0], "미해결 학습 마커 1개")) {
				t.Fatalf("completion summary missing from test_result: %#v", gotSummary)
			}
		})
	}
}
