//go:build windows

package pairing

import (
	"flag"
	"fmt"
	"os"
)

// RunDialogHelper 实现 padlinkd 的隐藏子命令 _pairing-dialog：原生 Win32 弹窗阻塞至
// 点击确认或倒计时归零；成功 0，失败非 0（父进程据此回落）。由 cmd/padlinkd/main 最早期分流。
// 标志集与 dialoghelper_darwin.go 对齐（除 icon：Windows 侧 M2 无 .ico 资产，参数留而不接）。
func RunDialogHelper(args []string) int {
	fs := flag.NewFlagSet("_pairing-dialog", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	code := fs.String("code", "", "配对码")
	_ = fs.String("icon", "", "ico 路径（M2 未接，保留参数对齐 darwin）")
	pre := fs.String("pre", "", "倒计时前缀")
	suf := fs.String("suf", "", "倒计时后缀")
	ok := fs.String("ok", "OK", "确认按钮文案")
	ttl := fs.Int("ttl", 60, "有效秒数")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := showPairDialog(*code, *ttl, *pre, *suf, *ok); err != nil {
		fmt.Fprintf(os.Stderr, "pairing-dialog: %v\n", err)
		return 1
	}
	return 0
}
