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

// bash 只加 -i 时，readline 会补命令名（shut → shutdown），
// 但 ip 这类子命令要 bash-completion。很多系统把这段挂在登录 profile 上，
// 非登录的交互 shell 读不到。这里在最终的 bash 里补加载，不改成登录 shell。
const bashCompletionExec = `rc="$HOME/.cache/lite-agent/bashrc"
mkdir -p "$HOME/.cache/lite-agent" && cat > "$rc" << 'LITE_BASHRC'
if [ -f /etc/bash.bashrc ]; then
  . /etc/bash.bashrc
fi
if [ -f "$HOME/.bashrc" ]; then
  . "$HOME/.bashrc"
fi
if ! type _completion_loader >/dev/null 2>&1 && ! type __load_completion >/dev/null 2>&1; then
  if [ -f /usr/share/bash-completion/bash_completion ]; then
    . /usr/share/bash-completion/bash_completion
  elif [ -f /etc/bash_completion ]; then
    . /etc/bash_completion
  elif [ -f /etc/profile.d/bash_completion.sh ]; then
    . /etc/profile.d/bash_completion.sh
  fi
fi
LITE_BASHRC
if [ -f "$rc" ]; then
  exec "$1" -i --rcfile "$rc"
fi
exec "$1" -i`

// zsh 没有 compinit 时同样只补命令名。用单独的 ZDOTDIR 接上用户的 zshrc，再初始化补全。
const zshCompletionExec = `rcdir="$HOME/.cache/lite-agent/zsh"
mkdir -p "$rcdir" && cat > "$rcdir/.zshrc" << 'LITE_ZSHRC'
if [ -n "$LITE_AGENT_ZSHRC" ]; then
  return
fi
LITE_AGENT_ZSHRC=1
if [ -f "$HOME/.zshrc" ]; then
  . "$HOME/.zshrc"
fi
autoload -Uz compinit
compinit -u
LITE_ZSHRC
if [ -f "$rcdir/.zshrc" ]; then
  ZDOTDIR="$rcdir" exec "$1" -i
fi
exec "$1" -i`

func motdExecScript(shell string) string {
	switch shellBaseName(shell) {
	case "bash":
		return bashCompletionExec
	case "zsh":
		return zshCompletionExec
	default:
		if shellWantsInteractiveFlag(shell) {
			return `exec "$1" -i`
		}
		return `exec "$1"`
	}
}

func motdShellPreludeFor(shell string) string {
	return motdPreludePrefix + motdExecScript(shell)
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
