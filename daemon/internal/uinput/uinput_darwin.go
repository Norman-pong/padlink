//go:build darwin

package uinput

import (
	"fmt"
	"os"
	"sync"
	"time"

	"padlink/daemon/internal/inject"
	"padlink/daemon/internal/quartz"
)

// Device 是 macOS Quartz CGEvent 注入后端，实现 inject.DeviceWriter。
// 与 Linux uinput 后端的语义差异（docs/PLAN-MACOS.md §2.2/§2.3）：
// REL 事件先入缓冲、Sync 时合成一次绝对定位 post（OS 加速曲线不作用于绝对定位）；
// legacy REL_WHEEL 忽略，像素滚轮只从 REL_WHEEL_HI_RES 单边推导，双发会重复滚动。
type Device struct {
	mu    sync.Mutex
	dx    int32  // 待合成 REL_X 累积
	dy    int32  // 待合成 REL_Y 累积
	wheel int32  // 待合成 REL_WHEEL_HI_RES 累积（1/120 格）
	mods  uint64 // 当前按住的修饰键 quartz Flag* 位（合成事件必须显式携带修饰标志）
}

// TCC 辅助功能授权等待：弹窗后轮询直到授权；不设超时——launchd 场景下超时会引发
// 退出/重启反复弹窗，前台场景用户可自行 Ctrl+C 中断。
const axPollInterval = 500 * time.Millisecond

// Open 过 TCC 辅助功能门控后返回后端。未授权时 CGEvent 注入被系统静默丢弃
// （PoC 实测），所以先弹系统授权框再驻留轮询授权，而不是带病启动。
func Open() (*Device, error) {
	if quartz.Trusted(false) {
		return &Device{}, nil
	}
	fmt.Fprintln(os.Stderr, "padlinkd 需要「辅助功能」权限才能注入键鼠事件：")
	fmt.Fprintln(os.Stderr, "  系统设置 → 隐私与安全性 → 辅助功能 → 勾选本终端（或 padlinkd）")
	quartz.Trusted(true) // 拉起系统授权弹窗
	fmt.Fprintln(os.Stderr, "等待授权中…（授权后自动继续；前台运行可按 Ctrl+C 取消）")
	remind := time.NewTicker(30 * time.Second)
	defer remind.Stop()
	for {
		if quartz.Trusted(false) {
			fmt.Fprintln(os.Stderr, "已获得辅助功能权限")
			return &Device{}, nil
		}
		select {
		case <-time.After(axPollInterval):
		case <-remind.C:
			fmt.Fprintln(os.Stderr, "仍在等待辅助功能授权（系统设置 → 隐私与安全性 → 辅助功能）…")
		}
	}
}

// KeyEvent 投递键盘/鼠标按钮事件；value 非 0 即按下（含连发值 2）。
func (d *Device) KeyEvent(code uint16, value int32) error {
	if btn, ok := cgButton(code); ok {
		return quartz.PostButton(btn, value != 0)
	}
	kc, ok := keyToCGKeyCode[code]
	if !ok {
		return fmt.Errorf("uinput(darwin): KEY code %d 无 CGKeyCode 映射", code)
	}
	return quartz.PostKey(kc, value != 0, d.trackMod(code, value != 0))
}

// trackMod 更新按住修饰键状态并返回本事件应携带的 flags：
// 修饰键自身按下含自身位、抬起不含（同物理键盘）；非修饰键原样返回当前掩码。
func (d *Device) trackMod(code uint16, down bool) uint64 {
	bit, isMod := cgModFlag(code)
	d.mu.Lock()
	defer d.mu.Unlock()
	if isMod {
		if down {
			d.mods |= bit
		} else {
			d.mods &^= bit
		}
	}
	return d.mods
}

// RelEvent 只缓冲不投递，Sync 收帧时统一合成。
func (d *Device) RelEvent(code uint16, value int32) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch code {
	case inject.RelX:
		d.dx += value
	case inject.RelY:
		d.dy += value
	case inject.RelWheelHiRes:
		d.wheel += value
	case inject.RelWheel:
		// 帧内 legacy 补发是 Linux 内核惯例，darwin 只消费 HI_RES
	default:
		return fmt.Errorf("uinput(darwin): 未知 REL code %d", code)
	}
	return nil
}

// Sync 收帧：缓冲的位移合成一次绝对定位 move，滚动合成一次像素滚轮。
func (d *Device) Sync() error {
	d.mu.Lock()
	dx, dy, wheel := d.dx, d.dy, d.wheel
	d.dx, d.dy, d.wheel = 0, 0, 0
	d.mu.Unlock()

	if dx != 0 || dy != 0 {
		x, y := quartz.CursorPos()
		err := quartz.PostMouseMove(quartz.MouseMove{
			X: x + float64(dx), Y: y + float64(dy),
			DeltaX: dx, DeltaY: dy,
		})
		if err != nil {
			return fmt.Errorf("uinput(darwin): 位移注入: %w", err)
		}
	}
	if wheel != 0 {
		if err := quartz.PostScrollPixel(hiResToPixels(wheel)); err != nil {
			return fmt.Errorf("uinput(darwin): 滚轮注入: %w", err)
		}
	}
	return nil
}

// Close darwin 后端无设备句柄，空操作。
func (d *Device) Close() error { return nil }

// hiResToPixels 把 1/120 格换算为像素：1 格≈53px（Mac 触控板行滚手感起步值，
// 手感系数由 M-1/M-4 真机实测校准，docs/PLAN-MACOS.md §2.3）。非零输入保底 ±1px。
func hiResToPixels(hiRes int32) int32 {
	px := hiRes * 53 / 120
	if px == 0 && hiRes != 0 {
		if hiRes > 0 {
			return 1
		}
		return -1
	}
	return px
}
