package inject

import "testing"

func TestHIDToKeySpotChecks(t *testing.T) {
	// 抽查对照 linux/input-event-codes.h
	cases := []struct {
		hid uint16
		key uint16
	}{
		{0x04, 30},  // A   KEY_A
		{0x1D, 44},  // Z   KEY_Z
		{0x08, 18},  // E   KEY_E
		{0x13, 25},  // P   KEY_P
		{0x0F, 38},  // L   KEY_L
		{0x19, 47},  // V   KEY_V
		{0x1E, 2},   // 1   KEY_1
		{0x27, 11},  // 0   KEY_0
		{0x28, 28},  // Enter  KEY_ENTER
		{0x29, 1},   // Esc    KEY_ESC
		{0x2A, 14},  // Backspace KEY_BACKSPACE
		{0x2B, 15},  // Tab    KEY_TAB
		{0x2C, 57},  // Space  KEY_SPACE
		{0x2D, 12},  // -   KEY_MINUS
		{0x2E, 13},  // =   KEY_EQUAL
		{0x2F, 26},  // [   KEY_LEFTBRACE
		{0x30, 27},  // ]   KEY_RIGHTBRACE
		{0x31, 43},  // \   KEY_BACKSLASH
		{0x33, 39},  // ;   KEY_SEMICOLON
		{0x34, 40},  // '   KEY_APOSTROPHE
		{0x35, 41},  // `   KEY_GRAVE
		{0x36, 51},  // ,   KEY_COMMA
		{0x37, 52},  // .   KEY_DOT
		{0x38, 53},  // /   KEY_SLASH
		{0x39, 58},  // CapsLock KEY_CAPSLOCK
		{0x3A, 59},  // F1  KEY_F1
		{0x45, 88},  // F12 KEY_F12
		{0x44, 87},  // F11 KEY_F11
		{0x46, 99},  // PrintScreen KEY_SYSRQ
		{0x47, 70},  // ScrollLock  KEY_SCROLLLOCK
		{0x48, 119}, // Pause       KEY_PAUSE
		{0x49, 110}, // Insert  KEY_INSERT
		{0x4A, 102}, // Home    KEY_HOME
		{0x4B, 104}, // PageUp  KEY_PAGEUP
		{0x4C, 111}, // Delete  KEY_DELETE
		{0x4D, 107}, // End     KEY_END
		{0x4E, 109}, // PageDown KEY_PAGEDOWN
		{0x4F, 106}, // Right KEY_RIGHT
		{0x50, 105}, // Left  KEY_LEFT
		{0x51, 108}, // Down  KEY_DOWN
		{0x52, 103}, // Up    KEY_UP
		{0x53, 69},  // NumLock KEY_NUMLOCK
		{0x54, 98},  // KP /    KEY_KPSLASH
		{0x55, 55},  // KP *    KEY_KPASTERISK
		{0x56, 74},  // KP -    KEY_KPMINUS
		{0x57, 78},  // KP +    KEY_KPPLUS
		{0x58, 96},  // KP Enter KEY_KPENTER
		{0x5F, 71},  // KP7  KEY_KP7
		{0x62, 82},  // KP0  KEY_KP0
		{0x63, 83},  // KP . KEY_KPDOT
		{0xE0, 29},  // LCtrl  KEY_LEFTCTRL
		{0xE1, 42},  // LShift KEY_LEFTSHIFT
		{0xE2, 56},  // LAlt   KEY_LEFTALT
		{0xE3, 125}, // LSuper KEY_LEFTMETA
		{0xE4, 97},  // RCtrl  KEY_RIGHTCTRL
		{0xE5, 54},  // RShift KEY_RIGHTSHIFT
		{0xE6, 100}, // RAlt   KEY_RIGHTALT
		{0xE7, 126}, // RSuper KEY_RIGHTMETA
	}
	seen := make(map[uint16]bool, len(cases))
	for _, c := range cases {
		got, ok := HIDToKey[c.hid]
		if !ok {
			t.Errorf("HID usage 0x%04X 缺失", c.hid)
			continue
		}
		if got != c.key {
			t.Errorf("HID 0x%04X → %d, want %d", c.hid, got, c.key)
		}
		if seen[c.hid] {
			t.Errorf("HID usage 0x%04X 重复定义", c.hid)
		}
		seen[c.hid] = true
	}
}

func TestHIDToKeyCompleteness(t *testing.T) {
	// 26 字母 + 10 数字 + 5 控制 + 11 符号 + CapsLock + 12 F 键
	// + 3 屏幕/锁定 + 6 导航 + 4 方向 + 6 小键盘锁定/运算符 + 11 小键盘数字/点 + 8 修饰键
	const wantCount = 103
	if len(HIDToKey) != wantCount {
		t.Fatalf("映射表条目 = %d, want %d（出现意外增删请同步更新本断言）", len(HIDToKey), wantCount)
	}
	// 全表 KEY 码必须落在 Linux 有效区间且不为 0
	for hid, key := range HIDToKey {
		if key == 0 || key > 248 {
			t.Errorf("HID 0x%04X → 非法 KEY 码 %d", hid, key)
		}
	}
	// 组合键与自测依赖的常量必须与表一致
	for name, pair := range map[string][2]uint16{
		"HIDA": {HIDA, 0x04}, "HIDV": {HIDV, 0x19},
		"HIDP": {HIDP, 0x13}, "HIDL": {HIDL, 0x0F},
		"HIDLeftCtrl": {HIDLeftCtrl, 0xE0},
	} {
		if pair[0] != pair[1] {
			t.Errorf("常量 %s = 0x%04X, want 0x%04X", name, pair[0], pair[1])
		}
	}
}
