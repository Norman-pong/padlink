//go:build darwin

package uinput

import (
	"testing"

	"padlink/daemon/internal/inject"
)

// 键表覆盖：inject.HIDToKey 值域内每个 Linux KEY_* 都必须有 CGKeyCode 映射，
// 否则该键在 macOS 上按下时才以运行时错误暴露——提前到测试期拦截。
func TestKeyMapCoversHIDToKey(t *testing.T) {
	for hid, code := range inject.HIDToKey {
		if _, ok := keyToCGKeyCode[code]; !ok {
			t.Errorf("HID usage 0x%02X → KEY code %d 缺少 CGKeyCode 映射", hid, code)
		}
	}
}

// 鼠标按钮走 cgButton 分支，不应混进键盘映射表。
func TestKeyMapExcludesButtonCodes(t *testing.T) {
	for _, btn := range []uint16{inject.BtnLeft, inject.BtnRight, inject.BtnMiddle} {
		if _, ok := keyToCGKeyCode[btn]; ok {
			t.Errorf("BTN code %d 不应出现在键盘映射表", btn)
		}
		if _, ok := cgButton(btn); !ok {
			t.Errorf("BTN code %d 缺少按钮映射", btn)
		}
	}
}

// 滚轮换算：1 格(120)=53px，符号保持（正=上滚），非零保底 ±1px。
func TestHiResToPixels(t *testing.T) {
	cases := []struct{ in, want int32 }{
		{120, 53}, {-120, -53}, {240, 106},
		{1, 1}, {-1, -1}, // 亚格增量不丢
		{0, 0},
	}
	for _, c := range cases {
		if got := hiResToPixels(c.in); got != c.want {
			t.Errorf("hiResToPixels(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}
