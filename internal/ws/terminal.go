package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

type resizeMsg struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

func ServeTerminal(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = os.Getenv("HOME")
	}
	log.Printf("terminal: new session dir=%s", dir)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("terminal: ws upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	log.Printf("terminal: starting shell=%s", shell)

	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
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
		cmd.Process.Kill()
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
