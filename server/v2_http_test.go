package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestGatewayRejectedCompressedJSON(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want bool
	}{
		{name: "html json error", body: "<html><meta charset=\"utf-8\" /><title>json格式错误</title><div>请传递正确的json参数</div></html>\n", want: true},
		{name: "json unauthorized", body: `{"status":"error","message":"Unauthorized."}`, want: false},
		{name: "jsonrpc", body: `{"jsonrpc":"2.0","result":{}}`, want: false},
		{name: "empty", body: "  ", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := gatewayRejectedCompressedJSON([]byte(tc.body)); got != tc.want {
				t.Fatalf("gatewayRejectedCompressedJSON(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

func TestPostV2JSONRPCFallsBackWhenGzipRejectedAsHTML(t *testing.T) {
	resetHTTPJSONRPCGzipBlockedForTest()
	t.Cleanup(resetHTTPJSONRPCGzipBlockedForTest)

	originalEndpoint := flags.Endpoint
	originalToken := flags.Token
	originalCompression := flags.DisableCompression
	flags.DisableCompression = false
	flags.Token = "agent-token"
	t.Cleanup(func() {
		flags.Endpoint = originalEndpoint
		flags.Token = originalToken
		flags.DisableCompression = originalCompression
		resetHTTPJSONRPCGzipBlockedForTest()
	})

	var gzipHits, plainHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Encoding") == "gzip" {
			gzipHits.Add(1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><meta charset="utf-8" /><title>json格式错误</title><div>请传递正确的json参数</div></html>` + "\n"))
			return
		}
		plainHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{}}`))
	}))
	t.Cleanup(server.Close)
	flags.Endpoint = server.URL

	status, body, err := postV2JSONRPC(context.Background(), []byte(`{"jsonrpc":"2.0","method":"agent.basicInfo"}`), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if !bytes.Contains(body, []byte(`"jsonrpc"`)) {
		t.Fatalf("body = %s", body)
	}
	if gzipHits.Load() != 1 || plainHits.Load() != 1 {
		t.Fatalf("first post gzip=%d plain=%d", gzipHits.Load(), plainHits.Load())
	}

	status, _, err = postV2JSONRPC(context.Background(), []byte(`{"jsonrpc":"2.0","method":"agent.basicInfo"}`), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("second status = %d", status)
	}
	if gzipHits.Load() != 1 || plainHits.Load() != 2 {
		t.Fatalf("sticky gzip=%d plain=%d, want gzip=1 plain=2", gzipHits.Load(), plainHits.Load())
	}
}

func TestPostV2JSONRPCDoesNotFallbackOnJSONUnauthorized(t *testing.T) {
	resetHTTPJSONRPCGzipBlockedForTest()
	t.Cleanup(resetHTTPJSONRPCGzipBlockedForTest)

	originalEndpoint := flags.Endpoint
	originalToken := flags.Token
	originalCompression := flags.DisableCompression
	flags.DisableCompression = false
	flags.Token = "agent-token"
	t.Cleanup(func() {
		flags.Endpoint = originalEndpoint
		flags.Token = originalToken
		flags.DisableCompression = originalCompression
		resetHTTPJSONRPCGzipBlockedForTest()
	})

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status":"error","message":"Unauthorized."}`))
	}))
	t.Cleanup(server.Close)
	flags.Endpoint = server.URL

	status, body, err := postV2JSONRPC(context.Background(), []byte(`{"jsonrpc":"2.0"}`), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d", status)
	}
	if !bytes.Contains(body, []byte("Unauthorized")) {
		t.Fatalf("body = %s", body)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, JSON error must not retry uncompressed", hits.Load())
	}
	if httpJSONRPCGzipBlocked.Load() {
		t.Fatal("JSON unauthorized must not disable HTTP gzip")
	}
}
