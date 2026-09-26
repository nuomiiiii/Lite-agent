package terminal

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestShellFromPasswdUsesHomeMatch(t *testing.T) {
	content := strings.Join([]string{
		"root:x:0:0:root:/root:/bin/bash",
		"nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin",
		"dashuser:x:1000:1000::/home/dash:/bin/sh",
	}, "\n")

	if got := shellFromPasswd(content, "/root"); got != "/bin/bash" {
		t.Fatalf("root shell = %q", got)
	}
	if got := shellFromPasswd(content, "/home/dash"); got != "/bin/sh" {
		t.Fatalf("dash user shell = %q", got)
	}
	if got := shellFromPasswd(content, "/home/missing"); got != "" {
		t.Fatalf("missing home shell = %q", got)
	}
}

func TestSelectTerminalShell(t *testing.T) {
	available := func(names ...string) func(string) bool {
		set := map[string]bool{}
		for _, name := range names {
			set[name] = true
		}
		return func(name string) bool { return set[name] }
	}

	tests := []struct {
		name    string
		passwd  string
		lookup  func(string) bool
		want    string
		wantErr bool
	}{
		{name: "keep bash", passwd: "/bin/bash", lookup: available("/bin/bash", "zsh"), want: "/bin/bash"},
		{name: "keep zsh", passwd: "/usr/bin/zsh", lookup: available("/usr/bin/zsh", "bash"), want: "/usr/bin/zsh"},
		{name: "keep fish", passwd: "/usr/bin/fish", lookup: available("/usr/bin/fish", "zsh", "bash"), want: "/usr/bin/fish"},
		{name: "sh prefers zsh", passwd: "/bin/sh", lookup: available("/bin/sh", "zsh", "bash"), want: "zsh"},
		{name: "sh uses bash when zsh is missing", passwd: "/bin/sh", lookup: available("/bin/sh", "bash"), want: "bash"},
		{name: "dash stays when bash and zsh are missing", passwd: "/bin/dash", lookup: available("/bin/dash"), want: "/bin/dash"},
		{name: "ash prefers bash", passwd: "/bin/ash", lookup: available("/bin/ash", "bash"), want: "bash"},
		{name: "keep ksh", passwd: "/bin/ksh", lookup: available("/bin/ksh", "bash"), want: "/bin/ksh"},
		{name: "keep unknown shell", passwd: "/usr/bin/nu", lookup: available("/usr/bin/nu", "zsh"), want: "/usr/bin/nu"},
		{name: "empty passwd falls back to zsh", passwd: "", lookup: available("zsh", "bash", "sh"), want: "zsh"},
		{name: "empty passwd falls back to sh", passwd: "", lookup: available("sh"), want: "sh"},
		{name: "missing passwd shell falls back", passwd: "/bin/bash", lookup: available("zsh"), want: "zsh"},
		{name: "nothing available", passwd: "", lookup: available(), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectTerminalShell(tt.passwd, tt.lookup)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("selectTerminalShell: %v", err)
			}
			if got != tt.want {
				t.Fatalf("shell = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMotdPreludeInteractiveFlag(t *testing.T) {
	interactive := []string{"/usr/bin/fish", "/bin/ksh", "mksh", "/bin/dash", "/bin/ash", "sh", "/bin/csh", "/usr/bin/tcsh"}
	for _, shell := range interactive {
		prelude := motdShellPreludeFor(shell)
		if !strings.HasSuffix(prelude, `exec "$1" -i`) {
			t.Fatalf("%s prelude = %q", shell, prelude)
		}
		if strings.Contains(prelude, `||`) {
			t.Fatalf("%s prelude must not use an exec fallback: %q", shell, prelude)
		}
		if strings.Contains(prelude, `exec "$0"`) {
			t.Fatalf("%s prelude execs $0: %q", shell, prelude)
		}
		if !strings.HasPrefix(prelude, motdPreludePrefix) {
			t.Fatalf("%s prelude changed the motd prefix: %q", shell, prelude)
		}
	}

	bashPrelude := motdShellPreludeFor("/bin/bash")
	if !strings.Contains(bashPrelude, `exec "$1" -i --rcfile "$rc"`) || !strings.Contains(bashPrelude, "/usr/share/bash-completion/bash_completion") {
		t.Fatalf("bash prelude = %q", bashPrelude)
	}
	if strings.Contains(bashPrelude, `||`) || strings.Contains(bashPrelude, `exec "$0"`) {
		t.Fatalf("bash prelude = %q", bashPrelude)
	}
	zshPrelude := motdShellPreludeFor("/usr/bin/zsh")
	if !strings.Contains(zshPrelude, `ZDOTDIR="$rcdir" exec "$1" -i`) || !strings.Contains(zshPrelude, "compinit -u") {
		t.Fatalf("zsh prelude = %q", zshPrelude)
	}

	plain := motdShellPreludeFor("/usr/bin/nu")
	if !strings.HasSuffix(plain, `exec "$1"`) || strings.Contains(plain, "-i") {
		t.Fatalf("unknown shell prelude = %q", plain)
	}
}

func TestBuildMotdShellCommandArgs(t *testing.T) {
	const userShell = "/usr/bin/fish"
	cmd := buildMotdShellCommand(userShell)
	wantArgs := []string{"/bin/sh", "-c", motdShellPreludeFor(userShell), "lite-motd", userShell}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("unexpected command args:\nwant %#v\n got %#v", wantArgs, cmd.Args)
	}
	if cmd.Path != "/bin/sh" {
		t.Fatalf("expected MOTD prelude to run with /bin/sh, got %q", cmd.Path)
	}

	plain := buildMotdShellCommand("/usr/bin/nu")
	if strings.Contains(plain.Args[2], "-i") {
		t.Fatalf("unknown shell command includes -i: %#v", plain.Args)
	}
}

func TestWindowsTerminalCommandLine(t *testing.T) {
	powershell := `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`
	got := windowsTerminalCommandLine(func(name string) (string, error) {
		if name != "powershell.exe" {
			t.Fatalf("lookup name = %q", name)
		}
		return powershell, nil
	})
	if got != powershell {
		t.Fatalf("powershell command line = %q", got)
	}
	if strings.Contains(got, "-NoLogo") || strings.Contains(got, "-NoExit") || strings.Contains(got, "/f:on") {
		t.Fatalf("powershell command line was modified: %q", got)
	}

	missing := windowsTerminalCommandLine(func(string) (string, error) {
		return "", errors.New("not found")
	})
	if missing != "cmd.exe /f:on" {
		t.Fatalf("missing powershell command line = %q", missing)
	}

	empty := windowsTerminalCommandLine(func(string) (string, error) {
		return "  ", nil
	})
	if empty != "cmd.exe /f:on" {
		t.Fatalf("blank powershell command line = %q", empty)
	}
}
