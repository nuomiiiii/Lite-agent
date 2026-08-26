package hostguard

import (
	"testing"
	"time"
)

func TestEndpointTargetsLocalAddress(t *testing.T) {
	if !endpointTargetsLocalAddress("http://127.0.0.1:25774") {
		t.Fatal("loopback Lite endpoint was not recognized as local")
	}
	if endpointTargetsLocalAddress("not a URL") {
		t.Fatal("invalid endpoint was recognized as local")
	}
}

func TestEndpointTargetsReportedAddress(t *testing.T) {
	reported := map[string]struct{}{
		"203.0.113.10": {},
		"2001:db8::10": {},
	}
	for _, endpoint := range []string{
		"https://203.0.113.10:25774",
		"https://[2001:db8:0:0::10]:25774",
	} {
		if !endpointTargetsReportedAddress(endpoint, reported) {
			t.Fatalf("reported address was not matched for %q", endpoint)
		}
	}
	if endpointTargetsReportedAddress("https://198.51.100.20:25774", reported) {
		t.Fatal("unrelated endpoint was matched to a reported address")
	}
}

func TestSetReportedAddressesInvalidatesDetectionCache(t *testing.T) {
	detectionCache.Lock()
	previousAddresses := detectionCache.reportedAddresses
	previousCheckedAt := detectionCache.checkedAt
	detectionCache.reportedAddresses = map[string]struct{}{"192.0.2.1": {}}
	detectionCache.checkedAt = time.Now()
	detectionCache.Unlock()
	t.Cleanup(func() {
		detectionCache.Lock()
		detectionCache.reportedAddresses = previousAddresses
		detectionCache.checkedAt = previousCheckedAt
		detectionCache.Unlock()
	})

	SetReportedAddresses("203.0.113.10", "invalid", "2001:0db8::10")

	detectionCache.Lock()
	defer detectionCache.Unlock()
	if !detectionCache.checkedAt.IsZero() {
		t.Fatal("changing reported addresses did not invalidate the detection cache")
	}
	for _, address := range []string{"203.0.113.10", "2001:db8::10"} {
		if _, exists := detectionCache.reportedAddresses[address]; !exists {
			t.Fatalf("normalized address %q was not recorded", address)
		}
	}
	if len(detectionCache.reportedAddresses) != 2 {
		t.Fatalf("invalid reported address was retained: %#v", detectionCache.reportedAddresses)
	}

	checkedAt := time.Now()
	detectionCache.checkedAt = checkedAt
	detectionCache.Unlock()
	SetReportedAddresses("", "invalid")
	detectionCache.Lock()
	if !detectionCache.checkedAt.Equal(checkedAt) || len(detectionCache.reportedAddresses) != 2 {
		t.Fatal("empty lookup result cleared the last known reported addresses")
	}
}

func TestKomariServerProcessRecognition(t *testing.T) {
	for _, name := range []string{"lite", "Lite.exe", "lite-server", "komari", "Komari.exe", "komari-server", "KOMARI-SERVER.EXE"} {
		if !isKomariServerProcess(name) {
			t.Fatalf("expected %q to be recognized as the panel process", name)
		}
	}
	for _, name := range []string{"lite-agent", "lite-agent.exe", "Lite-agent", "Lite-agent.exe", "komari-agent", "komari-agent.exe", "docker", "other-komari"} {
		if isKomariServerProcess(name) {
			t.Fatalf("did not expect %q to be recognized as Lite Server", name)
		}
	}
}

func TestKomariDockerContainerRecognition(t *testing.T) {
	tests := []struct {
		name      string
		container dockerProcess
		want      bool
	}{
		{name: "lite image", container: dockerProcess{Image: "ghcr.io/nuomiiiii/lite:2.2.3", Names: "lite"}, want: true},
		{name: "fork image", container: dockerProcess{Image: "ghcr.io/nuomiiiii/komari:2.1.5", Names: "komari"}, want: true},
		{name: "upstream image", container: dockerProcess{Image: "ghcr.io/komari-monitor/komari:latest", Names: "monitor"}, want: true},
		{name: "digest image", container: dockerProcess{Image: "ghcr.io/nuomiiiii/komari@sha256:abc", Names: "panel"}, want: true},
		{name: "local image", container: dockerProcess{Image: "local-build", Names: "komari-server-main"}, want: true},
		{name: "agent", container: dockerProcess{Image: "ghcr.io/nuomiiiii/komari-agent:latest", Names: "komari-agent"}, want: false},
		{name: "lite agent image", container: dockerProcess{Image: "ghcr.io/nuomiiiii/lite-agent:latest", Names: "Lite-agent"}, want: false},
		{name: "unrelated docker app", container: dockerProcess{Image: "postgres:17", Names: "database"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isKomariServerContainer(test.container); got != test.want {
				t.Fatalf("isKomariServerContainer(%#v) = %v, want %v", test.container, got, test.want)
			}
		})
	}
}

func TestDetectKomariServerAllowsRemoteControlOnServerHost(t *testing.T) {
	previous := serverHostDetector
	serverHostDetector = func() bool { return true }
	t.Cleanup(func() { serverHostDetector = previous })

	if reason := detectKomariServer("http://127.0.0.1:25774"); reason != "" {
		t.Fatalf("server host was still blocked: %q", reason)
	}
}

func TestDetectKomariServerKeepsProtectionForOtherNodes(t *testing.T) {
	previous := serverHostDetector
	serverHostDetector = func() bool { return false }
	t.Cleanup(func() { serverHostDetector = previous })

	if reason := detectKomariServer("http://127.0.0.1:25774"); reason == "" {
		t.Fatal("non-server node lost loopback endpoint protection")
	}
}
