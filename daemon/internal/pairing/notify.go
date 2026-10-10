package pairing

import (
	"context"
	"os/exec"
	"runtime"
	"time"
)

// NotifyDesktop 桌面通知展示确认码（PRD §4.1）：Linux 用 notify-send，
// macOS 用 osascript display notification。失败静默忽略——无桌面环境
// 属正常，确认码仍经返回值/日志/padlinkctl 可见。
func NotifyDesktop(code string) {
	name, args := notifyCmd(code)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if err := cmd.Start(); err != nil {
		return
	}
	go func() { _ = cmd.Wait() }() // 后台收尸，不阻塞调用方
}

// notifyCmd 按平台选通知命令。
func notifyCmd(code string) (string, []string) {
	if runtime.GOOS == "darwin" {
		return "osascript", []string{"-e", `display notification "配对确认码: ` + code + `" with title "PadLink"`}
	}
	return "notify-send", []string{"PadLink", "配对确认码: " + code}
}
