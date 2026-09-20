//go:build windows

package monitoring

import (
	"encoding/binary"
	"syscall"
	"unsafe"
)

var (
	iphlpapi         = syscall.NewLazyDLL("iphlpapi.dll")
	procGetTcpTable  = iphlpapi.NewProc("GetTcpTable")
	procGetUdpTable  = iphlpapi.NewProc("GetUdpTable")
	procGetTcp6Table = iphlpapi.NewProc("GetTcp6Table")
	procGetUdp6Table = iphlpapi.NewProc("GetUdp6Table")
)

const errorInsufficientBuffer = 122

func platformConnectionsCount() (tcpCount, udpCount int, err error) {
	tcp4, err := countIPHelperTable(procGetTcpTable)
	if err != nil {
		return gopsutilConnectionsCount()
	}
	udp4, err := countIPHelperTable(procGetUdpTable)
	if err != nil {
		return gopsutilConnectionsCount()
	}
	tcp6, _ := countIPHelperTable(procGetTcp6Table)
	udp6, _ := countIPHelperTable(procGetUdp6Table)
	return tcp4 + tcp6, udp4 + udp6, nil
}

func countIPHelperTable(proc *syscall.LazyProc) (int, error) {
	if err := proc.Find(); err != nil {
		return 0, err
	}
	var size uint32
	r1, _, e1 := proc.Call(0, uintptr(unsafe.Pointer(&size)), 0)
	if r1 == 0 {
		return 0, nil
	}
	if r1 != errorInsufficientBuffer {
		if e1 != syscall.Errno(0) {
			return 0, e1
		}
		return 0, syscall.EINVAL
	}
	if size < 4 {
		return 0, nil
	}
	buf := make([]byte, size)
	r1, _, e1 = proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0)
	if r1 != 0 {
		if e1 != syscall.Errno(0) {
			return 0, e1
		}
		return 0, syscall.EINVAL
	}
	if size < 4 {
		return 0, nil
	}
	return int(binary.LittleEndian.Uint32(buf[:4])), nil
}
