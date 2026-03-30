package lsp

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var serverCmds = map[string][]string{
	"go":         {"gopls", "serve"},
	"typescript": {"typescript-language-server", "--stdio"},
	"javascript": {"typescript-language-server", "--stdio"},
	"python":     {"pylsp"},
	"solidity":   {"nomicfoundation-solidity-language-server", "--stdio"},
	"rust":       {"rust-analyzer"},
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "https://tutor.abcfe.net" ||
			origin == "https://clinic.abcfe.net" ||
			strings.HasPrefix(origin, "http://localhost:") ||
			strings.HasPrefix(origin, "http://127.0.0.1:")
	},
}

// ServeWS handles GET /ws/lsp?lang=go&root=/abs/path&token=<jwt>
func ServeWS(c *gin.Context) {
	lang := c.Query("lang")
	rootPath := c.Query("root")
	if lang == "" || rootPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lang and root are required"})
		return
	}

	cmdArgs, ok := serverCmds[lang]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported language: " + lang})
		return
	}

	// Resolve language server binary — check PATH first, then common Go/npm bin dirs
	resolvedBin, err := resolveBin(cmdArgs[0])
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "language server not installed: " + cmdArgs[0]})
		return
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("lsp: ws upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Start language server process
	args := append([]string{resolvedBin}, cmdArgs[1:]...)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = rootPath

	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Printf("lsp: stdin pipe error: %v", err)
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("lsp: stdout pipe error: %v", err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Printf("lsp: stderr pipe error: %v", err)
		return
	}

	if err := cmd.Start(); err != nil {
		log.Printf("lsp: start error: %v", err)
		return
	}
	defer func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}()

	log.Printf("lsp: started %s for lang=%s root=%s", cmdArgs[0], lang, rootPath)

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("lsp %s: %s", cmdArgs[0], scanner.Text())
		}
	}()

	// WS → LS stdin (goroutine)
	wsToLS := make(chan error, 1)
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				wsToLS <- err
				return
			}
			// Wrap JSON with Content-Length header for LSP protocol
			header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(msg))
			if _, err := io.WriteString(stdin, header); err != nil {
				wsToLS <- err
				return
			}
			if _, err := stdin.Write(msg); err != nil {
				wsToLS <- err
				return
			}
		}
	}()

	// LS stdout → WS (goroutine)
	lsToWS := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(stdout)
		for {
			// Parse Content-Length header
			contentLength := -1
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					lsToWS <- err
					return
				}
				line = strings.TrimRight(line, "\r\n")
				if line == "" {
					break // End of headers
				}
				if strings.HasPrefix(line, "Content-Length:") {
					val := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
					contentLength, _ = strconv.Atoi(val)
				}
			}

			if contentLength < 0 {
				continue
			}

			// Read exactly contentLength bytes
			body := make([]byte, contentLength)
			if _, err := io.ReadFull(reader, body); err != nil {
				lsToWS <- err
				return
			}

			if err := conn.WriteMessage(websocket.TextMessage, body); err != nil {
				lsToWS <- err
				return
			}
		}
	}()

	// Wait for either side to close
	select {
	case err := <-wsToLS:
		log.Printf("lsp: ws→ls closed: %v", err)
	case err := <-lsToWS:
		log.Printf("lsp: ls→ws closed: %v", err)
	}
}

// resolveBin finds a binary by checking PATH and then common install locations.
func resolveBin(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}

	// Common extra directories where language servers are often installed
	home, _ := os.UserHomeDir()
	goBin := filepath.Join(home, "go", "bin")
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		goBin = filepath.Join(gopath, "bin")
	}

	candidates := []string{
		filepath.Join(goBin, name),                   // Go tools (gopls, etc.)
		filepath.Join(home, ".local", "bin", name),    // pip --user installs
		"/usr/local/bin/" + name,
		"/opt/homebrew/bin/" + name,                   // Homebrew on Apple Silicon
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("%s not found", name)
}
