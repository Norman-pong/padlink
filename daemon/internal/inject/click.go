package inject

import (
	"math"
	"time"
)

// 多击合成默认值。
const (
	// DefaultDoubleClickInterval 相邻两次按下的最大间隔；Linux 侧多击判定不消费本值。
	DefaultDoubleClickInterval = 500 * time.Millisecond
	// DefaultDoubleClickDistance 判定"同一位置点击"的最大指针位移（计数）。
	DefaultDoubleClickDistance = 8
	// MaxClickCount 合成的最大点击序号；Apple 只定义 1/2/3，再加计无意义。
	MaxClickCount = 3
)

// PointerState 是一次按钮事件发生时注入端的指针状态。
type PointerState struct {
	ClickCount  int   // 本次点击序号：1=单击 2=双击 3=三击
	ButtonsDown uint8 // 当前按住的按钮位掩码：1=左 2=中 4=右
}

// clickTracker 合成多击点击序号：同键、限时（interval）、限距（distance）的连续按下递增计数；
// 时钟由调用方传入（纯逻辑，可在任意平台单测）。
//
// 平台差异：Linux 桌面由内核时间戳 + 合成器/工具包判定多击与拖拽，uinput 后端不消费本计数；
// macOS 的 CGEvent 不会由 WindowServer 补算序号，应用侧 clickCount 恒为 1，必须显式写
// kCGMouseEventClickState。
type clickTracker struct {
	interval time.Duration
	distance float64

	count    int       // 上一轮点击序号（0 = 尚无点击）
	lastDown time.Time // 上一次按下时刻（间隔判据基准）
	lastBtn  uint8     // 上一次按下的按钮位
	dx, dy   float64   // 距上次按下的累计位移（限距判据）
	down     uint8     // 当前按住的按钮位掩码
}

func newClickTracker(interval time.Duration, distance int32) *clickTracker {
	if interval <= 0 {
		interval = DefaultDoubleClickInterval
	}
	if distance <= 0 {
		distance = DefaultDoubleClickDistance
	}
	return &clickTracker{interval: interval, distance: float64(distance)}
}

func (c *clickTracker) moved(dx, dy int32) {
	c.dx += float64(dx)
	c.dy += float64(dy)
}

func (c *clickTracker) press(btn uint8, now time.Time) PointerState {
	bit := buttonBit(btn)
	if bit == 0 {
		return PointerState{ClickCount: 1, ButtonsDown: c.down}
	}
	count := 1
	contiguous := c.count > 0 && c.lastBtn == bit &&
		now.Sub(c.lastDown) <= c.interval &&
		math.Hypot(c.dx, c.dy) <= c.distance
	if contiguous {
		// 封顶在 3：第 4 次快速点击不重开新序列（应用侧按三击处理最稳）。
		count = c.count + 1
		if count > MaxClickCount {
			count = MaxClickCount
		}
	}
	c.count, c.lastDown, c.lastBtn = count, now, bit
	c.dx, c.dy = 0, 0
	c.down |= bit
	return PointerState{ClickCount: count, ButtonsDown: c.down}
}

// release 记录一次抬起；点击序号保持不变（按下/抬起须携带同一序号）。
func (c *clickTracker) release(btn uint8) PointerState {
	if bit := buttonBit(btn); bit != 0 {
		c.down &^= bit
	}
	return PointerState{ClickCount: c.count, ButtonsDown: c.down}
}

// state 返回当前指针状态（不推进多击序列；位移事件用）。
func (c *clickTracker) state() PointerState {
	return PointerState{ClickCount: c.count, ButtonsDown: c.down}
}

// buttonBit 按钮号（协议 1=左 2=中 3=右）→ 位掩码。
func buttonBit(btn uint8) uint8 {
	switch btn {
	case 1:
		return 1
	case 2:
		return 2
	case 3:
		return 4
	default:
		return 0
	}
}
