package remotecontrol

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteAtomicRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFileName)
	if err := WriteAtomic(path, true); err != nil {
		t.Fatal(err)
	}
	enabled, ok, err := Read(path)
	if err != nil || !ok || !enabled {
		t.Fatalf("read enabled=%t ok=%t err=%v", enabled, ok, err)
	}
	if err := WriteAtomic(path, false); err != nil {
		t.Fatal(err)
	}
	enabled, ok, err = Read(path)
	if err != nil || !ok || enabled {
		t.Fatalf("read disabled=%t ok=%t err=%v", enabled, ok, err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestReadMissingAndEmptyState(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFileName)
	enabled, ok, err := Read(path)
	if err != nil || ok || enabled {
		t.Fatalf("missing state enabled=%t ok=%t err=%v", enabled, ok, err)
	}
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	enabled, ok, err = Read(path)
	if err != nil || ok || enabled {
		t.Fatalf("empty state enabled=%t ok=%t err=%v", enabled, ok, err)
	}
}

func TestReadCorruptStateFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFileName)
	if err := os.WriteFile(path, []byte(`not-json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Read(path); err == nil {
		t.Fatal("corrupt state must fail")
	}
}
