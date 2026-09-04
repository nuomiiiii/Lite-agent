//go:build !windows

package terminal

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestTerminalWorkingDirectoryUsesCurrentUserHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("current user home is unavailable: %v", err)
	}
	if got := terminalWorkingDirectory(); got != home {
		t.Fatalf("expected terminal working directory %q, got %q", home, got)
	}
}

func TestBuildMotdShellCommandUsesPOSIXShell(t *testing.T) {
	const userShell = "/usr/bin/fish"

	cmd := buildMotdShellCommand(userShell)

	if cmd.Path != "/bin/sh" {
		t.Fatalf("expected MOTD prelude to run with /bin/sh, got %q", cmd.Path)
	}

	wantArgs := []string{"/bin/sh", "-c", motdShellPrelude, "lite-motd", userShell}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("unexpected command args:\nwant %#v\n got %#v", wantArgs, cmd.Args)
	}
}

func TestMotdShellPreludeExecsSelectedShellFromArgument(t *testing.T) {
	if !strings.Contains(motdShellPrelude, `exec "$1"`) {
		t.Fatalf("expected prelude to exec selected shell from $1, got %q", motdShellPrelude)
	}
	if strings.Contains(motdShellPrelude, `exec "$0"`) {
		t.Fatalf("prelude should not exec $0 because it is reserved for the wrapper name: %q", motdShellPrelude)
	}
}

func TestMotdShellPreludeIsPOSIXShSyntax(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-n", "-c", motdShellPrelude)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("MOTD prelude is not valid POSIX sh syntax: %v\n%s", err, output)
	}
}
