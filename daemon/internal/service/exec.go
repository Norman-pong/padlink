package service

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"runtime"
	"time"

	"padlink/daemon/internal/control"
)

// Result 是一次 Plan 执行的结果（卸载末尾分类清单的来源）。
type Result struct {
	DryRun   bool     // 是否 dry-run（未产生任何副作用）
	Commands []string // 已执行（dry-run 下为将执行）的命令行
	Removed  []string // 已删除（dry-run 下为将删除）的路径
	Skipped  []string // 执行时已不存在而跳过的路径
	Kept     []string // 刻意保留的说明
	Manual   []string // 需用户手动处理的命令或说明
	Warnings []string // 失败但未中断的步骤
}

// HostProbe 读取真实环境（$HOME、$XDG_CONFIG_HOME、uid）与文件系统，返回本机安装形态。
func HostProbe() Probe {
	home, _ := os.UserHomeDir()
	return BuildInstallProbe(runtime.GOOS, home, os.Getenv("XDG_CONFIG_HOME"), os.Getuid(), pathExists)
}

// pathExists 报告路径是否存在；悬空符号链接也算存在（需清理的残留）。
func pathExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Lstat(path)
	return err == nil
}

// Run 按顺序执行 plan 的步骤。dryRun 为 true 时只打印将执行的命令与将删除的路径，
// 不产生任何副作用。删除前再次校验路径守卫；步骤失败会如实打印并继续执行剩余步骤，
// 最终用 errors.Join 汇总返回。
func Run(plan Plan, dryRun bool, out io.Writer) (Result, error) {
	res := Result{DryRun: dryRun}
	var errs []error
	for _, st := range plan.Steps {
		switch st.Kind {
		case StepRunCommand:
			line := st.CommandLine()
			res.Commands = append(res.Commands, line)
			if dryRun {
				fmt.Fprintf(out, "[dry-run] 将执行: %s\n", line)
				continue
			}
			cmd := exec.Command(st.Cmd, st.Args...)
			cmd.Stdout, cmd.Stderr = out, out
			if err := cmd.Run(); err != nil {
				msg := fmt.Sprintf("%s 执行失败: %v", line, err)
				if st.Optional {
					fmt.Fprintf(out, "警告: %s（继续卸载）\n", msg)
					res.Warnings = append(res.Warnings, msg)
					continue
				}
				fmt.Fprintf(out, "错误: %s\n", msg)
				errs = append(errs, errors.New(msg))
				continue
			}
			fmt.Fprintf(out, "已执行: %s\n", line)
		case StepRemoveFile, StepRemoveDir:
			if err := runRemove(st, dryRun, out, &res); err != nil {
				errs = append(errs, err)
			}
		case StepNotice:
			switch st.Notice {
			case NoticeManual:
				fmt.Fprintf(out, "需手动处理: %s\n", st.Text)
				res.Manual = append(res.Manual, st.Text)
			case NoticeKeep:
				fmt.Fprintf(out, "保留: %s\n", st.Text)
				res.Kept = append(res.Kept, st.Text)
			default:
				fmt.Fprintf(out, "提示: %s\n", st.Text)
			}
		}
	}
	return res, errors.Join(errs...)
}

// runRemove 删除单个文件或目录：先校验路径守卫，再用 os.Remove 删除
// （不用 RemoveAll；目录非空等失败如实报错并给出可复制的手动命令）。
func runRemove(st Step, dryRun bool, out io.Writer, res *Result) error {
	if err := CheckRemovable(st.Path, st.Guard); err != nil {
		fmt.Fprintf(out, "拒绝删除: %v\n", err)
		return err
	}
	what := "文件"
	if st.Kind == StepRemoveDir {
		what = "目录"
	}
	if dryRun {
		fmt.Fprintf(out, "[dry-run] 将删除%s: %s\n", what, st.Path)
		res.Removed = append(res.Removed, st.Path)
		return nil
	}
	if err := os.Remove(st.Path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(out, "跳过（已不存在）: %s\n", st.Path)
			res.Skipped = append(res.Skipped, st.Path)
			return nil
		}
		manual := "rm -f " + st.Path
		if st.Kind == StepRemoveDir {
			manual = "rm -rf " + st.Path
		}
		msg := fmt.Sprintf("删除%s %s 失败: %v（请手动执行: %s）", what, st.Path, err, manual)
		fmt.Fprintf(out, "错误: %s\n", msg)
		return errors.New(msg)
	}
	fmt.Fprintf(out, "已删除%s: %s\n", what, st.Path)
	res.Removed = append(res.Removed, st.Path)
	return nil
}

// Control 执行 start/stop/restart：探测安装形态、构造并执行计划。
// 不经控制 socket，padlinkd 未运行时也可用。
func Control(action ControlAction, out io.Writer) error {
	p := HostProbe()
	plan, err := BuildControl(p, action)
	if err != nil {
		return err
	}
	res, err := Run(plan, false, out)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "padlinkd 已%s（%s）\n", action.label(), joinCommands(res.Commands))
	if p.GOOS == "darwin" && action == ActionStop {
		fmt.Fprintln(out, "提示: 已卸载 LaunchAgent（padlinkctl start 可立即恢复；下次登录 launchd 也会按 plist 自动加载）")
	}
	return nil
}

// UninstallOptions 是 uninstall 命令的选项。
type UninstallOptions struct {
	Purge  bool // 连配置与 token 一起删除（需二次确认）
	Yes    bool // 跳过 --purge 的二次确认
	DryRun bool // 只打印将执行的命令与将删除的路径
}

// Uninstall 执行卸载：探测安装形态 →（--purge）二次确认 → 构造并执行计划 → 打印末尾清单。
// 返回错误表示有步骤未完成（其余步骤已尽力执行）。
func Uninstall(opts UninstallOptions, in io.Reader, out io.Writer) error {
	p := HostProbe()
	purge := opts.Purge
	if purge && !opts.DryRun {
		ok, err := ConfirmPurge(in, out, opts.Yes)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(out, "--purge 未确认：配置与 token 保留（确认删除可重新执行，或加 --yes 跳过确认）")
			purge = false
		}
	} else if purge && !opts.Yes {
		fmt.Fprintln(out, "[dry-run] --purge 已启用：实际执行前会要求二次确认（--yes 可跳过）")
	}
	plan, err := BuildUninstall(p, purge)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "安装形态: %s\n", p.Kind)
	res, err := Run(plan, opts.DryRun, out)
	printSummary(out, res)
	if DaemonRunning() {
		fmt.Fprintln(out, "注意: padlinkd 仍在运行（控制 socket 可连或存在同名进程）；若已移除服务定义，请确认进程确实退出（pgrep padlinkd）")
	}
	return err
}

// printSummary 打印「已移除 / 保留 / 需手动处理」三类清单。
func printSummary(out io.Writer, res Result) {
	title := "已移除"
	if res.DryRun {
		title = "将移除"
	}
	fmt.Fprintf(out, "\n%s:\n", title)
	printList(out, res.Removed)
	fmt.Fprintln(out, "保留:")
	printList(out, res.Kept)
	fmt.Fprintln(out, "需手动处理:")
	printList(out, res.Manual)
	if len(res.Skipped) > 0 {
		fmt.Fprintln(out, "已跳过（执行时已不存在）:")
		printList(out, res.Skipped)
	}
	if len(res.Warnings) > 0 {
		fmt.Fprintln(out, "警告:")
		printList(out, res.Warnings)
	}
}

// printList 逐行输出清单项；空清单打印「（无）」。
func printList(out io.Writer, items []string) {
	if len(items) == 0 {
		fmt.Fprintln(out, "  （无）")
		return
	}
	for _, item := range items {
		fmt.Fprintf(out, "  %s\n", item)
	}
}

// joinCommands 拼接命令行文本（计划正常时只有一条）。
func joinCommands(cmds []string) string {
	switch len(cmds) {
	case 0:
		return ""
	case 1:
		return cmds[0]
	default:
		return fmt.Sprintf("%s 等 %d 条命令", cmds[0], len(cmds))
	}
}

// DaemonRunning 判断 padlinkd 是否仍在运行：优先连接控制 socket，
// 失败再退回同名进程检查（socket 目录不可用时也能发现残留进程）。
func DaemonRunning() bool {
	if path, err := control.SocketPath(); err == nil {
		if conn, err := net.DialTimeout("unix", path, 300*time.Millisecond); err == nil {
			conn.Close()
			return true
		}
	}
	return exec.Command("pgrep", "-x", daemonBinName).Run() == nil
}
