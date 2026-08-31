package server

import (
	"strings"
	"testing"

	v2 "github.com/nuomiiiii/lite-agent/protocol/v2"
)

func TestResolveRouteTargetLiteralAddress(t *testing.T) {
	ip, err := resolveRouteTarget("1.1.1.1", 4)
	if err != nil || ip.String() != "1.1.1.1" {
		t.Fatalf("resolve IPv4 literal = %v, %v", ip, err)
	}
	if _, err := resolveRouteTarget("1.1.1.1", 6); err == nil {
		t.Fatal("IPv4 literal unexpectedly accepted for IPv6 task")
	}
}

func TestRouteTargetReachedMatchesHopIP(t *testing.T) {
	hops := []v2.RouteHop{
		{IP: "192.0.2.1"},
		{IP: "1.1.1.1"},
	}
	if !routeTargetReached(hops, "1.1.1.1", 4) {
		t.Fatal("expected target reached")
	}
	if routeTargetReached(hops, "8.8.8.8", 4) {
		t.Fatal("did not expect target reached")
	}
	if routeTargetReached([]v2.RouteHop{{IP: "1.1.1.1", Timeout: true}}, "1.1.1.1", 4) {
		t.Fatal("timeout hop should not count as reached")
	}
}

func TestRouteResolvedTargetLiteral(t *testing.T) {
	if got := routeResolvedTarget("1.1.1.1", 4, nil); got != "1.1.1.1" {
		t.Fatalf("resolved = %q", got)
	}
}

func TestRunRouteProbeConvertsPanicsToErrors(t *testing.T) {
	previous := routeProbeImplementation
	routeProbeImplementation = func(string, int, int) ([]v2.RouteHop, error) {
		panic("probe panic")
	}
	t.Cleanup(func() { routeProbeImplementation = previous })

	_, err := runRouteProbe("1.1.1.1", 4, 1)
	if err == nil || !strings.Contains(err.Error(), "probe panic") {
		t.Fatalf("runRouteProbe() error = %v, want recovered panic", err)
	}
}
