package netstatic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAggregateByResetSplitsFlushAcrossBoundary(t *testing.T) {
	boundary := uint64(time.Date(2026, 9, 15, 12, 38, 12, 0, time.UTC).Unix())
	flushTs := boundary + 108 // 12:40:00
	arr := []TrafficData{
		{Timestamp: boundary - 8*60, Tx: 100, Rx: 10},
		{Timestamp: boundary - 2, Tx: 50, Rx: 5},
		{Timestamp: boundary, Tx: 7, Rx: 1},
		{Timestamp: flushTs, Tx: 3, Rx: 2},
	}

	got := aggregateByReset(arr, flushTs, boundary)
	if len(got) != 2 {
		t.Fatalf("records = %#v, want 2", got)
	}
	if got[0].Timestamp != boundary-1 || got[0].Tx != 150 || got[0].Rx != 15 {
		t.Fatalf("before bucket = %#v", got[0])
	}
	if got[1].Timestamp != flushTs || got[1].Tx != 10 || got[1].Rx != 3 {
		t.Fatalf("after bucket = %#v", got[1])
	}

	if one := aggregateByReset(arr, flushTs, 0); len(one) != 1 || one[0].Tx != 160 {
		t.Fatalf("no-boundary aggregate = %#v", one)
	}
}

func TestFlushCacheDoesNotAttributePreResetTrafficToNewCycle(t *testing.T) {
	dir := t.TempDir()
	SaveFilePath = filepath.Join(dir, "net_static.json")
	t.Cleanup(func() { SaveFilePath = "./net_static.json" })

	mu.Lock()
	store = NetStatic{Interfaces: map[string][]TrafficData{}}
	staticCache = map[string][]TrafficData{}
	config = configOrDefault(NetStaticConfig{})
	mu.Unlock()

	SetResetClock(15, "12:38:12", "UTC")
	boundary := time.Date(2026, 9, 15, 12, 38, 12, 0, time.UTC)
	flushAt := boundary.Add(108 * time.Second)

	mu.Lock()
	staticCache["eth0"] = []TrafficData{
		{Timestamp: uint64(boundary.Add(-8 * time.Minute).Unix()), Tx: 80, Rx: 8},
		{Timestamp: uint64(boundary.Add(-12 * time.Second).Unix()), Tx: 20, Rx: 2},
		{Timestamp: uint64(boundary.Unix()), Tx: 5, Rx: 1},
		{Timestamp: uint64(flushAt.Unix()), Tx: 15, Rx: 3},
	}
	flushCacheLocked(uint64(flushAt.Unix()))
	_ = saveToFileLocked()
	mu.Unlock()

	after, err := GetTotalTrafficBetween(uint64(boundary.Unix()), uint64(flushAt.Unix()))
	if err != nil {
		t.Fatalf("query after: %v", err)
	}
	if after["eth0"].Tx != 20 || after["eth0"].Rx != 4 {
		t.Fatalf("new cycle = %#v, want tx=20 rx=4", after["eth0"])
	}

	before, err := GetTotalTrafficBetween(uint64(boundary.Add(-10*time.Minute).Unix()), uint64(boundary.Unix())-1)
	if err != nil {
		t.Fatalf("query before: %v", err)
	}
	if before["eth0"].Tx != 100 || before["eth0"].Rx != 10 {
		t.Fatalf("old cycle = %#v, want tx=100 rx=10", before["eth0"])
	}

	data, err := os.ReadFile(SaveFilePath)
	if err != nil {
		t.Fatalf("read persist file: %v", err)
	}
	var persisted NetStatic
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("decode persist file: %v", err)
	}
	if got := persisted.Interfaces["eth0"]; len(got) != 2 {
		t.Fatalf("persisted buckets = %#v", got)
	}

	mu.Lock()
	store = NetStatic{Interfaces: map[string][]TrafficData{}}
	staticCache = map[string][]TrafficData{}
	mu.Unlock()
	if err := func() error {
		mu.Lock()
		defer mu.Unlock()
		return loadFromFileLocked()
	}(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	afterReload, err := GetTotalTrafficBetween(uint64(boundary.Unix()), uint64(flushAt.Unix()))
	if err != nil {
		t.Fatalf("query after reload: %v", err)
	}
	if afterReload["eth0"].Tx != 20 {
		t.Fatalf("reloaded new cycle = %#v", afterReload["eth0"])
	}
}

func TestFlushCacheWithoutResetKeepsSingleBucket(t *testing.T) {
	SetResetClock(0, "", "")
	arr := []TrafficData{
		{Timestamp: 100, Tx: 4, Rx: 1},
		{Timestamp: 200, Tx: 6, Rx: 2},
	}
	got := aggregateByReset(arr, 200, resetBoundaryUnix(200))
	if len(got) != 1 || got[0].Timestamp != 200 || got[0].Tx != 10 {
		t.Fatalf("unscheduled flush = %#v", got)
	}
}
