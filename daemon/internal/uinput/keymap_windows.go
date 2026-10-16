//go:build windows

package uinput

// scanKey 是 PS/2 set-1 扫描码；ext 标记扩展键（E0 前缀，注入时置 KEYEVENTF_EXTENDEDKEY）。
type scanKey struct {
	code   uint8
	ext    bool
	vkOnly bool // set-1 无单字节序列的键（Pause 的 E1 14 77），走 VK 通道
}

// keyToScancode：Linux KEY_*（inject.HIDToKey 的值域）→ PS/2 set-1 扫描码。
// KEYEVENTF_SCANCODE 走物理键位语义（经键盘布局），与真实打字同路——中文 IME 场景
// 与物理键盘行为一致。键位表与 keymap_darwin.go 同源自 inject.HIDToKey，覆盖面必须一致。
var keyToScancode = map[uint16]scanKey{
	// 字母 A-Z（QWERTY 物理位）
	16: {code: 0x10}, // Q
	17: {code: 0x11}, // W
	18: {code: 0x12}, // E
	19: {code: 0x13}, // R
	20: {code: 0x14}, // T
	21: {code: 0x15}, // Y
	22: {code: 0x16}, // U
	23: {code: 0x17}, // I
	24: {code: 0x18}, // O
	25: {code: 0x19}, // P
	30: {code: 0x1E}, // A
	31: {code: 0x1F}, // S
	32: {code: 0x20}, // D
	33: {code: 0x21}, // F
	34: {code: 0x22}, // G
	35: {code: 0x23}, // H
	36: {code: 0x24}, // J
	37: {code: 0x25}, // K
	38: {code: 0x26}, // L
	44: {code: 0x2C}, // Z
	45: {code: 0x2D}, // X
	46: {code: 0x2E}, // C
	47: {code: 0x2F}, // V
	48: {code: 0x30}, // B
	49: {code: 0x31}, // N
	50: {code: 0x32}, // M

	// 数字行 1-9,0
	2:  {code: 0x02},
	3:  {code: 0x03},
	4:  {code: 0x04},
	5:  {code: 0x05},
	6:  {code: 0x06},
	7:  {code: 0x07},
	8:  {code: 0x08},
	9:  {code: 0x09},
	10: {code: 0x0A},
	11: {code: 0x0B},

	// 控制键
	28: {code: 0x1C}, // Enter
	1:  {code: 0x01}, // Esc
	14: {code: 0x0E}, // Backspace
	15: {code: 0x0F}, // Tab
	57: {code: 0x39}, // Space
	58: {code: 0x3A}, // CapsLock

	// 符号区
	12: {code: 0x0C}, // -
	13: {code: 0x0D}, // =
	26: {code: 0x1A}, // [
	27: {code: 0x1B}, // ]
	43: {code: 0x2B}, // \（Z 与 \ 的扫描码恰同为物理位设定，KEY code 不同勿混）
	39: {code: 0x27}, // ;
	40: {code: 0x28}, // '
	41: {code: 0x29}, // `
	51: {code: 0x33}, // ,
	52: {code: 0x34}, // .
	53: {code: 0x35}, // /

	// F1-F12
	59: {code: 0x3B}, 60: {code: 0x3C}, 61: {code: 0x3D}, 62: {code: 0x3E},
	63: {code: 0x3F}, 64: {code: 0x40}, 65: {code: 0x41}, 66: {code: 0x42},
	67: {code: 0x43}, 68: {code: 0x44}, 87: {code: 0x57}, 88: {code: 0x58},

	// 锁定/屏幕键
	99:  {code: 0x37, ext: true}, // PrintScreen（E0 37）
	70:  {code: 0x46},            // ScrollLock
	119: {vkOnly: true},          // Pause（E1 14 77 多字节序列，走 VK_PAUSE）

	// 导航区（全部 E0 扩展键）
	110: {code: 0x52, ext: true}, // Insert
	102: {code: 0x47, ext: true}, // Home
	104: {code: 0x49, ext: true}, // PageUp
	111: {code: 0x53, ext: true}, // Delete
	107: {code: 0x4F, ext: true}, // End
	109: {code: 0x51, ext: true}, // PageDown

	// 方向键（E0 扩展键）
	106: {code: 0x4D, ext: true}, // Right
	105: {code: 0x4B, ext: true}, // Left
	108: {code: 0x50, ext: true}, // Down
	103: {code: 0x48, ext: true}, // Up

	// 小键盘（非 ext 基本区；KP/ 与 KP Enter 是 E0 扩展键）
	69: {code: 0x45},            // NumLock
	98: {code: 0x35, ext: true}, // KP /
	55: {code: 0x37},            // KP *
	74: {code: 0x4A},            // KP -
	78: {code: 0x4E},            // KP +
	96: {code: 0x1C, ext: true}, // KP Enter
	79: {code: 0x4F},            // KP1
	80: {code: 0x50},            // KP2
	81: {code: 0x51},            // KP3
	75: {code: 0x4B},            // KP4
	76: {code: 0x4C},            // KP5
	77: {code: 0x4D},            // KP6
	71: {code: 0x47},            // KP7
	72: {code: 0x48},            // KP8
	73: {code: 0x49},            // KP9
	82: {code: 0x52},            // KP0
	83: {code: 0x53},            // KP .

	// 修饰键
	29:  {code: 0x1D},            // LCtrl
	42:  {code: 0x2A},            // LShift
	56:  {code: 0x38},            // LAlt
	125: {code: 0x5B, ext: true}, // LSuper（E0 5B）
	97:  {code: 0x1D, ext: true}, // RCtrl
	54:  {code: 0x36},            // RShift
	100: {code: 0x38, ext: true}, // RAlt（E0 38，AltGr）
	126: {code: 0x5C, ext: true}, // RSuper
}
