//go:build windows

package service

import (
	"os/exec"
	"syscall"
)

// setDetached 分离启动 daemon：不继承 ctl 控制台（用户关窗不得带走 daemon）、不闪新窗口、
// 独立进程组。daemon 的 stdout 由自身重定向到日志文件（cmd/padlinkd 的 windows 分支）。
// 0x00000008 = DETACHED_PROCESS（syscall 包未导出该常量）。
func setDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x00000008,
	}
}
