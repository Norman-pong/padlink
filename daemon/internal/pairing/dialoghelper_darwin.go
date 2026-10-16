//go:build darwin

package pairing

import (
	"flag"
	"fmt"
	"os"

	"padlink/daemon/internal/quartz"
)

// RunDialogHelper 实现 padlinkd 的隐藏子命令 _pairing-dialog：原生 ObjC 弹窗阻塞至
// 点击或倒计时归零；成功 0，失败非 0（父进程据此回落）。由 cmd/padlinkd/main 在最早期分流。
func RunDialogHelper(args []string) int {
	fs := flag.NewFlagSet("_pairing-dialog", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	code := fs.String("code", "", "配对码")
	icon := fs.String("icon", "", "icns 路径（空 = 无图标）")
	pre := fs.String("pre", "", "倒计时前缀")
	suf := fs.String("suf", "", "倒计时后缀")
	ok := fs.String("ok", "OK", "确认按钮文案")
	ttl := fs.Int("ttl", 60, "有效秒数")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	err := quartz.ShowPairingDialog(quartz.PairingDialogOptions{
		Code:       *code,
		IconPath:   *icon,
		HintPre:    *pre,
		HintSuf:    *suf,
		OKTitle:    *ok,
		TTLSeconds: *ttl,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "pairing-dialog: %v\n", err)
		return 1
	}
	return 0
}
