//go:build darwin

package pairing

import (
	"strings"
	"testing"
	"time"
)

func TestDialogHelperArgs(t *testing.T) {
	args := dialogHelperArgs("/bin/padlinkd", "1234", 60*time.Second, "/x/PadLink.icns", textsZh)
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"_pairing-dialog", "--code 1234", "--ttl 60",
		"--pre 配对码 · 剩余 ", "--suf  秒", "--ok 好的", "--icon /x/PadLink.icns",
	} {
		if !strings.Contains(joined+" ", want) {
			t.Errorf("参数缺 %q: %v", want, args)
		}
	}
	noIcon := dialogHelperArgs("/bin/padlinkd", "1234", 60*time.Second, "", textsEn)
	if strings.Contains(strings.Join(noIcon, " "), "--icon") {
		t.Errorf("无图标时不应带 --icon: %v", noIcon)
	}
}

func TestSimpleDialogScriptDarwin(t *testing.T) {
	script := simpleDialogScriptDarwin("1234", 60*time.Second, "", textsZh)
	for _, want := range []string{"display dialog", "配对码：1234（60 秒内有效）", "giving up after 60"} {
		if !strings.Contains(script, want) {
			t.Errorf("脚本缺 %q: %s", want, script)
		}
	}
	withIcon := simpleDialogScriptDarwin("1234", 60*time.Second, "/x/PadLink.icns", textsEn)
	if !strings.Contains(withIcon, `with icon file POSIX file "/x/PadLink.icns"`) {
		t.Errorf("带图标时缺图标子句: %s", withIcon)
	}
	if !strings.Contains(withIcon, "Pairing code: 1234 (valid for 60s)") {
		t.Errorf("英文回落文案缺失: %s", withIcon)
	}
}

func TestEscapeAppleScriptString(t *testing.T) {
	if got := escapeAppleScriptString(`a"b\c`); got != `a\"b\\c` {
		t.Errorf("转义结果 = %q", got)
	}
}

func TestNotifyCmdDarwin(t *testing.T) {
	name, args := notifyCmdDarwin(textsZh, "1234")
	if name != "osascript" || !strings.Contains(strings.Join(args, " "), "配对码: 1234") {
		t.Errorf("通知命令不符: %q %v", name, args)
	}
}
