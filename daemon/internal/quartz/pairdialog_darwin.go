//go:build darwin

package quartz

/*
#cgo LDFLAGS: -framework AppKit
#include <stdlib.h>

int padlink_show_pairing_dialog(const char *code, const char *icon, const char *pre,
								const char *suf, const char *ok, int ttl_sec);
*/
import "C"

import (
	"fmt"
	"runtime"
	"unsafe"
)

// PairingDialogOptions 配对码弹窗参数（文案本地化在调用方完成）。
type PairingDialogOptions struct {
	Code       string
	IconPath   string // 空 = 不显示图标
	HintPre    string // 倒计时前缀
	HintSuf    string // 倒计时后缀
	OKTitle    string
	TTLSeconds int
}

// ShowPairingDialog 模态展示配对码弹窗，阻塞至点击或倒计时归零；
// 必须在进程主线程调用（Cocoa 要求），失败返回 error 由调用方回落。
func ShowPairingDialog(o PairingDialogOptions) error {
	if o.Code == "" || o.TTLSeconds < 1 {
		return fmt.Errorf("quartz: 弹窗参数不完整")
	}
	runtime.LockOSThread() // Cocoa 模态必须在主线程
	defer runtime.UnlockOSThread()

	code := C.CString(o.Code)
	defer C.free(unsafe.Pointer(code))
	pre := C.CString(o.HintPre)
	defer C.free(unsafe.Pointer(pre))
	suf := C.CString(o.HintSuf)
	defer C.free(unsafe.Pointer(suf))
	ok := C.CString(o.OKTitle)
	defer C.free(unsafe.Pointer(ok))
	icon := C.CString(o.IconPath)
	defer C.free(unsafe.Pointer(icon))

	if rc := C.padlink_show_pairing_dialog(code, icon, pre, suf, ok, C.int(o.TTLSeconds)); rc != 0 {
		return fmt.Errorf("quartz: 弹窗启动失败 (rc=%d)", int(rc))
	}
	return nil
}
