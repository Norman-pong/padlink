package service

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

// StepKind 是 Plan 中单步动作的种类。
type StepKind int

const (
	// StepRunCommand 执行外部命令（systemctl / launchctl）。
	StepRunCommand StepKind = iota
	// StepRemoveFile 删除单个文件（os.Remove）。
	StepRemoveFile
	// StepRemoveDir 删除单个目录（os.Remove；目录非空即失败并给出手动命令，不用 RemoveAll）。
	StepRemoveDir
	// StepNotice 只输出提示，不产生副作用。
	StepNotice
)

// NoticeKind 是 StepNotice 的归类，决定它进入卸载末尾清单的哪一类。
type NoticeKind int

const (
	// NoticeInfo 仅说明（不进末尾清单）。
	NoticeInfo NoticeKind = iota
	// NoticeManual 需用户手动处理（如 sudo rm udev 规则、apt/dnf remove）。
	NoticeManual
	// NoticeKeep 刻意保留（如未加 --purge 时的配置目录）。
	NoticeKeep
)

// Step 是 Plan 中有序的一步。
type Step struct {
	Kind StepKind // 步骤种类

	Cmd      string   // StepRunCommand：可执行文件
	Args     []string // StepRunCommand：参数
	Optional bool     // StepRunCommand：失败只记警告、不中断（卸载时的尽力停服等）

	Path  string // StepRemoveFile / StepRemoveDir：删除目标（绝对路径）
	Guard string // 删除目标必须位于其下的父目录前缀（构建时与执行时都校验）

	Text   string     // StepNotice：提示文本
	Notice NoticeKind // StepNotice：归类
}

// CommandLine 返回可复制执行的命令行文本；非命令步骤返回空串。
func (s Step) CommandLine() string {
	if s.Kind != StepRunCommand {
		return ""
	}
	return strings.Join(append([]string{s.Cmd}, s.Args...), " ")
}

// Plan 是一次操作的有序步骤集合。
type Plan struct {
	Action string      // start / stop / restart / uninstall
	Kind   InstallKind // 探测到的安装形态（输出用）
	Purge  bool        // uninstall 是否包含配置/token 删除
	Steps  []Step
}

// CheckRemovable 校验删除目标：path 必须是与 prefix 同为绝对路径、已规范化（Clean 后不变）、
// 位于 prefix 之下（或等于 prefix），且路径中含 "padlink" 关键字。
// 目的是即使路径拼接出错也不会删到无关目录。
func CheckRemovable(path, prefix string) error {
	if path == "" || prefix == "" {
		return errors.New("删除目标或允许前缀为空，拒绝删除")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("删除目标不是绝对路径: %q", path)
	}
	if !filepath.IsAbs(prefix) {
		return fmt.Errorf("删除前缀不是绝对路径: %q（目标 %q）", prefix, path)
	}
	if filepath.Clean(path) != path {
		return fmt.Errorf("删除目标未规范化（含 . / .. / 重复分隔符）: %q", path)
	}
	if !strings.Contains(path, "padlink") {
		return fmt.Errorf("删除目标不含 padlink 关键字: %q", path)
	}
	if prefix == string(filepath.Separator) {
		return fmt.Errorf("拒绝以根目录作为删除前缀: %q", path)
	}
	if path != prefix && !strings.HasPrefix(path, prefix+string(filepath.Separator)) {
		return fmt.Errorf("删除目标 %q 不在允许前缀 %q 之下", path, prefix)
	}
	return nil
}

// BuildControl 构造 start/stop/restart 计划：Linux 走 systemctl --user，
// macOS 走 launchctl（stop = bootout 卸载 job，start/restart = bootstrap + kickstart）。
// 服务定义不存在、uid 不可用或平台不支持时返回错误（错误消息含安装提示）。
func BuildControl(p Probe, action ControlAction) (Plan, error) {
	switch action {
	case ActionStart, ActionStop, ActionRestart:
	default:
		return Plan{}, fmt.Errorf("未知的启停动作 %q（支持 start/stop/restart）", action)
	}
	plan := Plan{Action: string(action), Kind: p.Kind}
	switch p.GOOS {
	case "linux":
		if p.unitPath() == "" {
			return Plan{}, fmt.Errorf("未找到 padlink 的 systemd user 单元（%s 或 %s）\n"+
				"  tar.gz 手工安装：请先在解压目录执行 ./install.sh\n"+
				"  deb/rpm 包安装：请先执行 sudo apt install padlink（或 sudo dnf install padlink）",
				filepath.Join(p.Home, ".config", "systemd", "user", UnitName+".service"), systemUnitPath)
		}
		plan.Steps = append(plan.Steps, Step{
			Kind: StepRunCommand,
			Cmd:  "systemctl",
			Args: []string{"--user", string(action), UnitName},
		})
	case "darwin":
		if p.Plist == "" {
			return Plan{}, fmt.Errorf("未找到 LaunchAgent（%s）\n"+
				"  请先在解压目录执行 ./install.sh 安装", filepath.Join(p.Home, "Library", "LaunchAgents", AgentLabel+".plist"))
		}
		target, err := p.agentTarget()
		if err != nil {
			return Plan{}, err
		}
		if action == ActionStop {
			// plist 为 KeepAlive=true：kill 会被 launchd 立刻拉起，必须 bootout 卸载 job 才算停住。
			// plist 文件保留——下次登录 launchd 会重新加载，padlinkctl start 也可立即恢复。
			// 未注册时 bootout 报「Could not find service」，对 stop 而言等价于已停止：记警告不中断。
			plan.Steps = append(plan.Steps, Step{
				Kind:     StepRunCommand,
				Cmd:      "launchctl",
				Args:     []string{"bootout", target},
				Optional: true,
			})
			return plan, nil
		}
		// bootstrap 保证 job 已注册（已注册时该步失败无害，故 optional），kickstart 拉起；
		// restart 用 -k 先杀后拉，start 不带 -k（已在运行则原样保持）。
		args := []string{"kickstart", target}
		if action == ActionRestart {
			args = []string{"kickstart", "-k", target}
		}
		plan.Steps = append(plan.Steps,
			Step{Kind: StepRunCommand, Cmd: "launchctl",
				Args: []string{"bootstrap", "gui/" + strconv.Itoa(p.UID), p.Plist}, Optional: true},
			Step{Kind: StepRunCommand, Cmd: "launchctl", Args: args},
		)
	default:
		return Plan{}, fmt.Errorf("不支持的平台 %q（仅支持 linux / darwin）", p.GOOS)
	}
	return plan, nil
}

// BuildUninstall 构造 uninstall 计划，按探测到的安装形态编排：
// 停服务 → 移除服务定义 → 删除用户级二进制 →（purge）删除配置与 token
// → 输出需手动处理的项（udev 规则 / 包管理器卸载）与刻意保留的项。
// deb/rpm 形态不产生任何删除 /usr 下文件的步骤。purge 由调用方在二次确认后决定。
func BuildUninstall(p Probe, purge bool) (Plan, error) {
	plan := Plan{Action: "uninstall", Kind: p.Kind, Purge: purge}
	add := func(steps ...Step) { plan.Steps = append(plan.Steps, steps...) }
	// remove 先过路径守卫再排入计划：守卫失败即整体报错（宁可不删，也不删错）。
	remove := func(kind StepKind, path, guard string) error {
		if err := CheckRemovable(path, guard); err != nil {
			return err
		}
		plan.Steps = append(plan.Steps, Step{Kind: kind, Path: path, Guard: guard})
		return nil
	}
	optionalRun := func(cmd string, args ...string) Step {
		return Step{Kind: StepRunCommand, Cmd: cmd, Args: args, Optional: true}
	}
	// tail 收集删除动作之后的提示（需手动处理 / 保留 / 说明），让清单集中在末尾输出
	var tail []Step

	switch p.GOOS {
	case "linux":
		if p.unitPath() != "" {
			// 尽力停服：未运行/无 user manager 时失败也不该中断卸载
			add(optionalRun("systemctl", "--user", "stop", UnitName))
		}
		if p.UserUnit != "" {
			// 先去掉 enable 产生的链接，再删单元文件，最后 daemon-reload 让 systemd 忘记它
			add(optionalRun("systemctl", "--user", "disable", UnitName))
			if err := remove(StepRemoveFile, p.UserUnit, filepath.Dir(p.UserUnit)); err != nil {
				return Plan{}, err
			}
			add(optionalRun("systemctl", "--user", "daemon-reload"))
		}
		if p.SystemUnit != "" {
			// /usr 下的文件由包管理器管理，ctl 既不删也不能删
			tail = append(tail, Step{Kind: StepNotice, Notice: NoticeManual, Text: "deb/rpm 安装：/usr 下的单元与二进制由包管理器管理，padlinkctl 不删除；请执行 sudo apt remove padlink（Debian/Ubuntu）或 sudo dnf remove padlink（Fedora/RHEL）"})
			tail = append(tail, Step{Kind: StepNotice, Notice: NoticeKeep, Text: systemUnitPath + "、/usr/bin/" + daemonBinName + "、/usr/bin/" + ctlBinName + "（由包管理器管理，未删除）"})
		}
		if p.unitPath() == "" {
			tail = append(tail, Step{Kind: StepNotice, Notice: NoticeInfo, Text: "未检测到 padlink 的 systemd user 单元（可能已卸载）"})
		}
		if p.UdevRule != "" {
			// 需 root：不自动 sudo，只给可直接复制的命令
			tail = append(tail, Step{Kind: StepNotice, Notice: NoticeManual, Text: "udev 规则需 root 删除，请手动执行：sudo rm -f " + p.UdevRule + " && sudo udevadm control --reload && sudo udevadm trigger /dev/uinput"})
		}
	case "darwin":
		if p.Plist != "" {
			target, err := p.agentTarget()
			if err != nil {
				return Plan{}, err
			}
			// bootout 卸载 job 并终止其进程（KeepAlive 不阻止卸载）；未注册时无害
			add(optionalRun("launchctl", "bootout", target))
			if err := remove(StepRemoveFile, p.Plist, filepath.Dir(p.Plist)); err != nil {
				return Plan{}, err
			}
		} else {
			tail = append(tail, Step{Kind: StepNotice, Notice: NoticeInfo, Text: "未找到 LaunchAgent（可能已卸载）"})
		}
	default:
		return Plan{}, fmt.Errorf("不支持的平台 %q（仅支持 linux / darwin）", p.GOOS)
	}

	// 用户级二进制（tar.gz / macOS 形态）：逐个删；padlinkctl 自身正在运行也可删
	for _, bin := range p.Bins {
		if err := remove(StepRemoveFile, bin, p.UserBinDir); err != nil {
			return Plan{}, err
		}
	}

	// 配置与 token（含 macOS 日志）只在 --purge（且确认）后删除
	if purge {
		if p.GOOS == "darwin" && p.LogFile != "" {
			if err := remove(StepRemoveFile, p.LogFile, filepath.Dir(p.LogFile)); err != nil {
				return Plan{}, err
			}
		}
		for _, f := range p.ConfigFiles {
			if err := remove(StepRemoveFile, f, p.XDGConfigHome); err != nil {
				return Plan{}, err
			}
		}
		if p.ConfigDir != "" {
			if err := remove(StepRemoveDir, p.ConfigDir, p.XDGConfigHome); err != nil {
				return Plan{}, err
			}
		}
	} else {
		if p.ConfigDir != "" {
			tail = append(tail, Step{Kind: StepNotice, Notice: NoticeKeep, Text: "配置与 token 保留：" + p.ConfigDir + "（--purge 可一并删除）"})
		}
		if p.LogFile != "" {
			tail = append(tail, Step{Kind: StepNotice, Notice: NoticeKeep, Text: "日志保留：" + p.LogFile + "（--purge 可一并删除）"})
		}
	}
	add(tail...)
	return plan, nil
}

// ConfirmPurge 处理 --purge 的二次确认：assumeYes（--yes）直接通过；
// 否则向 out 打印提示并从 in 读一行，仅 y/yes（去空白、忽略大小写）视为确认；
// 非交互 stdin（EOF 且无内容）视为拒绝。
func ConfirmPurge(in io.Reader, out io.Writer, assumeYes bool) (bool, error) {
	if assumeYes {
		return true, nil
	}
	fmt.Fprint(out, "--purge 将删除配置与 token（不可恢复），确认继续？[y/N] ")
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("读取 --purge 确认输入: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
