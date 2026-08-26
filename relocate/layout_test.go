package relocate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDetectPlanUsesLiteAgentProcessName(t *testing.T) {
	plan, ok := detectPlan("linux", "/opt/komari/agent", "", "")
	if !ok {
		t.Fatal("default linux layout should relocate")
	}
	if plan.To.BinaryName != "Lite-agent" {
		t.Fatalf("linux process name = %q, want Lite-agent", plan.To.BinaryName)
	}
	if plan.To.Dir != "/opt/lite-agent" || plan.To.Service != "lite-agent" {
		t.Fatalf("linux destination = %+v", plan.To)
	}

	plan, ok = detectPlan("darwin", "/usr/local/komari/agent", "/Users/joey", "")
	if !ok {
		t.Fatal("default darwin layout should relocate")
	}
	if plan.To.BinaryName != "Lite-agent" {
		t.Fatalf("darwin process name = %q, want Lite-agent", plan.To.BinaryName)
	}

	plan, ok = detectPlan("windows", `C:\Program Files\Komari\komari-agent.exe`, "", `C:\Program Files`)
	if !ok {
		t.Fatal("default windows layout should relocate")
	}
	if plan.To.BinaryName != "Lite-agent.exe" {
		t.Fatalf("windows process name = %q, want Lite-agent.exe", plan.To.BinaryName)
	}
	if plan.To.Dir != filepath.Join(`C:\Program Files`, "Lite") {
		t.Fatalf("windows destination dir = %q", plan.To.Dir)
	}
}

func TestDetectPlanSkipsCustomAndModernPaths(t *testing.T) {
	if _, ok := detectPlan("linux", "/opt/custom/agent", "", ""); ok {
		t.Fatal("custom install dir must not relocate")
	}
	if _, ok := detectPlan("linux", "/opt/lite-agent/Lite-agent", "", ""); ok {
		t.Fatal("already-modern path must not relocate again")
	}
	if _, ok := detectPlan("windows", `D:\Apps\Lite-agent.exe`, "", `C:\Program Files`); ok {
		t.Fatal("custom windows dir must not relocate")
	}
}

func TestRewriteLaunchArgsRewritesConfigInOldDir(t *testing.T) {
	got := rewriteLaunchArgs(
		[]string{"/opt/komari/agent", "-e", "example.com", "--config", "/opt/komari/node.json"},
		"/opt/komari",
		"/opt/lite-agent",
		"/opt/lite-agent/Lite-agent",
		"linux",
	)
	if got[0] != "/opt/lite-agent/Lite-agent" && filepath.Base(got[0]) != "Lite-agent" {
		t.Fatalf("argv0 = %q, want new process path", got[0])
	}
	wantConfig := filepath.Join("/opt/lite-agent", "node.json")
	if got[len(got)-1] != wantConfig && filepath.ToSlash(got[len(got)-1]) != "/opt/lite-agent/node.json" {
		t.Fatalf("config path = %q", got[len(got)-1])
	}
}

func TestCopySidecarsAndConfig(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()
	for _, name := range []string{"auto-discovery.json", "net_static.json", "net_static.json.bak"} {
		if err := os.WriteFile(filepath.Join(oldDir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	config := filepath.Join(oldDir, "node.json")
	if err := os.WriteFile(config, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copySidecars(oldDir, newDir, runtime.GOOS, []string{config}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"auto-discovery.json", "net_static.json", "net_static.json.bak", "node.json"} {
		if _, err := os.Stat(filepath.Join(newDir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}

func TestIsContainerAndCgroup(t *testing.T) {
	if isContainer(func(string) error { return os.ErrNotExist }) {
		t.Fatal("no marker should not look like a container")
	}
	if !isContainer(func(path string) error {
		if path == "/.lite-agent-container" {
			return nil
		}
		return os.ErrNotExist
	}) {
		t.Fatal("lite container marker should skip relocate")
	}
	if !isContainer(func(path string) error {
		if path == "/.komari-agent-container" {
			return nil
		}
		return os.ErrNotExist
	}) {
		t.Fatal("legacy container marker should skip relocate")
	}

	got := serviceFromCgroup("0::/system.slice/komari-agent.service")
	if got != "komari-agent" {
		t.Fatalf("cgroup service = %q", got)
	}
}

func TestSystemdUnitStartsLiteAgentBinary(t *testing.T) {
	unit := systemdUnit(spec{
		Name:    "lite-agent",
		Binary:  "/opt/lite-agent/Lite-agent",
		Args:    []string{"-e", "example.com"},
		WorkDir: "/opt/lite-agent",
	})
	if !strings.Contains(unit, "ExecStart=/opt/lite-agent/Lite-agent") {
		t.Fatalf("unit missing Lite-agent process path:\n%s", unit)
	}
	if strings.Contains(unit, "/opt/komari/agent") {
		t.Fatal("unit still points at the old agent process name")
	}
}
