//go:build darwin

// Package quartz 是 macOS Quartz CGEvent 注入原语的薄 cgo 封装（PRD 附录 B v2 路线）。
// 只暴露函数级原语；累积、键码映射、权限门控等业务语义全部留在 Go 侧调用方。
// 未获 TCC 辅助功能权限时 Post* 的事件被系统静默丢弃（docs/PLAN-MACOS.md §0 实测），
// 调用方必须先过 Trusted 门控再注入。
package quartz

/*
#cgo LDFLAGS: -framework ApplicationServices -framework CoreFoundation -framework AppKit
#include <ApplicationServices/ApplicationServices.h>

// CGEventCreateScrollWheelEvent 为变参函数，cgo 不能直调；统一以 static 助手收口。

static Boolean axTrusted(int prompt) {
	if (!prompt) {
		return AXIsProcessTrusted();
	}
	const void *keys[] = { kAXTrustedCheckOptionPrompt };
	const void *vals[] = { kCFBooleanTrue };
	CFDictionaryRef opts = CFDictionaryCreate(NULL, keys, vals, 1,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	Boolean r = AXIsProcessTrustedWithOptions(opts);
	CFRelease(opts);
	return r;
}

static CGPoint cursorPos(void) {
	CGEventRef ev = CGEventCreate(NULL);
	if (ev == NULL) {
		return CGPointZero;
	}
	CGPoint p = CGEventGetLocation(ev);
	CFRelease(ev);
	return p;
}

static int postMouseMove(double x, double y, long dx, long dy, int buttonsDown, long clickState) {
	// 按住按钮时的位移必须走 mouseDragged：投递 mouseMoved 时应用收不到拖拽
	// （NSView.mouseDragged: 不触发，拖文件/选词/拖窗口全部失效）。
	CGEventType type = kCGEventMouseMoved;
	CGMouseButton b = kCGMouseButtonLeft;
	if (buttonsDown & 1) {
		type = kCGEventLeftMouseDragged;
		b = kCGMouseButtonLeft;
	} else if (buttonsDown & 4) {
		type = kCGEventRightMouseDragged;
		b = kCGMouseButtonRight;
	} else if (buttonsDown & 2) {
		type = kCGEventOtherMouseDragged;
		b = kCGMouseButtonCenter;
	}
	CGEventRef ev = CGEventCreateMouseEvent(NULL, type, CGPointMake(x, y), b);
	if (ev == NULL) {
		return -1;
	}
	CGEventSetIntegerValueField(ev, kCGMouseEventDeltaX, dx);
	CGEventSetIntegerValueField(ev, kCGMouseEventDeltaY, dy);
	// 拖拽事件同样携带点击序号，应用才能区分"双击后拖拽"与"普通拖拽"。
	if (clickState > 0) {
		CGEventSetIntegerValueField(ev, kCGMouseEventClickState, clickState);
	}
	CGEventPost(kCGHIDEventTap, ev);
	CFRelease(ev);
	return 0;
}

static int postScrollPixel(long dy) {
	CGEventRef ev = CGEventCreateScrollWheelEvent(NULL, kCGScrollEventUnitPixel, 1, (int)dy);
	if (ev == NULL) {
		return -1;
	}
	// 像素单位事件按触控板惯例标记为连续滚动，平滑滚动视图才按像素消费。
	CGEventSetIntegerValueField(ev, kCGScrollWheelEventIsContinuous, 1);
	CGEventPost(kCGHIDEventTap, ev);
	CFRelease(ev);
	return 0;
}

// flags 必须显式设置：CGEvent 投递的合成修饰键 keyDown 不会进入系统修饰状态，
// 后续合成按键事件的 flags 恒 0（实测 Cmd 按下后 V 事件被当裸键，目标 App 收到 v 而非粘贴）。
static int postKey(CGKeyCode code, int down, uint64_t flags) {
	CGEventRef ev = CGEventCreateKeyboardEvent(NULL, code, down ? true : false);
	if (ev == NULL) {
		return -1;
	}
	CGEventSetFlags(ev, (CGEventFlags)flags);
	CGEventPost(kCGHIDEventTap, ev);
	CFRelease(ev);
	return 0;
}

// button: 0=左 1=右 2=中（与 Go 侧 ButtonLeft/Right/Middle 常量一致）。
// clickState：kCGMouseEventClickState（1=单击 2=双击 3=三击）；合成事件不写该字段时
// 应用侧 NSEvent.clickCount 恒为 1，双击打开文件/三击选段全部失效。
static int postButton(int button, int down, long clickState) {
	CGPoint p = cursorPos();
	CGEventType type;
	CGMouseButton b;
	switch (button) {
	case 0:
		type = down ? kCGEventLeftMouseDown : kCGEventLeftMouseUp;
		b = kCGMouseButtonLeft;
		break;
	case 1:
		type = down ? kCGEventRightMouseDown : kCGEventRightMouseUp;
		b = kCGMouseButtonRight;
		break;
	default:
		type = down ? kCGEventOtherMouseDown : kCGEventOtherMouseUp;
		b = kCGMouseButtonCenter;
		break;
	}
	CGEventRef ev = CGEventCreateMouseEvent(NULL, type, p, b);
	if (ev == NULL) {
		return -1;
	}
	if (clickState > 0) {
		CGEventSetIntegerValueField(ev, kCGMouseEventClickState, clickState);
	}
	CGEventPost(kCGHIDEventTap, ev);
	CFRelease(ev);
	return 0;
}

// padlinkDoubleClickInterval 定义在 interval_darwin.m（cgo 的 .c 文件按 C 编译，
// 调不到 AppKit 的类方法，故单独用 Objective-C 文件承载）。
extern double padlinkDoubleClickInterval(void);
*/
import "C"

import (
	"errors"
	"time"
)

// errEventCreate CGEventCreate* 返回 NULL（极端资源情形，正常不发生）。
var errEventCreate = errors.New("quartz: CGEventCreate 返回 NULL")

// 鼠标按钮号（与 C 侧 postButton 分支一致）。
const (
	ButtonLeft   = 0
	ButtonRight  = 1
	ButtonMiddle = 2
)

// 修饰键标志位（CGEventTypes.h kCGEventFlagMask*，ABI 稳定常量）。
// 由 Go 侧按当前按住的修饰键合成，随 PostKey 显式写入事件。
const (
	FlagShift   uint64 = 1 << 17 // kCGEventFlagMaskShift
	FlagControl uint64 = 1 << 18 // kCGEventFlagMaskControl
	FlagAlt     uint64 = 1 << 19 // kCGEventFlagMaskAlternate
	FlagCommand uint64 = 1 << 20 // kCGEventFlagMaskCommand
)

// Trusted 报告当前进程是否已获 TCC 辅助功能权限；prompt=true 时同时拉起系统授权弹窗。
func Trusted(prompt bool) bool {
	if prompt {
		return C.axTrusted(1) != 0
	}
	return C.axTrusted(0) != 0
}

// CursorPos 返回光标全局坐标（主屏左上角原点，y 向下；多显示器为统一坐标系）。
func CursorPos() (x, y float64) {
	p := C.cursorPos()
	return float64(p.x), float64(p.y)
}

// MouseMove 一次绝对定位位移。X/Y 为合成后的目标全局坐标；
// DeltaX/Y 附带写入事件 delta 字段，供消费原始位移的应用读取。
// ButtonsDown 为按住按钮位掩码（1=左 2=中 4=右），决定投递 mouseMoved 还是 mouseDragged；
// ClickState 为本次点击序号（0 表示不写该字段）。
type MouseMove struct {
	X, Y           float64
	DeltaX, DeltaY int32
	ButtonsDown    uint8
	ClickState     int
}

// PostMouseMove 投递绝对定位位移。OS 指针加速只作用于 HID 相对事件，
// 绝对定位不经过加速曲线（docs/PLAN-MACOS.md §2.2）；
// 有按钮按住时投递 mouseDragged（否则应用收不到拖拽）。
func PostMouseMove(m MouseMove) error {
	if C.postMouseMove(C.double(m.X), C.double(m.Y), C.long(m.DeltaX), C.long(m.DeltaY),
		C.int(m.ButtonsDown), C.long(m.ClickState)) != 0 {
		return errEventCreate
	}
	return nil
}

// DoubleClickInterval 返回系统设置的双击间隔（NSEvent.doubleClickInterval）。
// 取不到或明显越界时返回 0，由调用方回落默认值（macOS 默认 0.5s）。
func DoubleClickInterval() time.Duration {
	sec := float64(C.padlinkDoubleClickInterval())
	if sec < 0.1 || sec > 2.0 {
		return 0
	}
	return time.Duration(sec * float64(time.Second))
}

// PostScrollPixel 投递像素单位滚轮事件；符号约定同 Linux REL_WHEEL（正=内容上滚）。
func PostScrollPixel(dy int32) error {
	if C.postScrollPixel(C.long(dy)) != 0 {
		return errEventCreate
	}
	return nil
}

// PostKey 投递键盘事件（CGKeyCode 虚拟键码，按下/抬起）。
// flags 为调用方合成的修饰键标志（Flag* 位或）；修饰键自身事件的
// 标志约定同物理键盘：keyDown 含自身位，keyUp 不含。
func PostKey(code uint16, down bool, flags uint64) error {
	d := 0
	if down {
		d = 1
	}
	if C.postKey(C.CGKeyCode(code), C.int(d), C.uint64_t(flags)) != 0 {
		return errEventCreate
	}
	return nil
}

// PostButton 在当前光标处投递鼠标按钮事件（ButtonLeft/Right/Middle）。
// clickState 为本次点击序号（1=单击 2=双击 3=三击）：合成的 down/up 都必须携带，
// 应用侧 NSEvent.clickCount 才会是 2/3；传 0 表示不写该字段（按单击处理）。
func PostButton(button int, down bool, clickState int) error {
	d := 0
	if down {
		d = 1
	}
	if C.postButton(C.int(button), C.int(d), C.long(clickState)) != 0 {
		return errEventCreate
	}
	return nil
}
