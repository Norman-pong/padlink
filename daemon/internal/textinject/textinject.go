// Package textinject 实现语音文本注入（PRD §4.5）：剪贴板备份 → 写入 → 粘贴组合键 → 延迟恢复。
// 平台剪贴板差异收敛在 clipboard 操作集：Linux 走 wl-clipboard（严格按 RESEARCH-WAYLAND.md §3
// 规避四坑：wl-paste 一律 --no-newline、注入文本显式 --type、恢复不用 -n、固定可配恢复延迟）；
// macOS 走 pbcopy/pbpaste（系统自带零依赖，见 docs/PLAN-MACOS.md §2.5）。
package textinject

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	// pasteType 注入文本的显式 MIME（规避 wl-copy 文本类型推断失灵）。
	pasteType = "text/plain;charset=utf-8"
	// execTimeout 单个剪贴板子进程超时；超时按功能降级处理。
	execTimeout = 3 * time.Second
	// DefaultRestoreDelay 粘贴组合键后恢复原剪贴板的默认延迟（可配 300–500ms）。
	DefaultRestoreDelay = 300 * time.Millisecond
	// InstallHint wl-clipboard 缺失/超时的降级提示（Linux）。
	InstallHint = "sudo apt install wl-clipboard"
)

// ErrUnavailable 哨兵：剪贴板工具不可用、超时或会话环境缺失。结构化降级错误。
var ErrUnavailable = errors.New("剪贴板工具不可用")

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

// clipboard 承载平台剪贴板差异：环境门槛、MIME 记录、读、写。
type clipboard struct {
	name    string // 工具名（错误文案用）
	hint    string // 不可用时的处置指引
	needEnv string // 非空时要求该环境变量存在才可用（Linux=WAYLAND_DISPLAY；macOS 无门槛）
	// readMIME 记录原剪贴板 MIME；平台无类型概念时返回 ""。
	readMIME func(r Runner) (string, error)
	read     func(r Runner) ([]byte, error)
	// write 写入内容；mime 为空时由实现决定默认类型（wl-clipboard 按 pasteType 恢复）。
	write func(r Runner, data []byte, mime string) error
}

// wlClipboard：Linux wl-clipboard 后端。
var wlClipboard = clipboard{
	name:    "wl-clipboard",
	hint:    InstallHint,
	needEnv: "WAYLAND_DISPLAY",
	readMIME: func(r Runner) (string, error) {
		out, err := runCmd(r, "wl-paste", nil, "--list-types")
		if err != nil {
			return "", err
		}
		return firstLine(out), nil
	},
	// --no-newline 防尾部多出换行
	read: func(r Runner) ([]byte, error) { return runCmd(r, "wl-paste", nil, "--no-newline") },
	// 恢复走原样字节写回（不用 -n 防裁掉合法尾部换行）
	write: func(r Runner, data []byte, mime string) error {
		if mime == "" {
			mime = pasteType // 空剪贴板无类型可记，按文本类型恢复
		}
		_, err := runCmd(r, "wl-copy", data, "--type", mime)
		return err
	},
}

// pbClipboard：macOS pbcopy/pbpaste 后端（系统自带）。
// 已知限制：CLI 只有纯文本面，非文本剪贴板（图片/文件）备份不了，
// 恢复即写回文本——v1 接受（docs/PLAN-MACOS.md §2.5）。
var pbClipboard = clipboard{
	name: "pbcopy/pbpaste",
	hint: "macOS 系统自带，失败请查日志",
	readMIME: func(Runner) (string, error) {
		return "", nil
	},
	read: func(r Runner) ([]byte, error) { return runCmd(r, "pbpaste", nil) },
	write: func(r Runner, data []byte, _ string) error {
		_, err := runCmd(r, "pbcopy", data)
		return err
	},
}

// platformClipboard 按运行平台选默认剪贴板后端；其余平台回落 wl-clipboard
// （不可用即结构化降级，行为同 Linux 缺工具）。
func platformClipboard() clipboard {
	if runtime.GOOS == "darwin" {
		return pbClipboard
	}
	return wlClipboard
}

// Injector 执行一次文本注入。CtrlV 由注入器提供（经会话层串行化调用）；
// Env 可在测试中替换环境查询（nil 用 os.Getenv）。
type Injector struct {
	run          Runner
	clip         clipboard
	CtrlV        func() error
	RestoreDelay time.Duration
	Env          func(key string) string
}

// New 构造文本注入器（剪贴板后端按平台默认）。restoreDelay ≤ 0 时取默认 300ms。
func New(run Runner, ctrlV func() error, restoreDelay time.Duration) *Injector {
	if restoreDelay <= 0 {
		restoreDelay = DefaultRestoreDelay
	}
	return &Injector{run: run, clip: platformClipboard(), CtrlV: ctrlV, RestoreDelay: restoreDelay}
}

// Available 报告剪贴板环境是否就绪（Linux 要求 WAYLAND_DISPLAY；macOS 无环境门槛）。
func (in *Injector) Available() bool {
	if in.clip.needEnv == "" {
		return true
	}
	return in.env()(in.clip.needEnv) != ""
}

func (in *Injector) Inject(text string) error {
	if !in.Available() {
		return fmt.Errorf("%w: %s 未设置（工具 %s）", ErrUnavailable, in.clip.needEnv, in.clip.name)
	}
	if in.CtrlV == nil {
		return fmt.Errorf("textinject: 未配置粘贴组合键注入器")
	}

	// ① 记录原 MIME（超时/失败按降级处理；macOS 无类型概念恒空）
	mime, err := in.clip.readMIME(in.run)
	if err != nil {
		return in.unavailable("记录原 MIME 失败", err)
	}

	// ② 备份原内容（仅驻内存）
	backup, err := in.clip.read(in.run)
	if err != nil {
		return in.unavailable("备份剪贴板失败", err)
	}

	// ③ 写入待注入文本
	if err := in.clip.write(in.run, []byte(text), pasteType); err != nil {
		return in.unavailable("写入剪贴板失败", err)
	}

	// ④ 粘贴组合键（失败仍尝试恢复，剪贴板不能留在注入文本）
	pasteErr := in.CtrlV()

	// ⑤ 固定恢复延迟（恢复时机竞态为已知接受边界，PRD §4.5）
	time.Sleep(in.RestoreDelay)

	// ⑥ 恢复原内容
	if err := in.clip.write(in.run, backup, mime); err != nil {
		if pasteErr != nil {
			return fmt.Errorf("粘贴失败 (%v) 且恢复剪贴板失败: %v", pasteErr, err)
		}
		return fmt.Errorf("恢复剪贴板失败（%s）: %v", in.clip.name, err)
	}
	return pasteErr
}

// unavailable 组装结构化降级错误：哨兵 + 阶段 + 工具名 + 处置指引。
func (in *Injector) unavailable(stage string, err error) error {
	return fmt.Errorf("%w: %s（工具 %s，处置：%s）: %v", ErrUnavailable, stage, in.clip.name, in.clip.hint, err)
}

// runCmd 以 3s 超时执行单个剪贴板子进程。
func runCmd(r Runner, name string, stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	return r.Run(ctx, name, args, stdin)
}

func (in *Injector) env() func(string) string {
	if in.Env != nil {
		return in.Env
	}
	return os.Getenv
}

func firstLine(b []byte) string {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}
