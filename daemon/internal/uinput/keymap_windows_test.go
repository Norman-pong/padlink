//go:build windows

package uinput

import (
	"testing"

	"padlink/daemon/internal/inject"
)

// 键表覆盖：inject.HIDToKey 值域内每个 Linux KEY_* 都必须有 scancode 映射，
// 否则该键在 Windows 上按下时才以运行时错误暴露。
func TestKeyMapCoversHIDToKey(t *testing.T) {
	for hid, code := range inject.HIDToKey {
		if _, ok := keyToScancode[code]; !ok {
			t.Errorf("HID usage 0x%02X → KEY code %d 缺少 scancode 映射", hid, code)
		}
	}
}

// 鼠标按钮走 buttonFlags 分支，不应混进键盘映射表。
func TestKeyMapExcludesButtonCodes(t *testing.T) {
	for _, btn := range []uint16{inject.BtnLeft, inject.BtnRight, inject.BtnMiddle} {
		if _, ok := keyToScancode[btn]; ok {
			t.Errorf("BTN code %d 不应出现在键盘映射表", btn)
		}
		if flags, ok := buttonFlags(btn, 1); !ok {
			t.Errorf("BTN code %d 缺少按钮映射", btn)
		} else if flags == 0 {
			t.Errorf("BTN code %d 按下标志为 0", btn)
		}
	}
}

// 扫描码唯一性：同一扫描码可以出现在不同 KEY code（物理位如此），但 ext 标志必须使二者可区分
// ——同码同 ext 是真冲突（如 KP4=0x4B 非 ext 与 Left=0x4B ext 靠 ext 区分）。
func TestScanCodeExtConsistency(t *testing.T) {
	seen := map[uint16]bool{}
	for key, sk := range keyToScancode {
		if sk.vkOnly {
			continue
		}
		id := uint16(sk.code)<<1 | boolToUint(sk.ext)
		if seen[id] {
			t.Errorf("KEY code %d 的扫描码 0x%02X(ext=%v) 与其他键冲突", key, sk.code, sk.ext)
		}
		seen[id] = true
	}
	// 明确应不同的同扫描码对：KP4(0x4B 非 ext) 与 Left(0x4B ext)、KP6/Right、KP8/Up、KP2/Down
	if keyToScancode[75].ext || !keyToScancode[105].ext {
		t.Error("KP4 应非 ext、Left 应 ext")
	}
	if keyToScancode[77].ext || !keyToScancode[106].ext {
		t.Error("KP6 应非 ext、Right 应 ext")
	}
}

// Pause 走 VK 例外且不占扫描码位；字母行抽样断言物理位正确（防表错位）。
func TestKeyMapSpotChecks(t *testing.T) {
	if !keyToScancode[119].vkOnly {
		t.Error("Pause 必须走 vkOnly 通道")
	}
	spots := []struct {
		key  uint16
		code uint8
	}{
		{30, 0x1E}, // A
		{16, 0x10}, // Q
		{44, 0x2C}, // Z
		{50, 0x32}, // M
		{28, 0x1C}, // Enter
		{57, 0x39}, // Space
		{11, 0x0B}, // 0
	}
	for _, s := range spots {
		if got := keyToScancode[s.key].code; got != s.code {
			t.Errorf("KEY code %d 扫描码 = 0x%02X, want 0x%02X", s.key, got, s.code)
		}
	}
}

// 绝对坐标归一化：边界钳制与比例换算（虚拟桌面语义）。
func TestAbsNorm(t *testing.T) {
	cases := []struct {
		x, origin, span int32
		want            uint32
	}{
		{0, 0, 1920, 0},
		{1919, 0, 1920, 65535},
		{960, 0, 1920, 32784},      // 960*65535/1919
		{-5, 0, 1920, 0},           // 负值钳到 0
		{5000, 0, 1920, 65535},     // 超界钳满
		{1920, -1920, 1920, 65535}, // 副屏：相对原点偏移
	}
	for _, c := range cases {
		if got := absNorm(c.x, c.origin, c.span); got != c.want {
			t.Errorf("absNorm(%d,%d,%d) = %d, want %d", c.x, c.origin, c.span, got, c.want)
		}
	}
	if absNorm(0, 0, 1) != 0 {
		t.Error("span<=1 应安全返回 0")
	}
}

// 滚轮跨帧累积（纯逻辑）：满格即发、余量保留、负向同样成立；不打真 SendInput。
func TestWheelAccumulation(t *testing.T) {
	cases := []struct {
		in                int32
		wantN, wantRemain int32
	}{
		{60, 0, 60},   // 半格保留
		{120, 1, 0},   // 满格
		{-120, -1, 0}, // 负满格
		{-60, 0, -60}, // 负半格
		{250, 2, 10},  // 两格余 10
		{0, 0, 0},
	}
	for _, c := range cases {
		n, r := wheelSplit(c.in)
		if n != c.wantN || r != c.wantRemain {
			t.Errorf("wheelSplit(%d) = (%d,%d), want (%d,%d)", c.in, n, r, c.wantN, c.wantRemain)
		}
	}
}

// CapsLockActive 必须非 nil（main 装配 inject.Config 的钩子直接引用）——编译期符号存在性。
func TestCapsLockHookSymbol(t *testing.T) {
	var f func() bool = CapsLockActive
	_ = f
}

func boolToUint(b bool) uint16 {
	if b {
		return 1
	}
	return 0
}
