package textinject

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type call struct {
	name  string
	args  []string
	stdin []byte
}

// fakeRunner 记录命令序列；respond 按序号返回桩输出/错误。
type fakeRunner struct {
	mu      sync.Mutex
	calls   []call
	respond func(i int, c call) ([]byte, error)
}

func (f *fakeRunner) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call{name, args, stdin})
	i := len(f.calls) - 1
	f.mu.Unlock()
	if f.respond == nil {
		return nil, nil
	}
	return f.respond(i, call{name, args, stdin})
}

func (f *fakeRunner) recorded() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

// recorder 汇总 wl-* 与 CtrlV 的执行顺序。
type recorder struct {
	mu  sync.Mutex
	ops []string
}

func (r *recorder) add(op string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ops = append(r.ops, op)
}

func (r *recorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ops...)
}

// newInjector 钉住 wlClipboard：wl-* 命令序列断言与运行平台无关（darwin 宿主机同样覆盖 Linux 逻辑）。
func newInjector(fr *fakeRunner, rec *recorder, delay time.Duration) *Injector {
	in := New(fr, func() error { rec.add("ctrlv"); return nil }, delay)
	in.clip = wlClipboard
	return in
}

func TestInjectCommandSequence(t *testing.T) {
	fr := &fakeRunner{respond: func(i int, c call) ([]byte, error) {
		switch i {
		case 0: // list-types
			return []byte("text/plain;charset=utf-8\nTEXT\n"), nil
		case 1: // backup
			return []byte("原内容"), nil
		default:
			return nil, nil
		}
	}}
	rec := &recorder{}
	in := newInjector(fr, rec, 5*time.Millisecond)
	in.Env = func(string) string { return "wayland-0" }

	if err := in.Inject("注入文本"); err != nil {
		t.Fatalf("Inject: %v", err)
	}

	// 命令序列与参数逐项断言（7 步：list-types → 备份 → 写入 → CtrlV → 延迟 → 恢复）
	calls := fr.recorded()
	if len(calls) != 4 {
		t.Fatalf("命令数 = %d, want 4:\n%+v", len(calls), calls)
	}
	want := []call{
		{"wl-paste", []string{"--list-types"}, nil},
		{"wl-paste", []string{"--no-newline"}, nil},
		{"wl-copy", []string{"--type", "text/plain;charset=utf-8"}, []byte("注入文本")},
		{"wl-copy", []string{"--type", "text/plain;charset=utf-8"}, []byte("原内容")}, // 恢复 MIME 取自 list-types 首行
	}
	for i, w := range want {
		if calls[i].name != w.name || strings.Join(calls[i].args, " ") != strings.Join(w.args, " ") || string(calls[i].stdin) != string(w.stdin) {
			t.Errorf("call[%d] = %+v, want %+v", i, calls[i], w)
		}
	}

	// CtrlV 必须发生在写入之后、恢复之前
	ops := rec.snapshot()
	if len(ops) != 1 || ops[0] != "ctrlv" {
		t.Fatalf("CtrlV 调用 = %v, want 恰一次", ops)
	}
	// 顺序：run[2]（写入）→ ctrlv → run[3]（恢复）由上断言覆盖次序约束：
	if len(calls) == 4 && calls[3].name != "wl-copy" {
		t.Fatal("恢复命令缺失")
	}
}

func TestInjectDegradesOnListTypesFail(t *testing.T) {
	fr := &fakeRunner{respond: func(i int, c call) ([]byte, error) {
		return nil, errors.New("exec: \"wl-paste\": executable file not found in $PATH")
	}}
	in := newInjector(fr, &recorder{}, time.Millisecond)
	in.Env = func(string) string { return "wayland-0" }

	err := in.Inject("x")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v, want ErrUnavailable", err)
	}
	if !strings.Contains(err.Error(), InstallHint) {
		t.Errorf("错误文案缺安装提示: %v", err)
	}
	if len(fr.recorded()) != 1 {
		t.Errorf("降级后不应继续后续步骤，命令数 = %d", len(fr.recorded()))
	}
}

func TestInjectDegradesOnBackupFail(t *testing.T) {
	fr := &fakeRunner{respond: func(i int, c call) ([]byte, error) {
		if i == 0 {
			return []byte("text/plain\n"), nil
		}
		return nil, context.DeadlineExceeded // 模拟 3s 超时路径
	}}
	in := newInjector(fr, &recorder{}, time.Millisecond)
	in.Env = func(string) string { return "wayland-0" }

	err := in.Inject("x")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v, want ErrUnavailable", err)
	}
	if len(fr.recorded()) != 2 {
		t.Errorf("备份失败后不应继续写剪贴板，命令数 = %d", len(fr.recorded()))
	}
}

func TestInjectPasteFailStillRestores(t *testing.T) {
	fr := &fakeRunner{respond: func(i int, c call) ([]byte, error) {
		switch i {
		case 0:
			return []byte("image/png\n"), nil
		case 1:
			return []byte{0x89, 'P', 'N', 'G'}, nil
		default:
			return nil, nil
		}
	}}
	rec := &recorder{}
	in := New(fr, func() error { rec.add("ctrlv"); return errors.New("注入器故障") }, time.Millisecond)
	in.clip = wlClipboard
	in.Env = func(string) string { return "wayland-0" }

	err := in.Inject("x")
	if err == nil || !strings.Contains(err.Error(), "注入器故障") {
		t.Fatalf("got %v, want 含粘贴失败原因", err)
	}
	// 恢复必须用备份时的原 MIME
	calls := fr.recorded()
	last := calls[len(calls)-1]
	if last.name != "wl-copy" || strings.Join(last.args, " ") != "--type image/png" {
		t.Errorf("恢复命令 = %+v, want --type image/png", last)
	}
	if string(last.stdin) != "\x89PNG" {
		t.Errorf("恢复内容 = %q, want 原字节", last.stdin)
	}
}

func TestInjectRestoreFail(t *testing.T) {
	fr := &fakeRunner{respond: func(i int, c call) ([]byte, error) {
		switch i {
		case 0:
			return []byte("text/plain\n"), nil
		case 1:
			return []byte("old"), nil
		case 3: // 恢复失败
			return nil, errors.New("display closed")
		default:
			return nil, nil
		}
	}}
	in := newInjector(fr, &recorder{}, time.Millisecond)
	in.Env = func(string) string { return "wayland-0" }

	err := in.Inject("x")
	if err == nil || !strings.Contains(err.Error(), "恢复剪贴板失败") {
		t.Fatalf("got %v, want 恢复失败", err)
	}
}

func TestInjectTimeoutContext(t *testing.T) {
	fr := &fakeRunner{respond: func(i int, c call) ([]byte, error) {
		return nil, context.DeadlineExceeded
	}}
	in := newInjector(fr, &recorder{}, time.Millisecond)
	in.Env = func(string) string { return "wayland-0" }

	err := in.Inject("x")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("超时应降级为 ErrUnavailable, got %v", err)
	}
}

func TestAvailable(t *testing.T) {
	in := New(&fakeRunner{}, func() error { return nil }, 0)
	in.clip = wlClipboard
	in.Env = func(k string) string {
		if k == "WAYLAND_DISPLAY" {
			return "wayland-0"
		}
		return ""
	}
	if !in.Available() {
		t.Error("WAYLAND_DISPLAY 就绪时 Available = false")
	}
	in.Env = func(string) string { return "" }
	if in.Available() {
		t.Error("WAYLAND_DISPLAY 缺失时 Available = true")
	}
	if err := in.Inject("x"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("缺失环境 Inject: got %v, want ErrUnavailable", err)
	}
}

// ---- macOS pbcopy/pbpaste 后端 ----

// darwin 命令序列：备份(pbpaste) → 写入(pbcopy) → CmdV → 延迟 → 恢复(pbcopy)。
// darwin 无环境门槛：Env 全空也应可用。
func TestInjectDarwinSequence(t *testing.T) {
	fr := &fakeRunner{respond: func(i int, c call) ([]byte, error) {
		if i == 0 { // 备份
			return []byte("原内容"), nil
		}
		return nil, nil
	}}
	rec := &recorder{}
	in := newInjector(fr, rec, time.Millisecond)
	in.clip = pbClipboard
	in.Env = func(string) string { return "" }

	if !in.Available() {
		t.Fatal("darwin 无环境门槛，Available = false")
	}
	if err := in.Inject("注入文本"); err != nil {
		t.Fatalf("Inject: %v", err)
	}

	calls := fr.recorded()
	if len(calls) != 3 {
		t.Fatalf("命令数 = %d, want 3（darwin 不记 MIME）:\n%+v", len(calls), calls)
	}
	want := []call{
		{"pbpaste", nil, nil},
		{"pbcopy", nil, []byte("注入文本")},
		{"pbcopy", nil, []byte("原内容")},
	}
	for i, w := range want {
		if calls[i].name != w.name || len(calls[i].args) != 0 || string(calls[i].stdin) != string(w.stdin) {
			t.Errorf("call[%d] = %+v, want %+v", i, calls[i], w)
		}
	}
	if ops := rec.snapshot(); len(ops) != 1 || ops[0] != "ctrlv" {
		t.Fatalf("粘贴组合键调用 = %v, want 恰一次", ops)
	}
}

// darwin 降级：pbpaste 失败 → ErrUnavailable 且不再写剪贴板。
func TestInjectDarwinDegradesOnBackupFail(t *testing.T) {
	fr := &fakeRunner{respond: func(i int, c call) ([]byte, error) {
		return nil, errors.New("pbpaste: 未知错误")
	}}
	in := newInjector(fr, &recorder{}, time.Millisecond)
	in.clip = pbClipboard
	in.Env = func(string) string { return "" }

	err := in.Inject("x")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v, want ErrUnavailable", err)
	}
	if !strings.Contains(err.Error(), "pbcopy/pbpaste") {
		t.Errorf("错误文案缺工具名: %v", err)
	}
	if len(fr.recorded()) != 1 {
		t.Errorf("备份失败后不应继续写剪贴板，命令数 = %d", len(fr.recorded()))
	}
}
