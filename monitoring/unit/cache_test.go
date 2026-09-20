package monitoring

import (
	"errors"
	"testing"
	"time"
)

func TestTTLCacheHitWithinTTL(t *testing.T) {
	var cache ttlCache[int]
	loads := 0
	load := func() (int, error) {
		loads++
		return loads, nil
	}

	first, err := cache.getErr(time.Minute, "k", load)
	if err != nil || first != 1 {
		t.Fatalf("first get = %d, %v", first, err)
	}
	second, err := cache.getErr(time.Minute, "k", load)
	if err != nil || second != 1 {
		t.Fatalf("cached get = %d, %v", second, err)
	}
	if loads != 1 {
		t.Fatalf("load called %d times, want 1", loads)
	}
}

func TestTTLCacheKeyChangeReloads(t *testing.T) {
	var cache ttlCache[string]
	loads := 0
	got, _ := cache.getErr(time.Minute, "a", func() (string, error) {
		loads++
		return "a", nil
	})
	if got != "a" {
		t.Fatalf("got %q", got)
	}
	got, _ = cache.getErr(time.Minute, "b", func() (string, error) {
		loads++
		return "b", nil
	})
	if got != "b" || loads != 2 {
		t.Fatalf("got %q loads=%d", got, loads)
	}
}

func TestTTLCacheExpiredReloads(t *testing.T) {
	var cache ttlCache[int]
	loads := 0
	_, _ = cache.getErr(5*time.Millisecond, "", func() (int, error) {
		loads++
		return 1, nil
	})
	time.Sleep(20 * time.Millisecond)
	got, err := cache.getErr(5*time.Millisecond, "", func() (int, error) {
		loads++
		return 2, errors.New("ignored on hit")
	})
	if err == nil || got != 2 || loads != 2 {
		t.Fatalf("got=%d err=%v loads=%d", got, err, loads)
	}
}

func TestTTLCacheZeroTTLSticky(t *testing.T) {
	var cache ttlCache[int]
	loads := 0
	_, _ = cache.getErr(0, "", func() (int, error) {
		loads++
		return 7, nil
	})
	got, _ := cache.getErr(0, "", func() (int, error) {
		loads++
		return 8, nil
	})
	if got != 7 || loads != 1 {
		t.Fatalf("sticky cache got=%d loads=%d", got, loads)
	}
}
