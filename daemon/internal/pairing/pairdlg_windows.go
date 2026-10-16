//go:build windows

package pairing

import (
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32Dlg            = syscall.NewLazyDLL("user32.dll")
	procDlgGetSysMetrics = user32Dlg.NewProc("GetSystemMetrics")
	procRegisterClassD   = user32Dlg.NewProc("RegisterClassExW")
	procCreateWindowD    = user32Dlg.NewProc("CreateWindowExW")
	procDefWindowProcD   = user32Dlg.NewProc("DefWindowProcW")
	procGetMessageD      = user32Dlg.NewProc("GetMessageW")
	procTranslateMsgD    = user32Dlg.NewProc("TranslateMessage")
	procDispatchMsgD     = user32Dlg.NewProc("DispatchMessageW")
	procSetTimer         = user32Dlg.NewProc("SetTimer")
	procDestroyWindowD   = user32Dlg.NewProc("DestroyWindow")
	procPostQuitMsgD     = user32Dlg.NewProc("PostQuitMessage")
	procSetWindowTextD   = user32Dlg.NewProc("SetWindowTextW")
	procSendMessageD     = user32Dlg.NewProc("SendMessageW")
	procSetTextColorD    = syscall.NewLazyDLL("gdi32.dll").NewProc("SetTextColor")
	procGetSysBrushD     = user32Dlg.NewProc("GetSysColorBrush")
	procGetModuleHandleD = syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW")
	procCreateFontD      = syscall.NewLazyDLL("gdi32.dll").NewProc("CreateFontW")
	procGetStockObjectD  = syscall.NewLazyDLL("gdi32.dll").NewProc("GetStockObject")
)

const (
	dlgWsVisible  = 0x10000000
	dlgWsOverlapt = 0x00CF0000
	dlgExTopmost  = 0x00000008
	dlgExTool     = 0x00000080
	dlgWsChild    = 0x40000000
	dlgSsCenter   = 0x00000001
	dlgWmCommand  = 0x0111
	dlgWmTimer    = 0x0113
	dlgWmDestroy  = 0x0002
	dlgWmSetFont  = 0x0030
	dlgBsDefPush  = 0x00000001
	dlgWmCtlColor = 0x0138
	dlgIdok       = 1
	dlgTimerID    = 1
	dlgGuiFont    = 17 // DEFAULT_GUI_FONT
	smCxScreenD   = 0
	smCyScreenD   = 1
)

// dlgState 承载运行中的弹窗。helper 进程一次只弹一个（自旋子命令），包级变量即安全；
// 倒计时由 WM_TIMER 在泵线程触发，加锁读改。
type dlgState struct {
	mu        sync.Mutex
	remaining int
	pre, suf  string
	hintCtrl  uintptr
}

var dlg dlgState

// 布局：窗口 360×200，标题 / 40pt 配对码 / 12pt 倒计时 / 确认按钮。
const (
	dlgW, dlgH = 360, 200
	margin     = 20
	codeY      = 56
	codeH      = 64
	hintY      = 130
	hintH      = 22
	btnW, btnH = 90, 30
)

func metricD(idx uintptr) uint32 {
	v, _, _ := procDlgGetSysMetrics.Call(idx)
	return uint32(v)
}

func createFont(heightPx int32, weight uint32) uintptr {
	// CreateFontW 全参按序：高度取负（字符高度语义），其余默认，字面 Segoe UI
	face, _ := syscall.UTF16PtrFromString("Segoe UI")
	h, _, _ := procCreateFontD.Call(
		uintptr(int32(heightPx)), 0, 0, 0,
		uintptr(weight), 0, 0, 0,
		0 /*DEFAULT_CHARSET*/, 0 /*OUT_DEFAULT_PRECIS*/, 0, 0,
		0 /*CLEARTYPE_QUALITY 无所谓*/, 0,
		uintptr(unsafe.Pointer(face)))
	return h
}

func makeControl(parent, inst uintptr, cls, text *uint16, x, y, w, h int32, style uintptr, font uintptr) uintptr {
	hc, _, _ := procCreateWindowD.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(text)),
		style|dlgWsChild|dlgWsVisible, uintptr(x), uintptr(y), uintptr(w), uintptr(h), parent, 0, inst, 0)
	procSendMessageD.Call(hc, dlgWmSetFont, font, 1)
	return hc
}

// dlgWndProc 回调签名必须全指针尺寸（syscall.NewCallback 硬约束，尖峰实证）。
func dlgWndProc(hwnd, msg, wp, lp uintptr) uintptr {
	switch msg {
	case dlgWmTimer:
		dlg.mu.Lock()
		dlg.remaining--
		if dlg.remaining > 0 {
			t, _ := syscall.UTF16PtrFromString(dlg.pre + strconv.Itoa(dlg.remaining) + dlg.suf)
			procSetWindowTextD.Call(dlg.hintCtrl, uintptr(unsafe.Pointer(t)))
			dlg.mu.Unlock()
		} else {
			dlg.mu.Unlock()
			procDestroyWindowD.Call(hwnd) // 归零自关（与 darwin abortModal 同语义）
		}
		return 0
	case dlgWmCommand:
		if wp&0xFFFF == dlgIdok { // 低 16 位是控件 ID
			procDestroyWindowD.Call(hwnd)
		}
		return 0
	case dlgWmCtlColor:
		// 倒计时行置灰：lParam 是控件句柄
		if lp == dlg.hintCtrl {
			procSetTextColorD.Call(wp, 0x00999999)           // COLORREF BGR 灰
			if b, _, _ := procGetSysBrushD.Call(1); b != 0 { // COLOR_WINDOW
				return b
			}
		}
	case dlgWmDestroy:
		procPostQuitMsgD.Call(0)
	}
	r, _, _ := procDefWindowProcD.Call(hwnd, msg, wp, lp)
	return r
}

// showPairDialog 弹出配对码对话框并阻塞至点击确认或倒计时归零。
// 整个函数锁 OS 线程内联执行——窗口创建线程必须等于泵消息线程（尖峰实证）。
func showPairDialog(code string, ttlSec int, pre, suf, okTitle string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	dlg.mu.Lock()
	dlg.remaining, dlg.pre, dlg.suf = ttlSec, pre, suf
	dlg.mu.Unlock()

	clsName, _ := syscall.UTF16PtrFromString("PLPairDlg")
	inst, _, _ := procGetModuleHandleD.Call(0)
	wc := wndClassExD{
		size:       80,
		wndProc:    syscall.NewCallback(dlgWndProc),
		instance:   inst,
		background: 6, // COLOR_WINDOW+1
		className:  uintptr(unsafe.Pointer(clsName)),
	}
	if r, _, err := procRegisterClassD.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassExW: %v", err)
	}

	wx, wy := (int32(metricD(smCxScreenD))-dlgW)/2, (int32(metricD(smCyScreenD))-dlgH)/2
	title, _ := syscall.UTF16PtrFromString("PadLink")
	hwnd, _, err := procCreateWindowD.Call(
		dlgExTopmost|dlgExTool,
		uintptr(unsafe.Pointer(clsName)), uintptr(unsafe.Pointer(title)),
		dlgWsOverlapt|dlgWsVisible,
		uintptr(wx), uintptr(wy), dlgW, dlgH, 0, 0, inst, 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW: %v", err)
	}

	guiFont, _, _ := procGetStockObjectD.Call(dlgGuiFont)
	titleFont := createFont(-22, 600)
	codeFont := createFont(-54, 700)
	hintFont := createFont(-16, 400)

	brand, _ := syscall.UTF16PtrFromString("PadLink")
	codeT, _ := syscall.UTF16PtrFromString(code)
	hintT, _ := syscall.UTF16PtrFromString(pre + strconv.Itoa(ttlSec) + suf)
	okT, _ := syscall.UTF16PtrFromString(okTitle)
	staticCls, _ := syscall.UTF16PtrFromString("STATIC")
	buttonCls, _ := syscall.UTF16PtrFromString("BUTTON")

	makeControl(hwnd, inst, staticCls, brand, margin, 16, dlgW-2*margin, 26, 0, titleFont)
	makeControl(hwnd, inst, staticCls, codeT, margin, codeY, dlgW-2*margin, codeH, dlgSsCenter, codeFont)
	dlg.mu.Lock()
	dlg.hintCtrl = makeControl(hwnd, inst, staticCls, hintT, margin, hintY, dlgW-2*margin, hintH, dlgSsCenter, hintFont)
	dlg.mu.Unlock()
	makeControl(hwnd, inst, buttonCls, okT, dlgW-btnW-margin, dlgH-btnH-12, btnW, btnH, dlgBsDefPush, guiFont)

	procSetTimer.Call(hwnd, dlgTimerID, 1000, 0)

	var m [64]byte // win32 MSG 48 字节；泵只透传指针给 Get/Dispatch，不解字段
	for {
		r, _, _ := procGetMessageD.Call(uintptr(unsafe.Pointer(&m[0])), 0, 0, 0)
		if r == 0 || uintptr(r) == ^uintptr(0) {
			break
		}
		// GetMessage 后必须 Dispatch（尖峰实证：漏了回调永不执行）
		procTranslateMsgD.Call(uintptr(unsafe.Pointer(&m[0])))
		procDispatchMsgD.Call(uintptr(unsafe.Pointer(&m[0])))
	}
	return nil
}

// wndClassExD 按 x64 WNDCLASSEXW 布局手排（80 字节：4+4+8+4+4+8×6+8+8）。
type wndClassExD struct {
	size                     uint32
	style                    uint32
	wndProc                  uintptr
	clsExtra, wndExtra       int32
	instance                 uintptr
	icon, cursor, background uintptr
	menuName                 uintptr
	className                uintptr
	iconSm                   uintptr
}
