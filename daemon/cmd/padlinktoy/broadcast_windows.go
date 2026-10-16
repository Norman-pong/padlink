//go:build windows

package main

import "net"

// Go 的 Windows UDP 套接字默认已设 SO_BROADCAST（net 包 setDefaultSockopts），无需显式开启。
func enableBroadcast(*net.UDPConn) {}
