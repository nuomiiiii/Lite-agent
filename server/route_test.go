package server

import "testing"

func TestResolveRouteTargetLiteralAddress(t *testing.T) {
	ip, err := resolveRouteTarget("1.1.1.1", 4)
	if err != nil || ip.String() != "1.1.1.1" {
		t.Fatalf("resolve IPv4 literal = %v, %v", ip, err)
	}
	if _, err := resolveRouteTarget("1.1.1.1", 6); err == nil {
		t.Fatal("IPv4 literal unexpectedly accepted for IPv6 task")
	}
}
