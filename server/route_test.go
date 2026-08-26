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
