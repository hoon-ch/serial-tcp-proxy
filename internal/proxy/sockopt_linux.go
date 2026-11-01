package proxy

import (
	"net"
	"syscall"
	"time"
)

const tcpUserTimeout = 0x12

func setUserTimeout(conn *net.TCPConn, d time.Duration) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		sockErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, tcpUserTimeout, int(d.Milliseconds()))
	}); err != nil {
		return err
	}
	return sockErr
}
