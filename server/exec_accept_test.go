package server

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	v2 "github.com/nuomiiiii/lite-agent/protocol/v2"
	"github.com/nuomiiiii/lite-agent/tasklog"
)

func TestProcessV2ExecBeginFailureDoesNotRunOrAck(t *testing.T) {
	resetSeenEventsForTest(t)
	store, err := tasklog.Open(filepath.Join(t.TempDir(), "lite-agent-task-log.json"))
	if err != nil {
		t.Fatal(err)
	}
	attachTaskLogForTest(t, store)
	stubTaskUploads(t, nil)
	store.FailNextSave(errors.New("disk full"))

	marker := filepath.Join(t.TempDir(), "ran.txt")
	acked := processV2Event(nil, v2.MethodAgentExec, map[string]string{
		"task_id": "task-begin-fail",
		"command": "Set-Content -Path '" + marker + "' -Value ran",
	}, "event-begin-fail")
	if acked {
		t.Fatal("Begin persist failure must not ACK")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("command ran after Begin failure")
	}
	if !markV2EventSeen("event-begin-fail") {
		t.Fatal("failed Begin must forget the in-memory event mark")
	}
}

func TestProcessV2ExecDuplicateTaskIDAcksWithoutRerun(t *testing.T) {
	resetSeenEventsForTest(t)
	store, err := tasklog.Open(filepath.Join(t.TempDir(), "lite-agent-task-log.json"))
	if err != nil {
		t.Fatal(err)
	}
	attachTaskLogForTest(t, store)
	stubTaskUploads(t, nil)
	if _, err := store.Begin("task-once"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Finish("task-once", "done", 0); err != nil {
		t.Fatal(err)
	}
	if err := store.Ack("task-once"); err != nil {
		t.Fatal(err)
	}

	marker := filepath.Join(t.TempDir(), "ran.txt")
	acked := processV2Event(nil, v2.MethodAgentExec, map[string]string{
		"task_id": "task-once",
		"command": "Set-Content -Path '" + marker + "' -Value ran",
	}, "event-dup")
	if !acked {
		t.Fatal("already finished task must ACK")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("duplicate task_id was executed")
	}
}

func TestProcessV2ExecLogFullDoesNotAck(t *testing.T) {
	resetSeenEventsForTest(t)
	store, err := tasklog.Open(filepath.Join(t.TempDir(), "lite-agent-task-log.json"))
	if err != nil {
		t.Fatal(err)
	}
	attachTaskLogForTest(t, store)
	stubTaskUploads(t, nil)
	if _, err := store.Begin("task-keep"); err != nil {
		t.Fatal(err)
	}
	store.SetLimitsForTest(time.Hour, 1)
	acked := processV2Event(nil, v2.MethodAgentExec, map[string]string{
		"task_id": "task-log-full",
		"command": "hostname",
	}, "event-full")
	if acked {
		t.Fatal("full task log must not ACK a new exec")
	}
	if !markV2EventSeen("event-full") {
		t.Fatal("full log must forget the in-memory event mark")
	}
}

func TestFinishUploadsWhenPersistFails(t *testing.T) {
	store, err := tasklog.Open(filepath.Join(t.TempDir(), "lite-agent-task-log.json"))
	if err != nil {
		t.Fatal(err)
	}
	attachTaskLogForTest(t, store)
	originalAttempts := finishPersistMaxAttempts
	finishPersistMaxAttempts = 1
	originalDelay := finishRetryDelay
	finishRetryDelay = func(int) time.Duration { return 0 }
	t.Cleanup(func() {
		finishPersistMaxAttempts = originalAttempts
		finishRetryDelay = originalDelay
	})
	if _, err := store.Begin("task-finish"); err != nil {
		t.Fatal(err)
	}
	store.FailNextSave(errors.New("disk full"))
	var uploads int
	var gotResult, gotStatus string
	stubTaskUploads(t, func(_ string, result string, _ int, _ time.Time, status string) bool {
		uploads++
		gotResult = result
		gotStatus = status
		return true
	})
	finishTask("task-finish", "hello", 0)
	if uploads != 1 || gotResult != "hello" || gotStatus != v2.TaskResultStatusFinished {
		t.Fatalf("persist failure should still upload finished result: uploads=%d result=%q status=%q", uploads, gotResult, gotStatus)
	}
	entry, _ := store.Lookup("task-finish")
	if entry.State != tasklog.StateStarted || entry.Acked {
		t.Fatalf("state after failed Finish = %#v", entry)
	}
}
