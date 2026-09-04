package tasklog

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestBeginIsIdempotentForSameTaskID(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "lite-agent-task-log.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Begin("task-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Begin("task-a"); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("second begin = %v", err)
	}
	if _, err := store.Finish("task-a", "ok", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Begin("task-a"); !errors.Is(err, ErrAlreadyFinished) {
		t.Fatalf("finished begin = %v", err)
	}
}

func TestRestartMarksStartedTasksInterrupted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lite-agent-task-log.json")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Begin("task-crash"); err != nil {
		t.Fatal(err)
	}
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	interrupted := second.Interrupted()
	if len(interrupted) != 1 || interrupted[0].TaskID != "task-crash" {
		t.Fatalf("interrupted = %#v", interrupted)
	}
	if _, err := second.Begin("task-crash"); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("restarted begin = %v", err)
	}
}

func TestConcurrentBeginOnlyStartsOnce(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "lite-agent-task-log.json"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make([]error, 8)
	wg.Add(len(results))
	for i := range results {
		go func(i int) {
			defer wg.Done()
			_, results[i] = store.Begin("shared-task")
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, err := range results {
		if err == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("successes = %d, want 1", ok)
	}
}

func TestFinishReturnsSaveErrorAndKeepsStarted(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "lite-agent-task-log.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Begin("task-finish"); err != nil {
		t.Fatal(err)
	}
	store.FailNextSave(errors.New("disk full"))
	if _, err := store.Finish("task-finish", "hello", 0); err == nil {
		t.Fatal("Finish ignored save error")
	}
	entry, ok := store.Lookup("task-finish")
	if !ok || entry.State != StateStarted {
		t.Fatalf("finish save failure changed state: %#v", entry)
	}
}

func TestAckReturnsSaveErrorAndDoesNotMarkAcked(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "lite-agent-task-log.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Begin("task-ack"); err != nil {
		t.Fatal(err)
	}
	store.failNextSave = errors.New("disk full")
	if err := store.Ack("task-ack"); err == nil {
		t.Fatal("Ack ignored save error")
	}
	entry, ok := store.Lookup("task-ack")
	if !ok || entry.Acked {
		t.Fatalf("acked despite save failure: %#v", entry)
	}
}

func TestAckedInterruptedStaysForDedupAndIsNotReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lite-agent-task-log.json")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Begin("task-crash"); err != nil {
		t.Fatal(err)
	}
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Ack("task-crash"); err != nil {
		t.Fatal(err)
	}
	third, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := third.Interrupted(); len(got) != 0 {
		t.Fatalf("acked interrupted was reported again: %#v", got)
	}
	if _, err := third.Begin("task-crash"); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("acked interrupted begin = %v", err)
	}
}

func TestOpenPrunesExpiredAndCapsCapacityWithoutTouchingRetained(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lite-agent-task-log.json")
	now := time.Now().UTC()
	stored := []Entry{
		{TaskID: "keep-recent", State: StateFinished, StartedAt: now.Add(-time.Hour), FinishedAt: now.Add(-time.Hour), Summary: "ok"},
		{TaskID: "keep-boundary", State: StateFinished, StartedAt: now.Add(-24*time.Hour + time.Second), FinishedAt: now.Add(-24*time.Hour + time.Second), Summary: "edge"},
		{TaskID: "drop-old", State: StateFinished, StartedAt: now.Add(-25 * time.Hour), FinishedAt: now.Add(-25 * time.Hour), Summary: "old", Acked: true},
		{TaskID: "was-started", State: StateStarted, StartedAt: now.Add(-time.Minute)},
	}
	data, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	log, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := log.Lookup("drop-old"); ok {
		t.Fatal("expired record was kept")
	}
	if _, ok := log.Lookup("keep-recent"); !ok {
		t.Fatal("recent record was pruned")
	}
	if _, ok := log.Lookup("keep-boundary"); !ok {
		t.Fatal("24h boundary record was pruned")
	}
	entry, ok := log.Lookup("was-started")
	if !ok || entry.State != StateInterrupted {
		t.Fatalf("started leftover = %#v", entry)
	}
}

func TestOpenEnforcesEntryCapacity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lite-agent-task-log.json")
	now := time.Now().UTC()
	stored := make([]Entry, 0, 300)
	for i := 0; i < 300; i++ {
		stamp := now.Add(-time.Duration(300-i) * time.Second)
		stored = append(stored, Entry{TaskID: "cap-" + itoa(i), State: StateFinished, StartedAt: stamp, FinishedAt: stamp, Summary: "x", Acked: true})
	}
	data, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	log, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.entries) != defaultCapacity {
		t.Fatalf("capacity = %d, want %d", len(log.entries), defaultCapacity)
	}
	if _, ok := log.Lookup("cap-299"); !ok {
		t.Fatal("newest record was pruned")
	}
	if _, ok := log.Lookup("cap-0"); ok {
		t.Fatal("oldest overflow record was kept")
	}
}

func TestOpenDoesNotRewriteUnchangedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lite-agent-task-log.json")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Begin("task-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Finish("task-a", "ok", 0); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("unchanged open rewrote file\nbefore %s\nafter %s", before, after)
	}
}

func TestPruneKeepsProtectedEntriesAndBeginFailsWhenFull(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "lite-agent-task-log.json"))
	if err != nil {
		t.Fatal(err)
	}
	store.SetLimitsForTest(time.Hour, 2)
	now := time.Now().UTC()
	store.entries["old-acked"] = Entry{
		TaskID:     "old-acked",
		State:      StateFinished,
		StartedAt:  now.Add(-2 * time.Hour),
		FinishedAt: now.Add(-2 * time.Hour),
		Acked:      true,
		Summary:    "done",
	}
	store.entries["unacked"] = Entry{
		TaskID:     "unacked",
		State:      StateFinished,
		StartedAt:  now.Add(-2 * time.Hour),
		FinishedAt: now.Add(-2 * time.Hour),
		Summary:    "keep",
	}
	store.entries["started"] = Entry{
		TaskID:    "started",
		State:     StateStarted,
		StartedAt: now.Add(-2 * time.Hour),
	}
	store.pruneLocked(now)
	if _, ok := store.entries["old-acked"]; ok {
		t.Fatal("acked expired finished record should be pruned")
	}
	if _, ok := store.entries["unacked"]; !ok {
		t.Fatal("unacked finished record was pruned")
	}
	if _, ok := store.entries["started"]; !ok {
		t.Fatal("started record was pruned")
	}
	if _, err := store.Begin("next"); !errors.Is(err, ErrLogFull) {
		t.Fatalf("Begin = %v, want ErrLogFull", err)
	}
}

func TestCapacityPrefersDroppingAckedFinished(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "lite-agent-task-log.json"))
	if err != nil {
		t.Fatal(err)
	}
	store.SetLimitsForTest(24*time.Hour, 2)
	if _, err := store.Begin("first"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Finish("first", "ok", 0); err != nil {
		t.Fatal(err)
	}
	if err := store.Ack("first"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Begin("second"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Finish("second", "ok", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Begin("third"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Lookup("first"); ok {
		t.Fatal("acked finished record should be dropped to make room")
	}
	if _, ok := store.Lookup("second"); !ok {
		t.Fatal("unacked finished record was dropped")
	}
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
