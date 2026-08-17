package api

import (
	"bufio"
	"bytes"
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/coding-tutor/internal/config"
	"github.com/coding-tutor/internal/pathguard"
	"github.com/gin-gonic/gin"
)

const (
	maxFileReadBytes   = 4 << 20
	maxSearchFileBytes = 1 << 20
	maxSearchBytes     = 16 << 20
	maxTreeEntries     = 5000
	maxGitDiffBytes    = 2 << 20
)

var ignoredFSDirs = map[string]bool{
	".git": true, ".snapshots": true, "node_modules": true, "vendor": true,
	"dist": true, "build": true, "target": true, ".venv": true, "venv": true,
}

func resolveRequestPath(c *gin.Context, candidate string, childOnly bool) (string, bool) {
	var (
		path string
		err  error
	)
	if childOnly {
		path, err = pathguard.ResolveChildNoSymlinks(config.Global.BaseDir, candidate)
	} else {
		path, err = pathguard.Resolve(config.Global.BaseDir, candidate)
	}
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return "", false
	}
	return path, true
}

type FileMatch struct {
	RelPath string `json:"relPath"`
	AbsPath string `json:"absPath"`
	Name    string `json:"name"`
}

type ContentMatch struct {
	RelPath     string `json:"relPath"`
	AbsPath     string `json:"absPath"`
	LineNum     int    `json:"lineNum"`
	LineContent string `json:"lineContent"`
	ColStart    int    `json:"colStart"`
}

func SearchFiles(c *gin.Context) {
	q := strings.ToLower(c.Query("q"))
	requestedRoot := c.Query("path")
	if requestedRoot == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path required"})
		return
	}
	root, ok := resolveRequestPath(c, requestedRoot, false)
	if !ok {
		return
	}

	var matches []FileMatch
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || len(matches) >= 50 {
			return nil
		}
		if path != root && (ignoredFSDirs[info.Name()] || strings.HasPrefix(info.Name(), ".")) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if q == "" || strings.Contains(strings.ToLower(info.Name()), q) {
			rel, _ := filepath.Rel(root, path)
			matches = append(matches, FileMatch{RelPath: rel, AbsPath: path, Name: info.Name()})
		}
		return nil
	})
	if matches == nil {
		matches = []FileMatch{}
	}
	c.JSON(http.StatusOK, matches)
}

func SearchContent(c *gin.Context) {
	q := strings.ToLower(c.Query("q"))
	requestedRoot := c.Query("path")
	if requestedRoot == "" || q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path and q required"})
		return
	}
	root, ok := resolveRequestPath(c, requestedRoot, false)
	if !ok {
		return
	}

	var matches []ContentMatch
	fileCounts := map[string]int{}
	var searchedBytes int64

	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || len(matches) >= 200 {
			return nil
		}
		if path != root && (ignoredFSDirs[info.Name()] || strings.HasPrefix(info.Name(), ".")) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if info.Size() > maxSearchFileBytes || searchedBytes+info.Size() > maxSearchBytes {
			return nil
		}
		searchedBytes += info.Size()

		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		rel, _ := filepath.Rel(root, path)
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64<<10), 256<<10)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if !utf8.ValidString(line) {
				return nil // binary file
			}
			lower := strings.ToLower(line)
			idx := strings.Index(lower, q)
			if idx >= 0 && fileCounts[path] < 20 && len(matches) < 200 {
				matches = append(matches, ContentMatch{
					RelPath:     rel,
					AbsPath:     path,
					LineNum:     lineNum,
					LineContent: line,
					ColStart:    idx,
				})
				fileCounts[path]++
			}
		}
		return nil
	})
	if matches == nil {
		matches = []ContentMatch{}
	}
	c.JSON(http.StatusOK, matches)
}

type FileEntry struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	IsDir    bool        `json:"isDir"`
	Children []FileEntry `json:"children,omitempty"`
}

func ListDir(c *gin.Context) {
	requestedPath := c.Query("path")
	if requestedPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path required"})
		return
	}
	path, ok := resolveRequestPath(c, requestedPath, false)
	if !ok {
		return
	}

	entries, err := buildTree(path, path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, entries)
}

func buildTree(root, dir string) ([]FileEntry, error) {
	count := 0
	return buildTreeLimited(root, dir, &count)
}

func buildTreeLimited(root, dir string, count *int) ([]FileEntry, error) {
	infos, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var entries []FileEntry
	for _, info := range infos {
		if strings.HasPrefix(info.Name(), ".") || ignoredFSDirs[info.Name()] {
			continue
		}
		if *count >= maxTreeEntries {
			break
		}
		(*count)++
		full := filepath.Join(dir, info.Name())
		rel, _ := filepath.Rel(root, full)
		entry := FileEntry{
			Name:  info.Name(),
			Path:  rel,
			IsDir: info.IsDir(),
		}
		if info.IsDir() {
			children, err := buildTreeLimited(root, full, count)
			if err == nil {
				entry.Children = children
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func ReadFile(c *gin.Context) {
	requestedPath := c.Query("path")
	if requestedPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path required"})
		return
	}
	path, ok := resolveRequestPath(c, requestedPath, false)
	if !ok {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileReadBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file is too large or not regular"})
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": string(data)})
}

type WriteFileReq struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func WriteFile(c *gin.Context) {
	var req WriteFileReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Content) > maxFileReadBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file is too large"})
		return
	}
	path, ok := resolveRequestPath(c, req.Path, true)
	if !ok {
		return
	}

	if err := os.WriteFile(path, []byte(req.Content), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type RenameReq struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func RenameFile(c *gin.Context) {
	var req RenameReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.From == "" || req.To == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from and to required"})
		return
	}
	from, ok := resolveRequestPath(c, req.From, true)
	if !ok {
		return
	}
	to, ok := resolveRequestPath(c, req.To, true)
	if !ok {
		return
	}
	fromInfo, err := os.Lstat(from)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "source does not exist"})
		return
	}
	if !fromInfo.IsDir() && !fromInfo.Mode().IsRegular() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "source must be a regular file or directory"})
		return
	}
	if from == to {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	if toInfo, err := os.Lstat(to); err == nil {
		if os.SameFile(fromInfo, toInfo) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": "destination already exists"})
		return
	} else if !os.IsNotExist(err) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot inspect destination"})
		return
	}
	if err := os.Rename(from, to); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type DeleteFileReq struct {
	Path string `json:"path"`
}

func DeleteFsEntry(c *gin.Context) {
	var req DeleteFileReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path required"})
		return
	}
	path, ok := resolveRequestPath(c, req.Path, true)
	if !ok {
		return
	}
	info, err := os.Lstat(path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path must be a regular file or directory"})
		return
	}
	if info.IsDir() {
		err = os.RemoveAll(path)
	} else {
		err = os.Remove(path)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type DiffLine struct {
	LineNum int    `json:"lineNum"`
	DType   string `json:"type"` // "added" | "deleted"
}

func GitDiff(c *gin.Context) {
	requestedFilePath := c.Query("path")
	requestedDir := c.Query("dir")
	if requestedFilePath == "" || requestedDir == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path and dir required"})
		return
	}
	dir, ok := resolveRequestPath(c, requestedDir, false)
	if !ok {
		return
	}
	filePath, err := pathguard.Resolve(dir, requestedFilePath)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}
	filePath, err = filepath.Rel(dir, filePath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out := executeGitDiff(ctx, dir, "diff", "HEAD", "--", filePath)
	if len(out) == 0 {
		// Try staged diff too
		out = executeGitDiff(ctx, dir, "diff", "--cached", "HEAD", "--", filePath)
	}

	lines := parseGitDiff(string(out))
	if lines == nil {
		lines = []DiffLine{}
	}
	c.JSON(http.StatusOK, lines)
}

type cappedGitOutput struct {
	buffer bytes.Buffer
}

func (w *cappedGitOutput) Write(p []byte) (int, error) {
	written := len(p)
	remaining := maxGitDiffBytes - w.buffer.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = w.buffer.Write(p)
	}
	return written, nil
}

func executeGitDiff(ctx context.Context, dir string, args ...string) []byte {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	prepareBoundedCommand(cmd)
	output := &cappedGitOutput{}
	cmd.Stdout = output
	_ = cmd.Run()
	return output.buffer.Bytes()
}

func parseGitDiff(diff string) []DiffLine {
	var result []DiffLine
	hunkRe := regexp.MustCompile(`@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
	newLine := 0
	for _, line := range strings.Split(diff, "\n") {
		if m := hunkRe.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			newLine = n
			continue
		}
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "\\") {
			continue
		}
		if strings.HasPrefix(line, "+") {
			result = append(result, DiffLine{LineNum: newLine, DType: "added"})
			newLine++
		} else if strings.HasPrefix(line, "-") {
			result = append(result, DiffLine{LineNum: newLine, DType: "deleted"})
		} else {
			newLine++
		}
	}
	return result
}

func ValidateDir(c *gin.Context) {
	requestedPath := c.Query("path")
	if requestedPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path required"})
		return
	}
	path, ok := resolveRequestPath(c, requestedPath, false)
	if !ok {
		return
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		c.JSON(http.StatusOK, gin.H{"valid": false})
		return
	}
	// Check if TUTORSYS.md exists
	tutorPath, tutorErr := pathguard.ResolveRelative(path, "TUTORSYS.md")
	if tutorErr == nil {
		_, tutorErr = os.Stat(tutorPath)
	}
	c.JSON(http.StatusOK, gin.H{
		"valid":       true,
		"hasTutorSys": tutorErr == nil,
	})
}
