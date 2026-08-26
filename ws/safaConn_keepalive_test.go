package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestReadTimesOutWithoutPong(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		time.Sleep(70 * time.Second)
	}))
	t.Cleanup(server.Close)

	raw, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	conn := NewSafeConn(raw)
	defer conn.Close()

	if err := conn.AttachReadKeepalive(60 * time.Second); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	_, _, readErr := conn.ReadMessage()
	elapsed := time.Since(started)
	if readErr == nil {
		t.Fatal("ReadMessage returned nil error against a silent peer")
	}
	if elapsed > 70*time.Second {
		t.Fatalf("ReadMessage blocked %v, want about 60s", elapsed)
	}
}

func TestPongExtendsReadDeadline(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	raw, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	conn := NewSafeConn(raw)
	defer conn.Close()

	if err := conn.AttachReadKeepalive(60 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
		t.Fatal(err)
	}

	readErr := make(chan error, 1)
	go func() {
		_, _, err := conn.ReadMessage()
		readErr <- err
	}()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	deadline := time.After(70 * time.Second)
	for {
		select {
		case err := <-readErr:
			if err != nil {
				t.Fatalf("read failed while the peer was ponging: %v", err)
			}
			t.Fatal("read returned without a data frame")
		case <-ticker.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				t.Fatalf("ping failed: %v", err)
			}
		case <-deadline:
			return
		}
	}
}
