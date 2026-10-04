package inject

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type eventKind uint8

const (
	kindKeyEvent eventKind = iota
	kindRelEvent
	kindSync
)

type recordedEvent struct {
	kind  eventKind
	code  uint16
	value int32
}

func (e recordedEvent) String() string {
	switch e.kind {
	case kindKeyEvent:
		return fmt.Sprintf("KEY(%d,%d)", e.code, e.value)
	case kindRelEvent:
		return fmt.Sprintf("REL(%d,%d)", e.code, e.value)
	default:
		return "SYN"
	}
}

// fakeDeviceWriter 记录事件序列，供断言；置 fail 后所有调用返回错误。
type fakeDeviceWriter struct {
	mu      sync.Mutex
	events  []recordedEvent
	fail    bool
	closed  bool
	failErr error
}

var errFake = errors.New("fake writer failure")

func (f *fakeDeviceWriter) KeyEvent(code uint16, value int32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return f.failErr
	}
	f.events = append(f.events, recordedEvent{kindKeyEvent, code, value})
	return nil
}

func (f *fakeDeviceWriter) RelEvent(code uint16, value int32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return f.failErr
	}
	f.events = append(f.events, recordedEvent{kindRelEvent, code, value})
	return nil
}

func (f *fakeDeviceWriter) Sync() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return f.failErr
	}
	f.events = append(f.events, recordedEvent{kind: kindSync})
	return nil
}

func (f *fakeDeviceWriter) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeDeviceWriter) recorded() []recordedEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedEvent(nil), f.events...)
}

func (f *fakeDeviceWriter) countKey(code uint16, value int32) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.events {
		if e.kind == kindKeyEvent && e.code == code && e.value == value {
			n++
		}
	}
	return n
}

// syn 分帧后取一帧内的事件（不含 SYN 本身）。
func frames(t *testing.T, evs []recordedEvent) [][]recordedEvent {
	t.Helper()
	var out [][]recordedEvent
	var cur []recordedEvent
	for _, e := range evs {
		if e.kind == kindSync {
			out = append(out, cur)
			cur = nil
			continue
		}
		cur = append(cur, e)
	}
	if len(cur) > 0 {
		t.Fatalf("事件序列存在未收帧的尾部: %v", cur)
	}
	return out
}

func requireEvents(t *testing.T, got, want []recordedEvent) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("事件数 = %d, want %d\n got:  %v\n want: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events[%d] = %v, want %v\n got:  %v\n want: %v", i, got[i], want[i], got, want)
		}
	}
}

func TestMove(t *testing.T) {
	fw := &fakeDeviceWriter{}
	inj := NewInjector(fw, Config{})
	if err := inj.Move(-5, 3); err != nil {
		t.Fatalf("Move: %v", err)
	}
	requireEvents(t, fw.recorded(), []recordedEvent{
		{kindRelEvent, RelX, -5},
		{kindRelEvent, RelY, 3},
		{kind: kindSync},
	})
}

func TestScroll(t *testing.T) {
	t.Run("+240 补发 REL_WHEEL +2", func(t *testing.T) {
		fw := &fakeDeviceWriter{}
		inj := NewInjector(fw, Config{})
		if err := inj.Scroll(240); err != nil {
			t.Fatalf("Scroll: %v", err)
		}
		requireEvents(t, fw.recorded(), []recordedEvent{
			{kindRelEvent, RelWheelHiRes, 240},
			{kindRelEvent, RelWheel, 2},
			{kind: kindSync},
		})
	})
	t.Run("-119 不补发 legacy（向零取整为 0）", func(t *testing.T) {
		fw := &fakeDeviceWriter{}
		inj := NewInjector(fw, Config{})
		if err := inj.Scroll(-119); err != nil {
			t.Fatalf("Scroll: %v", err)
		}
		requireEvents(t, fw.recorded(), []recordedEvent{
			{kindRelEvent, RelWheelHiRes, -119},
			{kind: kindSync},
		})
	})
	t.Run("+130 补发 REL_WHEEL +1", func(t *testing.T) {
		fw := &fakeDeviceWriter{}
		inj := NewInjector(fw, Config{})
		if err := inj.Scroll(130); err != nil {
			t.Fatalf("Scroll: %v", err)
		}
		requireEvents(t, fw.recorded(), []recordedEvent{
			{kindRelEvent, RelWheelHiRes, 130},
			{kindRelEvent, RelWheel, 1},
			{kind: kindSync},
		})
	})
	t.Run("0 不发任何事件", func(t *testing.T) {
		fw := &fakeDeviceWriter{}
		inj := NewInjector(fw, Config{})
		if err := inj.Scroll(0); err != nil {
			t.Fatalf("Scroll: %v", err)
		}
		requireEvents(t, fw.recorded(), nil)
	})
}

func TestButton(t *testing.T) {
	cases := []struct {
		btn  uint8
		code uint16
	}{
		{1, BtnLeft},
		{2, BtnMiddle},
		{3, BtnRight},
	}
	for _, c := range cases {
		fw := &fakeDeviceWriter{}
		inj := NewInjector(fw, Config{})
		if err := inj.Button(c.btn, true); err != nil {
			t.Fatalf("Button(%d,down): %v", c.btn, err)
		}
		if err := inj.Button(c.btn, false); err != nil {
			t.Fatalf("Button(%d,up): %v", c.btn, err)
		}
		requireEvents(t, fw.recorded(), []recordedEvent{
			{kindKeyEvent, c.code, 1}, {kind: kindSync},
			{kindKeyEvent, c.code, 0}, {kind: kindSync},
		})
	}
	t.Run("非法按钮号丢弃并报错", func(t *testing.T) {
		fw := &fakeDeviceWriter{}
		inj := NewInjector(fw, Config{})
		if err := inj.Button(9, true); err == nil {
			t.Fatal("Button(9) 未报错")
		}
		if evs := fw.recorded(); len(evs) != 0 {
			t.Fatalf("非法按钮产生了事件: %v", evs)
		}
	})
}

func TestKey(t *testing.T) {
	fw := &fakeDeviceWriter{}
	inj := NewInjector(fw, Config{})
	defer inj.Close()
	if err := inj.Key(0x04, true); err != nil { // HID A → KEY_A(30)
		t.Fatalf("Key down: %v", err)
	}
	if err := inj.Key(0x04, false); err != nil {
		t.Fatalf("Key up: %v", err)
	}
	requireEvents(t, fw.recorded(), []recordedEvent{
		{kindKeyEvent, 30, 1}, {kind: kindSync},
		{kindKeyEvent, 30, 0}, {kind: kindSync},
	})
	t.Run("未知 usage 丢弃并报错", func(t *testing.T) {
		if err := inj.Key(0x99, true); err == nil {
			t.Fatal("Key(0x99) 未报错")
		}
	})
	t.Run("注入路径错误透传", func(t *testing.T) {
		fw := &fakeDeviceWriter{fail: true, failErr: errFake}
		inj := NewInjector(fw, Config{})
		defer inj.Close()
		if err := inj.Key(0x04, true); !errors.Is(err, errFake) {
			t.Fatalf("got %v, want %v", err, errFake)
		}
	})
}

func TestCtrlVSequence(t *testing.T) {
	fw := &fakeDeviceWriter{}
	inj := NewInjector(fw, Config{})
	defer inj.Close()
	if err := inj.CtrlV(); err != nil {
		t.Fatalf("CtrlV: %v", err)
	}
	// LCTRL down → V down → V up → LCTRL up，每步 Sync；组合键不触发连发
	requireEvents(t, fw.recorded(), []recordedEvent{
		{kindKeyEvent, 29, 1}, {kind: kindSync},
		{kindKeyEvent, 47, 1}, {kind: kindSync},
		{kindKeyEvent, 47, 0}, {kind: kindSync},
		{kindKeyEvent, 29, 0}, {kind: kindSync},
	})
}

func TestKeyRepeat(t *testing.T) {
	fw := &fakeDeviceWriter{}
	inj := NewInjector(fw, Config{RepeatDelay: 20 * time.Millisecond, RepeatInterval: 20 * time.Millisecond})
	if err := inj.Key(0x04, true); err != nil {
		t.Fatalf("Key down: %v", err)
	}
	time.Sleep(120 * time.Millisecond)
	if got := fw.countKey(30, 1); got < 4 { // 初始 1 次 + 连发 ≥3 次
		t.Fatalf("连发次数不足: down 事件 %d 次, want ≥4", got)
	}
	if got := fw.countKey(30, 0); got != 0 {
		t.Fatalf("连发期间不应有 up 事件, got %d", got)
	}
	if err := inj.Key(0x04, false); err != nil {
		t.Fatalf("Key up: %v", err)
	}
	final := fw.countKey(30, 1)
	time.Sleep(100 * time.Millisecond)
	if got := fw.countKey(30, 1); got != final {
		t.Fatalf("up 后仍在连发: %d → %d", final, got)
	}
	if got := fw.countKey(30, 0); got != 1 {
		t.Fatalf("up 事件数 = %d, want 1", got)
	}
	inj.Close()
}

func TestKeyRepeatStopsOnClose(t *testing.T) {
	fw := &fakeDeviceWriter{}
	inj := NewInjector(fw, Config{RepeatDelay: 10 * time.Millisecond, RepeatInterval: 10 * time.Millisecond})
	if err := inj.Key(0x04, true); err != nil {
		t.Fatalf("Key down: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := inj.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	final := fw.countKey(30, 1)
	time.Sleep(80 * time.Millisecond)
	if got := fw.countKey(30, 1); got != final {
		t.Fatalf("Close 后仍在连发: %d → %d", final, got)
	}
	if !fw.closed {
		t.Fatal("Close 未传递到 DeviceWriter")
	}
}

func TestRepeatRepressResetsCadence(t *testing.T) {
	fw := &fakeDeviceWriter{}
	inj := NewInjector(fw, Config{RepeatDelay: 30 * time.Millisecond, RepeatInterval: 30 * time.Millisecond})
	defer inj.Close()
	// 按住即将进入连发时重新按下：应重置延迟，而非立即连发
	if err := inj.Key(0x04, true); err != nil {
		t.Fatalf("Key down: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := inj.Key(0x04, true); err != nil {
		t.Fatalf("Key re-down: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	// 重新按下后 10ms（< 新的 30ms 延迟），不应有连发外的额外 down
	if got := fw.countKey(30, 1); got != 2 {
		t.Fatalf("重按下后 down 事件 = %d, want 2（两次按下，无立即连发）", got)
	}
}
