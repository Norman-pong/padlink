//go:build windows

package pairing

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

var procGetUserUILanguage = syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")

func init() {
	sysLocaleHook = windowsLocale
}

// windowsLocale 读系统 UI 语言主码（LANGID 低 10 位）：中文 0x04 返回 zh，其余回落 en
// （textsForLocale 只区分 zh 前缀，非 zh 给什么都走英文）。
func windowsLocale() string {
	lang, _, _ := procGetUserUILanguage.Call()
	if lang == 0 {
		return ""
	}
	if lang&0x3FF == 0x04 {
		return "zh"
	}
	return "en"
}

// NotifyDesktop 展示配对码（Windows）：首选自旋本二进制的 _pairing-dialog 子命令弹原生
// 窗口（倒计时逐秒刷新、归零自关，进程分离不阻塞配对）；自旋失败回落进程内 MessageBox
// （无倒计时刷新，后台线程弹出）；全部失败静默——配对码仍经终端/padlinkctl/手机端可见。
func NotifyDesktop(code string) {
	texts := pickTexts()
	ttlSec := int(DefaultCodeTTL / time.Second)
	if exe, err := os.Executable(); err == nil && exe != "" {
		args := []string{
			"_pairing-dialog",
			"--code", code,
			"--ttl", strconv.Itoa(ttlSec),
			"--pre", texts.hintPre,
			"--suf", texts.hintSuf,
			"--ok", texts.ok,
		}
		if runSilently(exe, args, DefaultCodeTTL+dialogGrace, func() {
			msgBoxAsync(code, texts, ttlSec)
		}) {
			return
		}
	}
	msgBoxAsync(code, texts, ttlSec)
}

// msgBoxAsync 进程内 MessageBox 兜底：必须后台线程（MessageBox 自带模态循环，
// 阻塞配对 goroutine 会拖住后续配对轮次）。
func msgBoxAsync(code string, texts dialogTexts, ttlSec int) {
	go func() {
		runtime.LockOSThread()
		msg, e1 := syscall.UTF16PtrFromString(fmt.Sprintf(texts.hintLine, code, ttlSec))
		title, e2 := syscall.UTF16PtrFromString("PadLink")
		if e1 != nil || e2 != nil {
			return
		}
		// 0x40 = MB_ICONINFORMATION；同步弹完才回收线程
		user32Dlg.NewProc("MessageBoxW").Call(0,
			uintptr(unsafe.Pointer(msg)), uintptr(unsafe.Pointer(title)), 0x40)
	}()
}
