package terminal

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

const motdPreludePrefix = `for f in /etc/update-motd.d/*; do [ -e "$f" ] && [ -x "$f" ] && "$f"; done; [ -r /etc/motd ] && cat /etc/motd; `

func shellBaseName(shell string) string {
	return filepath.Base(strings.TrimSpace(shell))
}

func isPlainSh(base string) bool {
	switch base {
	case "sh", "dash", "ash":
		return true
	default:
		return false
	}
}

func shellWantsInteractiveFlag(shell string) bool {
	switch shellBaseName(shell) {
	case "bash", "zsh", "fish", "ksh", "mksh", "dash", "ash", "sh", "csh", "tcsh":
		return true
	default:
		return false
	}
}

// shellFromPasswd returns the shell field of the first passwd line that
// mentions home. Matching stays the same as the previous line scan.
func shellFromPasswd(content, home string) string {
	if home == "" {
		return ""
	}
	for _, line := range strings.Split(content, "\n") {
		if !strings.Contains(line, home) {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) >= 7 && parts[6] != "" {
			return parts[6]
		}
	}
	return ""
}

// selectTerminalShell picks the shell to exec without starting a process.
// bash, zsh, and fish are kept. sh, dash, and ash are replaced by zsh or
// bash when one of those is available. lookup receives the passwd path for
// the current shell, and the short name for fallback candidates.
func selectTerminalShell(passwdShell string, lookup func(string) bool) (string, error) {
	shell := strings.TrimSpace(passwdShell)
	if shell != "" && !lookup(shell) {
		shell = ""
	}
	// zsh、bash 在前，最后才是 sh。plain sh 只借用前两个，避免又选回自己。
	fallback := []string{"zsh", "bash", "sh"}
	if shell != "" {
		if isPlainSh(shellBaseName(shell)) {
			for _, candidate := range fallback[:2] {
				if lookup(candidate) {
					return candidate, nil
				}
			}
		}
		return shell, nil
	}
	for _, candidate := range fallback {
		if lookup(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no supported shell found among %v", fallback)
}

func motdShellPreludeFor(shell string) string {
	suffix := `exec "$1"`
	if shellWantsInteractiveFlag(shell) {
		suffix = `exec "$1" -i`
	}
	return motdPreludePrefix + suffix
}

func buildMotdShellCommand(shell string) *exec.Cmd {
	return exec.Command("/bin/sh", "-c", motdShellPreludeFor(shell), "lite-motd", shell)
}

// windowsTerminalCommandLine is the command line passed to ConPTY.
// PowerShell is used unchanged. cmd.exe is only used when PowerShell is absent,
// and then file-name completion is turned on.
func windowsTerminalCommandLine(lookup func(string) (string, error)) string {
	shell, err := lookup("powershell.exe")
	if err != nil || strings.TrimSpace(shell) == "" {
		return "cmd.exe /f:on"
	}
	return shell
}
