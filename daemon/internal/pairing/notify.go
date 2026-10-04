package pairing

import (
	"context"
	"os/exec"
	"time"
)

// NotifyDesktop 用 notify-send 展示确认码（PRD §4.1）。
// 失败静默忽略——无桌面环境属正常，确认码仍经返回值/日志/padlinkctl 可见。
func NotifyDesktop(code string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "notify-send", "PadLink", "配对确认码: "+code)
	if err := cmd.Start(); err != nil {
		return
	}
	go func() { _ = cmd.Wait() }() // 后台收尸，不阻塞调用方
}
