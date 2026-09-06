package relocate

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOfficialServiceNamesAreFixed(t *testing.T) {
	got := officialServiceNames()
	if !sameStringSlice(got, []string{legacyServiceName, newServiceName}) {
		t.Fatalf("official services = %v", got)
	}
}

func TestRelocationSourcesDoNotEnumerateOrPatchLaunch(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(".", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(body, []byte("ListServices()")) {
			t.Errorf("%s enumerates all Windows services", entry.Name())
		}
		if bytes.Contains(body, []byte("PatchLaunch")) {
			t.Errorf("%s still patches service launch arguments", entry.Name())
		}
		if entry.Name() == "service_darwin.go" && bytes.Contains(body, []byte("ReadDir")) {
			t.Errorf("%s enumerates launchd plist directories", entry.Name())
		}
	}
}

func TestPathsReferToSameExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		if !pathsReferToSameExecutable(`C:\Program Files\Lite\Lite-agent.exe`, `c:\program files\lite\lite-agent.exe`) {
			t.Fatal("windows paths should match case-insensitively")
		}
		return
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "Lite-agent")
	if err := os.WriteFile(exe, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !pathsReferToSameExecutable(exe, exe) {
		t.Fatal("identical paths should match")
	}
}
