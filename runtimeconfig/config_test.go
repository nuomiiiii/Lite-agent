package runtimeconfig

import (
	"sync"
	"testing"
)

func TestConcurrentSnapshotAndSet(t *testing.T) {
	previous := Snapshot()
	t.Cleanup(func() { Initialize(previous) })
	Initialize(State{Interval: DefaultReportInterval})

	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				state := Snapshot()
				state.Interval = float64(1 + (worker+i)%30)
				Set(state)
				_ = Snapshot()
			}
		}(worker)
	}
	wg.Wait()
	if Snapshot().Interval < 1 {
		t.Fatalf("invalid interval after concurrent updates: %+v", Snapshot())
	}
}
