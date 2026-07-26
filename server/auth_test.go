package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	v2 "github.com/komari-monitor/komari-agent/protocol/v2"
)

func configureAccessTest(t *testing.T, endpoint, clientID, clientSecret string) {
	t.Helper()
	originalEndpoint := flags.Endpoint
	originalToken := flags.Token
	originalID := flags.CFAccessClientID
	originalSecret := flags.CFAccessClientSecret
	originalCompression := flags.DisableCompression
	originalRetries := flags.MaxRetries
	flags.Endpoint = endpoint
	flags.Token = "agent-token"
	flags.CFAccessClientID = clientID
	flags.CFAccessClientSecret = clientSecret
	flags.DisableCompression = true
	flags.MaxRetries = 0
	t.Cleanup(func() {
		flags.Endpoint = originalEndpoint
		flags.Token = originalToken
		flags.CFAccessClientID = originalID
		flags.CFAccessClientSecret = originalSecret
		flags.DisableCompression = originalCompression
		flags.MaxRetries = originalRetries
	})
}

func requireAccessHeaders(request *http.Request) bool {
	return request.Header.Get("Authorization") == "Bearer agent-token" &&
		request.Header.Get("CF-Access-Client-Id") == "access-id" &&
		request.Header.Get("CF-Access-Client-Secret") == "access-secret"
}

func TestAgentAuthorizationUsesHeader(t *testing.T) {
	configureAccessTest(t, "", "access-id", "access-secret")
	request, err := http.NewRequest(http.MethodGet, "https://example.com/api/clients/v2/rpc", nil)
	if err != nil {
		t.Fatal(err)
	}
	authorizeAgentRequest(request, "secret-token")
	if got := request.Header.Get("Authorization"); got != "Bearer secret-token" {
		t.Fatalf("unexpected Authorization header: %q", got)
	}
	if got := request.Header.Get("CF-Access-Client-Id"); got != "access-id" {
		t.Fatalf("unexpected Cloudflare Access client ID: %q", got)
	}
	if got := request.Header.Get("CF-Access-Client-Secret"); got != "access-secret" {
		t.Fatalf("unexpected Cloudflare Access client secret: %q", got)
	}
	if request.URL.RawQuery != "" {
		t.Fatalf("agent credential leaked into query: %q", request.URL.RawQuery)
	}
}

func TestRemoteHeadersPreserveAllAuthenticationLayers(t *testing.T) {
	configureAccessTest(t, "", "access-id", "access-secret")

	terminalHeaders := terminalAuthorizationHeader("agent-token", "terminal-session")
	if terminalHeaders.Get("X-Komari-Terminal-Session") != "terminal-session" || !headersContainAccessCredentials(terminalHeaders) {
		t.Fatalf("terminal headers are incomplete: %#v", terminalHeaders)
	}
	remoteHeaders := remoteAuthorizationHeader("agent-token", "remote-session", "remote-ticket")
	if remoteHeaders.Get("X-Komari-Remote-Session") != "remote-session" ||
		remoteHeaders.Get("X-Komari-Remote-Ticket") != "remote-ticket" ||
		!headersContainAccessCredentials(remoteHeaders) {
		t.Fatalf("remote headers are incomplete: %#v", remoteHeaders)
	}
}

func headersContainAccessCredentials(headers http.Header) bool {
	return headers.Get("Authorization") == "Bearer agent-token" &&
		headers.Get("CF-Access-Client-Id") == "access-id" &&
		headers.Get("CF-Access-Client-Secret") == "access-secret"
}

func TestCloudflareAccessProtectedWebSocket(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !requireAccessHeaders(request) {
			http.Error(writer, "Cloudflare Access denied", http.StatusForbidden)
			return
		}
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		_ = connection.Close()
	}))
	t.Cleanup(server.Close)

	websocketEndpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/clients/v2/rpc"
	configureAccessTest(t, server.URL, "", "")
	if connection, err := connectWebSocket(websocketEndpoint); err == nil {
		_ = connection.Close()
		t.Fatal("WebSocket unexpectedly passed Cloudflare Access without service-token headers")
	}

	flags.CFAccessClientID = "access-id"
	flags.CFAccessClientSecret = "access-secret"
	connection, err := connectWebSocket(websocketEndpoint)
	if err != nil {
		t.Fatalf("WebSocket with both authentication layers failed: %v", err)
	}
	_ = connection.Close()
}

func TestCloudflareAccessProtectedAgentHTTPFlows(t *testing.T) {
	seen := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !requireAccessHeaders(request) {
			http.Error(writer, "Cloudflare Access denied", http.StatusForbidden)
			return
		}
		if request.URL.RawQuery != "" {
			t.Errorf("credential-bearing query remains on %s: %q", request.URL.Path, request.URL.RawQuery)
		}
		seen[request.URL.Path]++
		writer.WriteHeader(http.StatusOK)
		if request.URL.Path == "/api/clients/v2/rpc" {
			_ = json.NewEncoder(writer).Encode(v2.Response{JSONRPC: v2.Version, ID: "access-test", Result: map[string]string{"status": "ok"}})
		}
	}))
	t.Cleanup(server.Close)
	configureAccessTest(t, server.URL, "", "")

	if err := tryUploadDataWithProtocol(map[string]interface{}{"name": "test"}, 1); err == nil {
		t.Fatal("HTTP request unexpectedly passed Cloudflare Access without service-token headers")
	}
	flags.CFAccessClientID = "access-id"
	flags.CFAccessClientSecret = "access-secret"

	if err := tryUploadDataWithProtocol(map[string]interface{}{"name": "test"}, 1); err != nil {
		t.Fatalf("v1 basic info failed: %v", err)
	}
	if err := tryUploadDataWithProtocol(map[string]interface{}{"name": "test"}, 2); err != nil {
		t.Fatalf("v2 basic info failed: %v", err)
	}
	uploadTaskResult("task-id", "ok", 0, time.Now())
	if err := postV2RPC(v2.Request{JSONRPC: v2.Version, ID: "task", Method: v2.MethodAgentTaskResult}); err != nil {
		t.Fatalf("v2 task result failed: %v", err)
	}
	if _, err := postV2Request(v2.NewRequest("access-test", v2.MethodAgentPull, nil)); err != nil {
		t.Fatalf("v2 POST fallback failed: %v", err)
	}

	if seen["/api/clients/uploadBasicInfo"] != 1 {
		t.Fatalf("v1 basic info requests = %d", seen["/api/clients/uploadBasicInfo"])
	}
	if seen["/api/clients/task/result"] != 1 {
		t.Fatalf("v1 task-result requests = %d", seen["/api/clients/task/result"])
	}
	if seen["/api/clients/v2/rpc"] != 3 {
		t.Fatalf("v2 POST requests = %d", seen["/api/clients/v2/rpc"])
	}
}

func TestWebSocketEndpointDoesNotContainAgentToken(t *testing.T) {
	originalEndpoint, originalToken := flags.Endpoint, flags.Token
	flags.Endpoint, flags.Token = "https://example.com", "secret-token"
	t.Cleanup(func() {
		flags.Endpoint, flags.Token = originalEndpoint, originalToken
	})

	for _, protocolVersion := range []int{1, 2} {
		endpoint := buildWebSocketEndpoint(protocolVersion)
		parsed, err := url.Parse(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.RawQuery != "" {
			t.Fatalf("v%d websocket URL contains query data: %q", protocolVersion, parsed.RawQuery)
		}
		if endpoint == "" || parsed.Host != "example.com" {
			t.Fatalf("unexpected websocket endpoint: %q", endpoint)
		}
	}
}
