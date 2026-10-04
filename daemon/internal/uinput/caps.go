// Package uinput 是 DeviceWriter 的 Linux uinput 后端。
// 本文件（无 build tag）承载可跨平台断言的纯逻辑：能力位组装与内核 ABI 常量。
package uinput

import (
	"padlink/daemon/internal/inject"
	"sort"
)

// 与 linux/input.h 对齐的事件类型（EV_*）与 SYN_REPORT。
const (
	EvSyn     uint16 = 0x00
	EvKey     uint16 = 0x01
	EvRel     uint16 = 0x02
	SynReport uint16 = 0x00
)

// 与 linux/uinput.h 对齐的 ioctl 请求号（asm-generic _IOC 布局，amd64/arm64 通用）：
// UI_DEV_CREATE/DESTROY 为 _IO('U',1/2)；UI_SET_* 为 _IOW('U',100+nr,int)；
// UI_DEV_SETUP 为 _IOW('U',3,struct uinput_setup)，size=92=0x5C。
const (
	ioctlUISetEvbit   = 0x40045564 // _IOW('U', 100, int)
	ioctlUISetKeybit  = 0x40045565 // _IOW('U', 101, int)
	ioctlUISetRelbit  = 0x40045566 // _IOW('U', 102, int)
	ioctlUIDevSetup   = 0x405C5503 // _IOW('U', 3, struct uinput_setup)
	ioctlUIDevCreate  = 0x5501     // _IO('U', 1)
	ioctlUIDevDestroy = 0x5502     // _IO('U', 2)
)

// 内核结构体尺寸（64 位 amd64/arm64）：
// input_event = timeval(16) + u16 type + u16 code + s32 value = 24；
// uinput_setup = input_id(8) + name[80] + u32 ff_effects_max = 92。
const (
	sizeofInputEvent  = 24
	sizeofUinputSetup = 92
)

// uinput 虚拟设备标识（自定义 vendor，避开任何 libinput quirk 命中）。
const (
	DeviceName    = "PadLink Virtual Pointer"
	idBusUSB      = 0x0003 // BUS_USB
	idVendor      = 0x504C // 'PL'
	idProduct     = 0x0001
	idVersion     = 0x0001
	devUinputPath = "/dev/uinput"
)

// RelBits 注册的相对轴。REL_WHEEL 与 REL_WHEEL_HI_RES 同时注册：
// mutter 只消费 HI_RES 路径；legacy 轴由 inject 层按帧内取整补发
// （若只注册 HI_RES 却不发、或反之，会触发 libinput 告警回退，见 RESEARCH-WAYLAND.md §1）。
func RelBits() []uint16 {
	return []uint16{
		inject.RelX,
		inject.RelY,
		inject.RelWheel,
		inject.RelWheelHiRes,
	}
}

// KeyBits 注册的键位：鼠标三键 + HIDToKey 全表，升序去重。
func KeyBits() []uint16 {
	set := map[uint16]struct{}{
		inject.BtnLeft:   {},
		inject.BtnRight:  {},
		inject.BtnMiddle: {},
	}
	for _, code := range inject.HIDToKey {
		set[code] = struct{}{}
	}
	out := make([]uint16, 0, len(set))
	for code := range set {
		out = append(out, code)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// EvBits 注册的事件类型。
func EvBits() []uint16 {
	return []uint16{EvSyn, EvKey, EvRel}
}
