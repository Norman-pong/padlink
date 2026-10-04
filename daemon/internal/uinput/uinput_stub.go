//go:build !linux

package uinput

import "errors"

// Device 在非 Linux 平台不可用；仅为保证 darwin 等平台可编译。
type Device struct{}

// Open 非 Linux 平台无 uinput，恒返回错误。
func Open() (*Device, error) {
	return nil, errors.New("uinput: 仅支持 Linux 平台（当前平台无 /dev/uinput）")
}

func (d *Device) KeyEvent(code uint16, value int32) error {
	return errors.New("uinput: 仅支持 Linux 平台")
}

func (d *Device) RelEvent(code uint16, value int32) error {
	return errors.New("uinput: 仅支持 Linux 平台")
}

func (d *Device) Sync() error {
	return errors.New("uinput: 仅支持 Linux 平台")
}

func (d *Device) Close() error {
	return nil
}
