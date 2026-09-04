package tasklog

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	StateStarted     = "started"
	StateFinished    = "finished"
	StateInterrupted = "interrupted"
	defaultFileName  = "lite-agent-task-log.json"
	defaultRetain    = 24 * time.Hour
	defaultCapacity  = 256
)

var (
	ErrAlreadyStarted  = errors.New("task already started")
	ErrAlreadyFinished = errors.New("task already finished")
	ErrInterrupted     = errors.New("task execution was interrupted")
	ErrLogFull         = errors.New("task log is full")
)

type Entry struct {
	TaskID     string    `json:"task_id"`
	State      string    `json:"state"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	ExitCode   int       `json:"exit_code,omitempty"`
	Acked      bool      `json:"acked,omitempty"`
}

type Log struct {
	mu           sync.Mutex
	path         string
	retain       time.Duration
	capacity     int
	entries      map[string]Entry
	failNextSave error
}

func PathForConfig(configFile string) string {
	if configFile != "" {
		return filepath.Join(filepath.Dir(configFile), defaultFileName)
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), defaultFileName)
	}
	return defaultFileName
}

func Open(path string) (*Log, error) {
	log := &Log{
		path:     path,
		retain:   defaultRetain,
		capacity: defaultCapacity,
		entries:  make(map[string]Entry),
	}
	if err := log.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	now := time.Now().UTC()
	dirty := log.pruneLocked(now)
	for id, entry := range log.entries {
		if entry.State == StateStarted {
			entry.State = StateInterrupted
			entry.Summary = truncateSummary("execution status unknown")
			entry.ExitCode = -1
			if entry.FinishedAt.IsZero() {
				entry.FinishedAt = now
			}
			log.entries[id] = entry
			dirty = true
		}
	}
	if dirty {
		if err := log.saveLocked(); err != nil {
			return nil, err
		}
	}
	return log, nil
}

func (l *Log) Begin(taskID string) (Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(time.Now().UTC())
	if existing, ok := l.entries[taskID]; ok {
		switch existing.State {
		case StateFinished:
			return existing, ErrAlreadyFinished
		case StateInterrupted:
			return existing, ErrInterrupted
		default:
			return existing, ErrAlreadyStarted
		}
	}
	for len(l.entries) >= l.capacity {
		victim := oldestDroppableTaskLogEntry(l.entries)
		if victim == "" {
			return Entry{}, ErrLogFull
		}
		delete(l.entries, victim)
	}
	entry := Entry{TaskID: taskID, State: StateStarted, StartedAt: time.Now().UTC()}
	l.entries[taskID] = entry
	if err := l.saveLocked(); err != nil {
		delete(l.entries, taskID)
		return Entry{}, err
	}
	return entry, nil
}

func (l *Log) Finish(taskID, summary string, exitCode int) (Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	previous, existed := l.entries[taskID]
	entry := previous
	if !existed {
		entry = Entry{TaskID: taskID, StartedAt: time.Now().UTC()}
	}
	entry.State = StateFinished
	entry.FinishedAt = time.Now().UTC()
	entry.Summary = truncateSummary(summary)
	entry.ExitCode = exitCode
	l.entries[taskID] = entry
	if err := l.saveLocked(); err != nil {
		if existed {
			l.entries[taskID] = previous
		} else {
			delete(l.entries, taskID)
		}
		return Entry{}, err
	}
	return entry, nil
}

func (l *Log) Ack(taskID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.entries[taskID]
	if !ok {
		return nil
	}
	entry.Acked = true
	l.entries[taskID] = entry
	if err := l.saveLocked(); err != nil {
		entry.Acked = false
		l.entries[taskID] = entry
		return err
	}
	return nil
}

func (l *Log) Interrupted() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Entry, 0)
	for _, entry := range l.entries {
		if entry.State == StateInterrupted && !entry.Acked {
			out = append(out, entry)
		}
	}
	return out
}

func (l *Log) PendingReports() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Entry, 0)
	for _, entry := range l.entries {
		if entry.Acked {
			continue
		}
		if entry.State == StateInterrupted || entry.State == StateFinished {
			out = append(out, entry)
		}
	}
	return out
}

func (l *Log) FailNextSave(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failNextSave = err
}

func (l *Log) Lookup(taskID string) (Entry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.entries[taskID]
	return entry, ok
}

func (l *Log) load() error {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return err
	}
	var stored []Entry
	if err := json.Unmarshal(data, &stored); err != nil {
		return err
	}
	for _, entry := range stored {
		if entry.TaskID == "" {
			continue
		}
		l.entries[entry.TaskID] = entry
	}
	return nil
}

func (l *Log) saveLocked() error {
	if l.failNextSave != nil {
		err := l.failNextSave
		l.failNextSave = nil
		return err
	}
	l.pruneLocked(time.Now().UTC())
	stored := make([]Entry, 0, len(l.entries))
	for _, entry := range l.entries {
		stored = append(stored, entry)
	}
	sort.Slice(stored, func(i, j int) bool {
		return stored[i].TaskID < stored[j].TaskID
	})
	data, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil && !os.IsExist(err) {
		if filepath.Dir(l.path) != "." && filepath.Dir(l.path) != "" {
			return err
		}
	}
	temp, err := os.CreateTemp(filepath.Dir(l.path), ".lite-agent-task-log-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return os.Rename(tempPath, l.path)
}

func (l *Log) pruneLocked(now time.Time) bool {
	dirty := false
	for id, entry := range l.entries {
		if isProtectedTaskLogEntry(entry) {
			continue
		}
		stamp := entry.StartedAt
		if !entry.FinishedAt.IsZero() {
			stamp = entry.FinishedAt
		}
		if now.Sub(stamp) > l.retain {
			delete(l.entries, id)
			dirty = true
		}
	}
	for len(l.entries) > l.capacity {
		victim := oldestDroppableTaskLogEntry(l.entries)
		if victim == "" {
			break
		}
		delete(l.entries, victim)
		dirty = true
	}
	return dirty
}

func isProtectedTaskLogEntry(entry Entry) bool {
	if entry.State == StateStarted {
		return true
	}
	if (entry.State == StateFinished || entry.State == StateInterrupted) && !entry.Acked {
		return true
	}
	return false
}

func oldestDroppableTaskLogEntry(entries map[string]Entry) string {
	var oldestID string
	var oldest time.Time
	for id, entry := range entries {
		if isProtectedTaskLogEntry(entry) {
			continue
		}
		stamp := entry.StartedAt
		if oldestID == "" || stamp.Before(oldest) {
			oldestID = id
			oldest = stamp
		}
	}
	return oldestID
}

func (l *Log) SetLimitsForTest(retain time.Duration, capacity int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if retain > 0 {
		l.retain = retain
	}
	if capacity > 0 {
		l.capacity = capacity
	}
}

func truncateSummary(summary string) string {
	const max = 256
	if len(summary) <= max {
		return summary
	}
	return summary[:max]
}
