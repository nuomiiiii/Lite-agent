//go:build !windows

package monitoring

func platformConnectionsCount() (tcpCount, udpCount int, err error) {
	return gopsutilConnectionsCount()
}
