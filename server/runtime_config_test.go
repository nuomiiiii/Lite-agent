package server

import (
	"testing"

	v2 "github.com/komari-monitor/komari-agent/protocol/v2"
	"github.com/komari-monitor/komari-agent/runtimeconfig"
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
