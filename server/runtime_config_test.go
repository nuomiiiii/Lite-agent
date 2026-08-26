package server

import (
	"testing"

	v2 "github.com/nuomiiiii/lite-agent/protocol/v2"
	"github.com/nuomiiiii/lite-agent/runtimeconfig"
)

func TestProcessBasicInfoResponseAppliesDisabledConfig(t *testing.T) {
	previous := runtimeconfig.Snapshot()
	t.Cleanup(func() { runtimeconfig.Initialize(previous) })
	runtimeconfig.Initialize(runtimeconfig.State{MonthRotate: 26, Interval: 3})

	body := []byte(`{"jsonrpc":"2.0","id":"test","result":{"status":"success","config":{"month_rotate":0}}}`)
	if err := processBasicInfoResponse(body, 2); err != nil {
		t.Fatalf("processBasicInfoResponse() error = %v", err)
	}
	if got := runtimeconfig.MonthRotateDay(); got != 0 {
		t.Fatalf("MonthRotateDay() = %d, want 0", got)
	}
}

func TestApplyRuntimeConfigRejectsInvalidDay(t *testing.T) {
	invalid := 32
	if _, err := applyRuntimeConfig(v2.ConfigParams{MonthRotate: &invalid}); err == nil {
		t.Fatal("applyRuntimeConfig() expected an error")
	}
}

func TestApplyRuntimeConfigUpdatesSafeFieldsOnly(t *testing.T) {
	previous := runtimeconfig.Snapshot()
	t.Cleanup(func() { runtimeconfig.Initialize(previous) })
	runtimeconfig.Initialize(runtimeconfig.State{Interval: 3})

	interval := 12.5
	include := "eth0"
	exclude := "lo"
	mountpoints := "/;/data"
	memoryIncludeCache := true
	enableGPU := true
	changed, err := applyRuntimeConfig(v2.ConfigParams{
		Interval:           &interval,
		IncludeNics:        &include,
		ExcludeNics:        &exclude,
		IncludeMountpoints: &mountpoints,
		MemoryIncludeCache: &memoryIncludeCache,
		EnableGPU:          &enableGPU,
	})
	if err != nil {
		t.Fatalf("applyRuntimeConfig() error = %v", err)
	}
	if !changed {
		t.Fatal("applyRuntimeConfig() did not report the applied change")
	}
	got := runtimeconfig.Snapshot()
	if got.Interval != interval || got.IncludeNics != include || got.ExcludeNics != exclude ||
		got.IncludeMountpoints != mountpoints || !got.MemoryIncludeCache || !got.EnableGPU {
		t.Fatalf("unexpected runtime state: %+v", got)
	}
}

func TestCurrentRuntimeConfigParamsReportsEveryRuntimeField(t *testing.T) {
	previous := runtimeconfig.Snapshot()
	t.Cleanup(func() { runtimeconfig.Initialize(previous) })
	runtimeconfig.Initialize(runtimeconfig.State{
		MonthRotate:        9,
		Interval:           18,
		IncludeNics:        "eth0",
		ExcludeNics:        "lo",
		IncludeMountpoints: "/;/data",
		MemoryIncludeCache: true,
		EnableGPU:          true,
	})

	got := currentRuntimeConfigParams()
	if got.MonthRotate == nil || *got.MonthRotate != 9 ||
		got.Interval == nil || *got.Interval != 18 ||
		got.IncludeNics == nil || *got.IncludeNics != "eth0" ||
		got.ExcludeNics == nil || *got.ExcludeNics != "lo" ||
		got.IncludeMountpoints == nil || *got.IncludeMountpoints != "/;/data" ||
		got.MemoryIncludeCache == nil || !*got.MemoryIncludeCache ||
		got.EnableGPU == nil || !*got.EnableGPU {
		t.Fatalf("incomplete runtime config state: %+v", got)
	}
}

func TestVersionedRuntimeConfigReportsAppliedFailedAndIgnoresStaleRevisions(t *testing.T) {
	previous := runtimeconfig.Snapshot()
	processedConfigRevision.Store(0)
	appliedConfigRevision.Store(0)
	pendingConfigResultMu.Lock()
	pendingConfigResult = nil
	pendingConfigResultMu.Unlock()
	drainRuntimeConfigUploadRequests()
	t.Cleanup(func() {
		runtimeconfig.Initialize(previous)
		processedConfigRevision.Store(0)
		appliedConfigRevision.Store(0)
		pendingConfigResultMu.Lock()
		pendingConfigResult = nil
		pendingConfigResultMu.Unlock()
		drainRuntimeConfigUploadRequests()
	})

	interval := 7.0
	if !processRuntimeConfig(v2.ConfigParams{Revision: 1, Interval: &interval}, "event-1") {
		t.Fatal("revision 1 was not processed")
	}
	result := snapshotPendingConfigResult()
	if result == nil || result.Revision != 1 || result.EventID != "event-1" || result.Status != "applied" {
		t.Fatalf("applied result = %+v", result)
	}
	if appliedRuntimeConfigRevision() != 1 || runtimeconfig.ReportInterval() != interval {
		t.Fatalf("applied revision/state = %d/%v", appliedRuntimeConfigRevision(), runtimeconfig.ReportInterval())
	}
	clearPendingConfigResult(result)

	invalidInterval := 0.5
	if !processRuntimeConfig(v2.ConfigParams{Revision: 2, Interval: &invalidInterval}, "event-2") {
		t.Fatal("failed revision must still be acknowledged")
	}
	result = snapshotPendingConfigResult()
	if result == nil || result.Revision != 2 || result.Status != "failed" || result.Error == "" {
		t.Fatalf("failed result = %+v", result)
	}
	if appliedRuntimeConfigRevision() != 1 || runtimeconfig.ReportInterval() != interval {
		t.Fatalf("failed config changed applied state = %d/%v", appliedRuntimeConfigRevision(), runtimeconfig.ReportInterval())
	}
	clearPendingConfigResult(result)

	staleInterval := 9.0
	if !processRuntimeConfig(v2.ConfigParams{Revision: 1, Interval: &staleInterval}, "stale-event") {
		t.Fatal("stale revision was not acknowledged")
	}
	if result := snapshotPendingConfigResult(); result != nil {
		t.Fatalf("stale revision generated a result: %+v", result)
	}
	if runtimeconfig.ReportInterval() != interval {
		t.Fatalf("stale revision changed interval to %v", runtimeconfig.ReportInterval())
	}
}

func TestPendingConfigResultKeepsLatestRevision(t *testing.T) {
	pendingConfigResultMu.Lock()
	pendingConfigResult = nil
	pendingConfigResultMu.Unlock()
	t.Cleanup(func() {
		pendingConfigResultMu.Lock()
		pendingConfigResult = nil
		pendingConfigResultMu.Unlock()
	})

	older := v2.ConfigResultParams{Revision: 4, EventID: "event-4", Status: "applied"}
	newer := v2.ConfigResultParams{Revision: 5, EventID: "event-5", Status: "failed"}
	setPendingConfigResult(older)
	setPendingConfigResult(newer)
	setPendingConfigResult(older)

	got := snapshotPendingConfigResult()
	if got == nil || *got != newer {
		t.Fatalf("pending config result = %+v, want %+v", got, newer)
	}
}

func TestClearingSentConfigResultPreservesNewerResult(t *testing.T) {
	pendingConfigResultMu.Lock()
	pendingConfigResult = nil
	pendingConfigResultMu.Unlock()
	t.Cleanup(func() {
		pendingConfigResultMu.Lock()
		pendingConfigResult = nil
		pendingConfigResultMu.Unlock()
	})

	sent := v2.ConfigResultParams{Revision: 8, EventID: "event-8", Status: "applied"}
	newer := v2.ConfigResultParams{Revision: 9, EventID: "event-9", Status: "applied"}
	setPendingConfigResult(sent)
	setPendingConfigResult(newer)
	clearPendingConfigResult(&sent)
	if got := snapshotPendingConfigResult(); got == nil || *got != newer {
		t.Fatalf("newer config result was cleared: %+v", got)
	}

	clearPendingConfigResult(&newer)
	if got := snapshotPendingConfigResult(); got != nil {
		t.Fatalf("acknowledged config result was retained: %+v", got)
	}
}

func drainRuntimeConfigUploadRequests() {
	for {
		select {
		case <-runtimeConfigStateUploadRequests:
		default:
			return
		}
	}
}
