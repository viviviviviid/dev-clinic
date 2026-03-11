package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

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
