// Package textinject 实现语音文本注入（PRD §4.5）：剪贴板备份 → 写入 → 粘贴组合键 → 延迟恢复。
// 平台剪贴板差异收敛在 clipboard 操作集：Linux 走 wl-clipboard（严格按 RESEARCH-WAYLAND.md §3
// 规避已记录的坑：wl-paste 一律 --no-newline、注入文本显式 --type、恢复不用 -n、固定可配恢复延迟、
// 空剪贴板按无内容处理而非降级、子进程输出落文件防 Wait 挂死）；
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

// errNoSelection 内部哨兵：剪贴板当前没有任何选择内容（空剪贴板）。
// 空剪贴板不是工具故障——注入照常进行，只把收尾从"写回备份"改为"清空选择"。
// 不区分它会让中文/语音注入在空剪贴板下全部静默失败（wl-paste --list-types 退出码 1）。
var errNoSelection = errors.New("剪贴板无选择内容")

// noSelectionMarker 是 wl-clipboard 报告"无选择内容"的固定英文文案（该工具不做本地化）。
// 不能用错误类型判定：CommandContext 超时同样是 *exec.ExitError（"signal: killed"），
// 而超时属于真实故障，必须继续走降级路径。
const noSelectionMarker = "Nothing is copied"

// isNoSelection 判定错误是否为"无选择内容"（而非工具故障/超时）。
func isNoSelection(err error) bool {
	return err != nil && strings.Contains(err.Error(), noSelectionMarker)
}

// Runner 抽象外部命令执行（测试注入 fake）。
type Runner interface {
	Run(ctx context.Context, name string, args []string, stdin []byte) (stdout []byte, err error)
}

// ExecRunner 以 exec 执行真实命令。
type ExecRunner struct{}

// Run 执行命令并带回 stdout/stderr。
// 输出落临时文件而非 bytes.Buffer：os/exec 对非 *os.File 的 Stdout/Stderr 会建管道并用
// goroutine 复制，Wait 需等管道 EOF；而 wl-copy 写完剪贴板后 fork 出的常驻子进程持有继承的
// stderr（实测 PPid=1、fd 2 → pipe），EOF 永不到来 → Wait 永久阻塞（3s context 只作用于
// 直接子进程，对已退出的父进程与孙进程均无效），注入 worker 随之卡死：整机输入失效、进程
// 无法优雅退出。文件无复制 goroutine，直接子进程退出即返回；WaitDelay 兜底 stdin 管道。
func (ExecRunner) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error) {
	outF, outDone, err := tempCapture()
	if err != nil {
		return nil, fmt.Errorf("创建临时输出文件失败: %w", err)
	}
	errF, errDone, err := tempCapture()
	if err != nil {
		outDone()
		return nil, fmt.Errorf("创建临时错误文件失败: %w", err)
	}

	cmd := exec.CommandContext(ctx, name, args...)
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	cmd.Stdout, cmd.Stderr = outF, errF
	cmd.WaitDelay = execTimeout

	runErr := cmd.Run()
	out, errOut := outDone(), errDone()
	if runErr != nil {
		if msg := strings.TrimSpace(string(errOut)); msg != "" {
			return out, fmt.Errorf("%v: %s", runErr, msg)
		}
		return out, runErr
	}
	return out, nil
}

// tempCapture 建临时文件承载子进程输出，返回文件与读回收尾函数（读后删除；
// 孙进程仍持有时按已删除句柄处理，不影响内容）。
func tempCapture() (*os.File, func() []byte, error) {
	f, err := os.CreateTemp("", "padlink-exec-*")
	if err != nil {
		return nil, nil, err
	}
	done := func() []byte {
		f.Close()
		b, _ := os.ReadFile(f.Name())
		os.Remove(f.Name())
		return b
	}
	return f, done, nil
}

// clipboard 承载平台剪贴板差异：环境门槛、MIME 记录、读、写、清空。
type clipboard struct {
	name    string // 工具名（错误文案用）
	hint    string // 不可用时的处置指引
	needEnv string // 非空时要求该环境变量存在才可用（Linux=WAYLAND_DISPLAY；macOS 无门槛）
	// readMIME 记录原剪贴板 MIME；平台无类型概念时返回 ""；无选择内容时返回 errNoSelection。
	readMIME func(r Runner) (string, error)
	read     func(r Runner) ([]byte, error)
	// write 写入内容；mime 为空时由实现决定默认类型（wl-clipboard 按 pasteType 恢复）。
	write func(r Runner, data []byte, mime string) error
	// clear 清空选择：原剪贴板本无内容时用它收尾（不能把注入文本留在剪贴板）。
	clear func(r Runner) error
}

// wlClipboard：Linux wl-clipboard 后端。
var wlClipboard = clipboard{
	name:    "wl-clipboard",
	hint:    InstallHint,
	needEnv: "WAYLAND_DISPLAY",
	readMIME: func(r Runner) (string, error) {
		out, err := runCmd(r, "wl-paste", nil, "--list-types")
		if err != nil {
			if isNoSelection(err) {
				return "", errNoSelection // 空剪贴板：无 MIME 可记，按无内容处理
			}
			return "", err
		}
		return firstLine(out), nil
	},
	// --no-newline 防尾部多出换行；备份恰逢选择消失同样按无内容处理
	read: func(r Runner) ([]byte, error) {
		out, err := runCmd(r, "wl-paste", nil, "--no-newline")
		if err != nil && isNoSelection(err) {
			return nil, errNoSelection
		}
		return out, err
	},
	// 恢复走原样字节写回（不用 -n 防裁掉合法尾部换行）
	write: func(r Runner, data []byte, mime string) error {
		if mime == "" {
			mime = pasteType // 空剪贴板无类型可记，按文本类型恢复
		}
		_, err := runCmd(r, "wl-copy", data, "--type", mime)
		return err
	},
	clear: func(r Runner) error {
		_, err := runCmd(r, "wl-copy", nil, "--clear")
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
	// pbcopy 空输入即清空粘贴板（macOS 无 --clear 开关）
	clear: func(r Runner) error {
		_, err := runCmd(r, "pbcopy", nil)
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

	// ① 记录原 MIME（空剪贴板走无内容分支；超时/工具故障按降级处理；macOS 无类型概念恒空）
	mime, err := in.clip.readMIME(in.run)
	noSelection := errors.Is(err, errNoSelection)
	if err != nil && !noSelection {
		return in.unavailable("记录原 MIME 失败", err)
	}

	// ② 备份原内容（仅驻内存）；原本无内容则无需备份
	var backup []byte
	if !noSelection {
		backup, err = in.clip.read(in.run)
		if errors.Is(err, errNoSelection) {
			noSelection = true // 备份间隙选择消失：等同空剪贴板
		} else if err != nil {
			return in.unavailable("备份剪贴板失败", err)
		}
	}

	// ③ 写入待注入文本
	if err := in.clip.write(in.run, []byte(text), pasteType); err != nil {
		return in.unavailable("写入剪贴板失败", err)
	}

	// ④ 粘贴组合键（失败仍尝试恢复，剪贴板不能留在注入文本）
	pasteErr := in.CtrlV()

	// ⑤ 固定恢复延迟（恢复时机竞态为已知接受边界，PRD §4.5）
	time.Sleep(in.RestoreDelay)

	// ⑥ 恢复原内容：原本无内容则清空选择（注入文本不得留在剪贴板）
	var restoreErr error
	if noSelection {
		restoreErr = in.clip.clear(in.run)
	} else {
		restoreErr = in.clip.write(in.run, backup, mime)
	}
	if restoreErr != nil {
		if pasteErr != nil {
			return fmt.Errorf("粘贴失败 (%v) 且恢复剪贴板失败: %v", pasteErr, restoreErr)
		}
		return fmt.Errorf("恢复剪贴板失败（%s）: %v", in.clip.name, restoreErr)
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
