package inject

// HID usage（USB HID 键盘页 0x07）→ Linux KEY_* 映射表。
// 值逐一对照 linux/input-event-codes.h；daemon 用它做 UI_SET_KEYBIT 注册与按键转换。
const (
	HIDLeftCtrl   uint16 = 0xE0
	HIDLeftShift  uint16 = 0xE1
	HIDLeftAlt    uint16 = 0xE2
	HIDLeftSuper  uint16 = 0xE3
	HIDRightCtrl  uint16 = 0xE4
	HIDRightShift uint16 = 0xE5
	HIDRightAlt   uint16 = 0xE6
	HIDRightSuper uint16 = 0xE7

	HIDA uint16 = 0x04
	HIDV uint16 = 0x19
	HIDP uint16 = 0x13
	HIDL uint16 = 0x0F
)

// HIDToKey 覆盖手机端全键盘所需键位（103 项）。
var HIDToKey = map[uint16]uint16{
	// 字母 A-Z（0x04-0x1D）→ KEY_A(30)…KEY_Z(44)
	0x04: 30, 0x05: 48, 0x06: 46, 0x07: 32, 0x08: 18, // A B C D E
	0x09: 33, 0x0A: 34, 0x0B: 35, 0x0C: 23, 0x0D: 36, // F G H I J
	0x0E: 37, 0x0F: 38, 0x10: 50, 0x11: 49, 0x12: 24, // K L M N O
	0x13: 25, 0x14: 16, 0x15: 19, 0x16: 31, 0x17: 20, // P Q R S T
	0x18: 22, 0x19: 47, 0x1A: 17, 0x1B: 45, 0x1C: 21, // U V W X Y
	0x1D: 44, // Z

	// 数字行 1-9,0（0x1E-0x27）→ KEY_1(2)…KEY_0(11)
	0x1E: 2, 0x1F: 3, 0x20: 4, 0x21: 5, 0x22: 6,
	0x23: 7, 0x24: 8, 0x25: 9, 0x26: 10, 0x27: 11,

	// 控制键
	0x28: 28, // Enter      KEY_ENTER
	0x29: 1,  // Esc        KEY_ESC
	0x2A: 14, // Backspace  KEY_BACKSPACE
	0x2B: 15, // Tab        KEY_TAB
	0x2C: 57, // Space      KEY_SPACE

	// 符号区
	0x2D: 12, // -       KEY_MINUS
	0x2E: 13, // =       KEY_EQUAL
	0x2F: 26, // [       KEY_LEFTBRACE
	0x30: 27, // ]       KEY_RIGHTBRACE
	0x31: 43, // \       KEY_BACKSLASH
	0x33: 39, // ;       KEY_SEMICOLON
	0x34: 40, // '       KEY_APOSTROPHE
	0x35: 41, // `       KEY_GRAVE
	0x36: 51, // ,       KEY_COMMA
	0x37: 52, // .       KEY_DOT
	0x38: 53, // /       KEY_SLASH

	0x39: 58, // CapsLock KEY_CAPSLOCK

	// F1-F12（0x3A-0x45）→ KEY_F1(59)…KEY_F12(88)
	0x3A: 59, 0x3B: 60, 0x3C: 61, 0x3D: 62, 0x3E: 63, 0x3F: 64,
	0x40: 65, 0x41: 66, 0x42: 67, 0x43: 68, 0x44: 87, 0x45: 88,

	// 锁定/屏幕键
	0x46: 99,  // PrintScreen KEY_SYSRQ
	0x47: 70,  // ScrollLock  KEY_SCROLLLOCK
	0x48: 119, // Pause       KEY_PAUSE

	// 导航区
	0x49: 110, // Insert  KEY_INSERT
	0x4A: 102, // Home    KEY_HOME
	0x4B: 104, // PageUp  KEY_PAGEUP
	0x4C: 111, // Delete  KEY_DELETE
	0x4D: 107, // End     KEY_END
	0x4E: 109, // PageDown KEY_PAGEDOWN

	// 方向键
	0x4F: 106, // Right KEY_RIGHT
	0x50: 105, // Left  KEY_LEFT
	0x51: 108, // Down  KEY_DOWN
	0x52: 103, // Up    KEY_UP

	// 小键盘：NumLock 与运算符
	0x53: 69, // NumLock    KEY_NUMLOCK
	0x54: 98, // KP /       KEY_KPSLASH
	0x55: 55, // KP *       KEY_KPASTERISK
	0x56: 74, // KP -       KEY_KPMINUS
	0x57: 78, // KP +       KEY_KPPLUS
	0x58: 96, // KP Enter   KEY_KPENTER

	// 小键盘数字 KP1-KP9,KP0 与 KP.（KEY_KP7=71…KEY_KP0=82）
	0x59: 79, 0x5A: 80, 0x5B: 81, 0x5C: 75, 0x5D: 76,
	0x5E: 77, 0x5F: 71, 0x60: 72, 0x61: 73, 0x62: 82,
	0x63: 83, // KP .  KEY_KPDOT

	// 修饰键
	0xE0: 29,  // LCtrl  KEY_LEFTCTRL
	0xE1: 42,  // LShift KEY_LEFTSHIFT
	0xE2: 56,  // LAlt   KEY_LEFTALT
	0xE3: 125, // LSuper KEY_LEFTMETA
	0xE4: 97,  // RCtrl  KEY_RIGHTCTRL
	0xE5: 54,  // RShift KEY_RIGHTSHIFT
	0xE6: 100, // RAlt   KEY_RIGHTALT
	0xE7: 126, // RSuper KEY_RIGHTMETA
}
