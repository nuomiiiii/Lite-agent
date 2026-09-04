package remotecontrol

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileAtomicOverwritesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	if err := os.WriteFile(path, []byte("old-config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("new-config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-config\n" {
		t.Fatalf("got %q, want new-config", got)
	}
}

func TestWriteFileAtomicKeepsOldFileWhenReplaceFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(path, []byte("old-config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := renameFile
	t.Cleanup(func() { renameFile = previous })
	renameFile = func(oldpath, newpath string) error {
		if newpath == path && !strings.Contains(filepath.Base(oldpath), ".bak") {
			return errors.New("replace blocked")
		}
		return os.Rename(oldpath, newpath)
	}
	if err := WriteFileAtomic(path, []byte("new-config\n"), 0o600); err == nil {
		t.Fatal("expected replace failure")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("old config was lost: %v", err)
	}
	if string(got) != "old-config\n" {
		t.Fatalf("got %q, want old-config", got)
	}
}

func TestWriteFileAtomicBackupReplaceWhenDirectRenameFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(path, []byte("old-config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := renameFile
	t.Cleanup(func() { renameFile = previous })
	directFails := true
	renameFile = func(oldpath, newpath string) error {
		if directFails && newpath == path && !strings.Contains(filepath.Base(oldpath), ".bak") {
			directFails = false
			return errors.New("direct replace blocked")
		}
		return os.Rename(oldpath, newpath)
	}
	if err := WriteFileAtomic(path, []byte("new-config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-config\n" {
		t.Fatalf("got %q, want new-config", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".bak") {
			t.Fatalf("leftover backup %s", entry.Name())
		}
	}
}
