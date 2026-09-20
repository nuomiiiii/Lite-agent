package cmd

import (
	"math"
	"os"
	"runtime/debug"
	"testing"
)

func TestConfigureRuntimeMemoryDefault(t *testing.T) {
	if err := os.Unsetenv("GOMEMLIMIT"); err != nil {
		t.Fatal(err)
	}
	if err := os.Unsetenv("GOGC"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("GOMEMLIMIT")
		_ = os.Unsetenv("GOGC")
	})

	prevLimit := debug.SetMemoryLimit(math.MaxInt64)
	prevGC := debug.SetGCPercent(100)
	t.Cleanup(func() {
		debug.SetMemoryLimit(prevLimit)
		debug.SetGCPercent(prevGC)
	})

	configureRuntimeMemory()

	gotLimit := debug.SetMemoryLimit(defaultAgentMemoryLimit)
	if gotLimit != defaultAgentMemoryLimit {
		t.Fatalf("memory limit = %d, want %d", gotLimit, defaultAgentMemoryLimit)
	}
	gotGC := debug.SetGCPercent(50)
	if gotGC != 50 {
		t.Fatalf("GOGC = %d, want 50", gotGC)
	}
}

func TestConfigureRuntimeMemoryRespectsEnv(t *testing.T) {
	t.Setenv("GOMEMLIMIT", "256MiB")
	t.Setenv("GOGC", "200")

	const keepLimit int64 = 123 << 20
	prevLimit := debug.SetMemoryLimit(keepLimit)
	prevGC := debug.SetGCPercent(100)
	t.Cleanup(func() {
		debug.SetMemoryLimit(prevLimit)
		debug.SetGCPercent(prevGC)
	})

	configureRuntimeMemory()

	gotLimit := debug.SetMemoryLimit(keepLimit)
	if gotLimit != keepLimit {
		t.Fatalf("GOMEMLIMIT env should skip override, got %d", gotLimit)
	}
	gotGC := debug.SetGCPercent(100)
	if gotGC != 100 {
		t.Fatalf("GOGC env should skip override, got %d", gotGC)
	}
}
