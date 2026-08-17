package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coding-tutor/internal/config"
	"github.com/coding-tutor/internal/pathguard"
	"github.com/coding-tutor/internal/project"
	wsserver "github.com/coding-tutor/internal/ws"
	"github.com/gin-gonic/gin"
)

type GotoReq struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
}

type GotoResp struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
}

const maxGotoOutputBytes = 256 << 10

var (
	gotoPattern           = regexp.MustCompile(`^(.+):(\d+):(\d+)`)
	goplsCommand          = "gopls"
	gotoDefinitionTimeout = 3 * time.Second
)

type cappedCombinedOutput struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	truncated bool
}

func (w *cappedCombinedOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	written := len(p)
	remaining := maxGotoOutputBytes - w.buffer.Len()
	if len(p) > remaining {
		w.truncated = true
	}
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = w.buffer.Write(p)
	}
	return written, nil
}

func (w *cappedCombinedOutput) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.String()
}

func resolveGotoFile(baseDir, projectDir, candidate string) (string, string, error) {
	projectRoot, err := pathguard.Resolve(baseDir, projectDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve project directory: %w", err)
	}
	projectInfo, err := os.Lstat(projectRoot)
	if err != nil || !projectInfo.IsDir() {
		return "", "", fmt.Errorf("project directory is not available")
	}

	if strings.TrimSpace(candidate) == "" {
		return "", "", fmt.Errorf("file is required")
	}
	lexicalPath := candidate
	if !filepath.IsAbs(lexicalPath) {
		lexicalPath = filepath.Join(projectRoot, lexicalPath)
	}
	lexicalPath, err = filepath.Abs(lexicalPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve file path: %w", err)
	}
	lexicalPath = filepath.Clean(lexicalPath)
	rel, err := filepath.Rel(projectRoot, lexicalPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("file is outside the current project")
	}

	resolvedPath, err := pathguard.Resolve(projectRoot, lexicalPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve project file: %w", err)
	}
	// Resolve canonicalizes every existing symlink component. A different path
	// therefore means gopls would follow a link rather than the requested
	// project file; fail closed before starting the subprocess.
	if resolvedPath != lexicalPath {
		return "", "", fmt.Errorf("symbolic links are not allowed")
	}
	fileInfo, err := os.Lstat(resolvedPath)
	if err != nil {
		return "", "", fmt.Errorf("inspect project file: %w", err)
	}
	if !fileInfo.Mode().IsRegular() || fileInfo.Mode()&os.ModeSymlink != 0 {
		return "", "", fmt.Errorf("file is not a regular file")
	}
	return projectRoot, resolvedPath, nil
}

func runGoplsDefinition(ctx context.Context, projectDir, file string, line, col int) (string, error) {
	location := fmt.Sprintf("%s:%d:%d", file, line, col)
	cmd := exec.CommandContext(ctx, goplsCommand, "definition", location)
	cmd.Dir = projectDir
	cmd.Env = wsserver.SanitizedEnv(os.Environ())
	prepareBoundedCommand(cmd)
	output := &cappedCombinedOutput{}
	cmd.Stdout = output
	cmd.Stderr = output
	err := cmd.Run()
	return output.String(), err
}

// GotoDefinition runs gopls to find a symbol's definition location.
// POST /api/goto
func GotoDefinition(c *gin.Context) {
	var req GotoReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Line < 1 || req.Col < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "line and col must be positive"})
		return
	}
	if !project.Global.IsLoaded() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project not loaded"})
		return
	}
	projectDir, file, err := resolveGotoFile(config.Global.BaseDir, project.Global.GetDir(), req.File)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid project file"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), gotoDefinitionTimeout)
	defer cancel()

	out, err := runGoplsDefinition(ctx, projectDir, file, req.Line, req.Col)
	if err != nil {
		if ctx.Err() != nil {
			c.JSON(http.StatusOK, gin.H{"error": "gopls timed out"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"error": "gopls failed"})
		return
	}

	line := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
	m := gotoPattern.FindStringSubmatch(line)
	if m == nil {
		c.JSON(http.StatusOK, gin.H{"error": "could not parse gopls output"})
		return
	}

	lineNum, _ := strconv.Atoi(m[2])
	colNum, _ := strconv.Atoi(m[3])
	if lineNum < 1 || colNum < 1 {
		c.JSON(http.StatusOK, gin.H{"error": "invalid gopls definition location"})
		return
	}
	_, definitionFile, err := resolveGotoFile(config.Global.BaseDir, projectDir, m[1])
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"error": "definition is outside the current project"})
		return
	}

	c.JSON(http.StatusOK, GotoResp{
		File: definitionFile,
		Line: lineNum,
		Col:  colNum,
	})
}
