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

// newInjector 钉住 wlClipboard：New 的默认后端随宿主机平台变化，不钉住则 wl-* 断言在 darwin 上失效。
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

// 空剪贴板不是工具故障：注入必须照常完成并收尾清空选择，否则中文/语音输入会静默失败。
func TestInjectProceedsWhenClipboardEmpty(t *testing.T) {
	fr := &fakeRunner{respond: func(i int, c call) ([]byte, error) {
		if i == 0 {
			return nil, errors.New("exit status 1: Nothing is copied")
		}
		return nil, nil
	}}
	rec := &recorder{}
	in := newInjector(fr, rec, time.Millisecond)
	in.Env = func(string) string { return "wayland-0" }

	if err := in.Inject("注入文本"); err != nil {
		t.Fatalf("空剪贴板下 Inject = %v, want nil", err)
	}

	calls := fr.recorded()
	want := []call{
		{"wl-paste", []string{"--list-types"}, nil},
		{"wl-copy", []string{"--type", "text/plain;charset=utf-8"}, []byte("注入文本")},
		{"wl-copy", []string{"--clear"}, nil},
	}
	if len(calls) != len(want) {
		t.Fatalf("命令数 = %d, want %d:\n%+v", len(calls), len(want), calls)
	}
	for i, w := range want {
		if calls[i].name != w.name || strings.Join(calls[i].args, " ") != strings.Join(w.args, " ") || string(calls[i].stdin) != string(w.stdin) {
			t.Errorf("call[%d] = %+v, want %+v", i, calls[i], w)
		}
	}
	if ops := rec.snapshot(); len(ops) != 1 || ops[0] != "ctrlv" {
		t.Fatalf("CtrlV 调用 = %v, want 恰一次", ops)
	}
}

// 备份间隙选择消失同样按空剪贴板收尾，不能降级掉整次注入。
func TestInjectTreatedEmptyWhenBackupLosesSelection(t *testing.T) {
	fr := &fakeRunner{respond: func(i int, c call) ([]byte, error) {
		switch i {
		case 0:
			return []byte("text/plain\n"), nil
		case 1:
			return nil, errors.New("exit status 1: Nothing is copied")
		default:
			return nil, nil
		}
	}}
	in := newInjector(fr, &recorder{}, time.Millisecond)
	in.Env = func(string) string { return "wayland-0" }

	if err := in.Inject("注入文本"); err != nil {
		t.Fatalf("Inject = %v, want nil", err)
	}
	calls := fr.recorded()
	last := calls[len(calls)-1]
	if last.name != "wl-copy" || strings.Join(last.args, " ") != "--clear" {
		t.Errorf("收尾命令 = %+v, want wl-copy --clear", last)
	}
}

// 清空失败必须可见（不得静默把注入文本留在剪贴板）。
func TestInjectClearFail(t *testing.T) {
	fr := &fakeRunner{respond: func(i int, c call) ([]byte, error) {
		if i == 0 {
			return nil, errors.New("exit status 1: Nothing is copied")
		}
		if i == 2 { // 清空失败
			return nil, errors.New("display closed")
		}
		return nil, nil
	}}
	in := newInjector(fr, &recorder{}, time.Millisecond)
	in.Env = func(string) string { return "wayland-0" }

	err := in.Inject("x")
	if err == nil || !strings.Contains(err.Error(), "恢复剪贴板失败") {
		t.Fatalf("got %v, want 恢复剪贴板失败", err)
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

// ExecRunner 不得被孙进程持有的输出管道挂住：用 bytes.Buffer 时 os/exec 建管道 + 复制
// goroutine，Wait 要等 EOF，而 wl-copy fork 的常驻孙进程继承 stderr → 注入 worker 卡死。
func TestExecRunnerNotBlockedByOrphanChild(t *testing.T) {
	done := make(chan string, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := ExecRunner{}.Run(ctx, "sh", []string{"-c", "sleep 10 & echo ok"}, nil)
		if err != nil {
			done <- "Run 失败: " + err.Error()
			return
		}
		if got := strings.TrimSpace(string(out)); got != "ok" {
			done <- "stdout = " + got + ", want ok"
			return
		}
		done <- ""
	}()

	select {
	case msg := <-done:
		if msg != "" {
			t.Fatal(msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run 被孙进程持有的管道挂住（应随直接子进程退出立即返回）")
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

// darwin 命令序列：备份(pbpaste) → 写入(pbcopy) → CmdV → 延迟 → 恢复(pbcopy)；无环境门槛。
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

// 两端共同契约回归：备份不到内容就取消注入，绝不覆盖剪贴板。

// darwin 真·空粘贴板：照常注入，收尾写空。
func TestInjectDarwinProceedsWhenPasteboardEmpty(t *testing.T) {
	fr := &fakeRunner{respond: func(i int, c call) ([]byte, error) {
		return nil, nil // pbpaste 空 + osascript 空
	}}
	rec := &recorder{}
	in := newInjector(fr, rec, time.Millisecond)
	in.clip = pbClipboard
	in.Env = func(string) string { return "" }

	if err := in.Inject("空粘贴板注入"); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	want := []call{
		{"pbpaste", nil, nil},
		{"osascript", []string{"-e", "clipboard info"}, nil},
		{"pbcopy", nil, []byte("空粘贴板注入")},
		{"pbcopy", nil, nil}, // 收尾：原无内容 → 不把注入文本留在剪贴板
	}
	calls := fr.recorded()
	if len(calls) != len(want) {
		t.Fatalf("命令数 = %d, want %d:\n%+v", len(calls), len(want), calls)
	}
	for i, w := range want {
		if calls[i].name != w.name || strings.Join(calls[i].args, " ") != strings.Join(w.args, " ") ||
			string(calls[i].stdin) != string(w.stdin) {
			t.Errorf("call[%d] = %+v, want %+v", i, calls[i], w)
		}
	}
	if ops := rec.snapshot(); len(ops) != 1 {
		t.Fatalf("粘贴组合键调用 = %v, want 恰一次", ops)
	}
}

// darwin 空文本粘贴板（类型表有文本类型、内容为空串）：同"无内容"处理。
func TestInjectDarwinProceedsWhenPasteboardHoldsEmptyText(t *testing.T) {
	fr := &fakeRunner{respond: func(i int, c call) ([]byte, error) {
		if i == 1 {
			return []byte("«class utf8», 0, string, 0, Unicode text, 0\n"), nil
		}
		return nil, nil
	}}
	rec := &recorder{}
	in := newInjector(fr, rec, time.Millisecond)
	in.clip = pbClipboard
	in.Env = func(string) string { return "" }

	if err := in.Inject("空文本注入"); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	var writes int
	for _, c := range fr.recorded() {
		if c.name == "pbcopy" {
			writes++
		}
	}
	if writes != 2 { // 写入注入文本 + 收尾写空
		t.Errorf("pbcopy 次数 = %d, want 2", writes)
	}
	if ops := rec.snapshot(); len(ops) != 1 {
		t.Fatalf("粘贴组合键调用 = %v, want 恰一次", ops)
	}
}

// darwin 非文本粘贴板（只有图片）：备份不到 → 取消注入，且一次 pbcopy 都不能有。
func TestInjectDarwinKeepsNonTextPasteboard(t *testing.T) {
	fr := &fakeRunner{respond: func(i int, c call) ([]byte, error) {
		if c.name == "osascript" {
			return []byte("TIFF picture, 6386, «class PNGf», 4077, JPEG picture, 4934\n"), nil
		}
		return nil, nil // pbpaste 空
	}}
	rec := &recorder{}
	in := newInjector(fr, rec, time.Millisecond)
	in.clip = pbClipboard
	in.Env = func(string) string { return "" }

	err := in.Inject("这段字不该注入")
	if !errors.Is(err, errNonText) {
		t.Fatalf("got %v, want errNonText", err)
	}
	if !strings.Contains(err.Error(), "不是文本") || !strings.Contains(err.Error(), "剪贴板未改动") {
		t.Errorf("错误文案未说明剪贴板未改动: %v", err)
	}
	for _, c := range fr.recorded() {
		if c.name == "pbcopy" {
			t.Fatalf("非文本剪贴板不得被覆盖，却调用了 pbcopy: %+v", c)
		}
	}
	if ops := rec.snapshot(); len(ops) != 0 {
		t.Fatalf("取消注入时不得发粘贴组合键，实际 %v", ops)
	}
}

// darwin 类型表查询失败：保守按备份失败降级（同样不写剪贴板）。
func TestInjectDarwinDegradesWhenTypeQueryFails(t *testing.T) {
	fr := &fakeRunner{respond: func(i int, c call) ([]byte, error) {
		if c.name == "osascript" {
			return nil, errors.New("osascript: 执行失败")
		}
		return nil, nil
	}}
	in := newInjector(fr, &recorder{}, time.Millisecond)
	in.clip = pbClipboard
	in.Env = func(string) string { return "" }

	err := in.Inject("x")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v, want ErrUnavailable", err)
	}
	if len(fr.recorded()) != 2 {
		t.Errorf("类型表查询失败后不应写剪贴板，命令数 = %d", len(fr.recorded()))
	}
}

// linux 非文本选择：list-types 有 MIME 但读取失败 → 取消注入、不动剪贴板。
func TestInjectWlKeepsNonTextSelection(t *testing.T) {
	fr := &fakeRunner{respond: func(i int, c call) ([]byte, error) {
		if i == 0 {
			return []byte("image/png\n"), nil
		}
		return nil, errors.New("exit status 1: Clipboard content is not available as inferred output type")
	}}
	rec := &recorder{}
	in := newInjector(fr, rec, time.Millisecond)
	in.Env = func(string) string { return "wayland-0" }

	err := in.Inject("这段字不该注入")
	if !errors.Is(err, errNonText) {
		t.Fatalf("got %v, want errNonText", err)
	}
	if !strings.Contains(err.Error(), "image/png") {
		t.Errorf("错误文案应带上原类型: %v", err)
	}
	for _, c := range fr.recorded() {
		if c.name == "wl-copy" {
			t.Fatalf("非文本剪贴板不得被覆盖，却调用了 wl-copy: %+v", c)
		}
	}
	if ops := rec.snapshot(); len(ops) != 0 {
		t.Fatalf("取消注入时不得发粘贴组合键，实际 %v", ops)
	}
}

// linux 非文本选择读取成功时：注入照常，恢复必须按原 MIME 写回原字节。
func TestInjectWlRestoresNonTextSelectionBytes(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x01, 0x02}
	fr := &fakeRunner{respond: func(i int, c call) ([]byte, error) {
		switch i {
		case 0: // list-types
			return []byte("image/png\n"), nil
		case 1: // 备份：原样读到图片字节
			return png, nil
		default:
			return nil, nil
		}
	}}
	rec := &recorder{}
	in := newInjector(fr, rec, time.Millisecond)
	in.Env = func(string) string { return "wayland-0" }

	if err := in.Inject("图片剪贴板下也能注入"); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	calls := fr.recorded()
	if len(calls) != 4 {
		t.Fatalf("命令数 = %d, want 4:\n%+v", len(calls), calls)
	}
	// 写入注入文本后，恢复必须带原 MIME 且字节与原备份完全一致
	restore := calls[3]
	if restore.name != "wl-copy" || strings.Join(restore.args, " ") != "--type image/png" ||
		string(restore.stdin) != string(png) {
		t.Errorf("恢复调用 = %+v，want wl-copy --type image/png + 原图片字节", restore)
	}
	if ops := rec.snapshot(); len(ops) != 1 {
		t.Fatalf("粘贴组合键调用 = %v, want 恰一次", ops)
	}
}

// 文本类 MIME 判定：wl-paste 读失败时用它区分"能力边界"与"工具故障"。
func TestIsNonTextMIME(t *testing.T) {
	cases := map[string]bool{
		"":                             false,
		"text/plain":                   false,
		"text/plain;charset=utf-8":     false,
		"text/html":                    false,
		"UTF8_STRING":                  false,
		"STRING":                       false,
		"image/png":                    true,
		"image/tiff":                   true,
		"application/pdf":              true,
		"application/octet-stream":     true,
		"x-special/gnome-copied-files": true,
	}
	for mime, want := range cases {
		if got := isNonTextMIME(mime); got != want {
			t.Errorf("isNonTextMIME(%q) = %v, want %v", mime, got, want)
		}
	}
}

// macOS 类型表分类回归（判定宁宽勿严）。
func TestIsTextPasteboardType(t *testing.T) {
	textTypes := []string{
		"«class utf8»", "«class ut16»", "string", "unicode text",
		"public.utf8-plain-text", "public.utf16-external-plain-text",
		"nsstringpboardtype", "next rich text format v1.0 pasteboard type",
	}
	nonTextTypes := []string{
		"tiff picture", "«class pngf»", "«class 8bps»", "gif picture", "jpeg picture",
		"«class avif»", "«class jp2 »", "«class tpic»", "«class furl»", "public.file-url",
		"«class weba»", "next smart paste pasteboard type",
	}
	for _, s := range textTypes {
		if !isTextPasteboardType(s) {
			t.Errorf("isTextPasteboardType(%q) = false, want true", s)
		}
	}
	for _, s := range nonTextTypes {
		if isTextPasteboardType(s) {
			t.Errorf("isTextPasteboardType(%q) = true, want false", s)
		}
	}
}
