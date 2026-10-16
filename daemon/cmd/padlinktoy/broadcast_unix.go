//go:build unix

package main

import (
	"net"
	"syscall"
)

// enableBroadcast 尽力开启 SO_BROADCAST；失败只影响广播路径，不算 fuzz 失败。
// Linux 内核要求显式开启才能发广播，其余 unix 由 Go 默认设置也保留同路。
func enableBroadcast(uc *net.UDPConn) {
	rc, err := uc.SyscallConn()
	if err != nil {
		return
	}
	_ = rc.Control(func(fd uintptr) {
		_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
	})
}
