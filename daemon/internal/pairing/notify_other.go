//go:build !darwin && !windows

package pairing

import (
	"os/exec"
	"time"
)

// NotifyDesktop 展示配对码（非 macOS）：zenity 弹窗（自带 --timeout 与 TTL 对齐），
// 不可用回落 notify-send 横幅。全部失败静默忽略——配对码仍经返回值/日志/padlinkctl 可见。
// Linux zenity 无法逐秒刷新倒计时，--timeout 到点自行关闭。
func NotifyDesktop(code string) {
	texts := pickTexts()
	if name, args, ok := dialogCmdLinux(code, DefaultCodeTTL, exec.LookPath, texts); ok {
		runSilently(name, args, DefaultCodeTTL+dialogGrace, func() {
			name, args := notifyCmdLinux(texts, code)
			runSilently(name, args, 3*time.Second, nil)
		})
		return
	}
	name, args := notifyCmdLinux(texts, code)
	runSilently(name, args, 3*time.Second, nil)
}
