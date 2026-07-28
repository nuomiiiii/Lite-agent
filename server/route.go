package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	v2 "github.com/komari-monitor/komari-agent/protocol/v2"
	"github.com/komari-monitor/komari-agent/ws"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const routeHopTimeout = 900 * time.Millisecond

var routeProbeMu sync.Mutex

func NewRouteTask(conn *ws.SafeConn, task v2.RouteParams) {
	if task.TaskID == 0 || strings.TrimSpace(task.Target) == "" {
		return
	}
	if task.MaxHops < 1 || task.MaxHops > 64 {
		task.MaxHops = 30
	}
	if task.IPVersion != 4 && task.IPVersion != 6 {
		task.IPVersion = 4
	}
	routeProbeMu.Lock()
	hops, err := traceRouteICMP(task.Target, task.IPVersion, task.MaxHops)
	routeProbeMu.Unlock()
	finishedAt := time.Now()
	errText := ""
	if err != nil {
		errText = err.Error()
		log.Printf("Return route task %d failed: %v", task.TaskID, err)
	}
	payload := v2.BuildRouteResultPayload(task, hops, errText, finishedAt)
	if conn == nil {
		if err := postV2RPC(payload); err != nil {
			log.Printf("Failed to upload return route result over POST: %v", err)
		}
		return
	}
	if err := conn.WriteJSON(payload); err != nil {
		log.Printf("Failed to upload return route result: %v", err)
	}
}

func resolveRouteTarget(target string, version int) (net.IP, error) {
	if host, _, err := net.SplitHostPort(target); err == nil {
		target = host
	}
	if ip := net.ParseIP(strings.Trim(target, "[]")); ip != nil {
		if (version == 4 && ip.To4() != nil) || (version == 6 && ip.To4() == nil) {
			return ip, nil
		}
		return nil, fmt.Errorf("target does not have the requested IPv%d address", version)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIP(ctx, "ip", target)
	if err != nil {
		return nil, fmt.Errorf("resolve target: %w", err)
	}
	for _, ip := range addresses {
		if (version == 4 && ip.To4() != nil) || (version == 6 && ip.To4() == nil) {
			return ip, nil
		}
	}
	return nil, fmt.Errorf("target does not have the requested IPv%d address", version)
}

func traceRouteICMP(target string, version, maxHops int) ([]v2.RouteHop, error) {
	destination, err := resolveRouteTarget(target, version)
	if err != nil {
		return nil, err
	}
	if version == 6 {
		return traceRouteICMPv6(destination, maxHops)
	}
	return traceRouteICMPv4(destination, maxHops)
}

func traceRouteICMPv4(destination net.IP, maxHops int) ([]v2.RouteHop, error) {
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return nil, fmt.Errorf("open built-in ICMP route probe (root/CAP_NET_RAW may be required): %w", err)
	}
	defer conn.Close()
	packet := ipv4.NewPacketConn(conn)
	hops := make([]v2.RouteHop, 0, maxHops)
	id := os.Getpid() & 0xffff
	for ttl := 1; ttl <= maxHops; ttl++ {
		if err := packet.SetTTL(ttl); err != nil {
			return hops, fmt.Errorf("set IPv4 TTL: %w", err)
		}
		message := icmp.Message{Type: ipv4.ICMPTypeEcho, Code: 0, Body: &icmp.Echo{ID: id, Seq: ttl, Data: []byte("komari-route")}}
		payload, err := message.Marshal(nil)
		if err != nil {
			return hops, err
		}
		start := time.Now()
		_ = conn.SetDeadline(start.Add(routeHopTimeout))
		if _, err := conn.WriteTo(payload, &net.IPAddr{IP: destination}); err != nil {
			return hops, fmt.Errorf("send IPv4 route probe: %w", err)
		}
		buffer := make([]byte, 1500)
		n, peer, err := conn.ReadFrom(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				hops = append(hops, v2.RouteHop{TTL: ttl, Timeout: true})
				continue
			}
			return hops, fmt.Errorf("read IPv4 route probe: %w", err)
		}
		reply, err := icmp.ParseMessage(1, buffer[:n])
		if err != nil {
			hops = append(hops, v2.RouteHop{TTL: ttl, Timeout: true})
			continue
		}
		ip := routePeerIP(peer)
		hops = append(hops, v2.RouteHop{TTL: ttl, IP: ip, LatencyMS: float64(time.Since(start).Microseconds()) / 1000})
		if reply.Type == ipv4.ICMPTypeEchoReply || net.ParseIP(ip).Equal(destination) {
			break
		}
	}
	return hops, nil
}

func traceRouteICMPv6(destination net.IP, maxHops int) ([]v2.RouteHop, error) {
	conn, err := icmp.ListenPacket("ip6:ipv6-icmp", "::")
	if err != nil {
		return nil, fmt.Errorf("open built-in IPv6 ICMP route probe (root/CAP_NET_RAW may be required): %w", err)
	}
	defer conn.Close()
	packet := ipv6.NewPacketConn(conn)
	hops := make([]v2.RouteHop, 0, maxHops)
	id := os.Getpid() & 0xffff
	for ttl := 1; ttl <= maxHops; ttl++ {
		if err := packet.SetHopLimit(ttl); err != nil {
			return hops, fmt.Errorf("set IPv6 hop limit: %w", err)
		}
		message := icmp.Message{Type: ipv6.ICMPTypeEchoRequest, Code: 0, Body: &icmp.Echo{ID: id, Seq: ttl, Data: []byte("komari-route")}}
		payload, err := message.Marshal(nil)
		if err != nil {
			return hops, err
		}
		start := time.Now()
		_ = conn.SetDeadline(start.Add(routeHopTimeout))
		if _, err := conn.WriteTo(payload, &net.IPAddr{IP: destination}); err != nil {
			return hops, fmt.Errorf("send IPv6 route probe: %w", err)
		}
		buffer := make([]byte, 1500)
		n, peer, err := conn.ReadFrom(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				hops = append(hops, v2.RouteHop{TTL: ttl, Timeout: true})
				continue
			}
			return hops, fmt.Errorf("read IPv6 route probe: %w", err)
		}
		reply, err := icmp.ParseMessage(58, buffer[:n])
		if err != nil {
			hops = append(hops, v2.RouteHop{TTL: ttl, Timeout: true})
			continue
		}
		ip := routePeerIP(peer)
		hops = append(hops, v2.RouteHop{TTL: ttl, IP: ip, LatencyMS: float64(time.Since(start).Microseconds()) / 1000})
		if reply.Type == ipv6.ICMPTypeEchoReply || net.ParseIP(ip).Equal(destination) {
			break
		}
	}
	return hops, nil
}

func routePeerIP(addr net.Addr) string {
	switch value := addr.(type) {
	case *net.IPAddr:
		return value.IP.String()
	case *net.UDPAddr:
		return value.IP.String()
	default:
		return strings.Split(addr.String(), "%")[0]
	}
}
