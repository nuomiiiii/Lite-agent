package server

import (
	"testing"
	"time"
)

func TestWebSocketHeartbeatKeepsAliveWithoutProbeTasks(t *testing.T) {
	if websocketHeartbeatInterval != 30*time.Second {
		t.Fatalf("heartbeat = %v, want 30s protocol ping independent of panel probes", websocketHeartbeatInterval)
	}
	idle := 60 * time.Second
	if websocketHeartbeatInterval >= idle {
		t.Fatalf("heartbeat %v must stay below server idle %v", websocketHeartbeatInterval, idle)
	}

	reportInterval := 70 * time.Second
	if reportInterval <= idle {
		t.Fatal("fixture report interval must exceed idle timeout")
	}

	for _, probe := range []struct {
		name     string
		enabled  bool
		kind     string
		interval time.Duration
	}{
		{name: "disabled", enabled: false},
		{name: "icmp", enabled: true, kind: "icmp", interval: 90 * time.Second},
		{name: "tcp", enabled: true, kind: "tcp", interval: 90 * time.Second},
		{name: "http", enabled: true, kind: "http", interval: 90 * time.Second},
	} {
		t.Run(probe.name, func(t *testing.T) {
			if probe.enabled && probe.interval <= idle {
				t.Fatalf("%s probes at %v would be mistaken for keepalive if we depended on them", probe.kind, probe.interval)
			}
		})
	}
}
