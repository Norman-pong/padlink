//go:build darwin

// Package quartz 是 macOS Quartz CGEvent 注入原语的薄 cgo 封装（PRD 附录 B v2 路线）。
// 只暴露函数级原语；累积、键码映射、权限门控等业务语义全部留在 Go 侧调用方。
// 未获 TCC 辅助功能权限时 Post* 的事件被系统静默丢弃（docs/PLAN-MACOS.md §0 实测），
// 调用方必须先过 Trusted 门控再注入。
package quartz

/*
#cgo LDFLAGS: -framework ApplicationServices -framework CoreFoundation
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

static int postMouseMove(double x, double y, long dx, long dy) {
	CGEventRef ev = CGEventCreateMouseEvent(NULL, kCGEventMouseMoved,
		CGPointMake(x, y), kCGMouseButtonLeft);
	if (ev == NULL) {
		return -1;
	}
	CGEventSetIntegerValueField(ev, kCGMouseEventDeltaX, dx);
	CGEventSetIntegerValueField(ev, kCGMouseEventDeltaY, dy);
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

static int postKey(CGKeyCode code, int down) {
	CGEventRef ev = CGEventCreateKeyboardEvent(NULL, code, down ? true : false);
	if (ev == NULL) {
		return -1;
	}
	CGEventPost(kCGHIDEventTap, ev);
	CFRelease(ev);
	return 0;
}

// button: 0=左 1=右 2=中（与 Go 侧 ButtonLeft/Right/Middle 常量一致）。
static int postButton(int button, int down) {
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
	CGEventPost(kCGHIDEventTap, ev);
	CFRelease(ev);
	return 0;
}
*/
import "C"

import "errors"

// errEventCreate CGEventCreate* 返回 NULL（极端资源情形，正常不发生）。
var errEventCreate = errors.New("quartz: CGEventCreate 返回 NULL")

// 鼠标按钮号（与 C 侧 postButton 分支一致）。
const (
	ButtonLeft   = 0
	ButtonRight  = 1
	ButtonMiddle = 2
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
type MouseMove struct {
	X, Y           float64
	DeltaX, DeltaY int32
}

// PostMouseMove 投递绝对定位 mouseMoved。OS 指针加速只作用于 HID 相对事件，
// 绝对定位不经过加速曲线（docs/PLAN-MACOS.md §2.2）。
func PostMouseMove(m MouseMove) error {
	if C.postMouseMove(C.double(m.X), C.double(m.Y), C.long(m.DeltaX), C.long(m.DeltaY)) != 0 {
		return errEventCreate
	}
	return nil
}

// PostScrollPixel 投递像素单位滚轮事件；符号约定同 Linux REL_WHEEL（正=内容上滚）。
func PostScrollPixel(dy int32) error {
	if C.postScrollPixel(C.long(dy)) != 0 {
		return errEventCreate
	}
	return nil
}

// PostKey 投递键盘事件（CGKeyCode 虚拟键码，按下/抬起）。
func PostKey(code uint16, down bool) error {
	d := 0
	if down {
		d = 1
	}
	if C.postKey(C.CGKeyCode(code), C.int(d)) != 0 {
		return errEventCreate
	}
	return nil
}

// PostButton 在当前光标处投递鼠标按钮事件（ButtonLeft/Right/Middle）。
func PostButton(button int, down bool) error {
	d := 0
	if down {
		d = 1
	}
	if C.postButton(C.int(button), C.int(d)) != 0 {
		return errEventCreate
	}
	return nil
}
