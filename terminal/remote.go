package terminal

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nuomiiiii/lite-agent/hostguard"
)

type remoteWriter struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (writer *remoteWriter) writeMessage(messageType int, data []byte) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.conn.WriteMessage(messageType, data)
}

func (writer *remoteWriter) writeJSON(value any) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.conn.WriteJSON(value)
}

func StartRemoteSession(conn *websocket.Conn) {
	writer := &remoteWriter{conn: conn}
	if flags.DisableWebSsh {
		_ = writer.writeJSON(map[string]any{"type": "remote.error", "message": "Remote control is disabled on this agent"})
		return
	}
	if reason := hostguard.RemoteControlBlockedReason(flags.Endpoint); reason != "" {
		_ = writer.writeJSON(map[string]any{"type": "remote.error", "message": reason})
		return
	}
	impl, err := newTerminalImpl()
	if err != nil {
		_ = writer.writeJSON(map[string]any{"type": "remote.error", "message": fmt.Sprintf("Failed to start terminal: %v", err)})
		return
	}
	manager := newFileManager(writer)
	done := make(chan struct{})
	errCh := make(chan error, 2)
	defer func() {
		close(done)
		manager.close()
		gracefulShutdown(impl.term)
		_ = impl.term.Close()
	}()

	_ = writer.writeJSON(map[string]any{
		"type":      "remote.ready",
		"roots":     filesystemRoots(),
		"home":      userHomeDirectory(),
		"separator": pathSeparator(),
	})

	go handleRemoteTerminalOutput(writer, impl.term, errCh, done)
	go func() {
		for {
			messageType, payload, err := conn.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			switch messageType {
			case websocket.BinaryMessage:
				if _, err := impl.term.Write(payload); err != nil {
					errCh <- err
					return
				}
			case websocket.TextMessage:
				var command struct {
					Type string `json:"type"`
					Cols int    `json:"cols,omitempty"`
					Rows int    `json:"rows,omitempty"`
				}
				if err := json.Unmarshal(payload, &command); err != nil {
					continue
				}
				switch command.Type {
				case "resize":
					if command.Cols > 0 && command.Rows > 0 {
						_ = impl.term.Resize(command.Cols, command.Rows)
					}
				case "heartbeat":
					_ = writer.writeJSON(map[string]any{"type": "heartbeat"})
				default:
					if isFileMessage(command.Type) {
						manager.handle(payload)
					}
				}
			}
		}
	}()
	guardTicker := time.NewTicker(15 * time.Second)
	defer guardTicker.Stop()
	for {
		select {
		case <-errCh:
			return
		case <-guardTicker.C:
			if reason := hostguard.RemoteControlBlockedReason(flags.Endpoint); reason != "" {
				_ = writer.writeJSON(map[string]any{"type": "remote.error", "message": reason})
				return
			}
		}
	}
}

func handleRemoteTerminalOutput(writer *remoteWriter, term Terminal, errCh chan<- error, done <-chan struct{}) {
	buffer := make([]byte, 4096)
	for {
		select {
		case <-done:
			return
		default:
		}
		count, err := term.Read(buffer)
		if err == nil && count > 0 {
			err = writer.writeMessage(websocket.BinaryMessage, buffer[:count])
		}
		if err != nil {
			select {
			case errCh <- err:
			default:
			}
			return
		}
	}
}
