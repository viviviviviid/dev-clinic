package api

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coding-tutor/internal/completion"
	"github.com/coding-tutor/internal/config"
	"github.com/coding-tutor/internal/pathguard"
	"github.com/coding-tutor/internal/project"
	"github.com/coding-tutor/internal/toolchain"
	wsserver "github.com/coding-tutor/internal/ws"
	"github.com/gin-gonic/gin"
)

const (
	maxStreamOutputBytes = 256 << 10
	streamReadBufferSize = 64 << 10
)

var (
	broadcastTestResult   = wsserver.Global.BroadcastTestResultForProject
	broadcastStepComplete = wsserver.Global.BroadcastStepCompleteForProject
)

// runCommand returns the command args for the given language.
func runCommand(language string) []string {
	spec, ok := toolchain.Lookup(language)
	if !ok {
		return nil
	}
	return spec.RunCommand()
}

// testCommand returns the command args for running tests in the given language.
// If funcName is non-empty, only that specific test function is run.
func testCommand(language, funcName string) []string {
	spec, ok := toolchain.Lookup(language)
	if !ok {
		return nil
	}
	return spec.TestCommand(funcName)
}

// RunCode streams the project's run output via SSE.
// GET /api/run
func RunCode(c *gin.Context) {
	if !project.Global.IsLoaded() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "프로젝트가 로드되지 않았습니다"})
		return
	}

	status := project.Global.GetStatus()
	dir, err := pathguard.Resolve(config.Global.BaseDir, status.Dir)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "project is outside base directory"})
		return
	}
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
	cmd.Dir = dir
	cmd.Env = wsserver.SanitizedEnv(os.Environ())
	prepareBoundedCommand(cmd)

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
		streamOutputLines(stdout, "", "", lines)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		streamOutputLines(stderr, "\x1b[31m", "\x1b[0m", lines)
	}()

	go func() {
		wg.Wait()
		close(lines)
	}()

	forwardLimitedOutput(lines, sendLine)

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	if ctx.Err() == context.DeadlineExceeded {
		sendLine("\x1b[31m✗ 실행 제한 시간(30초)을 초과했습니다.\x1b[0m")
	} else if exitCode == 0 {
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
	dir, err := pathguard.Resolve(config.Global.BaseDir, status.Dir)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "project is outside base directory"})
		return
	}
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
	cmd.Dir = dir
	cmd.Env = wsserver.SanitizedEnv(os.Environ())
	prepareBoundedCommand(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		summary := "[오류] stdout pipe: " + err.Error()
		sendLine(summary)
		if completionSummary := publishExplicitTestResult(funcName, dir, false, summary); completionSummary != "" {
			sendLine("[단계 완료 확인] " + completionSummary)
		}
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		summary := "[오류] stderr pipe: " + err.Error()
		sendLine(summary)
		if completionSummary := publishExplicitTestResult(funcName, dir, false, summary); completionSummary != "" {
			sendLine("[단계 완료 확인] " + completionSummary)
		}
		return
	}

	if err := cmd.Start(); err != nil {
		summary := "[테스트 오류] " + err.Error()
		sendLine(summary)
		if completionSummary := publishExplicitTestResult(funcName, dir, false, summary); completionSummary != "" {
			sendLine("[단계 완료 확인] " + completionSummary)
		}
		fmt.Fprintf(c.Writer, "event: done\ndata: 1\n\n")
		flusher.Flush()
		return
	}

	lines := make(chan string, 128)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		streamOutputLines(stdout, "", "", lines)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		streamOutputLines(stderr, "", "", lines)
	}()

	go func() {
		wg.Wait()
		close(lines)
	}()

	lastOutputLine := forwardLimitedOutputWithLastLine(lines, sendLine)

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	timedOut := ctx.Err() == context.DeadlineExceeded
	if timedOut {
		sendLine("\x1b[31m✗ 테스트 제한 시간(60초)을 초과했습니다.\x1b[0m")
	} else if exitCode == 0 {
		sendLine("\x1b[32m✓ 모든 테스트 통과\x1b[0m")
	} else {
		sendLine("\x1b[31m✗ 테스트 실패\x1b[0m")
	}

	passed, summary := explicitTestResult(exitCode, timedOut, lastOutputLine)
	if completionSummary := publishExplicitTestResult(funcName, dir, passed, summary); completionSummary != "" {
		sendLine("[단계 완료 확인] " + completionSummary)
	}

	fmt.Fprintf(c.Writer, "event: done\ndata: %d\n\n", exitCode)
	flusher.Flush()
}

func publishExplicitTestResult(funcName, projectDir string, passed bool, summary string) string {
	if funcName != "" {
		broadcastTestResult(projectDir, passed, summary)
		return ""
	}

	result := completion.Evaluate(config.Global.BaseDir, projectDir, passed)
	broadcastTestResult(projectDir, passed, completion.AppendSummary(summary, result))
	broadcastStepComplete(projectDir, result.Complete)
	return result.Summary()
}

func explicitTestResult(exitCode int, timedOut bool, lastOutputLine string) (bool, string) {
	lastOutputLine = strings.TrimSpace(lastOutputLine)
	if timedOut {
		return false, "테스트 제한 시간(60초)을 초과했습니다."
	}
	if exitCode == 0 {
		if lastOutputLine == "" {
			lastOutputLine = "모든 테스트 통과"
		}
		return true, lastOutputLine
	}
	if lastOutputLine == "" {
		lastOutputLine = fmt.Sprintf("테스트 실패 (종료 코드 %d)", exitCode)
	}
	return false, lastOutputLine
}

func streamOutputLines(reader io.Reader, prefix, suffix string, lines chan<- string) {
	buffered := bufio.NewReaderSize(reader, streamReadBufferSize)
	for {
		fragment, err := buffered.ReadSlice('\n')
		if len(fragment) > 0 {
			fragment = bytesTrimLineEnding(fragment)
			lines <- prefix + string(fragment) + suffix
		}
		if err != nil {
			if err != io.EOF && err != bufio.ErrBufferFull {
				lines <- prefix + "[output read error: " + err.Error() + "]" + suffix
			}
			if err != bufio.ErrBufferFull {
				return
			}
		}
	}
}

func bytesTrimLineEnding(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}
	if len(data) > 0 && data[len(data)-1] == '\r' {
		data = data[:len(data)-1]
	}
	return data
}

func forwardLimitedOutput(lines <-chan string, sendLine func(string)) {
	_ = forwardLimitedOutputWithLastLine(lines, sendLine)
}

func forwardLimitedOutputWithLastLine(lines <-chan string, sendLine func(string)) string {
	sent := 0
	truncated := false
	lastLine := ""
	for line := range lines {
		if strings.TrimSpace(line) != "" {
			lastLine = line
		}
		if truncated {
			continue
		}
		if sent+len(line) > maxStreamOutputBytes {
			sendLine("[output truncated after 256 KiB]")
			truncated = true
			continue
		}
		sendLine(line)
		sent += len(line)
	}
	return lastLine
}

func prepareBoundedCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if err == syscall.ESRCH {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
}
