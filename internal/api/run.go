package api

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/coding-tutor/internal/project"
	"github.com/gin-gonic/gin"
)

// normalizeLanguage reduces strings like "Go (v1.20+)" → "go".
func normalizeLanguage(language string) string {
	l := strings.ToLower(strings.TrimSpace(language))
	switch {
	case strings.HasPrefix(l, "go"):
		return "go"
	case strings.HasPrefix(l, "python"):
		return "python"
	case strings.HasPrefix(l, "rust"):
		return "rust"
	case strings.HasPrefix(l, "typescript"):
		return "typescript"
	case strings.HasPrefix(l, "javascript"):
		return "javascript"
	default:
		return l
	}
}

// runCommand returns the command args for the given language.
func runCommand(language string) []string {
	switch normalizeLanguage(language) {
	case "go":
		return []string{"go", "run", "."}
	case "python":
		return []string{"python3", "main.py"}
	case "rust":
		return []string{"cargo", "run"}
	case "typescript":
		return []string{"npx", "--yes", "ts-node", "src/index.ts"}
	case "javascript":
		return []string{"node", "index.js"}
	default:
		return nil
	}
}

// testCommand returns the command args for running tests in the given language.
// If funcName is non-empty, only that specific test function is run.
func testCommand(language, funcName string) []string {
	switch normalizeLanguage(language) {
	case "go":
		if funcName != "" {
			return []string{"go", "test", "-v", "-run", funcName, "./..."}
		}
		return []string{"go", "test", "-v", "./..."}
	case "python":
		if funcName != "" {
			return []string{"python3", "-m", "pytest", "-v", "-k", funcName}
		}
		return []string{"python3", "-m", "pytest", "-v"}
	case "rust":
		if funcName != "" {
			return []string{"cargo", "test", funcName}
		}
		return []string{"cargo", "test"}
	case "typescript", "javascript":
		if funcName != "" {
			return []string{"npx", "--yes", "jest", "--no-coverage", "-t", funcName}
		}
		return []string{"npx", "--yes", "jest", "--no-coverage"}
	default:
		return nil
	}
}

// RunCode streams the project's run output via SSE.
// GET /api/run
func RunCode(c *gin.Context) {
	if !project.Global.IsLoaded() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "프로젝트가 로드되지 않았습니다"})
		return
	}

	status := project.Global.GetStatus()
	args := runCommand(status.Language)
	if args == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "지원하지 않는 언어: " + status.Language})
		return
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming unsupported"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	sendLine := func(line string) {
		fmt.Fprintf(c.Writer, "data: %s\n\n", line)
		flusher.Flush()
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = status.Dir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		sendLine("[오류] stdout pipe: " + err.Error())
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		sendLine("[오류] stderr pipe: " + err.Error())
		return
	}

	if err := cmd.Start(); err != nil {
		sendLine("[실행 오류] " + err.Error())
		fmt.Fprintf(c.Writer, "event: done\ndata: 1\n\n")
		flusher.Flush()
		return
	}

	// stdout, stderr 동시 스캔
	lines := make(chan string, 128)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			lines <- "\x1b[31m" + scanner.Text() + "\x1b[0m"
		}
	}()

	go func() {
		wg.Wait()
		close(lines)
	}()

	for line := range lines {
		sendLine(line)
	}

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	if exitCode == 0 {
		sendLine("\x1b[32m✓ 실행 완료\x1b[0m")
	} else {
		sendLine(fmt.Sprintf("\x1b[31m✗ 종료 코드: %d\x1b[0m", exitCode))
	}

	fmt.Fprintf(c.Writer, "event: done\ndata: %d\n\n", exitCode)
	flusher.Flush()
}

// RunTest streams the project's test output via SSE.
// GET /api/test
func RunTest(c *gin.Context) {
	if !project.Global.IsLoaded() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "프로젝트가 로드되지 않았습니다"})
		return
	}

	status := project.Global.GetStatus()
	funcName := c.Query("func")
	args := testCommand(status.Language, funcName)
	if args == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "지원하지 않는 언어: " + status.Language})
		return
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming unsupported"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	sendLine := func(line string) {
		fmt.Fprintf(c.Writer, "data: %s\n\n", line)
		flusher.Flush()
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = status.Dir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		sendLine("[오류] stdout pipe: " + err.Error())
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		sendLine("[오류] stderr pipe: " + err.Error())
		return
	}

	if err := cmd.Start(); err != nil {
		sendLine("[테스트 오류] " + err.Error())
		fmt.Fprintf(c.Writer, "event: done\ndata: 1\n\n")
		flusher.Flush()
		return
	}

	lines := make(chan string, 128)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()

	go func() {
		wg.Wait()
		close(lines)
	}()

	for line := range lines {
		sendLine(line)
	}

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	if exitCode == 0 {
		sendLine("\x1b[32m✓ 모든 테스트 통과\x1b[0m")
	} else {
		sendLine("\x1b[31m✗ 테스트 실패\x1b[0m")
	}

	fmt.Fprintf(c.Writer, "event: done\ndata: %d\n\n", exitCode)
	flusher.Flush()
}
