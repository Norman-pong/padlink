// Package textinject 实现语音文本注入（PRD §4.5）：剪贴板备份 → wl-copy 写入 →
// Ctrl+V → 延迟恢复。严格按 RESEARCH-WAYLAND.md §3 规避四坑：
// wl-paste 一律 --no-newline、注入文本显式 --type、恢复不用 -n、固定可配恢复延迟。
package textinject

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	// pasteType 注入文本的显式 MIME（规避 wl-copy 文本类型推断失灵）。
	pasteType = "text/plain;charset=utf-8"
	// execTimeout 单个 wl-* 子进程超时；超时按功能降级处理。
	execTimeout = 3 * time.Second
	// DefaultRestoreDelay Ctrl+V 后恢复原剪贴板的默认延迟（可配 300–500ms）。
	DefaultRestoreDelay = 300 * time.Millisecond
	// InstallHint wl-clipboard 缺失/超时的降级提示。
	InstallHint = "sudo apt install wl-clipboard"
)

// ErrUnavailable 哨兵：wl-clipboard 不可用、超时或 WAYLAND_DISPLAY 缺失。
// 结构化降级错误，文案含安装命令。
var ErrUnavailable = errors.New("wl-clipboard 不可用（安装：" + InstallHint + "）")

// Runner 抽象外部命令执行（测试注入 fake）。
type Runner interface {
	Run(ctx context.Context, name string, args []string, stdin []byte) (stdout []byte, err error)
}

// ExecRunner 以 exec 执行真实命令。
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errb.String()); msg != "" {
			return out.Bytes(), fmt.Errorf("%v: %s", err, msg)
		}
		return out.Bytes(), err
	}
	return out.Bytes(), nil
}

// Injector 执行一次文本注入。CtrlV 由注入器提供（经会话层串行化调用）；
// Env 可在测试中替换环境查询（nil 用 os.Getenv）。
type Injector struct {
	run          Runner
	CtrlV        func() error
	RestoreDelay time.Duration
	Env          func(key string) string
}

// New 构造文本注入器。restoreDelay ≤ 0 时取默认 300ms。
func New(run Runner, ctrlV func() error, restoreDelay time.Duration) *Injector {
	if restoreDelay <= 0 {
		restoreDelay = DefaultRestoreDelay
	}
	return &Injector{run: run, CtrlV: ctrlV, RestoreDelay: restoreDelay}
}

// Available 报告 WAYLAND_DISPLAY 是否就绪（缺失时注入必降级）。
func (in *Injector) Available() bool {
	return in.env()("WAYLAND_DISPLAY") != ""
}

func (in *Injector) Inject(text string) error {
	if !in.Available() {
		return fmt.Errorf("%w: WAYLAND_DISPLAY 未设置", ErrUnavailable)
	}
	if in.CtrlV == nil {
		return fmt.Errorf("textinject: 未配置 Ctrl+V 注入器")
	}

	// ① 记录原 MIME（超时/失败按降级处理）
	typesOut, err := in.wl("wl-paste", "--list-types")
	if err != nil {
		return fmt.Errorf("%w: 记录原 MIME 失败 (wl-paste --list-types): %v", ErrUnavailable, err)
	}
	mime := firstLine(typesOut)
	if mime == "" {
		mime = pasteType // 空剪贴板无类型可记，按文本类型恢复
	}

	// ② 备份原内容（仅驻内存；--no-newline 防尾部多出换行）
	backup, err := in.wl("wl-paste", "--no-newline")
	if err != nil {
		return fmt.Errorf("%w: 备份剪贴板失败 (wl-paste --no-newline): %v", ErrUnavailable, err)
	}

	// ③ 写入待注入文本
	if _, err := in.wlStdin("wl-copy", []byte(text), "--type", pasteType); err != nil {
		return fmt.Errorf("%w: 写入剪贴板失败 (wl-copy): %v", ErrUnavailable, err)
	}

	// ④ Ctrl+V（失败仍尝试恢复，剪贴板不能留在注入文本）
	pasteErr := in.CtrlV()

	// ⑤ 固定恢复延迟（恢复时机竞态为已知接受边界，PRD §4.5）
	time.Sleep(in.RestoreDelay)

	// ⑥ 恢复原内容（原样字节写回，不用 -n 防裁掉合法尾部换行）
	if _, err := in.wlStdin("wl-copy", backup, "--type", mime); err != nil {
		if pasteErr != nil {
			return fmt.Errorf("粘贴失败 (%v) 且恢复剪贴板失败: %v", pasteErr, err)
		}
		return fmt.Errorf("恢复剪贴板失败 (wl-copy --type %s): %v", mime, err)
	}
	return pasteErr
}

// wl / wlStdin 以 3s 超时执行单个 wl-* 子进程。
func (in *Injector) wl(name string, args ...string) ([]byte, error) {
	return in.wlStdin(name, nil, args...)
}

func (in *Injector) wlStdin(name string, stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	return in.run.Run(ctx, name, args, stdin)
}

func (in *Injector) env() func(string) string {
	if in.Env != nil {
		return in.Env
	}
	return os.Getenv
}

func firstLine(b []byte) string {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		b = b[:i]
	}
	return strings.TrimSpace(string(b))
}
