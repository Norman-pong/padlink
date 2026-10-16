//go:build !darwin && !windows

package pairing

import (
	"fmt"
	"os"
)

// RunDialogHelper 非 macOS 无原生弹窗子命令（Linux 走 zenity，不经过本路径）。
func RunDialogHelper(args []string) int {
	fmt.Fprintln(os.Stderr, "pairing-dialog: 当前平台不支持原生弹窗子命令")
	return 2
}
