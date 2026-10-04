package main

import (
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"padlink/daemon/internal/inject"
)

type eventKind uint8

const (
	kindKey eventKind = iota
	kindRel
	kindSync
)

type event struct {
	kind  eventKind
	code  uint16
	value int32
}

// fakeWriter 记录事件流，供 runTest 序列断言（darwin 可跑，无需 uinput）。
type fakeWriter struct {
	mu     sync.Mutex
	events []event
}

func (f *fakeWriter) KeyEvent(code uint16, value int32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event{kindKey, code, value})
	return nil
}

func (f *fakeWriter) RelEvent(code uint16, value int32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event{kindRel, code, value})
	return nil
}

func (f *fakeWriter) Sync() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event{kind: kindSync})
	return nil
}

func (f *fakeWriter) Close() error { return nil }

func (f *fakeWriter) count(kind eventKind, code uint16, value int32) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.events {
		if e.kind == kind && e.code == code && e.value == value {
			n++
		}
	}
	return n
}

// sumRel 对指定 REL 轴求位移代数和（画圆闭合检查用）。
func (f *fakeWriter) sumRel(code uint16) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	total := int32(0)
	for _, e := range f.events {
		if e.kind == kindRel && e.code == code {
			total += e.value
		}
	}
	return int(total)
}

// captureStdout 捕获期间的标准输出。
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	fn()
	os.Stdout = orig
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("读取 stdout: %v", err)
	}
	return string(out)
}

func TestRunTestSequence(t *testing.T) {
	fw := &fakeWriter{}
	out := captureStdout(t, func() {
		if err := runTest(fw, false); err != nil {
			t.Fatalf("runTest: %v", err)
		}
	})

	// PROBE-LINUX §4 期望的输出行
	for _, want := range []string{
		"drawing 2 circles (r=200px, 120 segments each)…",
		"left click…",
		"typing 'pl' (HID usage map)…",
		"scrolling hi-res +2 notches / -1 notch…",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少 %q，实得:\n%s", want, out)
		}
	}
	if strings.Contains(out, "[verbose]") {
		t.Errorf("非 verbose 模式不应输出 [verbose] 行:\n%s", out)
	}

	// 画圆：REL_X(0)/REL_Y(1) 位移代数和为 0（两圆各自精确闭合）
	if x, y := fw.sumRel(0), fw.sumRel(1); x != 0 || y != 0 {
		t.Errorf("圆未闭合: ΣREL_X=%d ΣREL_Y=%d, want 0/0", x, y)
	}

	// 左键一次按下/抬起（BTN_LEFT=272）
	if got := fw.count(kindKey, 272, 1); got != 1 {
		t.Errorf("BTN_LEFT down = %d 次, want 1", got)
	}
	if got := fw.count(kindKey, 272, 0); got != 1 {
		t.Errorf("BTN_LEFT up = %d 次, want 1", got)
	}

	// 敲击 'pl'（KEY_P=25, KEY_L=38）
	if got := fw.count(kindKey, 25, 1); got != 1 {
		t.Errorf("KEY_P down = %d 次, want 1", got)
	}
	if got := fw.count(kindKey, 38, 1); got != 1 {
		t.Errorf("KEY_L down = %d 次, want 1", got)
	}

	// 滚动：HI_RES(11) +240 / -120；legacy REL_WHEEL(8) 帧内取整 +2 / -1
	if got := fw.count(kindRel, 11, 240); got != 1 {
		t.Errorf("REL_WHEEL_HI_RES +240 = %d 次, want 1", got)
	}
	if got := fw.count(kindRel, 11, -120); got != 1 {
		t.Errorf("REL_WHEEL_HI_RES -120 = %d 次, want 1", got)
	}
	if got := fw.count(kindRel, 8, 2); got != 1 {
		t.Errorf("REL_WHEEL +2 = %d 次, want 1", got)
	}
	if got := fw.count(kindRel, 8, -1); got != 1 {
		t.Errorf("REL_WHEEL -1 = %d 次, want 1", got)
	}
}

func TestRunTestVerbose(t *testing.T) {
	fw := &fakeWriter{}
	out := captureStdout(t, func() {
		if err := runTest(fw, true); err != nil {
			t.Fatalf("runTest: %v", err)
		}
	})
	want := "[verbose] device \"PadLink Virtual Pointer\" created, sequence complete"
	if !strings.Contains(out, want) {
		t.Errorf("verbose 输出缺少 %q，实得:\n%s", want, out)
	}
}

// 编译期确认 fakeWriter 满足 DeviceWriter。
var _ inject.DeviceWriter = (*fakeWriter)(nil)
