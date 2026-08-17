package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/coding-tutor/internal/config"
	"github.com/gin-gonic/gin"
)

func configureFSTest(t *testing.T) (base, outside string) {
	t.Helper()
	root := t.TempDir()
	base = filepath.Join(root, "base")
	outside = filepath.Join(root, "outside.txt")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	previous := config.Global.BaseDir
	config.Global.BaseDir = base
	t.Cleanup(func() { config.Global.BaseDir = previous })
	gin.SetMode(gin.TestMode)
	return base, outside
}

func TestReadFileConfinesPathsToBaseDir(t *testing.T) {
	base, outside := configureFSTest(t)
	inside := filepath.Join(base, "main.go")
	if err := os.WriteFile(inside, []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.GET("/read", ReadFile)

	request := httptest.NewRequest(http.MethodGet, "/read?path="+inside, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("inside read status = %d, body = %s", response.Code, response.Body.String())
	}

	for _, path := range []string{outside, "../outside.txt"} {
		request = httptest.NewRequest(http.MethodGet, "/read?path="+path, nil)
		response = httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("outside read %q status = %d, want %d", path, response.Code, http.StatusForbidden)
		}
	}
}

func TestReadFileRejectsSymlinkEscape(t *testing.T) {
	base, outside := configureFSTest(t)
	link := filepath.Join(base, "linked.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	router := gin.New()
	router.GET("/read", ReadFile)
	request := httptest.NewRequest(http.MethodGet, "/read?path="+link, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("symlink read status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestWriteAndDeleteRejectBaseEscape(t *testing.T) {
	base, _ := configureFSTest(t)
	router := gin.New()
	router.POST("/write", WriteFile)
	router.DELETE("/delete", DeleteFsEntry)

	body, _ := json.Marshal(WriteFileReq{Path: "../escape.txt", Content: "no"})
	request := httptest.NewRequest(http.MethodPost, "/write", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("outside write status = %d, want %d", response.Code, http.StatusForbidden)
	}

	body, _ = json.Marshal(DeleteFileReq{Path: base})
	request = httptest.NewRequest(http.MethodDelete, "/delete", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("base delete status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestMutationsRejectSymlinksWithoutChangingTargets(t *testing.T) {
	base, _ := configureFSTest(t)
	targetDir := filepath.Join(base, "target")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(targetDir, "main.go")
	if err := os.WriteFile(targetFile, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileLink := filepath.Join(base, "linked.go")
	dirLink := filepath.Join(base, "linked-dir")
	if err := os.Symlink(targetFile, fileLink); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.Symlink(targetDir, dirLink); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	router := gin.New()
	router.POST("/write", WriteFile)
	router.POST("/rename", RenameFile)
	router.DELETE("/delete", DeleteFsEntry)

	writeBody, _ := json.Marshal(WriteFileReq{Path: fileLink, Content: "overwritten"})
	assertJSONStatus(t, router, http.MethodPost, "/write", writeBody, http.StatusForbidden)
	renameBody, _ := json.Marshal(RenameReq{From: fileLink, To: filepath.Join(base, "moved.go")})
	assertJSONStatus(t, router, http.MethodPost, "/rename", renameBody, http.StatusForbidden)
	renameBody, _ = json.Marshal(RenameReq{From: targetFile, To: filepath.Join(dirLink, "moved.go")})
	assertJSONStatus(t, router, http.MethodPost, "/rename", renameBody, http.StatusForbidden)
	deleteBody, _ := json.Marshal(DeleteFileReq{Path: dirLink})
	assertJSONStatus(t, router, http.MethodDelete, "/delete", deleteBody, http.StatusForbidden)

	data, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("target was removed: %v", err)
	}
	if string(data) != "original" {
		t.Fatalf("target content = %q, want original", data)
	}
}

func TestRenameRejectsExistingDestinationAndAllowsSamePathNoOp(t *testing.T) {
	base, _ := configureFSTest(t)
	from := filepath.Join(base, "from.go")
	to := filepath.Join(base, "to.go")
	if err := os.WriteFile(from, []byte("from"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, []byte("to"), 0o644); err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.POST("/rename", RenameFile)
	body, _ := json.Marshal(RenameReq{From: from, To: to})
	assertJSONStatus(t, router, http.MethodPost, "/rename", body, http.StatusConflict)
	for path, want := range map[string]string{from: "from", to: "to"} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("%s after conflict = %q, %v; want %q", path, data, err, want)
		}
	}

	body, _ = json.Marshal(RenameReq{From: from, To: from})
	assertJSONStatus(t, router, http.MethodPost, "/rename", body, http.StatusOK)
}

func assertJSONStatus(t *testing.T, handler http.Handler, method, target string, body []byte, want int) {
	t.Helper()
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("%s %s status = %d, body = %s; want %d", method, target, response.Code, response.Body.String(), want)
	}
}
