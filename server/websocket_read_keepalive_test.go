package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nuomiiiii/lite-agent/ws"
)

func TestEmptyHandshakeIsNotTreatedAsHealthy(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		time.Sleep(websocketHandshakeAliveWait + 5*time.Second)
	}))
	t.Cleanup(server.Close)

	raw, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	conn := ws.NewSafeConn(raw)
	defer conn.Close()

	done := make(chan struct{})
	started := time.Now()
	go handleWebSocketMessages(conn, 2, done)

	select {
	case <-done:
	case <-time.After(websocketHandshakeAliveWait + 4*time.Second):
		t.Fatal("empty 101 was treated as a healthy connection")
	}
	elapsed := time.Since(started)
	if elapsed > websocketHandshakeAliveWait+4*time.Second {
		t.Fatalf("empty handshake lasted %v, want about %v", elapsed, websocketHandshakeAliveWait)
	}
}

func TestReadMessageTimesOutWhenPeerStopsPonging(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, _ = conn.ReadMessage()
		time.Sleep(websocketPongWait + 5*time.Second)
	}))
	t.Cleanup(server.Close)

	raw, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	conn := ws.NewSafeConn(raw)
	defer conn.Close()

	done := make(chan struct{})
	started := time.Now()
	go handleWebSocketMessages(conn, 2, done)

	select {
	case <-done:
	case <-time.After(websocketPongWait + 15*time.Second):
		t.Fatal("ReadMessage did not time out after the peer stopped ponging")
	}
	elapsed := time.Since(started)
	if elapsed > websocketPongWait+15*time.Second {
		t.Fatalf("silent peer lasted %v, want about %v", elapsed, websocketPongWait)
	}
}
