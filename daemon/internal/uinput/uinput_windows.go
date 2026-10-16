//go:build windows

package uinput

import (
	"fmt"
	"log"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"padlink/daemon/internal/inject"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procSendInput        = user32.NewProc("SendInput")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procGetAsyncKeyState = user32.NewProc("GetAsyncKeyState")
)

// INPUT 按 x64/arm64 布局（windows 目标全 64 位）：type(4)+pad(4)+联合体按 MOUSEINPUT
// 取 32 字节，总长 40。布局错 SendInput 会整批拒绝（init 断言兜底）。
type mouseEvt struct {
	typ uint32
	_   uint32
	mi  mouseInput
}

type mouseInput struct {
	dx, dy    int32
	mouseData uint32
	dwFlags   uint32
	time      uint32
	_         uint32
	extra     uintptr
}

type keybdEvt struct {
	typ uint32
	_   uint32
	ki  keybdInput
	_   [8]byte
}

type keybdInput struct {
	wVk, wScan uint16
	dwFlags    uint32
	time       uint32
	_          uint32
	extra      uintptr
}

const (
	inputMouse    = 0
	inputKeyboard = 1

	mevMove     = 0x0001
	mevLeftDown = 0x0002
	mevLeftUp   = 0x0004
	mevRightDn  = 0x0008
	mevRightUp  = 0x0010
	mevMidDn    = 0x0020
	mevMidUp    = 0x0040
	mevWheel    = 0x0800
	mevVirtual  = 0x4000
	mevAbsolute = 0x8000
	kevKeyUp    = 0x0002
	kevScan     = 0x0008
	kevExtended = 0x0100

	smXVirtual  = 76
	smYVirtual  = 77
	smWVirtual  = 78
	smHVirtual  = 79
	vkCapital   = 0x14
	vkPause     = 0x13
	wheelDelta  = 120
	sizeofInput = 40
)

func init() {
	if unsafe.Sizeof(mouseEvt{}) != sizeofInput || unsafe.Sizeof(keybdEvt{}) != sizeofInput {
		panic(fmt.Sprintf("uinput(windows): INPUT 结构尺寸异常 mouse=%d keybd=%d（应均为 %d）",
			unsafe.Sizeof(mouseEvt{}), unsafe.Sizeof(keybdEvt{}), sizeofInput))
	}
}

// Device 是 Windows SendInput 注入后端，实现 inject.DeviceWriter。
// 与 darwin 后端同款缓冲-Sync 模式：REL 位移在 Sync 时合成一次绝对定位
// （尖峰 A 实证绝对定位完全绕开「提高指针精确度」弹道，触摸板手感不受用户设置污染）；
// 滚轮无 hi-res 注入通道，HI_RES 按 1/120 格跨帧累积到整格再发（已知限制：整格滚动）。
type Device struct {
	mu    sync.Mutex
	dx    int32 // 待合成 REL_X 累积（像素）
	dy    int32 // 待合成 REL_Y 累积（像素）
	wheel int32 // 待合成 REL_WHEEL_HI_RES 累积（1/120 格，跨帧保留余量）
}

// Open 返回 SendInput 后端。SendInput 只在交互桌面会话内有效——经 SSH/服务 Session 0
// 启动时注入会被静默丢弃（winspike 实测），padlinkd 须以登录用户身份运行（HKCU Run 键）。
func Open(*log.Logger) (*Device, error) {
	return &Device{}, nil
}

// SetPointerState 空实现：尖峰 D 实证系统按接收窗口类样式自动合成连击
// （DBLCLK 替换第二次 DOWN），注入端只发裸 down/up，点击计数无消费方（同 Linux）。
func (d *Device) SetPointerState(st inject.PointerState) {}

// DoubleClickInterval 返回 0：多击判定在接收端由系统完成，注入端不消费该阈值。
func DoubleClickInterval() time.Duration { return 0 }

// KeyEvent 投递键盘/鼠标按钮事件；value 非 0 即按下（含连发值 2）。
func (d *Device) KeyEvent(code uint16, value int32) error {
	if flags, ok := buttonFlags(code, value); ok {
		return sendMouse(0, 0, 0, flags)
	}
	sk, ok := keyToScancode[code]
	if !ok {
		return fmt.Errorf("uinput(windows): KEY code %d 无 scancode 映射", code)
	}
	if sk.vkOnly {
		// Pause 的 set-1 序列是多字节 E1 14 77，SCANCODE 通道发不出，走 VK 例外
		return sendKeyVK(vkPause, value == 0)
	}
	return sendKeyScan(sk.code, sk.ext, value == 0)
}

// RelEvent 只缓冲不投递，Sync 收帧时统一合成（同 darwin）。
func (d *Device) RelEvent(code uint16, value int32) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch code {
	case inject.RelX:
		d.dx += value
	case inject.RelY:
		d.dy += value
	case inject.RelWheelHiRes:
		d.wheel += value
	case inject.RelWheel:
		// legacy 补发是 Linux 内核惯例，windows 只消费 HI_RES（双发会重复滚动）
	default:
		return fmt.Errorf("uinput(windows): 未知 REL code %d", code)
	}
	return nil
}

// Sync 收帧：缓冲位移按绝对坐标合成一次 move（虚拟桌面归一化，多显示器可达），
// 滚动按累积余量发整格（余量跨帧保留，慢速滚动不丢）。
func (d *Device) Sync() error {
	d.mu.Lock()
	dx, dy := d.dx, d.dy
	notches, remainder := wheelSplit(d.wheel)
	d.dx, d.dy = 0, 0
	// 余量跨帧保留（wheel 无 hi-res 注入通道的降级补偿）
	d.wheel = remainder
	d.mu.Unlock()

	if dx != 0 || dy != 0 {
		var p point
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
		if err := sendMouse(int32(absNorm(p.x+dx, vx(), vw())), int32(absNorm(p.y+dy, vy(), vh())), 0, mevMove|mevAbsolute|mevVirtual); err != nil {
			return fmt.Errorf("uinput(windows): 位移注入: %w", err)
		}
	}
	if notches != 0 {
		if err := sendMouse(0, 0, uint32(notches*wheelDelta), mevWheel); err != nil {
			return fmt.Errorf("uinput(windows): 滚轮注入: %w", err)
		}
	}
	return nil
}

type point struct{ x, y int32 }

// Close Windows 后端无设备句柄，空操作。
func (d *Device) Close() error { return nil }

// wheelSplit 把 HI_RES 累积（1/120 格）拆成整格与跨帧余量；余量保留使慢速滚动不丢格。
func wheelSplit(accum int32) (notches, remainder int32) {
	notches = accum / wheelDelta
	return notches, accum - notches*wheelDelta
}

// absNorm 像素坐标 → 0–65535 绝对坐标（相对虚拟桌面原点归一化）。
func absNorm(x, origin, span int32) uint32 {
	if span <= 1 {
		return 0
	}
	rel := x - origin
	if rel < 0 {
		rel = 0
	}
	if rel > span-1 {
		rel = span - 1
	}
	return uint32(uint64(rel) * 65535 / uint64(span-1))
}

func vx() int32 { return int32(metric(smXVirtual)) }
func vy() int32 { return int32(metric(smYVirtual)) }
func vw() int32 { return int32(metric(smWVirtual)) }
func vh() int32 { return int32(metric(smHVirtual)) }

func metric(idx uintptr) uint32 {
	v, _, _ := procGetSystemMetrics.Call(idx)
	return uint32(v)
}

// buttonFlags 把 BTN_* 映射为 MOUSEEVENTF_* 按下/抬起标志；非按钮 code 返回 ok=false。
func buttonFlags(code uint16, value int32) (uint32, bool) {
	down := value != 0
	switch code {
	case inject.BtnLeft:
		return pick(down, mevLeftDown, mevLeftUp), true
	case inject.BtnRight:
		return pick(down, mevRightDn, mevRightUp), true
	case inject.BtnMiddle:
		return pick(down, mevMidDn, mevMidUp), true
	}
	return 0, false
}

func pick(down bool, a, b uint32) uint32 {
	if down {
		return a
	}
	return b
}

func sendMouse(dx, dy int32, mouseData, flags uint32) error {
	e := mouseEvt{typ: inputMouse, mi: mouseInput{dx: dx, dy: dy, mouseData: mouseData, dwFlags: flags}}
	r, _, _ := procSendInput.Call(1, uintptr(unsafe.Pointer(&e)), sizeofInput)
	if r != 1 {
		return fmt.Errorf("uinput(windows): SendInput(mouse flags=%#x) 被拒绝", flags)
	}
	return nil
}

func sendKeyScan(scan uint8, ext, up bool) error {
	flags := uint32(kevScan)
	if ext {
		flags |= kevExtended
	}
	if up {
		flags |= kevKeyUp
	}
	e := keybdEvt{typ: inputKeyboard, ki: keybdInput{wScan: uint16(scan), dwFlags: flags}}
	r, _, _ := procSendInput.Call(1, uintptr(unsafe.Pointer(&e)), sizeofInput)
	if r != 1 {
		return fmt.Errorf("uinput(windows): SendKey(scan=0x%02X ext=%v up=%v) 被拒绝", scan, ext, up)
	}
	return nil
}

func sendKeyVK(vk uint16, up bool) error {
	flags := uint32(0)
	if up {
		flags = kevKeyUp
	}
	e := keybdEvt{typ: inputKeyboard, ki: keybdInput{wVk: vk, dwFlags: flags}}
	r, _, _ := procSendInput.Call(1, uintptr(unsafe.Pointer(&e)), sizeofInput)
	if r != 1 {
		return fmt.Errorf("uinput(windows): SendKey(vk=0x%02X) 被拒绝", vk)
	}
	return nil
}

// CapsLockActive 报告主机大写锁定是否生效。scancode 注入与物理键盘同路（经键盘布局），
// 主机大写锁会再反转一次字母——与 Linux 同因，走既有「手机意图 XOR 主机大写锁」补偿。
// GetAsyncKeyState（非 GetKeyState）：后者要求线程泵消息才准确，daemon 无窗口泵。
func CapsLockActive() bool {
	r, _, _ := procGetAsyncKeyState.Call(vkCapital)
	return r&1 != 0
}
