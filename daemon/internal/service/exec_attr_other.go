//go:build !windows

package service

import "os/exec"

// setDetached 非 windows 平台无此语义（start/stop 走 systemd/launchd），空实现。
func setDetached(*exec.Cmd) {}
