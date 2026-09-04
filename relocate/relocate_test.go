package relocate

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"

	"github.com/nuomiiiii/lite-agent/remotecontrol"
)

type fakeController struct {
	detectedName string
	detected     bool
	legacy       bool
	running      map[string]bool
	prevented    []string
	disabled     []string
	removed      []string
	installed    *spec
	started      []string
	collect      spec
	startErr     error
}

func (f *fakeController) DetectService(string) (string, bool) {
	return f.detectedName, f.detected
}
func (f *fakeController) LegacyServiceExists(string) bool { return f.legacy }
func (f *fakeController) Collect(string) (spec, error)    { return f.collect, nil }
func (f *fakeController) Install(next spec) error {
	f.installed = &next
	return nil
}
func (f *fakeController) Start(name string) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.started = append(f.started, name)
	if f.running == nil {
		f.running = map[string]bool{}
	}
	f.running[name] = true
	return nil
}
func (f *fakeController) Running(name string) bool { return f.running[name] }
func (f *fakeController) PreventRestart(name string) error {
	f.prevented = append(f.prevented, name)
	return nil
}
func (f *fakeController) DisableNoStop(name string) error {
	f.disabled = append(f.disabled, name)
	return nil
}
func (f *fakeController) StopDisableRemove(name string) error {
	f.removed = append(f.removed, name)
	if f.running != nil {
		f.running[name] = false
	}
	return nil
}

func restoreRelocateHooks() {
	lookupExecutable = os.Executable
	lookupStat = func(path string) error {
		_, err := os.Stat(path)
		return err
	}
	detectPlanFn = detectPlan
}

func TestDoRelocateSkipsContainerCustomServiceAndMissingLegacy(t *testing.T) {
	t.Cleanup(restoreRelocateHooks)
	lookupExecutable = func() (string, error) { return "/opt/komari/agent", nil }

	lookupStat = func(path string) error {
		if path == "/.lite-agent-container" {
			return nil
		}
		return os.ErrNotExist
	}
	ok, err := doRelocate("linux", []string{"/opt/komari/agent"}, nil, &fakeController{legacy: true})
	if err != nil || ok {
		t.Fatalf("container relocate = %v, %v", ok, err)
	}

	lookupStat = func(string) error { return os.ErrNotExist }
	ok, err = doRelocate("linux", []string{"/opt/komari/agent"}, nil, &fakeController{
		detected:     true,
		detectedName: "shop-agent",
		legacy:       true,
	})
	if err != nil || ok {
		t.Fatalf("custom service relocate = %v, %v", ok, err)
	}

	ok, err = doRelocate("linux", []string{"/opt/komari/agent"}, nil, &fakeController{})
	if err != nil || ok {
		t.Fatalf("missing legacy service relocate = %v, %v", ok, err)
	}
}

func TestDoRelocateDoesNotStopSelfWhenNewAlreadyRunning(t *testing.T) {
	t.Cleanup(restoreRelocateHooks)
	oldDir := t.TempDir()
	newDir := t.TempDir()
	src := filepath.Join(oldDir, "agent")
	if err := os.WriteFile(src, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	token := []byte(`{"uuid":"keep-me","token":"secret"}`)
	if err := os.WriteFile(filepath.Join(oldDir, "auto-discovery.json"), token, 0o644); err != nil {
		t.Fatal(err)
	}
	lookupExecutable = func() (string, error) { return src, nil }
	lookupStat = func(string) error { return os.ErrNotExist }
	detectPlanFn = func(string, string, string, string) (plan, bool) {
		return plan{
			From: layout{Dir: oldDir, BinaryName: "agent", Service: "komari-agent"},
			To:   layout{Dir: newDir, BinaryName: "Lite-agent", Service: "lite-agent"},
		}, true
	}
	ctrl := &fakeController{
		legacy:  true,
		running: map[string]bool{"lite-agent": true},
	}
	ok, err := doRelocate("linux", []string{src}, nil, ctrl)
	if err != nil || !ok {
		t.Fatalf("relocate = %v, %v", ok, err)
	}
	if len(ctrl.removed) != 0 {
		t.Fatalf("old process must not stop itself: removed=%v", ctrl.removed)
	}
	if len(ctrl.disabled) != 1 || ctrl.disabled[0] != "komari-agent" {
		t.Fatalf("disabled = %v", ctrl.disabled)
	}
	got, err := os.ReadFile(filepath.Join(newDir, "auto-discovery.json"))
	if err != nil || !bytes.Equal(got, token) {
		t.Fatalf("sidecar copy = %q err=%v", got, err)
	}
}

func TestDoRelocateKeepsOldServiceIfNewDoesNotStart(t *testing.T) {
	t.Cleanup(restoreRelocateHooks)
	oldDir := t.TempDir()
	newDir := t.TempDir()
	src := filepath.Join(oldDir, "agent")
	if err := os.WriteFile(src, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	lookupExecutable = func() (string, error) { return src, nil }
	lookupStat = func(string) error { return os.ErrNotExist }
	detectPlanFn = func(string, string, string, string) (plan, bool) {
		return plan{
			From: layout{Dir: oldDir, BinaryName: "agent", Service: "komari-agent"},
			To:   layout{Dir: newDir, BinaryName: "Lite-agent", Service: "lite-agent"},
		}, true
	}
	ctrl := &fakeController{legacy: true, startErr: os.ErrPermission}
	ok, err := doRelocate("linux", []string{src}, nil, ctrl)
	if err == nil || ok {
		t.Fatalf("expected failed start, got ok=%v err=%v", ok, err)
	}
	if len(ctrl.disabled) != 0 || len(ctrl.removed) != 0 {
		t.Fatalf("old service must stay when new service fails: disabled=%v removed=%v", ctrl.disabled, ctrl.removed)
	}
}

func TestUpgradeFrom2203PreservesDataThenNewProcessUninstallsOld(t *testing.T) {
	t.Cleanup(restoreRelocateHooks)

	oldDir := t.TempDir()
	newDir := t.TempDir()
	src := filepath.Join(oldDir, "agent")
	if err := os.WriteFile(src, []byte("komari-agent-2.2.0.3-overwritten-as-2.3.0.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	discovery := []byte("{\n  \"uuid\": \"node-from-2203\",\n  \"token\": \"keep-this-token\"\n}")
	traffic := []byte(`{"interfaces":{"eth0":[{"timestamp":1700000000,"tx":12345,"rx":67890}]},"config":{"data_preserve_day":31,"detect_interval":2,"save_interval":600}}`)
	backup := []byte(`{"interfaces":{"eth0":[]}}`)
	nodeCfg := []byte(`{"endpoint":"panel.example.com"}`)
	envFile := []byte("AGENT_TOKEN=keep-this-token\n")
	if err := os.WriteFile(filepath.Join(oldDir, "auto-discovery.json"), discovery, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "net_static.json"), traffic, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "net_static.json.bak"), backup, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "node.json"), nodeCfg, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "agent.env"), envFile, 0o644); err != nil {
		t.Fatal(err)
	}

	lookupExecutable = func() (string, error) { return src, nil }
	lookupStat = func(string) error { return os.ErrNotExist }
	detectPlanFn = func(string, string, string, string) (plan, bool) {
		return plan{
			From: layout{Dir: oldDir, BinaryName: "agent", Service: "komari-agent"},
			To:   layout{Dir: newDir, BinaryName: "Lite-agent", Service: "lite-agent"},
		}, true
	}

	oldProc := &fakeController{
		legacy:       true,
		detected:     true,
		detectedName: "komari-agent",
		collect: spec{
			Args:             []string{"-e", "panel.example.com", "-t", "keep-this-token", "--config", filepath.Join(oldDir, "node.json")},
			Environment:      []string{"AGENT_TOKEN=keep-this-token"},
			EnvironmentFiles: []string{filepath.Join(oldDir, "agent.env")},
		},
	}

	ok, err := doRelocate("linux", []string{src, "-e", "panel.example.com", "-t", "keep-this-token", "--config", filepath.Join(oldDir, "node.json")}, nil, oldProc)
	if err != nil || !ok {
		t.Fatalf("2.2.0.3 process relocate = %v, %v", ok, err)
	}
	if len(oldProc.removed) != 0 {
		t.Fatalf("2.2.0.3 process must not nssm/systemctl stop itself: %v", oldProc.removed)
	}
	if len(oldProc.disabled) != 1 || oldProc.disabled[0] != "komari-agent" {
		t.Fatalf("old service must be disabled without stop: %v", oldProc.disabled)
	}
	if oldProc.installed == nil || filepath.Base(oldProc.installed.Binary) != "Lite-agent" {
		t.Fatalf("new process name = %+v", oldProc.installed)
	}
	if oldProc.installed.WorkDir != newDir {
		t.Fatalf("new working directory = %q", oldProc.installed.WorkDir)
	}
	if got := oldProc.installed.Args; len(got) < 4 || got[0] != "-e" || got[1] != "panel.example.com" || got[2] != "-t" || got[3] != "keep-this-token" {
		t.Fatalf("launch args not preserved: %v", oldProc.installed.Args)
	}

	mustSame := func(name string, want []byte) {
		t.Helper()
		got, err := os.ReadFile(filepath.Join(newDir, name))
		if err != nil {
			t.Fatalf("new %s: %v", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("new %s = %s, want %s", name, got, want)
		}
		old, err := os.ReadFile(filepath.Join(oldDir, name))
		if err != nil || !bytes.Equal(old, want) {
			t.Fatalf("old %s was modified or deleted", name)
		}
	}
	mustSame("auto-discovery.json", discovery)
	mustSame("net_static.json", traffic)
	mustSame("net_static.json.bak", backup)
	mustSame("node.json", nodeCfg)
	mustSame("agent.env", envFile)
	enabled, ok, err := remotecontrol.Read(filepath.Join(newDir, remotecontrol.StateFileName))
	if err != nil || !ok || !enabled {
		t.Fatalf("relocated remote-control.state enabled=%t ok=%t err=%v", enabled, ok, err)
	}

	copied, err := os.ReadFile(filepath.Join(newDir, "Lite-agent"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(copied, []byte("2.3.0.0")) {
		t.Fatalf("new binary content = %q", copied)
	}

	// New Lite-agent process starts from the new directory and uninstalls leftover komari-agent.
	lookupExecutable = func() (string, error) { return filepath.Join(newDir, "Lite-agent"), nil }
	detectPlanFn = func(string, string, string, string) (plan, bool) { return plan{}, false }
	newProc := &fakeController{
		legacy:       true,
		detected:     true,
		detectedName: "lite-agent",
		running:      map[string]bool{"lite-agent": true, "komari-agent": true},
	}
	if err := retireLeftoverLegacy("linux", newProc); err != nil {
		t.Fatal(err)
	}
	if len(newProc.removed) != 1 || newProc.removed[0] != "komari-agent" {
		t.Fatalf("new process must uninstall leftover komari-agent: %v", newProc.removed)
	}
	if newProc.running["komari-agent"] {
		t.Fatal("legacy service still marked running")
	}
}

func TestSeedRemoteControlStateFromLegacyFlags(t *testing.T) {
	dir := t.TempDir()
	if err := seedRemoteControlState(dir, []string{"-e", "example.com", "--disable-web-ssh=true"}); err != nil {
		t.Fatal(err)
	}
	enabled, ok, err := remotecontrol.Read(filepath.Join(dir, remotecontrol.StateFileName))
	if err != nil || !ok || enabled {
		t.Fatalf("disable-web-ssh must seed remote off enabled=%t ok=%t err=%v", enabled, ok, err)
	}

	dir = t.TempDir()
	if err := seedRemoteControlState(dir, []string{"-e", "example.com", "-t", "token"}); err != nil {
		t.Fatal(err)
	}
	enabled, ok, err = remotecontrol.Read(filepath.Join(dir, remotecontrol.StateFileName))
	if err != nil || !ok || !enabled {
		t.Fatalf("legacy -e -t must seed remote on enabled=%t ok=%t err=%v", enabled, ok, err)
	}

	dir = t.TempDir()
	if err := seedRemoteControlState(dir, []string{"--enable-remote-control=false"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, remotecontrol.StateFileName)); !os.IsNotExist(err) {
		t.Fatal("explicit new remote flag must not invent a state file")
	}
}

func TestDecodeNssmOutput(t *testing.T) {
	plain := decodeNssmOutput([]byte("-e example.com\r\n"))
	if plain != "-e example.com" {
		t.Fatalf("plain = %q", plain)
	}
	u := utf16.Encode([]rune("-e panel.example.com -t token"))
	buf := make([]byte, len(u)*2)
	for i, v := range u {
		binary.LittleEndian.PutUint16(buf[i*2:], v)
	}
	got := decodeNssmOutput(buf)
	if got != "-e panel.example.com -t token" {
		t.Fatalf("utf16 = %q", got)
	}
}
