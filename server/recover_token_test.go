package server

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nuomiiiii/lite-agent/tasklog"
)

func TestSetTaskLogDoesNotStartRecoveryBeforeToken(t *testing.T) {
	resetTaskRecoveryForTest()
	path := filepath.Join(t.TempDir(), "lite-agent-task-log.json")
	first, err := tasklog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Begin("task-wait"); err != nil {
		t.Fatal(err)
	}
	store, err := tasklog.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	originalToken := flags.Token
	originalSleep := recoverMinSleep
	originalReconnect := flags.ReconnectInterval
	flags.Token = ""
	flags.ReconnectInterval = 0
	recoverMinSleep = 20 * time.Millisecond
	t.Cleanup(func() {
		flags.Token = originalToken
		flags.ReconnectInterval = originalReconnect
		recoverMinSleep = originalSleep
		resetTaskRecoveryForTest()
		execLog = nil
	})

	var uploads atomic.Int32
	stubTaskUploads(t, func(string, string, int, time.Time, string) bool {
		uploads.Add(1)
		return true
	})
	SetTaskLog(store)
	time.Sleep(80 * time.Millisecond)
	if uploads.Load() != 0 {
		t.Fatal("SetTaskLog started recovery before identity was ready")
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	StartTaskRecovery(ctx)
	time.Sleep(80 * time.Millisecond)
	if uploads.Load() != 0 {
		t.Fatal("recovery uploaded before Token was set")
	}

	flags.Token = "ready-token"
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if uploads.Load() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if uploads.Load() == 0 {
		t.Fatal("recovery did not resume after Token was ready")
	}
	cancel()
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		entry, ok := store.Lookup("task-wait")
		if ok && entry.Acked {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("successful recovery did not ack")
}
