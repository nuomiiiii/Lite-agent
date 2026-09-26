//go:build !windows

package terminal

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// newTerminalImpl 创建一个新的终端实例。
// 从 /etc/passwd 查找默认 shell；sh、dash、ash 会改用已安装的 zsh 或 bash。
// 支持 -i 的 shell 在打印 motd 后以交互模式启动。bash、zsh 还会加载系统的参数补全。pty 失败时回退为无参数 shell。
func newTerminalImpl() (*terminalImpl, error) {
	passwdShell := ""
	userHomeDir, err := os.UserHomeDir()
	if err == nil {
		passwdContent, readErr := os.ReadFile("/etc/passwd")
		if readErr == nil {
			passwdShell = shellFromPasswd(string(passwdContent), userHomeDir)
		} else {
			log.Printf("Error reading /etc/passwd: %v\n", readErr)
		}
	} else {
		log.Printf("Error getting user home directory: %v\n", err)
	}

	lookup := func(name string) bool {
		_, lookErr := exec.LookPath(name)
		return lookErr == nil
	}
	passwdAvailable := passwdShell != "" && lookup(passwdShell)
	if passwdShell != "" && !passwdAvailable {
		log.Printf("Shell '%s' from /etc/passwd not found in PATH, falling back.\n", passwdShell)
	}

	shell, err := selectTerminalShell(passwdShell, lookup)
	if err != nil {
		return nil, err
	}
	if !passwdAvailable {
		log.Println("Shell not found or invalid, trying default shells.")
		log.Printf("Using default shell: %s\n", shell)
	} else if shell != passwdShell {
		log.Printf("Using shell %s instead of %s\n", shell, passwdShell)
	}

	cmd := buildMotdShellCommand(shell)
	prepareTerminalCommand(cmd)

	tty, err := pty.Start(cmd)
	if err != nil {
		// pty 失败时直接启动 shell，不带 -i。
		cmd = exec.Command(shell)
		prepareTerminalCommand(cmd)
		tty, err = pty.Start(cmd)
		if err != nil {
			return nil, fmt.Errorf("failed to start pty with argv0 prelude and plain shell: %v", err)
		}
	}

	// 设置初始终端大小
	pty.Setsize(tty, &pty.Winsize{Rows: 24, Cols: 80})

	return &terminalImpl{
		term: &unixTerminal{
			tty: tty,
			cmd: cmd,
		},
	}, nil
}

func prepareTerminalCommand(cmd *exec.Cmd) {
	cmd.Dir = terminalWorkingDirectory()
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
	)
}

func terminalWorkingDirectory() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "/"
	}
	if info, statErr := os.Stat(home); statErr != nil || !info.IsDir() {
		return "/"
	}
	return home
}

// unixTerminal 实现了 Unix 系统下的终端接口。
type unixTerminal struct {
	tty *os.File  // 伪终端设备文件
	cmd *exec.Cmd // 启动的 shell 进程命令
}

// Close 关闭终端，并尝试优雅地终止 shell 进程及其子进程。
func (t *unixTerminal) Close() error {
	if t.cmd == nil || t.cmd.Process == nil {
		return fmt.Errorf("terminal process is already nil or not started")
	}

	// 获取进程组 ID (PGID)。如果获取失败，则使用进程 PID 作为回退。
	// 向进程组发送信号可以确保 shell 启动的子进程也能接收到信号。
	pgid, err := syscall.Getpgid(t.cmd.Process.Pid)
	if err != nil {
		log.Printf("Failed to get process group ID for PID %d: %v. Using PID as PGID.\n", t.cmd.Process.Pid, err)
		pgid = t.cmd.Process.Pid
	}

	// 发送 SIGTERM 信号，请求进程组优雅退出
	log.Printf("Sending SIGTERM to process group %d...\n", pgid)
	_ = syscall.Kill(-pgid, syscall.SIGTERM) // -pgid 表示发送给进程组

	done := make(chan error, 1)
	go func() {
		// 等待命令退出。如果命令已经退出，Wait()会立即返回。
		done <- t.cmd.Wait()
	}()

	select {
	case err := <-done:
		// 进程已退出
		if err == nil {
			return nil
		}
		// 如果是 ExitError 且进程已退出，也视为成功关闭
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.Exited() {
			return nil // 进程已退出，尽管可能不是0状态码，但我们认为它已关闭
		}
		return fmt.Errorf("process group did not exit gracefully: %v", err)
	case <-time.After(5 * time.Second):
		// 5 秒内未退出，发送 SIGKILL 强制终止
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		// 再次等待，确保进程被杀死，并获取最终的退出状态
		killErr := <-done
		if killErr == nil {
			return nil
		}
		if exitErr, ok := killErr.(*exec.ExitError); ok && exitErr.Exited() {
			return nil
		}
		log.Printf("Failed to kill process group %d after SIGKILL: %v\n", pgid, killErr)
		return fmt.Errorf("failed to kill process group %d: %v", pgid, killErr)
	}
}

// Read 从伪终端读取数据。
func (t *unixTerminal) Read(p []byte) (int, error) {
	if t.tty == nil {
		return 0, fmt.Errorf("tty is nil")
	}
	return t.tty.Read(p)
}

// Write 向伪终端写入数据。
func (t *unixTerminal) Write(p []byte) (int, error) {
	if t.tty == nil {
		return 0, fmt.Errorf("tty is nil")
	}
	return t.tty.Write(p)
}

// Resize 调整伪终端的大小。
func (t *unixTerminal) Resize(cols, rows int) error {
	if t.tty == nil {
		return fmt.Errorf("tty is nil")
	}
	return pty.Setsize(t.tty, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

// Wait 等待 shell 进程退出。
func (t *unixTerminal) Wait() error {
	if t.cmd == nil {
		return fmt.Errorf("command is nil")
	}
	return t.cmd.Wait()
}
