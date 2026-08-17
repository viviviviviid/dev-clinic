package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"syscall"

	"github.com/coding-tutor/internal/config"
	"github.com/coding-tutor/internal/pathguard"
	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

type resizeMsg struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

func ServeTerminal(w http.ResponseWriter, r *http.Request) {
	requestedDir := r.URL.Query().Get("dir")
	if requestedDir == "" {
		requestedDir = config.Global.BaseDir
	}
	dir, err := pathguard.Resolve(config.Global.BaseDir, requestedDir)
	if err != nil {
		http.Error(w, "invalid terminal directory", http.StatusForbidden)
		return
	}
	log.Printf("terminal: new session dir=%s", dir)

	conn, _, err := Upgrade(w, r)
	if err != nil {
		log.Printf("terminal: ws upgrade failed: %v", err)
		return
	}
	conn.SetReadLimit(1 << 20)
	defer conn.Close()

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	log.Printf("terminal: starting shell=%s", shell)

	cmd := exec.Command(shell)
	cmd.Env = append(SanitizedEnv(os.Environ()), "TERM=xterm-256color")
	cmd.Dir = dir

	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Printf("terminal: pty start failed: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte("PTY 시작 실패: "+err.Error()))
		return
	}
	log.Printf("terminal: pty started")
	defer func() {
		ptmx.Close()
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			_ = cmd.Process.Kill()
		}
	}()

	// PTY → WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		conn.Close()
	}()

	// WebSocket → PTY
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if msgType == websocket.TextMessage {
			// resize message
			var resize resizeMsg
			if json.Unmarshal(data, &resize) == nil && resize.Cols > 0 {
				pty.Setsize(ptmx, &pty.Winsize{Cols: resize.Cols, Rows: resize.Rows})
				continue
			}
		}
		ptmx.Write(data)
	}
}
