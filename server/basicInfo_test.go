package server

import (
	"testing"

	pkg_flags "github.com/nuomiiiii/lite-agent/cmd/flags"
)

func TestBasicInfoIncludesRemoteProtocol(t *testing.T) {
	original := flags.RemoteControlEnabled
	flags.RemoteControlEnabled = true
	t.Cleanup(func() { flags.RemoteControlEnabled = original })

	data := buildBasicInfoMap()
	if data["remote_protocol"] != 2 {
		t.Fatalf("remote_protocol = %#v, want 2", data["remote_protocol"])
	}
	enabled, ok := data["remote_control_enabled"].(bool)
	if !ok {
		t.Fatalf("remote_control_enabled missing: %#v", data["remote_control_enabled"])
	}
	if !enabled {
		t.Fatal("remote_control_enabled should follow the positive flag")
	}
	if _, ok := data["remote_control_protected"]; ok {
		t.Fatal("basic info must not report remote_control_protected")
	}
}

func TestBasicInfoRemoteControlEnabledFollowsFlag(t *testing.T) {
	original := flags.RemoteControlEnabled
	t.Cleanup(func() { flags.RemoteControlEnabled = original })

	flags.RemoteControlEnabled = false
	if pkg_flags.RemoteControlEnabled() {
		t.Fatal("RemoteControlEnabled() should be false")
	}
	data := buildBasicInfoMap()
	if data["remote_control_enabled"] != false {
		t.Fatalf("remote_control_enabled = %#v, want false", data["remote_control_enabled"])
	}

	flags.RemoteControlEnabled = true
	data = buildBasicInfoMap()
	if data["remote_control_enabled"] != true {
		t.Fatalf("remote_control_enabled = %#v, want true", data["remote_control_enabled"])
	}
}
