package api

import (
	"bufio"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

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
	root := c.Query("path")
	if root == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path required"})
		return
	}

	var matches []FileMatch
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || len(matches) >= 50 {
			return nil
		}
		if strings.Contains(path, "/.snapshots/") || strings.HasPrefix(info.Name(), ".") {
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
	root := c.Query("path")
	if root == "" || q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path and q required"})
		return
	}

	var matches []ContentMatch
	fileCounts := map[string]int{}

	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || len(matches) >= 200 {
			return nil
		}
		if strings.Contains(path, "/.snapshots/") || strings.HasPrefix(info.Name(), ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		rel, _ := filepath.Rel(root, path)
		scanner := bufio.NewScanner(f)
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
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path required"})
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
	infos, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var entries []FileEntry
	for _, info := range infos {
		if strings.HasPrefix(info.Name(), ".") {
			continue
		}
		full := filepath.Join(dir, info.Name())
		rel, _ := filepath.Rel(root, full)
		entry := FileEntry{
			Name:  info.Name(),
			Path:  rel,
			IsDir: info.IsDir(),
		}
		if info.IsDir() {
			children, err := buildTree(root, full)
			if err == nil {
				entry.Children = children
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func ReadFile(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path required"})
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

	if err := os.WriteFile(req.Path, []byte(req.Content), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func ValidateDir(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path required"})
		return
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		c.JSON(http.StatusOK, gin.H{"valid": false})
		return
	}
	// Check if TUTORSYS.md exists
	_, tutorErr := os.Stat(filepath.Join(path, "TUTORSYS.md"))
	c.JSON(http.StatusOK, gin.H{
		"valid":        true,
		"hasTutorSys":  tutorErr == nil,
	})
}
