package server

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	v2 "github.com/nuomiiiii/lite-agent/protocol/v2"
)

func TestParseMCPDeadlineUsesEarliestValue(t *testing.T) {
	lease := time.Now().UTC().Add(15 * time.Second).Format(time.RFC3339Nano)
	operation := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339Nano)
	got := parseMCPDeadline(lease, operation)
	want, err := time.Parse(time.RFC3339Nano, lease)
	if err != nil {
		t.Fatal(err)
	}
	if got.IsZero() || !got.Equal(want) {
		t.Fatalf("deadline = %v, want %v", got, want)
	}
}

func TestMCPExecContextUsesOperationDeadline(t *testing.T) {
	lease := time.Now().UTC().Add(15 * time.Second).Format(time.RFC3339Nano)
	operation := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339Nano)
	params := v2.MCPExecParams{
		ExecutionLeaseDeadline: lease,
		OperationDeadline:      operation,
	}
	got := parseMCPDeadline(params.OperationDeadline, params.ExpiresAt)
	want, err := time.Parse(time.RFC3339Nano, operation)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(want) {
		t.Fatalf("run deadline = %v, want operation deadline %v", got, want)
	}
}

func TestLimitedWriterDrainsAfterCap(t *testing.T) {
	writer := newLimitedWriter(8)
	n, err := writer.Write([]byte("abcdefghijklmnop"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 16 {
		t.Fatalf("wrote %d, want 16 so the pipe keeps draining", n)
	}
	got := string(writer.bytes())
	if !strings.HasSuffix(got, "\n[truncated]") {
		t.Fatalf("output = %q, want truncated marker", got)
	}
}

func TestRenewMCPLeaseExtendsHold(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	registerMCPRun("op_renew", "ls_renew", cancel, time.Now().UTC().Add(time.Minute))
	t.Cleanup(func() {
		unregisterMCPRun("op_renew")
		expireMCPLease("ls_renew")
	})
	later := time.Now().UTC().Add(2 * time.Minute)
	renewMCPLease("ls_renew", later)
	mcpRunMu.Lock()
	hold := mcpLeases["ls_renew"]
	deadline := time.Time{}
	if hold != nil {
		deadline = hold.deadline
	}
	mcpRunMu.Unlock()
	if deadline.Before(later.Add(-time.Second)) {
		t.Fatalf("lease deadline = %v, want around %v", deadline, later)
	}
}

func TestRevokeMCPLeaseCancelsRun(t *testing.T) {
	var cancelled atomic.Bool
	_, cancel := context.WithCancel(context.Background())
	registerMCPRun("op_revoke", "ls_revoke", func() {
		cancelled.Store(true)
		cancel()
	}, time.Now().UTC().Add(time.Minute))
	t.Cleanup(func() {
		unregisterMCPRun("op_revoke")
	})
	revokeMCPLease("ls_revoke")
	if !cancelled.Load() {
		t.Fatal("revoke did not cancel the running operation")
	}
}

func TestRevokeMCPLeaseRejectsLaterRegister(t *testing.T) {
	var cancelled atomic.Bool
	revokeMCPLease("ls_poison")
	t.Cleanup(func() {
		mcpRunMu.Lock()
		delete(mcpRevoked, "ls_poison")
		delete(mcpRuns, "op_late")
		mcpRunMu.Unlock()
	})
	_, cancel := context.WithCancel(context.Background())
	registerMCPRun("op_late", "ls_poison", func() {
		cancelled.Store(true)
		cancel()
	}, time.Now().UTC().Add(time.Hour))
	mcpRunMu.Lock()
	_, exists := mcpRuns["op_late"]
	mcpRunMu.Unlock()
	if exists {
		t.Fatal("revoked lease should not accept a new run")
	}
	if !cancelled.Load() {
		t.Fatal("register after revoke should cancel the new context")
	}
	run, ack := acceptMCPFile(v2.MCPFileParams{
		TaskID:  "op_late_file",
		LeaseID: "ls_poison",
		Request: []byte(`{"type":"file.write","id":"w","path":"/tmp/x","content":"x"}`),
	})
	if run || !ack {
		t.Fatalf("accept after revoke run=%v ack=%v", run, ack)
	}
}

func TestCancelBeforeRegisterRejectsLaterRun(t *testing.T) {
	var cancelled atomic.Bool
	cancelMCPOperation("op_cancel_first")
	t.Cleanup(func() {
		mcpRunMu.Lock()
		delete(mcpCancelled, "op_cancel_first")
		delete(mcpRuns, "op_cancel_first")
		mcpRunMu.Unlock()
	})
	_, cancel := context.WithCancel(context.Background())
	registerMCPRun("op_cancel_first", "ls_cancel_first", func() {
		cancelled.Store(true)
		cancel()
	}, time.Now().UTC().Add(time.Hour))
	mcpRunMu.Lock()
	_, exists := mcpRuns["op_cancel_first"]
	mcpRunMu.Unlock()
	if exists {
		t.Fatal("cancelled operation should not register later")
	}
	if !cancelled.Load() {
		t.Fatal("register after cancel should cancel the new context")
	}
}
