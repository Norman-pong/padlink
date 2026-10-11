// Package textinject 实现语音文本注入：剪贴板备份 → 写入 → 粘贴组合键 → 延迟恢复。
//
// 两端共同契约（行为一致，改动必须同时满足）：能备份到内容 → 注入后精确恢复；
// 备份不到内容（原剪贴板只有非文本、或读取失败）→ 取消本次注入并给出可见错误；
// 任何情况下都不得把注入文本留在剪贴板，也不得覆盖备份不到的原内容。
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
	// pasteType 注入文本的显式 MIME（wl-copy 的文本类型推断不可靠）。
	pasteType = "text/plain;charset=utf-8"
	// execTimeout 单个剪贴板子进程超时（超时按降级处理）。
	execTimeout = 3 * time.Second
	// DefaultRestoreDelay 粘贴组合键后恢复原剪贴板的默认延迟；改小必须确认目标应用已读完粘贴板。
	DefaultRestoreDelay = 300 * time.Millisecond
	// InstallHint wl-clipboard 不可用时的降级提示（Linux）。
	InstallHint = "sudo apt install wl-clipboard"
)

// ErrUnavailable 哨兵：剪贴板工具不可用、超时或会话环境缺失。
var ErrUnavailable = errors.New("剪贴板工具不可用")

// errNoSelection 内部哨兵：剪贴板当前没有任何选择内容。空剪贴板不是工具故障——注入照常进行，
// 只把收尾从"写回备份"改为"清空选择"；不区分它会让中文/语音注入在空剪贴板下全部静默失败。
var errNoSelection = errors.New("剪贴板无选择内容")

// noSelectionMarker 是 wl-clipboard 报告"无选择内容"的固定英文文案（该工具不做本地化）。
// 不能用错误类型判定：CommandContext 超时同样是 *exec.ExitError，必须继续走降级路径。
const noSelectionMarker = "Nothing is copied"

func isNoSelection(err error) bool {
	return err != nil && strings.Contains(err.Error(), noSelectionMarker)
}

// errNonText 内部哨兵：原剪贴板有内容但不是文本（CLI 备份不到），调用方据此取消本次注入。
// Linux 由 wl-paste 读取失败 + 已记录的 MIME 判定触发；macOS 由 pbpaste 取空 + 类型表无文本类型触发。
var errNonText = errors.New("剪贴板内容不是文本")

// isNonTextMIME 判定已记录的 MIME 是否为非文本（image/png、application/pdf…）：
// 文本类 MIME 读取失败属工具故障，非文本类读取失败属能力边界，两者降级语义不同。
func isNonTextMIME(mime string) bool {
	if mime == "" {
		return false
	}
	switch mime {
	case "STRING", "UTF8_STRING", "TEXT": // X11 遗留文本类型（GTK 应用仍可能提供）
		return false
	}
	return !strings.HasPrefix(mime, "text/")
}

// Runner 抽象外部命令执行（测试注入 fake）。
type Runner interface {
	Run(ctx context.Context, name string, args []string, stdin []byte) (stdout []byte, err error)
}

// ExecRunner 以 exec 执行真实命令。
type ExecRunner struct{}

// Run 执行命令并带回 stdout/stderr。
// 输出落临时文件而非 bytes.Buffer：os/exec 对非 *os.File 的 Stdout/Stderr 会建管道 + goroutine
// 复制，Wait 需等管道 EOF；而 wl-copy 写完剪贴板后 fork 的常驻孙进程持有继承的 stderr，EOF 永不
// 到来 → Wait 永久阻塞（context 只作用于直接子进程），注入 worker 随之卡死。文件无复制 goroutine，
// 直接子进程退出即返回；WaitDelay 兜底 stdin 管道。
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
	// read 读取备份（mime 为已记录的原始类型，macOS 恒空）；原内容非文本（CLI 备份不到）时返回 errNonText。
	read func(r Runner, mime string) ([]byte, error)
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
				return "", errNoSelection
			}
			return "", err
		}
		return firstLine(out), nil
	},
	// --no-newline 防尾部多出换行；非文本选择（图片/文件）通常能按字节读到并按记录的 MIME
	// 原样写回，只有读取确实失败时才是能力边界 → errNonText 取消注入，绝不用空备份覆盖原内容。
	read: func(r Runner, mime string) ([]byte, error) {
		out, err := runCmd(r, "wl-paste", nil, "--no-newline")
		if err != nil {
			if isNoSelection(err) {
				return nil, errNoSelection
			}
			if isNonTextMIME(mime) {
				return nil, fmt.Errorf("%w：原类型 %s", errNonText, mime)
			}
		}
		return out, err
	},
	// 恢复走原样字节写回：-n 会裁掉合法的尾部换行。
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

// pbClipboard：macOS pbcopy/pbpaste 后端（系统自带）。已知限制：CLI 只有纯文本面，
// 非文本剪贴板备份不了——备份不到即取消注入（见 read）。
var pbClipboard = clipboard{
	name: "pbcopy/pbpaste",
	hint: "macOS 系统自带，失败请查日志",
	readMIME: func(Runner) (string, error) {
		return "", nil // macOS 无 MIME 概念，恢复固定写回纯文本
	},
	read: func(r Runner, _ string) ([]byte, error) {
		out, err := runCmd(r, "pbpaste", nil)
		if err != nil || len(out) > 0 {
			return out, err
		}
		// pbpaste 取空有两种含义：粘贴板确实为空（含空文本），或里面只有非文本内容，两者都是
		// 退 0 + 空串，只能另查类型表区分；不区分就会在恢复阶段把用户的图片/文件覆盖成空文本。
		// 类型表查不出来（osascript 失败）按备份失败处理——同样不动剪贴板。
		hasContent, hasText, terr := pbTypes(r)
		if terr != nil {
			return nil, terr
		}
		if hasContent && !hasText {
			return nil, errNonText
		}
		return out, nil
	},
	write: func(r Runner, data []byte, _ string) error {
		_, err := runCmd(r, "pbcopy", data)
		return err
	},
	// pbcopy 空输入即清空粘贴板：macOS 无 wl-copy --clear 对应的 CLI，空文本与"真·无内容"
	// 对粘贴方等价（都粘不出东西），但类型表仍留有文本类型。
	clear: func(r Runner) error {
		_, err := runCmd(r, "pbcopy", nil)
		return err
	},
}

// pbTypes 查询 macOS 粘贴板类型表，返回"是否有任何类型"与"是否含文本类型"。
// 走系统自带的 osascript（clipboard info 为纯本地查询，不需要辅助功能/自动化授权），只在 pbpaste 取空时调用。
func pbTypes(r Runner) (hasContent bool, hasText bool, err error) {
	out, err := runCmd(r, "osascript", nil, "-e", "clipboard info")
	if err != nil {
		return false, false, err
	}
	// 形如「TIFF picture, 6386, «class utf8», 5」：类型名与字节数交替出现。
	parts := strings.Split(string(out), ",")
	for i := 0; i+1 < len(parts); i += 2 {
		name := strings.ToLower(strings.TrimSpace(parts[i]))
		if name == "" {
			continue
		}
		hasContent = true
		if isTextPasteboardType(name) {
			return hasContent, true, nil
		}
	}
	return hasContent, false, nil
}

// isTextPasteboardType 判定粘贴板类型是否为文本类。Apple 的类名/UTI 命名五花八门，判定宁宽勿严：
// 误判成文本只会退回"写回纯文本"的降级，误判成非文本会直接拒绝注入。
func isTextPasteboardType(name string) bool {
	for _, k := range []string{"text", "string", "utf8", "ut16", "txt"} {
		if strings.Contains(name, k) {
			return true
		}
	}
	return false
}

// platformClipboard 按运行平台选默认剪贴板后端；其余平台回落 wl-clipboard（不可用即降级）。
func platformClipboard() clipboard {
	if runtime.GOOS == "darwin" {
		return pbClipboard
	}
	return wlClipboard
}

// Injector 执行一次文本注入；CtrlV 经会话层串行化调用，Env 可在测试中替换环境查询（nil 用 os.Getenv）。
type Injector struct {
	run          Runner
	clip         clipboard
	CtrlV        func() error
	RestoreDelay time.Duration
	Env          func(key string) string
}

// New 构造文本注入器（剪贴板后端按平台默认）；restoreDelay ≤ 0 时取默认值。
func New(run Runner, ctrlV func() error, restoreDelay time.Duration) *Injector {
	if restoreDelay <= 0 {
		restoreDelay = DefaultRestoreDelay
	}
	return &Injector{run: run, clip: platformClipboard(), CtrlV: ctrlV, RestoreDelay: restoreDelay}
}

// Available 报告剪贴板环境是否就绪（Linux 要求 WAYLAND_DISPLAY；macOS 无门槛）。
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

	mime, err := in.clip.readMIME(in.run)
	noSelection := errors.Is(err, errNoSelection)
	if err != nil && !noSelection {
		return in.unavailable("记录原 MIME 失败", err)
	}

	// 备份只驻内存；原本无内容则无需备份。
	var backup []byte
	if !noSelection {
		backup, err = in.clip.read(in.run, mime)
		switch {
		case errors.Is(err, errNoSelection):
			noSelection = true // 备份间隙选择消失：等同空剪贴板
		case errors.Is(err, errNonText):
			// 备份不到内容（非文本且 CLI 读不出，或类型表查询失败）→ 取消本次注入；
			// 绝不"用空备份覆盖回去"，那会把用户的图片静默抹掉（两端共同契约）。
			return fmt.Errorf("%w（本次注入已取消，剪贴板未改动）", err)
		case err != nil:
			return in.unavailable("备份剪贴板失败", err)
		}
	}

	if err := in.clip.write(in.run, []byte(text), pasteType); err != nil {
		return in.unavailable("写入剪贴板失败", err)
	}

	// 粘贴失败也必须继续恢复：注入文本不得留在剪贴板。
	pasteErr := in.CtrlV()

	time.Sleep(in.RestoreDelay)

	// 原本无内容则清空选择，注入文本不得留在剪贴板。
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

func (in *Injector) unavailable(stage string, err error) error {
	return fmt.Errorf("%w: %s（工具 %s，处置：%s）: %v", ErrUnavailable, stage, in.clip.name, in.clip.hint, err)
}

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
