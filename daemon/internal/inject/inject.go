// Package inject 将协议事件编排为 uinput 事件序列（平台无关层）。
// 具体注入由 DeviceWriter 后端实现（Linux 为 internal/uinput）。
package inject

import (
	"fmt"
	"sync"
	"time"
)

// Linux input 事件码（linux/input-event-codes.h）。
const (
	RelX          uint16 = 0  // REL_X
	RelY          uint16 = 1  // REL_Y
	RelWheel      uint16 = 8  // REL_WHEEL
	RelWheelHiRes uint16 = 11 // REL_WHEEL_HI_RES

	BtnLeft   uint16 = 272 // 0x110
	BtnRight  uint16 = 273 // 0x111
	BtnMiddle uint16 = 274 // 0x112
)

// key repeat 参数（PRD §4.4：长按连发由 daemon 侧处理，与真实键盘一致）。
const (
	DefaultRepeatDelay    = 250 * time.Millisecond
	DefaultRepeatInterval = 33 * time.Millisecond
)

// DeviceWriter 是注入后端抽象：value 语义与 Linux input 事件一致
// （按下 1 / 抬起 0；相对位移为计数）。实现必须并发安全。
type DeviceWriter interface {
	KeyEvent(code uint16, value int32) error
	RelEvent(code uint16, value int32) error
	Sync() error
	Close() error
	// SetPointerState 更新注入端指针状态（多击序号 + 按住的按钮位），在按钮事件与位移帧前调用。
	// darwin 后端据此写 kCGMouseEventClickState 并选择 mouseDragged 事件类型；
	// Linux uinput 后端为空实现（多击与拖拽由内核时间戳/合成器自行判定，见 click.go）。
	SetPointerState(st PointerState)
}

// Config 可调参数；零值字段取默认值。
type Config struct {
	// RepeatDelay 按住到首次连发的延迟（默认 250ms）。
	RepeatDelay time.Duration
	// RepeatInterval 连发间隔（默认 33ms，≈30Hz）。
	RepeatInterval time.Duration
	// DoubleClickInterval 多击合成的最大按下间隔（默认 500ms，仅 darwin 消费）。
	DoubleClickInterval time.Duration
	// DoubleClickDistance 多击合成的最大指针位移（计数，默认 8，仅 darwin 消费）。
	DoubleClickDistance int32
	// CapsLockActive 报告主机大写锁定当前是否生效；nil 或恒 false 表示无需补偿。
	// 手机端的 Shift 表达的是"这个字母要大写"，而主机 CapsLock 会在注入侧再反转一次
	// （实测 Linux uinput 注入同样受影响：开锁后手机打小写、主机出大写），
	// 因此字母键需按 XOR 语义改写 Shift，保证"手机显示什么、主机就打什么"。
	// macOS 传恒 false：合成 CGEvent 带显式 flags，实测开锁后注入 a/b/c 仍得 abc。
	CapsLockActive func() bool
}

type repeatHandle struct {
	stop chan struct{}
	done chan struct{}
}

// Injector 把 MOVE/SCROLL/BUTTON/KEY 事件编排为带 SYN 边界的 uinput 事件序列，
// 并管理 daemon 侧 key repeat。零值不可用，经 NewInjector 创建。
type Injector struct {
	w     DeviceWriter
	cfg   Config
	click *clickTracker

	mu      sync.Mutex
	repeats map[uint16]*repeatHandle // KEY_* code → 活跃连发

	// CapsLock 补偿状态（都在 mu 下读写）：
	shiftHeld   bool // 手机端是否按住 Shift（大写意图的唯一依据）
	capsAdded   bool // 本层为补偿临时按下 Shift，字母抬起时释放
	capsBlocked bool // 本层为补偿临时松开手机按住的 Shift，字母抬起后按需恢复
}

func NewInjector(w DeviceWriter, cfg Config) *Injector {
	return &Injector{
		w:       w,
		cfg:     cfg,
		click:   newClickTracker(cfg.DoubleClickInterval, cfg.DoubleClickDistance),
		repeats: make(map[uint16]*repeatHandle),
	}
}

// Move 写 REL_X/REL_Y 并以 SYN 收帧。
func (in *Injector) Move(dx, dy int16) error {
	in.click.moved(int32(dx), int32(dy))
	in.w.SetPointerState(in.click.state()) // 拖拽中位移帧须带按住状态（darwin 选 mouseDragged）
	if err := in.w.RelEvent(RelX, int32(dx)); err != nil {
		return fmt.Errorf("REL_X: %w", err)
	}
	if err := in.w.RelEvent(RelY, int32(dy)); err != nil {
		return fmt.Errorf("REL_Y: %w", err)
	}
	return in.w.Sync()
}

// Scroll 写 REL_WHEEL_HI_RES（单位 = 1/120 格），并在同一 SYN 帧内按
// 内核惯例补发 legacy REL_WHEEL = dyHiRes/120（向零取整，不跨帧累积）。
// GNOME/mutter 只消费 HI_RES 路径，legacy 仅为非 GNOME 环境防御。
// dyHiRes == 0 不发任何事件。
func (in *Injector) Scroll(dyHiRes int32) error {
	if dyHiRes == 0 {
		return nil
	}
	if err := in.w.RelEvent(RelWheelHiRes, dyHiRes); err != nil {
		return fmt.Errorf("REL_WHEEL_HI_RES: %w", err)
	}
	if legacy := dyHiRes / 120; legacy != 0 { // Go 整除即向零取整
		if err := in.w.RelEvent(RelWheel, legacy); err != nil {
			return fmt.Errorf("REL_WHEEL: %w", err)
		}
	}
	return in.w.Sync()
}

// Button 注入鼠标按钮事件（btn 1=左 2=中 3=右），非法 btn 丢弃并报错。
// 按下/抬起前先合成多击序号并交给后端（darwin 写 kCGMouseEventClickState）。
func (in *Injector) Button(btn uint8, down bool) error {
	code, ok := buttonCode(btn)
	if !ok {
		return fmt.Errorf("inject: 未知按钮号 %d", btn)
	}
	if down {
		in.w.SetPointerState(in.click.press(btn, time.Now()))
	} else {
		in.w.SetPointerState(in.click.release(btn))
	}
	if err := in.w.KeyEvent(code, boolToInt32(down)); err != nil {
		return fmt.Errorf("BTN code=%d: %w", code, err)
	}
	return in.w.Sync()
}

// Key 注入键盘事件（HID usage → Linux KEY_*），down 时启动连发，
// 收到同键 up 或 Close 停止。未知 usage 丢弃并报错。
// 字母键在主机 CapsLock 生效时按 XOR 语义改写 Shift（见 keyWithCaps）。
func (in *Injector) Key(hidUsage uint16, down bool) error {
	code, ok := HIDToKey[hidUsage]
	if !ok {
		return fmt.Errorf("inject: 未映射的 HID usage 0x%04X", hidUsage)
	}
	if isShiftHID(hidUsage) {
		in.setShiftHeld(down) // 先记账：补偿判定只认手机端意图
		return in.keyRaw(code, down)
	}
	if isLetterHID(hidUsage) {
		if wantUpper, need := in.capsAdjust(); need {
			return in.keyWithCaps(code, down, wantUpper)
		}
	}
	return in.keyRaw(code, down)
}

// keyRaw 是原始投递路径：down 时启动连发，up 时停止。
func (in *Injector) keyRaw(code uint16, down bool) error {
	if !down {
		in.stopRepeat(code)
		return in.tapKey(code, 0)
	}
	in.stopRepeat(code) // 已按住时重复按下：重置连发节奏
	if err := in.tapKey(code, 1); err != nil {
		return err
	}
	in.startRepeat(code)
	return nil
}

// keyWithCaps 按 CapsLock 补偿语义投递字母键：
// 需要大写而手机没按 Shift 时临时按下 Shift；需要小写而手机按着 Shift 时临时松开，
// 字母抬起后恢复手机的原状态。wantUpper 已由 capsAdjust 按 XOR 算好。
func (in *Injector) keyWithCaps(code uint16, down, wantUpper bool) error {
	shiftCode, ok := HIDToKey[HIDLeftShift]
	if !ok {
		return in.keyRaw(code, down) // 映射表缺 Shift：退化为不补偿，不阻断输入
	}
	in.mu.Lock()
	shiftHeld := in.shiftHeld
	in.mu.Unlock()
	if wantUpper == shiftHeld {
		return in.keyRaw(code, down) // 语义一致，无需改写
	}
	if wantUpper { // 补 Shift：按下 → 字母 → 抬起
		in.mu.Lock()
		added := in.capsAdded
		in.capsAdded = true
		in.mu.Unlock()
		if down && !added {
			if err := in.tapKey(shiftCode, 1); err != nil {
				return err
			}
		}
		if err := in.keyRaw(code, down); err != nil {
			return err
		}
		if down {
			return nil
		}
		in.mu.Lock()
		in.capsAdded = false
		in.mu.Unlock()
		return in.tapKey(shiftCode, 0)
	}
	// 屏蔽 Shift：松开 → 字母 → 按需恢复
	if down {
		if err := in.tapKey(shiftCode, 0); err != nil {
			return err
		}
		in.mu.Lock()
		in.capsBlocked = true
		in.mu.Unlock()
		return in.keyRaw(code, down)
	}
	if err := in.keyRaw(code, down); err != nil {
		return err
	}
	in.mu.Lock()
	blocked, stillHeld := in.capsBlocked, in.shiftHeld
	in.capsBlocked = false
	in.mu.Unlock()
	if blocked && stillHeld { // 手机中途松开 Shift 就不再恢复，避免留下按住的修饰键
		return in.tapKey(shiftCode, 1)
	}
	return nil
}

// capsAdjust 给出字母键应达到的大写状态（XOR：手机意图 ⊕ 主机大写锁）。
// need=false 表示本平台不需要补偿。
func (in *Injector) capsAdjust() (wantUpper, need bool) {
	if in.cfg.CapsLockActive == nil || !in.cfg.CapsLockActive() {
		return false, false
	}
	in.mu.Lock()
	shift := in.shiftHeld
	in.mu.Unlock()
	return !shift, true
}

func (in *Injector) setShiftHeld(down bool) {
	in.mu.Lock()
	in.shiftHeld = down
	in.mu.Unlock()
}

// isLetterHID 判定字母键（HID 0x04-0x1D = A-Z）；大小写锁只影响字母。
func isLetterHID(hid uint16) bool { return hid >= 0x04 && hid <= 0x1D }

func isShiftHID(hid uint16) bool { return hid == HIDLeftShift || hid == HIDRightShift }

// PressCombo 依次按下 mods、点击 key、逆序抬起 mods（每步 Sync）。
// 组合键走原始按键路径，不触发 key repeat。
func (in *Injector) PressCombo(mods []uint16, key uint16) error {
	codes := make([]uint16, 0, len(mods)+1)
	for _, hid := range mods {
		code, ok := HIDToKey[hid]
		if !ok {
			return fmt.Errorf("inject: 未映射的 HID usage 0x%04X", hid)
		}
		codes = append(codes, code)
	}
	keyCode, ok := HIDToKey[key]
	if !ok {
		return fmt.Errorf("inject: 未映射的 HID usage 0x%04X", key)
	}
	for _, code := range codes {
		if err := in.tapKey(code, 1); err != nil {
			return err
		}
	}
	if err := in.tapKey(keyCode, 1); err != nil {
		return err
	}
	if err := in.tapKey(keyCode, 0); err != nil {
		return err
	}
	for i := len(codes) - 1; i >= 0; i-- {
		if err := in.tapKey(codes[i], 0); err != nil {
			return err
		}
	}
	return nil
}

// CtrlV 注入粘贴组合键（Linux 文本注入路径用）。
func (in *Injector) CtrlV() error {
	return in.PressCombo([]uint16{HIDLeftCtrl}, HIDV)
}

// SuperV 注入 Super+V（macOS 粘贴组合键 Cmd+V，文本注入路径按平台选用）。
func (in *Injector) SuperV() error {
	return in.PressCombo([]uint16{HIDLeftSuper}, HIDV)
}

// Close 停止所有连发并关闭后端。
func (in *Injector) Close() error {
	in.mu.Lock()
	handles := make([]*repeatHandle, 0, len(in.repeats))
	for code, h := range in.repeats {
		handles = append(handles, h)
		delete(in.repeats, code)
	}
	in.mu.Unlock()
	for _, h := range handles {
		close(h.stop)
		<-h.done
	}
	return in.w.Close()
}

func (in *Injector) tapKey(code uint16, value int32) error {
	if err := in.w.KeyEvent(code, value); err != nil {
		return fmt.Errorf("KEY code=%d: %w", code, err)
	}
	return in.w.Sync()
}

func (in *Injector) startRepeat(code uint16) {
	delay, interval := in.repeatParams()
	h := &repeatHandle{stop: make(chan struct{}), done: make(chan struct{})}
	in.mu.Lock()
	in.repeats[code] = h
	in.mu.Unlock()
	go func() {
		defer close(h.done)
		timer := time.NewTimer(delay)
		defer timer.Stop()
		for {
			select {
			case <-h.stop:
				return
			case <-timer.C:
				if err := in.tapKey(code, 1); err != nil {
					return // 写入失败即停连发，不吞错重试
				}
				timer.Reset(interval)
			}
		}
	}()
}

func (in *Injector) stopRepeat(code uint16) {
	in.mu.Lock()
	h := in.repeats[code]
	delete(in.repeats, code)
	in.mu.Unlock()
	if h != nil {
		close(h.stop)
		<-h.done
	}
}

func (in *Injector) repeatParams() (time.Duration, time.Duration) {
	delay, interval := in.cfg.RepeatDelay, in.cfg.RepeatInterval
	if delay <= 0 {
		delay = DefaultRepeatDelay
	}
	if interval <= 0 {
		interval = DefaultRepeatInterval
	}
	return delay, interval
}

func buttonCode(btn uint8) (uint16, bool) {
	switch btn {
	case 1:
		return BtnLeft, true
	case 2:
		return BtnMiddle, true
	case 3:
		return BtnRight, true
	default:
		return 0, false
	}
}

func boolToInt32(b bool) int32 {
	if b {
		return 1
	}
	return 0
}
