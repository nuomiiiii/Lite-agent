package relocate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSavedAgentIdentityRequiresUUIDAndToken(t *testing.T) {
	got, err := ParseSavedAgentIdentity([]byte(`{"uuid":"node-uuid","token":"node-token"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.UUID != "node-uuid" || got.Token != "node-token" {
		t.Fatalf("identity = %+v", got)
	}

	for _, body := range []string{
		`{"uuid":"","token":"node-token"}`,
		`{"uuid":"node-uuid","token":""}`,
		`{"token":"node-token"}`,
		``,
		`{"uuid":`,
	} {
		if _, err := ParseSavedAgentIdentity([]byte(body)); err == nil {
			t.Fatalf("expected error for %q", body)
		} else if strings.Contains(err.Error(), "node-token") {
			t.Fatalf("identity error leaked token: %v", err)
		}
	}
}

func TestValidateSavedIdentityFile(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "auto-discovery.json")
	if err := ValidateSavedIdentityFile(missing); err == nil {
		t.Fatal("missing file must fail")
	}

	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSavedIdentityFile(empty); err == nil {
		t.Fatal("empty file must fail")
	}

	ok := filepath.Join(dir, savedIdentityFileName)
	if err := os.WriteFile(ok, []byte(`{"uuid":"u","token":"t"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSavedIdentityFile(ok); err != nil {
		t.Fatal(err)
	}
}

func TestRequireSavedIdentitySkipsManualToken(t *testing.T) {
	dir := t.TempDir()
	err := requireSavedIdentityForLegacyLaunch(
		[]string{"agent", "-e", "https://panel.example", "-t", "cli-token", "--auto-discovery", "legacy-key"},
		nil,
		spec{},
		dir,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRequireSavedIdentityStopsWhenLegacyFileMissing(t *testing.T) {
	dir := t.TempDir()
	err := requireSavedIdentityForLegacyLaunch(
		[]string{"agent", "-e", "https://panel.example", "--auto-discovery", "legacy-key"},
		nil,
		spec{},
		dir,
	)
	if err == nil {
		t.Fatal("legacy auto-discovery without identity file must stop relocation")
	}
	if strings.Contains(err.Error(), "legacy-key") {
		t.Fatalf("error leaked discovery key: %v", err)
	}
}
