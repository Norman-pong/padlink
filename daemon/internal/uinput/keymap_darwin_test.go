//go:build darwin

package uinput

import (
	"testing"

	"padlink/daemon/internal/inject"
	"padlink/daemon/internal/quartz"
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

// 修饰键映射：键表修饰键段 8 枚（左右 Shift/Ctrl/Alt/Super）必须有 Flag 位，
// 非修饰键不得混入。
func TestCGModFlagCoversModifiers(t *testing.T) {
	mods := []uint16{42, 54, 29, 97, 56, 100, 125, 126}
	for _, code := range mods {
		bit, ok := cgModFlag(code)
		if !ok {
			t.Errorf("修饰键 KEY code %d 缺少 Flag 映射", code)
			continue
		}
		if bit&(bit-1) != 0 {
			t.Errorf("KEY code %d 的 Flag 应为单一位，得 %#x", code, bit)
		}
		if _, ok := keyToCGKeyCode[code]; !ok {
			t.Errorf("修饰键 KEY code %d 缺少 CGKeyCode 映射", code)
		}
	}
	for _, code := range []uint16{30 /* A */, 28 /* Enter */, 57 /* Space */} {
		if _, ok := cgModFlag(code); ok {
			t.Errorf("非修饰键 KEY code %d 不应有 Flag 映射", code)
		}
	}
}

// 修饰状态机：按住累积、抬起清位；修饰键自身按下含自身位、抬起不含；
// 非修饰键事件携带当前掩码且不改变掩码。
func TestTrackMod(t *testing.T) {
	d := &Device{}
	if f := d.trackMod(42, true); f != quartz.FlagShift { // LShift down
		t.Fatalf("LShift down flags = %#x, want FlagShift", f)
	}
	if f := d.trackMod(30, true); f != quartz.FlagShift { // A down 携带 Shift
		t.Fatalf("A down flags = %#x, want FlagShift", f)
	}
	if f := d.trackMod(125, true); f != quartz.FlagShift|quartz.FlagCommand { // 再按 LSuper
		t.Fatalf("LSuper down flags = %#x, want Shift|Command", f)
	}
	if f := d.trackMod(42, false); f != quartz.FlagCommand { // LShift up 不含自身位
		t.Fatalf("LShift up flags = %#x, want Command", f)
	}
	if f := d.trackMod(30, false); f != quartz.FlagCommand { // A up 仍携带 Command
		t.Fatalf("A up flags = %#x, want Command", f)
	}
	if f := d.trackMod(125, false); f != 0 {
		t.Fatalf("LSuper up flags = %#x, want 0", f)
	}
}
