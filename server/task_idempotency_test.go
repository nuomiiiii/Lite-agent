package server

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nuomiiiii/lite-agent/tasklog"
)

func attachTaskLogForTest(t *testing.T, store *tasklog.Log) {
	t.Helper()
	execLog = store
	t.Cleanup(func() { execLog = nil })
}

func stubTaskUploads(t *testing.T, upload taskResultUploader) {
	t.Helper()
	original := uploadTaskResultFn
	if upload == nil {
		upload = func(string, string, int, time.Time, string) bool { return true }
	}
	uploadTaskResultFn = upload
	t.Cleanup(func() { uploadTaskResultFn = original })
}

func TestNewTaskDoesNotRerunKnownTaskIDs(t *testing.T) {
	store, err := tasklog.Open(filepath.Join(t.TempDir(), "lite-agent-task-log.json"))
	if err != nil {
		t.Fatal(err)
	}
	attachTaskLogForTest(t, store)
	stubTaskUploads(t, nil)

	marker := filepath.Join(t.TempDir(), "ran.txt")
	command := "Set-Content -Path '" + marker + "' -Value ran"

	if _, err := store.Begin("task-dup"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Finish("task-dup", "already finished", 0); err != nil {
		t.Fatal(err)
	}
	NewTask("task-dup", command)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("finished task_id was executed again")
	}

	if _, err := store.Begin("task-started"); err != nil {
		t.Fatal(err)
	}
	NewTask("task-started", command)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("started task_id was executed again")
	}
}

func TestNewTaskAlreadyStartedDoesNotUploadUnknown(t *testing.T) {
	store, err := tasklog.Open(filepath.Join(t.TempDir(), "lite-agent-task-log.json"))
	if err != nil {
		t.Fatal(err)
	}
	attachTaskLogForTest(t, store)

	var uploads []string
	stubTaskUploads(t, func(taskID, result string, exitCode int, finishedAt time.Time, status string) bool {
		uploads = append(uploads, result)
		return true
	})
	if _, err := store.Begin("task-started"); err != nil {
		t.Fatal(err)
	}
	NewTask("task-started", "hostname")
	if len(uploads) != 0 {
		t.Fatalf("already-started task uploaded %#v", uploads)
	}
}

func TestNewTaskFinishedAckedIsIgnored(t *testing.T) {
	store, err := tasklog.Open(filepath.Join(t.TempDir(), "lite-agent-task-log.json"))
	if err != nil {
		t.Fatal(err)
	}
	attachTaskLogForTest(t, store)

	var uploads int
	stubTaskUploads(t, func(string, string, int, time.Time, string) bool {
		uploads++
		return true
	})
	if _, err := store.Begin("task-done"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Finish("task-done", "original result", 0); err != nil {
		t.Fatal(err)
	}
	if err := store.Ack("task-done"); err != nil {
		t.Fatal(err)
	}
	NewTask("task-done", "hostname")
	if uploads != 0 {
		t.Fatalf("acked finished task retransmitted %d times", uploads)
	}
}

func TestNewTaskFinishedUnackedRetransmitsOriginal(t *testing.T) {
	store, err := tasklog.Open(filepath.Join(t.TempDir(), "lite-agent-task-log.json"))
	if err != nil {
		t.Fatal(err)
	}
	attachTaskLogForTest(t, store)

	var results []string
	stubTaskUploads(t, func(taskID, result string, exitCode int, finishedAt time.Time, status string) bool {
		results = append(results, result)
		return true
	})
	if _, err := store.Begin("task-retry"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Finish("task-retry", "original result", 7); err != nil {
		t.Fatal(err)
	}
	NewTask("task-retry", "hostname")
	if len(results) != 1 || results[0] != "original result" {
		t.Fatalf("retransmit = %#v", results)
	}
	entry, _ := store.Lookup("task-retry")
	if !entry.Acked {
		t.Fatal("successful retransmit did not ack")
	}
}

func TestNewTaskInterruptedUnackedUploadsUnknownWithoutRerun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lite-agent-task-log.json")
	first, err := tasklog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Begin("task-crash"); err != nil {
		t.Fatal(err)
	}
	store, err := tasklog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	attachTaskLogForTest(t, store)

	var results []string
	stubTaskUploads(t, func(taskID, result string, exitCode int, finishedAt time.Time, status string) bool {
		results = append(results, result)
		return true
	})
	marker := filepath.Join(t.TempDir(), "ran.txt")
	NewTask("task-crash", "Set-Content -Path '"+marker+"' -Value ran")
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("interrupted task_id was executed again")
	}
	if len(results) != 1 || results[0] != "execution status unknown" {
		t.Fatalf("interrupted upload = %#v", results)
	}
	entry, _ := store.Lookup("task-crash")
	if !entry.Acked {
		t.Fatal("interrupted upload did not ack")
	}
}

func TestNewTaskConcurrentSameTaskIDStartsOnce(t *testing.T) {
	store, err := tasklog.Open(filepath.Join(t.TempDir(), "lite-agent-task-log.json"))
	if err != nil {
		t.Fatal(err)
	}
	attachTaskLogForTest(t, store)
	stubTaskUploads(t, nil)

	originalRemote := flags.RemoteControlEnabled
	flags.RemoteControlEnabled = true
	t.Cleanup(func() { flags.RemoteControlEnabled = originalRemote })

	marker := filepath.Join(t.TempDir(), "ran.txt")
	command := "Add-Content -Path '" + marker + "' -Value ran"
	var wg sync.WaitGroup
	wg.Add(8)
	for i := 0; i < 8; i++ {
		go func() {
			defer wg.Done()
			NewTask("shared-event-task", command)
		}()
	}
	wg.Wait()
	entry, ok := store.Lookup("shared-event-task")
	if !ok {
		t.Fatal("shared task was not recorded")
	}
	if entry.State != tasklog.StateFinished && entry.State != tasklog.StateStarted {
		t.Fatalf("shared task state = %s", entry.State)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, line := range splitLines(string(data)) {
		if line != "" {
			lines++
		}
	}
	if lines != 1 {
		t.Fatalf("command executions = %d, want 1 (%q)", lines, data)
	}
}

func TestRecoverInterruptedTasksBoundsWorkersAndAcks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lite-agent-task-log.json")
	first, err := tasklog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		id := "task-recover-" + itoa(i)
		if _, err := first.Begin(id); err != nil {
			t.Fatal(err)
		}
	}
	store, err := tasklog.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	var current, max atomic.Int32
	var uploads int32
	upload := func(taskID, result string, exitCode int, finishedAt time.Time, status string) bool {
		n := current.Add(1)
		defer current.Add(-1)
		for {
			seen := max.Load()
			if n <= seen || max.CompareAndSwap(seen, n) {
				break
			}
		}
		atomic.AddInt32(&uploads, 1)
		time.Sleep(40 * time.Millisecond)
		return true
	}
	recoverInterruptedTasksWith(store, upload)
	if max.Load() > int32(recoverWorkerCount) {
		t.Fatalf("recover concurrency = %d, want <= %d", max.Load(), recoverWorkerCount)
	}
	if uploads != 8 {
		t.Fatalf("uploads = %d, want 8", uploads)
	}
	if got := store.Interrupted(); len(got) != 0 {
		t.Fatalf("acked interrupted still reported: %#v", got)
	}
	reopened, err := tasklog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Interrupted(); len(got) != 0 {
		t.Fatalf("restart reported acked interrupted: %#v", got)
	}
}

func splitLines(value string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(value); i++ {
		if value[i] == '\n' {
			out = append(out, value[start:i])
			start = i + 1
		}
	}
	if start < len(value) {
		out = append(out, value[start:])
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
