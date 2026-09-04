package server

import (
	"context"
	"reflect"
	"testing"
)

func TestV2PullCapabilitiesIncludeRemoteNotTerminal(t *testing.T) {
	hasRemote, hasTerminal := false, false
	for _, capability := range v2PullCapabilities {
		if capability == "remote" {
			hasRemote = true
		}
		if capability == "terminal" {
			hasTerminal = true
		}
	}
	if !hasRemote {
		t.Fatal("pull capabilities must include remote")
	}
	if hasTerminal {
		t.Fatal("pull capabilities must not include terminal")
	}
	for _, required := range []string{"files", "exec", "ping", "route", "message", "event", "config"} {
		found := false
		for _, capability := range v2PullCapabilities {
			if capability == required {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("pull capabilities missing %q: %v", required, v2PullCapabilities)
		}
	}
}

func TestRunV2PullLoopHasNoErrCh(t *testing.T) {
	fn := reflect.TypeOf(runV2PullLoop)
	if fn.NumIn() != 1 {
		t.Fatalf("runV2PullLoop should only take context, got %d params", fn.NumIn())
	}
	if fn.In(0) != reflect.TypeOf((*context.Context)(nil)).Elem() {
		t.Fatalf("runV2PullLoop argument = %s, want context.Context", fn)
	}
}

func TestRemoteWebSocketReadLimitIs2MiB(t *testing.T) {
	if remoteWebSocketReadLimit != 2<<20 {
		t.Fatalf("remote WebSocket read limit = %d, want 2MiB", remoteWebSocketReadLimit)
	}
}
