//go:build darwin

package pairing

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// NotifyDesktop 展示配对码（macOS）：一级为自身二进制的隐藏子命令 _pairing-dialog
//（原生 ObjC 弹窗，倒计时每秒刷新、归零自关；osascript 的 ASOC 脚本对象做 timer
// target 会因桥接缺陷全部失败，故不走脚本）；二级回落系统 display dialog（无倒计时）；
// 最后回落通知横幅。全部失败静默忽略——配对码仍经返回值/日志/padlinkctl 可见。
func NotifyDesktop(code string) {
	texts := pickTexts()
	plainDialog := func() {
		script := simpleDialogScriptDarwin(code, DefaultCodeTTL, macDialogIcon(), texts)
		if !runSilently("osascript", []string{"-e", script}, DefaultCodeTTL+dialogGrace, nil) {
			name, args := notifyCmdDarwin(texts, code)
			runSilently(name, args, 3*time.Second, nil)
		}
	}
	if exe, err := os.Executable(); err == nil {
		args := dialogHelperArgs(exe, code, DefaultCodeTTL, macDialogIcon(), texts)
		// 弹窗自身倒计时归零关闭；ctx 只兜底挂死进程
		if runSilently(exe, args, DefaultCodeTTL+dialogGrace, plainDialog) {
			return
		}
	}
	plainDialog()
}

// dialogHelperArgs 组装弹窗子命令参数（纯函数便于断言；exe 仅作首参占位，实际由调用方使用）。
func dialogHelperArgs(_ string, code string, ttl time.Duration, icon string, texts dialogTexts) []string {
	args := []string{
		"_pairing-dialog",
		"--code", code,
		"--ttl", strconv.Itoa(int(ttl / time.Second)),
		"--pre", texts.hintPre,
		"--suf", texts.hintSuf,
		"--ok", texts.ok,
	}
	if icon != "" {
		args = append(args, "--icon", icon)
	}
	return args
}

// macDialogIcon 弹窗图标（icns）：优先 daemon 所在 app bundle 的 Resources，
// 其次常见安装位置；找不到返回空（弹窗无图标，不阻断）。
func macDialogIcon() string {
	var cands []string
	if exe, err := os.Executable(); err == nil {
		cands = append(cands, filepath.Join(filepath.Dir(exe), "..", "Resources", "PadLink.icns"))
	}
	for _, root := range []string{"/Applications", filepath.Join(os.Getenv("HOME"), "Applications")} {
		cands = append(cands, filepath.Join(root, "PadLink.app", "Contents", "Resources", "PadLink.icns"))
	}
	for _, p := range cands {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// simpleDialogScriptDarwin 系统原生 display dialog（二级回落；文本形态，无字号与倒计时控制）。
func simpleDialogScriptDarwin(code string, ttl time.Duration, icon string, texts dialogTexts) string {
	secs := int(ttl / time.Second)
	script := `display dialog "` + escapeAppleScriptString(fmt.Sprintf(texts.hintLine, code, secs)) +
		`" with title "PadLink" buttons {"` + escapeAppleScriptString(texts.ok) +
		`"} default button "` + escapeAppleScriptString(texts.ok) +
		`" giving up after ` + strconv.Itoa(secs)
	if icon != "" {
		script += ` with icon file POSIX file "` + escapeAppleScriptString(icon) + `"`
	}
	return script
}

func escapeAppleScriptString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
}

func notifyCmdDarwin(texts dialogTexts, code string) (string, []string) {
	return "osascript", []string{"-e", `display notification "` + texts.noticePre + code + `" with title "PadLink"`}
}
