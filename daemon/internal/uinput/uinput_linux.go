//go:build linux

package uinput

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
	"unsafe"
)

// input_event / input_id / uinput_setup 按 linux/uinput.h、linux/input.h 内联定义
// （64 位 amd64/arm64 布局；尺寸由 Open 内的运行期断言兜底）。
type inputID struct {
	Bustype uint16
	Vendor  uint16
	Product uint16
	Version uint16
}

// inputEvent 与内核 struct input_event 对齐：timeval(16B) + type/code/value(8B)。
// time 置零即可——内核对 uinput 写入忽略用户态时间戳，自行打点。
type inputEvent struct {
	Time  syscall.Timeval
	Type  uint16
	Code  uint16
	Value int32
}

type uinputSetup struct {
	ID           inputID
	Name         [80]byte
	FfEffectsMax uint32
}

// Device 是 /dev/uinput 虚拟输入设备，实现 inject.DeviceWriter。
type Device struct {
	f *os.File
}

// Open 创建虚拟输入设备。需要 /dev/uinput 的读写权限
// （udev uaccess 规则，见 daemon/dist/udev 与 PROBE-LINUX.md §2）。
func Open() (*Device, error) {
	if unsafe.Sizeof(inputEvent{}) != sizeofInputEvent {
		return nil, fmt.Errorf("uinput: input_event 布局 %d 字节与内核 ABI %d 不符", unsafe.Sizeof(inputEvent{}), sizeofInputEvent)
	}
	f, err := os.OpenFile(devUinputPath, os.O_RDWR, 0)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return nil, fmt.Errorf("uinput: 无权限打开 %s: %w（udev uaccess 规则未安装或未生效：重登录后用 `getfacl %s` 自查）", devUinputPath, err, devUinputPath)
		}
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("uinput: %s 不存在: %w（执行 `sudo modprobe uinput`；若仍无则内核未编译 uinput）", devUinputPath, err)
		}
		return nil, fmt.Errorf("uinput: 打开 %s: %w", devUinputPath, err)
	}
	d := &Device{f: f}
	if err := d.setup(); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func (d *Device) setup() error {
	for _, ev := range EvBits() {
		if err := d.ioctlUInt(ioctlUISetEvbit, uint(ev)); err != nil {
			return fmt.Errorf("uinput: UI_SET_EVBIT(%d): %w", ev, err)
		}
	}
	for _, code := range KeyBits() {
		if err := d.ioctlUInt(ioctlUISetKeybit, uint(code)); err != nil {
			return fmt.Errorf("uinput: UI_SET_KEYBIT(%d): %w", code, err)
		}
	}
	for _, rel := range RelBits() {
		if err := d.ioctlUInt(ioctlUISetRelbit, uint(rel)); err != nil {
			return fmt.Errorf("uinput: UI_SET_RELBIT(%d): %w", rel, err)
		}
	}

	var setup uinputSetup
	setup.ID = inputID{Bustype: idBusUSB, Vendor: idVendor, Product: idProduct, Version: idVersion}
	copy(setup.Name[:], DeviceName)
	if err := d.ioctlPtr(ioctlUIDevSetup, unsafe.Pointer(&setup)); err != nil {
		return fmt.Errorf("uinput: UI_DEV_SETUP: %w", err)
	}
	if err := d.ioctlUInt(ioctlUIDevCreate, 0); err != nil {
		return fmt.Errorf("uinput: UI_DEV_CREATE: %w", err)
	}
	return nil
}

// ioctlUInt/ioctlPtr：UI_SET_* 的参数是 int 值本身；UI_DEV_SETUP 传结构体指针。
func (d *Device) ioctlUInt(req uint32, arg uint) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, d.f.Fd(), uintptr(req), uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

func (d *Device) ioctlPtr(req uint32, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, d.f.Fd(), uintptr(req), uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

// emit 写入单个 input_event。os.File 并发写安全（单条 ≤24B 原子写入）。
func (d *Device) emit(typ, code uint16, value int32) error {
	ev := inputEvent{Type: typ, Code: code, Value: value}
	buf := (*[sizeofInputEvent]byte)(unsafe.Pointer(&ev))[:]
	if _, err := d.f.Write(buf); err != nil {
		return fmt.Errorf("uinput: 写入事件 type=%d code=%d: %w", typ, code, err)
	}
	return nil
}

func (d *Device) KeyEvent(code uint16, value int32) error {
	return d.emit(EvKey, code, value)
}

func (d *Device) RelEvent(code uint16, value int32) error {
	return d.emit(EvRel, code, value)
}

// Sync 写 EV_SYN/SYN_REPORT 收帧。
func (d *Device) Sync() error {
	return d.emit(EvSyn, SynReport, 0)
}

// Close 销毁虚拟设备并关闭节点。
func (d *Device) Close() error {
	if d.f == nil {
		return nil
	}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, d.f.Fd(), uintptr(ioctlUIDevDestroy), 0)
	closeErr := d.f.Close()
	if errno != 0 {
		return fmt.Errorf("uinput: UI_DEV_DESTROY: %w", errno)
	}
	return closeErr
}
