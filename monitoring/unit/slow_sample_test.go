package monitoring

import "testing"

func TestCpuStaticInfoCached(t *testing.T) {
	cpuStaticCache.reset()
	first := CpuStaticInfo()
	second := CpuStaticInfo()
	if first.CPUName != second.CPUName || first.CPUCores != second.CPUCores || first.CPUArchitecture != second.CPUArchitecture {
		t.Fatalf("cached CPU static info changed: %+v vs %+v", first, second)
	}
	if first.CPUCores < 1 {
		t.Fatalf("CPU cores = %d", first.CPUCores)
	}
}

func TestDiskReadable(t *testing.T) {
	info := Disk()
	if info.Total == 0 && info.Used != 0 {
		t.Fatalf("disk used %d with zero total", info.Used)
	}
}

func TestProcessCountNonNegative(t *testing.T) {
	if n := ProcessCount(); n < 1 {
		t.Fatalf("ProcessCount = %d", n)
	}
}
