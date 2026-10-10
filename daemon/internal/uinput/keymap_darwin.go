//go:build darwin

package uinput

import (
	"padlink/daemon/internal/inject"
	"padlink/daemon/internal/quartz"
)

// keyToCGKeyCode：Linux KEY_*（inject.HIDToKey 的值域）→ macOS CGKeyCode（kVK_*，ANSI 布局）。
// macOS 修饰键对应关系：Ctrl→Control、Alt→Option、Super(Meta)→Command。
// 覆盖面必须与 inject.HIDToKey 一一对应，由 keymap_darwin_test 在测试期兜底。
var keyToCGKeyCode = map[uint16]uint16{
	// 字母 A-Z
	30: 0x00, 48: 0x0B, 46: 0x08, 32: 0x02, 18: 0x0E, // A B C D E
	33: 0x03, 34: 0x05, 35: 0x04, 23: 0x22, 36: 0x26, // F G H I J
	37: 0x28, 38: 0x25, 50: 0x2E, 49: 0x2D, 24: 0x1F, // K L M N O
	25: 0x23, 16: 0x0C, 19: 0x0F, 31: 0x01, 20: 0x11, // P Q R S T
	22: 0x20, 47: 0x09, 17: 0x0D, 45: 0x07, 21: 0x10, // U V W X Y
	44: 0x06, // Z

	// 数字行 1-9,0
	2: 0x12, 3: 0x13, 4: 0x14, 5: 0x15, 6: 0x17,
	7: 0x16, 8: 0x1C, 9: 0x19, 10: 0x1D, 11: 0x1B,

	// 控制键
	28: 0x24, // Enter     kVK_Return
	1:  0x35, // Esc       kVK_Escape
	14: 0x33, // Backspace kVK_Delete
	15: 0x30, // Tab       kVK_Tab
	57: 0x31, // Space     kVK_Space

	// 符号区
	12: 0x1B, // -  kVK_ANSI_Minus
	13: 0x18, // =  kVK_ANSI_Equal
	26: 0x21, // [  kVK_ANSI_LeftBracket
	27: 0x1E, // ]  kVK_ANSI_RightBracket
	43: 0x2A, // \  kVK_ANSI_Backslash
	39: 0x29, // ;  kVK_ANSI_Semicolon
	40: 0x27, // '  kVK_ANSI_Quote
	41: 0x32, // `  kVK_ANSI_Grave
	51: 0x2B, // ,  kVK_ANSI_Comma
	52: 0x2F, // .  kVK_ANSI_Period
	53: 0x2C, // /  kVK_ANSI_Slash

	58: 0x39, // CapsLock kVK_CapsLock

	// F1-F12
	59: 0x7A, 60: 0x78, 61: 0x63, 62: 0x76, 63: 0x60, 64: 0x61,
	65: 0x62, 66: 0x64, 67: 0x65, 68: 0x6D, 87: 0x67, 88: 0x6F,

	// 锁定/屏幕键：macOS 键盘无对应实体键，按 Barrier/Chrome Remote Desktop 惯例映射 F13-F15
	99:  0x69, // PrintScreen → F13
	70:  0x6B, // ScrollLock  → F14
	119: 0x71, // Pause       → F15

	// 导航区
	110: 0x72, // Insert → kVK_Help（同物理键位）
	102: 0x73, // Home
	104: 0x74, // PageUp
	111: 0x75, // Delete → kVK_ForwardDelete
	107: 0x77, // End
	109: 0x79, // PageDown

	// 方向键
	106: 0x7C, 105: 0x7B, 108: 0x7D, 103: 0x7E,

	// 小键盘
	69: 0x47,                                         // NumLock → kVK_ANSI_KeypadClear（同物理键位）
	98: 0x4B,                                         // KP /     kVK_ANSI_KeypadDivide
	55: 0x43,                                         // KP *     kVK_ANSI_KeypadMultiply
	74: 0x4E,                                         // KP -     kVK_ANSI_KeypadMinus
	78: 0x45,                                         // KP +     kVK_ANSI_KeypadPlus
	96: 0x4C,                                         // KP Enter kVK_ANSI_KeypadEnter
	79: 0x53, 80: 0x54, 81: 0x55, 75: 0x56, 76: 0x57, // KP1-KP5
	77: 0x58, 71: 0x59, 72: 0x5B, 73: 0x5C, 82: 0x52, // KP6-KP9,KP0
	83: 0x41, // KP .     kVK_ANSI_KeypadDecimal

	// 修饰键
	29:  0x3B, // LCtrl  kVK_Control
	42:  0x38, // LShift kVK_Shift
	56:  0x3A, // LAlt   kVK_Option
	125: 0x37, // LSuper kVK_Command
	97:  0x3E, // RCtrl  kVK_RightControl
	54:  0x3C, // RShift kVK_RightShift
	100: 0x3D, // RAlt   kVK_RightOption
	126: 0x36, // RSuper kVK_RightCommand
}

// cgButton 把 BTN_* 映射为 quartz 按钮号；非按钮 code 返回 ok=false。
func cgButton(code uint16) (int, bool) {
	switch code {
	case inject.BtnLeft:
		return quartz.ButtonLeft, true
	case inject.BtnRight:
		return quartz.ButtonRight, true
	case inject.BtnMiddle:
		return quartz.ButtonMiddle, true
	}
	return 0, false
}

// cgModFlag 把修饰键 KEY_* 映射为 quartz Flag* 位；非修饰键返回 ok=false。
// 键值域与 keyToCGKeyCode 修饰键段一一对应（左右各四枚）。
func cgModFlag(code uint16) (uint64, bool) {
	switch code {
	case 42, 54: // LShift/RShift
		return quartz.FlagShift, true
	case 29, 97: // LCtrl/RCtrl
		return quartz.FlagControl, true
	case 56, 100: // LAlt/RAlt
		return quartz.FlagAlt, true
	case 125, 126: // LSuper/RSuper
		return quartz.FlagCommand, true
	}
	return 0, false
}
