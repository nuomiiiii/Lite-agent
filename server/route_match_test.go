package server

import (
	"net"
	"testing"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

func TestMatchIPv4RouteReplyIgnoresUnrelatedAndLatePackets(t *testing.T) {
	dest := net.ParseIP("203.0.113.8").To4()
	id, seq := 42, 5

	matched, reached := matchIPv4RouteReply(mustMarshalICMP(t, ipv4.ICMPTypeEchoReply, &icmp.Echo{ID: id, Seq: seq, Data: []byte("komari-route")}), dest.String(), dest, id, seq)
	if !matched || !reached {
		t.Fatal("matching echo reply should be accepted")
	}

	matched, _ = matchIPv4RouteReply(mustMarshalICMP(t, ipv4.ICMPTypeEchoReply, &icmp.Echo{ID: id + 1, Seq: seq, Data: []byte("komari-route")}), dest.String(), dest, id, seq)
	if matched {
		t.Fatal("echo reply with a different ID must be ignored")
	}

	matched, _ = matchIPv4RouteReply(mustMarshalICMP(t, ipv4.ICMPTypeEchoReply, &icmp.Echo{ID: id, Seq: seq - 1, Data: []byte("komari-route")}), dest.String(), dest, id, seq)
	if matched {
		t.Fatal("late packet from a previous TTL must be ignored")
	}

	matched, _ = matchIPv4RouteReply(mustMarshalICMP(t, ipv4.ICMPTypeEchoReply, &icmp.Echo{ID: id, Seq: seq, Data: []byte("komari-route")}), "198.51.100.1", dest, id, seq)
	if matched {
		t.Fatal("echo reply from another host must be ignored")
	}

	inner := append(ipv4TestHeader(net.ParseIP("198.51.100.1"), dest), mustMarshalICMP(t, ipv4.ICMPTypeEcho, &icmp.Echo{ID: id, Seq: seq, Data: []byte("komari-route")})...)
	matched, reached = matchIPv4RouteReply(mustMarshalICMP(t, ipv4.ICMPTypeTimeExceeded, &icmp.TimeExceeded{Data: inner}), "198.51.100.1", dest, id, seq)
	if !matched || reached {
		t.Fatalf("matching time exceeded = matched=%v reached=%v", matched, reached)
	}

	wrongDest := append(ipv4TestHeader(net.ParseIP("198.51.100.1"), net.ParseIP("203.0.113.9")), mustMarshalICMP(t, ipv4.ICMPTypeEcho, &icmp.Echo{ID: id, Seq: seq, Data: []byte("komari-route")})...)
	matched, _ = matchIPv4RouteReply(mustMarshalICMP(t, ipv4.ICMPTypeTimeExceeded, &icmp.TimeExceeded{Data: wrongDest}), "198.51.100.1", dest, id, seq)
	if matched {
		t.Fatal("time exceeded for another destination must be ignored")
	}
}

func TestMatchIPv6RouteReplyIgnoresUnrelatedAndLatePackets(t *testing.T) {
	dest := net.ParseIP("2001:db8::8")
	id, seq := 7, 3

	matched, reached := matchIPv6RouteReply(mustMarshalICMP(t, ipv6.ICMPTypeEchoReply, &icmp.Echo{ID: id, Seq: seq, Data: []byte("komari-route")}), dest.String(), dest, id, seq)
	if !matched || !reached {
		t.Fatal("matching IPv6 echo reply should be accepted")
	}

	matched, _ = matchIPv6RouteReply(mustMarshalICMP(t, ipv6.ICMPTypeEchoReply, &icmp.Echo{ID: id, Seq: seq - 1, Data: []byte("komari-route")}), dest.String(), dest, id, seq)
	if matched {
		t.Fatal("IPv6 late packet from a previous hop must be ignored")
	}

	inner := append(ipv6TestHeader(net.ParseIP("2001:db8::1"), dest), mustMarshalICMP(t, ipv6.ICMPTypeEchoRequest, &icmp.Echo{ID: id, Seq: seq, Data: []byte("komari-route")})...)
	matched, reached = matchIPv6RouteReply(mustMarshalICMP(t, ipv6.ICMPTypeTimeExceeded, &icmp.TimeExceeded{Data: inner}), "2001:db8::1", dest, id, seq)
	if !matched || reached {
		t.Fatalf("matching IPv6 time exceeded = matched=%v reached=%v", matched, reached)
	}

	wrongID := append(ipv6TestHeader(net.ParseIP("2001:db8::1"), dest), mustMarshalICMP(t, ipv6.ICMPTypeEchoRequest, &icmp.Echo{ID: id + 8, Seq: seq, Data: []byte("other")})...)
	matched, _ = matchIPv6RouteReply(mustMarshalICMP(t, ipv6.ICMPTypeTimeExceeded, &icmp.TimeExceeded{Data: wrongID}), "2001:db8::1", dest, id, seq)
	if matched {
		t.Fatal("IPv6 time exceeded with a different echo ID must be ignored")
	}
}

func mustMarshalICMP(t *testing.T, typ icmp.Type, body icmp.MessageBody) []byte {
	t.Helper()
	payload, err := (&icmp.Message{Type: typ, Code: 0, Body: body}).Marshal(nil)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func ipv4TestHeader(src, dest net.IP) []byte {
	header := make([]byte, 20)
	header[0] = 0x45
	header[9] = 1
	copy(header[12:16], src.To4())
	copy(header[16:20], dest.To4())
	return header
}

func ipv6TestHeader(src, dest net.IP) []byte {
	header := make([]byte, 40)
	header[0] = 0x60
	header[6] = 58
	copy(header[8:24], src.To16())
	copy(header[24:40], dest.To16())
	return header
}
