package terminal

import (
	"time"

	pkg_flags "github.com/nuomiiiii/lite-agent/cmd/flags"
)

var flags = pkg_flags.GlobalConfig

// Terminal 接口定义平台特定的终端操作
type Terminal interface {
	Close() error
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(cols, rows int) error
	Wait() error
}

// terminalImpl 封装终端和平台特定逻辑
type terminalImpl struct {
	shell      string
	workingDir string
	term       Terminal
}

func remoteControlEnabled() bool {
	return pkg_flags.RemoteControlEnabled()
}

// gracefulShutdown 尝试优雅地关闭终端
func gracefulShutdown(term Terminal) {
	//  Ctrl+C
	for i := 0; i < 3; i++ {
		if _, err := term.Write([]byte{3}); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(200 * time.Millisecond)

	//  Ctrl+D (EOF)
	term.Write([]byte{4})
	time.Sleep(100 * time.Millisecond)

	term.Write([]byte("exit\n"))
	time.Sleep(100 * time.Millisecond)
}
