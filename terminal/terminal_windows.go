//go:build windows

package terminal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/UserExistsError/conpty"
)

func newTerminalImpl() (*terminalImpl, error) {
	shell := windowsTerminalCommandLine(exec.LookPath)

	// 远程 Shell 从当前账户主目录启动，与 SSH 等常规登录体验一致。
	workingDir := terminalWorkingDirectory()

	// 启动 ConPTY
	tty, err := conpty.Start(shell, conpty.ConPtyWorkDir(workingDir))
	if err != nil {
		return nil, fmt.Errorf("failed to start conpty: %v", err)
	}

	// 设置初始终端大小
	tty.Resize(80, 24)

	return &terminalImpl{
		term: &windowsTerminal{
			tty: tty,
		},
	}, nil
}

func terminalWorkingDirectory() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if info, statErr := os.Stat(home); statErr == nil && info.IsDir() {
			return home
		}
	}
	if executable, err := os.Executable(); err == nil {
		return filepath.Dir(executable)
	}
	return "."
}

type windowsTerminal struct {
	tty    *conpty.ConPty
	closed bool
}

func (t *windowsTerminal) Close() error {
	if t.closed {
		return nil
	}
	if err := t.tty.Close(); err != nil {
		return err
	}
	t.closed = true
	return nil
}

func (t *windowsTerminal) Read(p []byte) (int, error) {
	return t.tty.Read(p)
}

func (t *windowsTerminal) Write(p []byte) (int, error) {
	return t.tty.Write(p)
}

func (t *windowsTerminal) Resize(cols, rows int) error {
	return t.tty.Resize(cols, rows)
}

func (t *windowsTerminal) Wait() error {
	_, err := t.tty.Wait(context.Background())
	return err
}
