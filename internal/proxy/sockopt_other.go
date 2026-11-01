//go:build !linux

package proxy

import (
	"net"
	"time"
)

func setUserTimeout(conn *net.TCPConn, d time.Duration) error {
	return nil
}
