package pairing

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// NotifyDesktop 展示确认码：优先系统弹窗（macOS osascript 对话框 / Linux zenity），
// 弹窗存活与确认码 TTL 对齐后自动消失；弹窗不可用回落桌面通知横幅。
// 全部失败静默忽略——无桌面环境属正常，确认码仍经返回值/日志/padlinkctl 可见。
func NotifyDesktop(code string) {
	name, args, ok := dialogCmd(runtime.GOOS, code, DefaultCodeTTL, exec.LookPath)
	if !ok {
		notify(code)
		return
	}
	// 弹窗进程放行到 TTL 之外再杀：ctx 只兜底挂死，不承担正常关闭。
	ctx, cancel := context.WithTimeout(context.Background(), DefaultCodeTTL+dialogGrace)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if err := cmd.Start(); err != nil {
		notify(code)
		return
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			notify(code) // 无 GUI 会话等失败：横幅兜底
		}
	}()
}

func notify(code string) {
	name, args := notifyCmd(code)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if err := cmd.Start(); err != nil {
		return
	}
	go func() { _ = cmd.Wait() }() // 后台收尸，不阻塞调用方
}

// dialogGrace 弹窗进程的额外存活余量（正常路径由 giving up after / --timeout 自行退出）。
const dialogGrace = 5 * time.Second

// dialogCmd 组装确认码弹窗命令；lookPath 注入（Linux zenity 探测），ok=false 表示平台无弹窗可用。
func dialogCmd(goos, code string, ttl time.Duration, lookPath func(string) (string, error)) (string, []string, bool) {
	secs := int(ttl / time.Second)
	switch goos {
	case "darwin":
		script := fmt.Sprintf(`display dialog "配对确认码：%s" with title "PadLink" buttons {"好的"} default button "好的" giving up after %d`, code, secs)
		return "osascript", []string{"-e", script}, true
	case "linux":
		if _, err := lookPath("zenity"); err != nil {
			return "", nil, false
		}
		return "zenity", []string{
			"--info", "--title=PadLink",
			"--text=" + fmt.Sprintf("配对确认码：%s", code),
			fmt.Sprintf("--timeout=%d", secs),
		}, true
	}
	return "", nil, false
}

func notifyCmd(code string) (string, []string) {
	if runtime.GOOS == "darwin" {
		return "osascript", []string{"-e", `display notification "配对确认码: ` + code + `" with title "PadLink"`}
	}
	return "notify-send", []string{"PadLink", "配对确认码: " + code}
}
