package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

const (
	mcpFileSessionIdle = 10 * time.Minute
	mcpFileSessionMax  = 16
)

type mcpFileWriter struct {
	mu sync.Mutex
	ch chan any
}

func (writer *mcpFileWriter) writeJSON(value any) error {
	writer.mu.Lock()
	ch := writer.ch
	writer.mu.Unlock()
	if ch == nil {
		return nil
	}
	select {
	case ch <- value:
	default:
	}
	return nil
}

type mcpFileSession struct {
	mu      sync.Mutex
	manager *fileManager
	writer  *mcpFileWriter
	last    time.Time
}

var (
	mcpFileMu       sync.Mutex
	mcpFileSessions = map[string]*mcpFileSession{}
)

func init() {
	go pruneMCPFileSessions()
}

func ExecuteMCPFile(payload []byte) (any, error) {
	return ExecuteMCPFileForLease(context.Background(), "", payload)
}

// ExecuteMCPFileForLease runs one file-manager request, keeping upload/download
// state for this lease until it is idle, revoked, or expired.
func ExecuteMCPFileForLease(ctx context.Context, leaseID string, payload []byte) (any, error) {
	if len(payload) == 0 {
		return nil, errors.New("invalid file request")
	}
	var probe struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil || probe.ID == "" || !isFileMessage(probe.Type) {
		return nil, errors.New("invalid file request")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	session := getMCPFileSession(leaseID)
	if err := lockMCPFileSession(ctx, session); err != nil {
		return nil, err
	}
	defer session.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	session.last = time.Now()
	ch := make(chan any, 4)
	session.writer.mu.Lock()
	session.writer.ch = ch
	session.writer.mu.Unlock()
	session.manager.handleCtx(ctx, payload)
	select {
	case value := <-ch:
		return value, nil
	case <-ctx.Done():
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("file operation cancelled")
	case <-time.After(30 * time.Second):
		return nil, errors.New("file operation timed out")
	}
}

func lockMCPFileSession(ctx context.Context, session *mcpFileSession) error {
	locked := make(chan struct{})
	go func() {
		session.mu.Lock()
		select {
		case locked <- struct{}{}:
		case <-ctx.Done():
			session.mu.Unlock()
		}
	}()
	select {
	case <-locked:
		return nil
	case <-ctx.Done():
		if err := ctx.Err(); err != nil {
			return err
		}
		return errors.New("file operation cancelled")
	}
}

func CloseMCPFileSession(leaseID string) {
	leaseID = mcpFileLeaseKey(leaseID)
	mcpFileMu.Lock()
	session := mcpFileSessions[leaseID]
	delete(mcpFileSessions, leaseID)
	mcpFileMu.Unlock()
	if session != nil && session.manager != nil {
		session.manager.close()
	}
}

func getMCPFileSession(leaseID string) *mcpFileSession {
	leaseID = mcpFileLeaseKey(leaseID)
	mcpFileMu.Lock()
	defer mcpFileMu.Unlock()
	if session := mcpFileSessions[leaseID]; session != nil {
		session.last = time.Now()
		return session
	}
	if len(mcpFileSessions) >= mcpFileSessionMax {
		dropOldestMCPFileSessionLocked()
	}
	writer := &mcpFileWriter{}
	session := &mcpFileSession{
		manager: newFileManager(writer),
		writer:  writer,
		last:    time.Now(),
	}
	mcpFileSessions[leaseID] = session
	return session
}

func dropOldestMCPFileSessionLocked() {
	var oldestID string
	var oldest time.Time
	for id, session := range mcpFileSessions {
		if oldestID == "" || session.last.Before(oldest) {
			oldestID = id
			oldest = session.last
		}
	}
	if oldestID == "" {
		return
	}
	session := mcpFileSessions[oldestID]
	delete(mcpFileSessions, oldestID)
	if session != nil && session.manager != nil {
		go session.manager.close()
	}
}

func pruneMCPFileSessions() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-mcpFileSessionIdle)
		mcpFileMu.Lock()
		var expired []*fileManager
		for id, session := range mcpFileSessions {
			if session.last.Before(cutoff) {
				expired = append(expired, session.manager)
				delete(mcpFileSessions, id)
			}
		}
		mcpFileMu.Unlock()
		for _, manager := range expired {
			if manager != nil {
				manager.close()
			}
		}
	}
}

func mcpFileLeaseKey(leaseID string) string {
	leaseID = strings.TrimSpace(leaseID)
	if leaseID == "" {
		return "_"
	}
	return leaseID
}
