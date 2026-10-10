//go:build !linux && !darwin

package uinput

import (
	"errors"
	"time"

	"padlink/daemon/internal/inject"
)

// Device 在无注入后端的平台不可用；仅为保证可编译（darwin 由 uinput_darwin.go 承载）。
type Device struct{}

// Open 无后端平台恒返回错误。
func Open() (*Device, error) {
	return nil, errors.New("uinput: 当前平台无注入后端（支持 Linux uinput / macOS CGEvent）")
}

func (d *Device) KeyEvent(code uint16, value int32) error {
	return errors.New("uinput: 当前平台无注入后端")
}

func (d *Device) RelEvent(code uint16, value int32) error {
	return errors.New("uinput: 当前平台无注入后端")
}

func (d *Device) Sync() error {
	return errors.New("uinput: 当前平台无注入后端")
}

func (d *Device) SetPointerState(st inject.PointerState) {}

// DoubleClickInterval 无注入后端平台恒返回 0（由 inject 层回落默认值）。
func DoubleClickInterval() time.Duration { return 0 }

func (d *Device) Close() error {
	return nil
}
