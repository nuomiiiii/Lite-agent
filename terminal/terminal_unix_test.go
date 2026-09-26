//go:build !windows

package terminal

import (
	"os"
	"os/exec"
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

func TestMotdShellPreludeIsPOSIXShSyntax(t *testing.T) {
	for _, shell := range []string{"/bin/bash", "/usr/bin/nu"} {
		cmd := exec.Command("/bin/sh", "-n", "-c", motdShellPreludeFor(shell))
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("MOTD prelude for %s is not valid POSIX sh syntax: %v\n%s", shell, err, output)
		}
	}
}
