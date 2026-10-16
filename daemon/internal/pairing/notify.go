package pairing

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// dialogTexts 弹窗/通知的本地化文案（中英）。
type dialogTexts struct {
	ok        string // 确认按钮
	hintLine  string // 单行说明（fallback 弹窗/zenity/通知用，含 code 与秒数两个占位）
	hintPre   string // 倒计时前缀（弹窗用）
	hintSuf   string // 倒计时后缀
	noticePre string // 通知横幅前缀
}

var textsZh = dialogTexts{
	ok:        "好的",
	hintLine:  "配对码：%s（%d 秒内有效）",
	hintPre:   "配对码 · 剩余 ",
	hintSuf:   " 秒",
	noticePre: "配对码: ",
}

var textsEn = dialogTexts{
	ok:        "OK",
	hintLine:  "Pairing code: %s (valid for %ds)",
	hintPre:   "Pairing code · ",
	hintSuf:   "s left",
	noticePre: "Pairing code: ",
}

// textsForLocale 按 locale 前缀选文案；未知/空回落英文（国际化惯例）。
func textsForLocale(loc string) dialogTexts {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(loc)), "zh") {
		return textsZh
	}
	return textsEn
}

// pickTexts 检测当前用户语言：macOS 读全局 AppleLocale（launchd 环境无 locale 变量），
// Linux 读 LC_ALL/LANG；全部缺失回落英文。
func pickTexts() dialogTexts {
	loc := ""
	if runtime.GOOS == "darwin" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "defaults", "read", "-g", "AppleLocale").Output(); err == nil {
			loc = string(out)
		}
	}
	if loc == "" {
		loc = os.Getenv("LC_ALL")
	}
	if loc == "" {
		loc = os.Getenv("LANG")
	}
	return textsForLocale(loc)
}

// 弹窗存活余量：弹窗自身按 TTL 退出的正常路径不受影响，仅兜底挂死进程。
const dialogGrace = 5 * time.Second

// runSilently 启动命令并后台收尸；返回 Start 是否成功。
// 超时（ctx 到点杀进程）属正常消失路径，不触发 onFail；非零退出（且非超时）触发。
func runSilently(name string, args []string, timeout time.Duration, onFail func()) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	cmd := exec.CommandContext(ctx, name, args...)
	if err := cmd.Start(); err != nil {
		cancel()
		return false
	}
	go func() {
		defer cancel()
		if err := cmd.Wait(); err != nil && ctx.Err() == nil && onFail != nil {
			onFail()
		}
	}()
	return true
}

// dialogCmdLinux 组装配对码弹窗命令；lookPath 注入（zenity 探测），ok=false 表示无弹窗可用。
func dialogCmdLinux(code string, ttl time.Duration, lookPath func(string) (string, error), texts dialogTexts) (string, []string, bool) {
	if _, err := lookPath("zenity"); err != nil {
		return "", nil, false
	}
	secs := int(ttl / time.Second)
	return "zenity", []string{
		"--info", "--title=PadLink",
		"--text=" + fmt.Sprintf(texts.hintLine, code, secs),
		fmt.Sprintf("--timeout=%d", secs),
	}, true
}

func notifyCmdLinux(texts dialogTexts, code string) (string, []string) {
	return "notify-send", []string{"PadLink", texts.noticePre + code}
}
