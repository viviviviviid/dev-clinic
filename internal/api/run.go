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

// runCommand returns the command args for the given language.
func runCommand(language string) []string {
	switch strings.ToLower(language) {
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
