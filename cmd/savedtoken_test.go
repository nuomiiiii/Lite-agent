package cmd

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func restoreSavedTokenFlags(t *testing.T) {
	t.Helper()
	originalToken := flags.Token
	originalKey := flags.AutoDiscoveryKey
	t.Cleanup(func() {
		flags.Token = originalToken
		flags.AutoDiscoveryKey = originalKey
	})
}

func writeSavedIdentity(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auto-discovery.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestApplySavedAgentTokenUsesExistingFile(t *testing.T) {
	restoreSavedTokenFlags(t)
	path := writeSavedIdentity(t, `{"uuid":"node-uuid","token":"saved-token"}`)

	flags.Token = ""
	flags.AutoDiscoveryKey = "legacy-key"
	result, err := applySavedAgentTokenFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if flags.Token != "saved-token" {
		t.Fatalf("Token = %q, want saved-token", flags.Token)
	}
	if !result.UsedSavedFile || result.UUID != "node-uuid" {
		t.Fatalf("result = %+v, want used saved file for node-uuid", result)
	}
}

func TestApplySavedAgentTokenKeepsExplicitToken(t *testing.T) {
	restoreSavedTokenFlags(t)
	path := writeSavedIdentity(t, `{"uuid":"node-uuid","token":"saved-token"}`)

	flags.Token = "cli-token"
	flags.AutoDiscoveryKey = "legacy-key"
	result, err := applySavedAgentTokenFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if flags.Token != "cli-token" {
		t.Fatalf("Token = %q, want cli-token", flags.Token)
	}
	if result.UsedSavedFile {
		t.Fatal("explicit -t must not read auto-discovery.json")
	}
}

func TestApplySavedAgentTokenKeepsEnvAndJSONToken(t *testing.T) {
	restoreSavedTokenFlags(t)
	path := writeSavedIdentity(t, `{"uuid":"node-uuid","token":"saved-token"}`)

	flags.Token = "env-or-json-token"
	flags.AutoDiscoveryKey = ""
	result, err := applySavedAgentTokenFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if flags.Token != "env-or-json-token" {
		t.Fatalf("Token = %q, want env-or-json-token", flags.Token)
	}
	if result.UsedSavedFile {
		t.Fatal("AGENT_TOKEN / JSON token must not be overwritten")
	}
}

func TestApplySavedAgentTokenIgnoresFileWithoutLegacyMarker(t *testing.T) {
	restoreSavedTokenFlags(t)
	path := writeSavedIdentity(t, `{"uuid":"node-uuid","token":"saved-token"}`)

	flags.Token = ""
	flags.AutoDiscoveryKey = ""
	result, err := applySavedAgentTokenFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if flags.Token != "" {
		t.Fatalf("Token = %q, want empty without a legacy marker", flags.Token)
	}
	if result.UsedSavedFile {
		t.Fatal("must not read leftover auto-discovery.json on a normal launch")
	}
}

func TestApplySavedAgentTokenFailsWhenFileMissing(t *testing.T) {
	restoreSavedTokenFlags(t)
	flags.Token = ""
	flags.AutoDiscoveryKey = "legacy-key"
	result, err := applySavedAgentTokenFrom(filepath.Join(t.TempDir(), "auto-discovery.json"))
	if err == nil {
		t.Fatal("missing auto-discovery.json must stop startup")
	}
	if !strings.Contains(err.Error(), autoDiscoveryRetiredMessage) {
		t.Fatalf("error = %v, want retired message", err)
	}
	if flags.Token != "" || result.UsedSavedFile {
		t.Fatalf("must not start with an empty token: token=%q result=%+v", flags.Token, result)
	}
}

func TestApplySavedAgentTokenFailsWhenJSONCorrupt(t *testing.T) {
	restoreSavedTokenFlags(t)
	path := writeSavedIdentity(t, `{"uuid":`)
	flags.Token = ""
	flags.AutoDiscoveryKey = "legacy-key"
	if _, err := applySavedAgentTokenFrom(path); err == nil {
		t.Fatal("corrupt auto-discovery.json must stop startup")
	}
	if flags.Token != "" {
		t.Fatalf("Token = %q, want empty", flags.Token)
	}
}

func TestApplySavedAgentTokenFailsWhenUUIDOrTokenEmpty(t *testing.T) {
	restoreSavedTokenFlags(t)
	flags.Token = ""
	flags.AutoDiscoveryKey = "legacy-key"
	for _, body := range []string{
		`{"uuid":"","token":"saved-token"}`,
		`{"uuid":"node-uuid","token":""}`,
		`{"uuid":"node-uuid"}`,
	} {
		path := writeSavedIdentity(t, body)
		flags.Token = ""
		if _, err := applySavedAgentTokenFrom(path); err == nil {
			t.Fatalf("incomplete identity %s must stop startup", body)
		}
		if flags.Token != "" {
			t.Fatalf("Token = %q, want empty for %s", flags.Token, body)
		}
	}
}

func TestLoadSavedAgentTokenRejectsEmptyIdentity(t *testing.T) {
	path := writeSavedIdentity(t, `{"uuid":"","token":""}`)
	if _, err := loadSavedAgentToken(path); err == nil {
		t.Fatal("empty uuid/token must fail")
	}
}

func TestApplySavedAgentTokenFailsWhenFileEmpty(t *testing.T) {
	restoreSavedTokenFlags(t)
	path := writeSavedIdentity(t, " \n")
	flags.Token = ""
	flags.AutoDiscoveryKey = "legacy-key"
	if _, err := applySavedAgentTokenFrom(path); err == nil {
		t.Fatal("empty auto-discovery.json must stop startup")
	}
	if flags.Token != "" {
		t.Fatalf("Token = %q, want empty", flags.Token)
	}
}

func TestApplySavedAgentTokenDoesNotRegister(t *testing.T) {
	restoreSavedTokenFlags(t)
	path := writeSavedIdentity(t, `{"uuid":"node-uuid","token":"saved-token"}`)
	flags.Token = ""
	flags.AutoDiscoveryKey = "legacy-key"

	originalTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("saved identity must not send %s %s", req.Method, req.URL)
		return nil, nil
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	result, err := applySavedAgentTokenFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if !result.UsedSavedFile || flags.Token != "saved-token" {
		t.Fatalf("result = %+v token=%q", result, flags.Token)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
