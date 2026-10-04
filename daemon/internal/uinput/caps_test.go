package uinput

import (
	"padlink/daemon/internal/inject"
	"slices"
	"testing"
)

func TestStructLayoutConstants(t *testing.T) {
	// 64 位 amd64/arm64 与 linux/uinput.h、linux/input.h 的 ABI 常量
	if sizeofInputEvent != 24 {
		t.Errorf("sizeofInputEvent = %d, want 24（timeval 16 + type/code 4 + value 4）", sizeofInputEvent)
	}
	if sizeofUinputSetup != 92 {
		t.Errorf("sizeofUinputSetup = %d, want 92（input_id 8 + name 80 + ff_effects_max 4）", sizeofUinputSetup)
	}
}

func TestIoctlNumbers(t *testing.T) {
	// 对照 linux/uinput.h 的 _IOC 布局推导值
	cases := map[string]uint32{
		"UI_SET_EVBIT":   0x40045564,
		"UI_SET_KEYBIT":  0x40045565,
		"UI_SET_RELBIT":  0x40045566,
		"UI_DEV_SETUP":   0x405C5503,
		"UI_DEV_CREATE":  0x5501,
		"UI_DEV_DESTROY": 0x5502,
	}
	got := map[string]uint32{
		"UI_SET_EVBIT":   ioctlUISetEvbit,
		"UI_SET_KEYBIT":  ioctlUISetKeybit,
		"UI_SET_RELBIT":  ioctlUISetRelbit,
		"UI_DEV_SETUP":   ioctlUIDevSetup,
		"UI_DEV_CREATE":  ioctlUIDevCreate,
		"UI_DEV_DESTROY": ioctlUIDevDestroy,
	}
	for name, want := range cases {
		if got[name] != want {
			t.Errorf("%s = %#08x, want %#08x", name, got[name], want)
		}
	}
}

func TestEvBits(t *testing.T) {
	want := []uint16{0x00, 0x01, 0x02} // EV_SYN EV_KEY EV_REL
	got := EvBits()
	if !slices.Equal(got, want) {
		t.Errorf("EvBits = %v, want %v", got, want)
	}
}

func TestRelBits(t *testing.T) {
	// REL_X(0) REL_Y(1) REL_WHEEL(8) REL_WHEEL_HI_RES(11)
	want := []uint16{0, 1, 8, 11}
	got := RelBits()
	if !slices.Equal(got, want) {
		t.Errorf("RelBits = %v, want %v", got, want)
	}
}

func TestKeyBits(t *testing.T) {
	bits := KeyBits()
	// 必须含鼠标三键
	for _, code := range []uint16{inject.BtnLeft, inject.BtnRight, inject.BtnMiddle} {
		if !slices.Contains(bits, code) {
			t.Errorf("KeyBits 缺少鼠标按钮 %d", code)
		}
	}
	// 必须覆盖 HIDToKey 全表
	for _, key := range inject.HIDToKey {
		if !slices.Contains(bits, key) {
			t.Errorf("KeyBits 缺少 KEY_%d（HIDToKey 映射值）", key)
		}
	}
	// 升序且无重复
	if !slices.IsSorted(bits) {
		t.Errorf("KeyBits 未排序: %v", bits)
	}
	dedup := slices.Compact(slices.Clone(bits))
	if len(dedup) != len(bits) {
		t.Errorf("KeyBits 存在重复: %v", bits)
	}
	// PROBE-LINUX §5 evtest 期望的关键键位（REL 位归 RelBits 校验）
	for _, tc := range []struct {
		code uint16
		name string
	}{
		{25, "KEY_P"},
		{272, "BTN_LEFT"},
	} {
		if !slices.Contains(bits, tc.code) {
			t.Errorf("KeyBits 缺少 %s(%d)", tc.name, tc.code)
		}
	}
}

func TestDeviceName(t *testing.T) {
	if DeviceName != "PadLink Virtual Pointer" {
		t.Errorf("DeviceName = %q, want %q（PROBE-LINUX §4/§6 按此名查找设备）", DeviceName, "PadLink Virtual Pointer")
	}
	if len(DeviceName) >= 80 {
		t.Errorf("DeviceName 长度 %d 超过 uinput name[80] 上限", len(DeviceName))
	}
}
