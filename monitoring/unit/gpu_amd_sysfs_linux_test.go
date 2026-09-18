//go:build linux

package monitoring

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListAMDSysfsCardsReadsBusyVRAMAndTemp(t *testing.T) {
	root := t.TempDir()
	writeAMDSysfsCard(t, root, "card0", "amdgpu", "Phoenix", "42", "1073741824", "268435456", "edge", "51000")
	writeDRMConnector(t, root, "card0-DP-1")
	writeAMDSysfsCard(t, root, "card1", "nvidia", "Ignored", "99", "1", "1", "edge", "40000")

	orig := amdSysfsDRMDir
	amdSysfsDRMDir = root
	t.Cleanup(func() { amdSysfsDRMDir = orig })

	cards, err := listAMDSysfsCards()
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 {
		t.Fatalf("cards = %d, want 1 amdgpu card (connectors and other drivers skipped)", len(cards))
	}
	card := cards[0]
	if card.name != "Phoenix" {
		t.Fatalf("name = %q, want Phoenix", card.name)
	}
	if card.utilization != 42 {
		t.Fatalf("utilization = %v, want 42", card.utilization)
	}
	if card.memoryTotal != 1073741824 || card.memoryUsed != 268435456 {
		t.Fatalf("vram = %d/%d", card.memoryUsed, card.memoryTotal)
	}
	if card.temperature != 51 {
		t.Fatalf("temperature = %d, want 51", card.temperature)
	}
}

func TestListAMDSysfsCardsSkipsCardsWithoutBusyPercent(t *testing.T) {
	root := t.TempDir()
	deviceDir := filepath.Join(root, "card0", "device")
	if err := os.MkdirAll(deviceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("amdgpu", filepath.Join(deviceDir, "driver")); err != nil {
		t.Fatal(err)
	}

	orig := amdSysfsDRMDir
	amdSysfsDRMDir = root
	t.Cleanup(func() { amdSysfsDRMDir = orig })

	if _, err := listAMDSysfsCards(); err == nil {
		t.Fatal("expected error when gpu_busy_percent is missing")
	}
}

func TestAMDSysfsDetailedInfo(t *testing.T) {
	infos, err := getAMDSysfsDetailedInfo()
	if err != nil {
		t.Skipf("no amdgpu sysfs metrics on this host: %v", err)
	}
	if len(infos) == 0 {
		t.Skip("no amdgpu sysfs metrics on this host")
	}
	for i, info := range infos {
		t.Logf("gpu[%d] name=%s util=%.1f mem=%d/%d temp=%d", i, info.Name, info.Utilization, info.MemoryUsed, info.MemoryTotal, info.Temperature)
		if info.Name == "" {
			t.Errorf("gpu[%d] missing name", i)
		}
	}
}

func writeAMDSysfsCard(t *testing.T, root, card, driver, name, busy, total, used, tempLabel, tempInput string) {
	t.Helper()
	deviceDir := filepath.Join(root, card, "device")
	if err := os.MkdirAll(filepath.Join(deviceDir, "hwmon", "hwmon0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(driver, filepath.Join(deviceDir, "driver")); err != nil {
		t.Fatal(err)
	}
	writeSysfs(t, filepath.Join(deviceDir, "product_name"), name+"\n")
	writeSysfs(t, filepath.Join(deviceDir, "gpu_busy_percent"), busy+"\n")
	writeSysfs(t, filepath.Join(deviceDir, "mem_info_vram_total"), total+"\n")
	writeSysfs(t, filepath.Join(deviceDir, "mem_info_vram_used"), used+"\n")
	writeSysfs(t, filepath.Join(deviceDir, "hwmon", "hwmon0", "temp1_label"), tempLabel+"\n")
	writeSysfs(t, filepath.Join(deviceDir, "hwmon", "hwmon0", "temp1_input"), tempInput+"\n")
}

func writeDRMConnector(t *testing.T, root, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeSysfs(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}
