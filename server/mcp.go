package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	pkg_flags "github.com/nuomiiiii/lite-agent/cmd/flags"
	v2 "github.com/nuomiiiii/lite-agent/protocol/v2"
	"github.com/nuomiiiii/lite-agent/terminal"
)

const mcpOutputLimit = 16 << 20

type mcpRun struct {
	cancel  context.CancelFunc
	leaseID string
	expired bool
}

type mcpLeaseHold struct {
	deadline time.Time
	cancels  map[string]context.CancelFunc
}

const (
	mcpRevokeMemoryTTL = 2 * time.Hour
	mcpRevokeMemoryMax = 4096
	mcpCancelMemoryTTL = 30 * time.Minute
)

var (
	mcpRunMu     sync.Mutex
	mcpRuns      = map[string]*mcpRun{}
	mcpLeases    = map[string]*mcpLeaseHold{}
	mcpRevoked   = map[string]time.Time{}
	mcpCancelled = map[string]time.Time{}
)

func parseMCPDeadline(values ...string) time.Time {
	var earliest time.Time
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, value)
		}
		if err != nil || parsed.IsZero() {
			continue
		}
		if earliest.IsZero() || parsed.Before(earliest) {
			earliest = parsed.UTC()
		}
	}
	return earliest
}

func acceptMCPExec(params v2.MCPExecParams) (run bool, ack bool) {
	taskID := strings.TrimSpace(params.TaskID)
	if taskID == "" {
		taskID = strings.TrimSpace(params.OperationID)
	}
	if taskID == "" || strings.TrimSpace(params.Command) == "" {
		return false, false
	}
	if !pkg_flags.RemoteControlEnabled() {
		finishTask(taskID, "remote control is disabled.", -1)
		return false, true
	}
	startBy := parseMCPDeadline(params.ExecutionLeaseDeadline)
	if !startBy.IsZero() && !time.Now().UTC().Before(startBy) {
		finishTask(taskID, "MCP execution lease has expired", -1)
		return false, true
	}
	if mcpLeaseRevoked(params.LeaseID) {
		finishTask(taskID, "MCP execution lease has expired", -1)
		return false, true
	}
	if mcpOperationCancelled(taskID) {
		finishTask(taskID, "cancelled", 130)
		return false, true
	}
	return acceptTask(taskID)
}

func executeMCPExec(params v2.MCPExecParams) {
	taskID := strings.TrimSpace(params.TaskID)
	if taskID == "" {
		taskID = strings.TrimSpace(params.OperationID)
	}
	if !pkg_flags.RemoteControlEnabled() {
		finishTask(taskID, "remote control is disabled.", -1)
		return
	}
	if len(params.Command) > 64<<10 {
		finishTask(taskID, "Command is too long", -1)
		return
	}
	if rejectMCPRun(taskID, params.LeaseID) {
		return
	}
	deadline := parseMCPDeadline(params.OperationDeadline, params.ExpiresAt)
	timeout := 5 * time.Minute
	if !deadline.IsZero() {
		remain := time.Until(deadline)
		if remain <= 0 {
			finishTask(taskID, "MCP execution deadline exceeded", -1)
			return
		}
		timeout = remain
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	registerMCPRun(taskID, params.LeaseID, cancel, parseMCPDeadline(params.ExecutionLeaseDeadline, params.OperationDeadline, params.ExpiresAt))
	defer func() {
		cancel()
		unregisterMCPRun(taskID)
	}()

	result, exitCode := runMCPCommand(ctx, params.Command, params.Cwd)
	if errors.Is(ctx.Err(), context.Canceled) {
		if mcpRunExpired(taskID) {
			finishTask(taskID, "MCP execution lease has expired", 124)
			return
		}
		finishTask(taskID, "cancelled", 130)
		return
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		finishTask(taskID, "MCP execution deadline exceeded", 124)
		return
	}
	finishTask(taskID, result, exitCode)
}

func acceptMCPFile(params v2.MCPFileParams) (run bool, ack bool) {
	taskID := firstNonEmpty(params.TaskID, params.OperationID)
	if taskID == "" || len(bytes.TrimSpace(params.Request)) == 0 {
		return false, false
	}
	if !pkg_flags.RemoteControlEnabled() {
		finishTask(taskID, "remote control is disabled.", -1)
		return false, true
	}
	startBy := parseMCPDeadline(params.ExecutionLeaseDeadline, params.OperationDeadline, params.ExpiresAt)
	if !startBy.IsZero() && !time.Now().UTC().Before(startBy) {
		finishTask(taskID, "MCP file deadline exceeded", -1)
		return false, true
	}
	if mcpLeaseRevoked(params.LeaseID) {
		finishTask(taskID, "MCP execution lease has expired", -1)
		return false, true
	}
	if mcpOperationCancelled(taskID) {
		finishTask(taskID, "cancelled", 130)
		return false, true
	}
	return acceptTask(taskID)
}

func executeMCPFile(params v2.MCPFileParams) {
	taskID := firstNonEmpty(params.TaskID, params.OperationID)
	if taskID == "" {
		return
	}
	if !pkg_flags.RemoteControlEnabled() {
		finishTask(taskID, "remote control is disabled.", -1)
		return
	}
	payload := bytes.TrimSpace(params.Request)
	if len(payload) == 0 {
		finishTask(taskID, "invalid file request", -1)
		return
	}
	if rejectMCPRun(taskID, params.LeaseID) {
		return
	}
	deadline := parseMCPDeadline(params.OperationDeadline, params.ExpiresAt)
	timeout := 45 * time.Second
	if !deadline.IsZero() {
		remain := time.Until(deadline)
		if remain <= 0 {
			finishTask(taskID, "MCP file deadline exceeded", -1)
			return
		}
		timeout = remain
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	registerMCPRun(taskID, params.LeaseID, cancel, parseMCPDeadline(params.ExecutionLeaseDeadline, params.OperationDeadline, params.ExpiresAt))
	defer func() {
		cancel()
		unregisterMCPRun(taskID)
	}()
	result, err := terminal.ExecuteMCPFileForLease(ctx, params.LeaseID, payload)
	if errors.Is(ctx.Err(), context.Canceled) {
		if mcpRunExpired(taskID) {
			finishTask(taskID, "MCP execution lease has expired", 124)
			return
		}
		finishTask(taskID, "cancelled", 130)
		return
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		finishTask(taskID, "MCP file deadline exceeded", 124)
		return
	}
	if err != nil {
		finishTask(taskID, err.Error(), -1)
		return
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		finishTask(taskID, err.Error(), -1)
		return
	}
	finishTask(taskID, string(encoded), 0)
}

func cancelMCPOperation(operationID string) bool {
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		return false
	}
	mcpRunMu.Lock()
	rememberCancelledLocked(operationID)
	run := mcpRuns[operationID]
	mcpRunMu.Unlock()
	if run == nil || run.cancel == nil {
		return true
	}
	run.cancel()
	return true
}

func renewMCPLease(leaseID string, deadline time.Time) {
	leaseID = strings.TrimSpace(leaseID)
	if leaseID == "" {
		return
	}
	mcpRunMu.Lock()
	defer mcpRunMu.Unlock()
	if leaseRevokedLocked(leaseID) {
		return
	}
	hold := mcpLeases[leaseID]
	if hold == nil {
		return
	}
	if deadline.IsZero() {
		return
	}
	if hold.deadline.IsZero() || deadline.After(hold.deadline) {
		hold.deadline = deadline.UTC()
	}
}

func revokeMCPLease(leaseID string) {
	leaseID = strings.TrimSpace(leaseID)
	if leaseID == "" {
		return
	}
	mcpRunMu.Lock()
	until := time.Now().UTC().Add(mcpRevokeMemoryTTL)
	if hold := mcpLeases[leaseID]; hold != nil && hold.deadline.After(until) {
		until = hold.deadline
	}
	rememberRevokedLocked(leaseID, until)
	mcpRunMu.Unlock()
	expireMCPLease(leaseID)
	terminal.CloseMCPFileSession(leaseID)
}

func rejectMCPRun(taskID, leaseID string) bool {
	if mcpLeaseRevoked(leaseID) {
		finishTask(taskID, "MCP execution lease has expired", -1)
		return true
	}
	if mcpOperationCancelled(taskID) {
		finishTask(taskID, "cancelled", 130)
		return true
	}
	return false
}

func registerMCPRun(taskID, leaseID string, cancel context.CancelFunc, leaseDeadline time.Time) {
	taskID = strings.TrimSpace(taskID)
	leaseID = strings.TrimSpace(leaseID)
	if taskID == "" || cancel == nil {
		return
	}
	mcpRunMu.Lock()
	if leaseRevokedLocked(leaseID) || operationCancelledLocked(taskID) {
		mcpRunMu.Unlock()
		cancel()
		return
	}
	mcpRuns[taskID] = &mcpRun{cancel: cancel, leaseID: leaseID}
	if leaseID != "" {
		hold := mcpLeases[leaseID]
		if hold == nil {
			hold = &mcpLeaseHold{cancels: map[string]context.CancelFunc{}}
			mcpLeases[leaseID] = hold
			go watchMCPLease(leaseID)
		}
		hold.cancels[taskID] = cancel
		if !leaseDeadline.IsZero() && (hold.deadline.IsZero() || leaseDeadline.After(hold.deadline)) {
			hold.deadline = leaseDeadline.UTC()
		}
	}
	mcpRunMu.Unlock()
}

func unregisterMCPRun(taskID string) {
	taskID = strings.TrimSpace(taskID)
	mcpRunMu.Lock()
	run := mcpRuns[taskID]
	delete(mcpRuns, taskID)
	if run != nil && run.leaseID != "" {
		if hold := mcpLeases[run.leaseID]; hold != nil {
			delete(hold.cancels, taskID)
		}
	}
	mcpRunMu.Unlock()
}

func mcpRunExpired(taskID string) bool {
	mcpRunMu.Lock()
	defer mcpRunMu.Unlock()
	run := mcpRuns[strings.TrimSpace(taskID)]
	return run != nil && run.expired
}

func watchMCPLease(leaseID string) {
	for {
		mcpRunMu.Lock()
		hold := mcpLeases[leaseID]
		if hold == nil || len(hold.cancels) == 0 {
			delete(mcpLeases, leaseID)
			mcpRunMu.Unlock()
			return
		}
		deadline := hold.deadline
		mcpRunMu.Unlock()
		if deadline.IsZero() {
			time.Sleep(time.Second)
			continue
		}
		remain := time.Until(deadline)
		if remain <= 0 {
			expireMCPLease(leaseID)
			return
		}
		if remain > time.Second {
			remain = time.Second
		}
		time.Sleep(remain)
	}
}

func expireMCPLease(leaseID string) {
	mcpRunMu.Lock()
	hold := mcpLeases[leaseID]
	delete(mcpLeases, leaseID)
	var cancels []context.CancelFunc
	if hold != nil {
		for id, cancel := range hold.cancels {
			if run := mcpRuns[id]; run != nil {
				run.expired = true
			}
			if cancel != nil {
				cancels = append(cancels, cancel)
			}
		}
	}
	mcpRunMu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func runMCPCommand(ctx context.Context, command, cwd string) (string, int) {
	cmd, cleanup, err := buildTaskCommand(command)
	if err != nil {
		return err.Error(), -1
	}
	defer cleanup()
	if trimmed := strings.TrimSpace(cwd); trimmed != "" {
		cmd.Dir = trimmed
	}
	setMCPSysProcAttr(cmd)

	stdout := newLimitedWriter(mcpOutputLimit)
	stderr := newLimitedWriter(mcpOutputLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return err.Error(), -1
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	select {
	case <-ctx.Done():
		killMCPProcess(cmd)
		waitErr = <-done
		if ctx.Err() != nil && waitErr == nil {
			waitErr = ctx.Err()
		}
	case waitErr = <-done:
	}

	result := decodeCommandOutput(stdout.bytes())
	if stderr.len() > 0 {
		result = appendErrorResult(result, decodeCommandOutput(stderr.bytes()))
	}
	result = strings.ReplaceAll(result, "\r\n", "\n")
	exitCode := 0
	if waitErr != nil {
		if exitError, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else if errors.Is(waitErr, context.Canceled) {
			exitCode = 130
			result = appendErrorResult(result, "cancelled")
		} else if errors.Is(waitErr, context.DeadlineExceeded) {
			exitCode = 124
			result = appendErrorResult(result, waitErr.Error())
		} else {
			result = appendErrorResult(result, waitErr.Error())
			exitCode = -1
		}
	}
	return result, exitCode
}

type limitedWriter struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func newLimitedWriter(limit int) *limitedWriter {
	return &limitedWriter{limit: limit}
}

func (writer *limitedWriter) Write(p []byte) (int, error) {
	if writer.limit <= 0 {
		return len(p), nil
	}
	remain := writer.limit - writer.buf.Len()
	if remain <= 0 {
		writer.truncated = true
		return len(p), nil
	}
	if len(p) > remain {
		writer.truncated = true
		_, _ = writer.buf.Write(p[:remain])
		return len(p), nil
	}
	return writer.buf.Write(p)
}

func (writer *limitedWriter) bytes() []byte {
	raw := writer.buf.Bytes()
	if !writer.truncated {
		return raw
	}
	trimmed := append([]byte(nil), raw...)
	for len(trimmed) > 0 && trimmed[len(trimmed)-1]&0xc0 == 0x80 {
		trimmed = trimmed[:len(trimmed)-1]
	}
	return append(trimmed, []byte("\n[truncated]")...)
}

func (writer *limitedWriter) len() int {
	return writer.buf.Len()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func mcpLeaseRevoked(leaseID string) bool {
	mcpRunMu.Lock()
	defer mcpRunMu.Unlock()
	return leaseRevokedLocked(leaseID)
}

func mcpOperationCancelled(taskID string) bool {
	mcpRunMu.Lock()
	defer mcpRunMu.Unlock()
	return operationCancelledLocked(taskID)
}

func rememberRevokedLocked(leaseID string, until time.Time) {
	pruneRevokedLocked(time.Now().UTC())
	if leaseID == "" {
		return
	}
	if until.IsZero() {
		until = time.Now().UTC().Add(mcpRevokeMemoryTTL)
	}
	mcpRevoked[leaseID] = until
	for len(mcpRevoked) > mcpRevokeMemoryMax {
		var oldest string
		var when time.Time
		for id, expiry := range mcpRevoked {
			if oldest == "" || expiry.Before(when) {
				oldest = id
				when = expiry
			}
		}
		delete(mcpRevoked, oldest)
	}
}

func rememberCancelledLocked(taskID string) {
	now := time.Now().UTC()
	for id, until := range mcpCancelled {
		if !until.After(now) {
			delete(mcpCancelled, id)
		}
	}
	if taskID == "" {
		return
	}
	mcpCancelled[taskID] = now.Add(mcpCancelMemoryTTL)
}

func leaseRevokedLocked(leaseID string) bool {
	leaseID = strings.TrimSpace(leaseID)
	if leaseID == "" {
		return false
	}
	until, ok := mcpRevoked[leaseID]
	if !ok {
		return false
	}
	if !until.After(time.Now().UTC()) {
		delete(mcpRevoked, leaseID)
		return false
	}
	return true
}

func operationCancelledLocked(taskID string) bool {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return false
	}
	until, ok := mcpCancelled[taskID]
	if !ok {
		return false
	}
	if !until.After(time.Now().UTC()) {
		delete(mcpCancelled, taskID)
		return false
	}
	return true
}

func pruneRevokedLocked(now time.Time) {
	for id, until := range mcpRevoked {
		if !until.After(now) {
			delete(mcpRevoked, id)
		}
	}
}

var _ io.Writer = (*limitedWriter)(nil)
