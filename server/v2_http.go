package server

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nuomiiiii/lite-agent/dnsresolver"
	"github.com/nuomiiiii/lite-agent/protocol/transport"
)

var httpJSONRPCGzipBlocked atomic.Bool

func resetHTTPJSONRPCGzipBlockedForTest() {
	httpJSONRPCGzipBlocked.Store(false)
}

func shouldGzipHTTPJSONRPC() bool {
	return !flags.DisableCompression && !httpJSONRPCGzipBlocked.Load()
}

func markHTTPJSONRPCGzipBlocked() {
	if httpJSONRPCGzipBlocked.CompareAndSwap(false, true) {
		log.Println("HTTP JSON-RPC gzip was rejected; later HTTP posts will skip gzip (WebSocket compression unchanged)")
	}
}

func gatewayRejectedCompressedJSON(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return false
	}
	switch trimmed[0] {
	case '{', '[':
		return false
	case '<':
		return true
	}
	lower := bytes.ToLower(trimmed)
	return bytes.Contains(lower, []byte("<html")) ||
		bytes.Contains(trimmed, []byte("json格式错误")) ||
		bytes.Contains(trimmed, []byte("请传递正确的json参数"))
}

func postV2JSONRPC(ctx context.Context, payload []byte, timeout time.Duration) (int, []byte, error) {
	compressed := shouldGzipHTTPJSONRPC()
	status, respBody, err := doV2JSONRPC(ctx, payload, timeout, compressed)
	if err != nil {
		return status, respBody, err
	}
	if compressed && gatewayRejectedCompressedJSON(respBody) {
		markHTTPJSONRPCGzipBlocked()
		return doV2JSONRPC(ctx, payload, timeout, false)
	}
	return status, respBody, nil
}

func doV2JSONRPC(ctx context.Context, payload []byte, timeout time.Duration, compress bool) (int, []byte, error) {
	body := payload
	if compress {
		gz, err := transport.GzipBytes(payload)
		if err != nil {
			return 0, nil, err
		}
		body = gz
	}
	endpoint := strings.TrimSuffix(flags.Endpoint, "/") + "/api/clients/v2/rpc"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	authorizeAgentRequest(req, flags.Token)
	if compress {
		req.Header.Set("Content-Encoding", "gzip")
	}
	client := dnsresolver.GetHTTPClientWithPreference(timeout, flags.PreferIPVersion)
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, respBody, nil
}
