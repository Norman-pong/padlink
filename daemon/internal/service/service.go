// Package service 提供 padlinkd 的服务启停与卸载能力：探测本机安装形态
// （Linux systemd user 单元 / macOS LaunchAgent / 未安装）并编排有序动作（Plan）。
//
// 分两层：
//   - 纯逻辑层（service.go、plan.go）：只按显式输入（goos、home、xdgConfigHome、uid、
//     「路径是否存在」探测函数）构造 Plan，不读环境变量、不触碰文件系统，可在任意平台单测；
//   - 执行层（exec.go）：读环境变量与文件系统完成真实探测，执行 Plan 中的命令与删除。
//
// 启停与卸载都不经控制 socket，padlinkd 未运行时也可用。
package service

import (
	"fmt"
	"path/filepath"
	"strconv"
)

const (
	// UnitName 是 systemd user 单元名（tar.gz 与 deb/rpm 两种安装形态同名）。
	UnitName = "padlink"
	// AgentLabel 是 macOS LaunchAgent 的 label（与 packaging/macos 的 plist 模板一致）。
	AgentLabel = "com.zhimingcool.padlink"

	// systemUnitPath 是 deb/rpm 形态的单元路径：由包管理器安装，ctl 不得删除。
	systemUnitPath = "/usr/lib/systemd/user/padlink.service"
	// udevRulePath 是 Linux uaccess 规则路径：需 root 删除，ctl 只提示、不自动 sudo。
	udevRulePath = "/etc/udev/rules.d/69-padlink-uinput.rules"

	daemonBinName = "padlinkd"
	ctlBinName    = "padlinkctl"
	clientsFile   = "clients.json"
	daemonLogName = "padlinkd.log"

	stateDirName = "padlink"
)

// InstallKind 是探测到的 padlink 安装形态。
type InstallKind string

const (
	// KindLinuxTar 是 Linux tar.gz 手工安装：二进制在 ~/.local/bin，用户单元在 ~/.config/systemd/user。
	KindLinuxTar InstallKind = "linux-tar"
	// KindDebRPM 是 Linux 包管理器安装：二进制在 /usr/bin，单元在 /usr/lib/systemd/user（ctl 不得删除 /usr 下文件）。
	KindDebRPM InstallKind = "deb-rpm"
	// KindMacOSAgent 是 macOS LaunchAgent 安装：plist 在 ~/Library/LaunchAgents，二进制在 ~/.local/bin。
	KindMacOSAgent InstallKind = "macos-agent"
	// KindUnknown 表示未检测到已知安装形态（可能未安装或已卸载）。
	KindUnknown InstallKind = "unknown"
)

// String 返回安装形态的中文说明（输出用）。
func (k InstallKind) String() string {
	switch k {
	case KindLinuxTar:
		return "Linux tar.gz 手工安装"
	case KindDebRPM:
		return "Linux deb/rpm 包安装"
	case KindMacOSAgent:
		return "macOS LaunchAgent 安装"
	default:
		return "未检测到已知安装形态"
	}
}

// ControlAction 是服务启停动作，取值与命令行子命令一致。
type ControlAction string

// 支持的启停动作。
const (
	ActionStart   ControlAction = "start"
	ActionStop    ControlAction = "stop"
	ActionRestart ControlAction = "restart"
)

// label 返回动作的中文名（输出用）。
func (a ControlAction) label() string {
	switch a {
	case ActionStart:
		return "启动"
	case ActionStop:
		return "停止"
	case ActionRestart:
		return "重启"
	default:
		return string(a)
	}
}

// Probe 是安装形态探测结果。除 GOOS/UID/Home/XDGConfigHome/UserBinDir 外，
// 其余路径字段仅在对应文件或目录确实存在时才填入（不存在为空串或空切片）。
type Probe struct {
	GOOS          string      // 目标平台（runtime.GOOS）
	UID           int         // 当前用户 uid（macOS launchctl gui/<uid> 用）
	Kind          InstallKind // 探测到的安装形态
	Home          string      // 用户主目录（$HOME）
	XDGConfigHome string      // 配置根：$XDG_CONFIG_HOME，未设置时 $HOME/.config
	UserBinDir    string      // 用户级二进制目录：$HOME/.local/bin

	UserUnit    string   // Linux tar.gz 形态单元：~/.config/systemd/user/padlink.service
	SystemUnit  string   // Linux deb/rpm 形态单元：/usr/lib/systemd/user/padlink.service
	Plist       string   // macOS LaunchAgent：~/Library/LaunchAgents/com.zhimingcool.padlink.plist
	Bins        []string // 存在的用户级二进制（~/.local/bin/{padlinkd,padlinkctl}）
	UdevRule    string   // 存在的 Linux udev 规则
	LogFile     string   // 存在的 macOS 日志：~/Library/Logs/padlinkd.log
	ConfigDir   string   // 存在的配置/token 目录：$XDG_CONFIG_HOME/padlink
	ConfigFiles []string // 存在的配置/token 文件：clients.json
}

// unitPath 返回 Linux 生效的 systemd user 单元：systemd 用户单元优先级为
// ~/.config > /usr/lib，因此 tar.gz 单元存在时它生效；未安装返回空串。
func (p Probe) unitPath() string {
	if p.UserUnit != "" {
		return p.UserUnit
	}
	return p.SystemUnit
}

// agentTarget 返回 macOS launchctl 的服务目标：gui/<uid>/<label>。
func (p Probe) agentTarget() (string, error) {
	if p.UID <= 0 {
		return "", fmt.Errorf("无法确定当前用户 uid（得到 %d），不能通过 launchctl 操作 %s", p.UID, AgentLabel)
	}
	return "gui/" + strconv.Itoa(p.UID) + "/" + AgentLabel, nil
}

// BuildInstallProbe 按显式输入探测安装形态；exists 用于判断路径是否存在，
// 不读环境变量、不触碰真实文件系统（因此可在任意平台单测）。
func BuildInstallProbe(goos, home, xdgConfigHome string, uid int, exists func(string) bool) Probe {
	if exists == nil {
		exists = func(string) bool { return false }
	}
	configRoot := xdgConfigHome
	if configRoot == "" && home != "" {
		configRoot = filepath.Join(home, ".config")
	}
	p := Probe{
		GOOS:          goos,
		UID:           uid,
		Home:          home,
		XDGConfigHome: configRoot,
		UserBinDir:    filepath.Join(home, ".local", "bin"),
	}
	for _, name := range []string{daemonBinName, ctlBinName} {
		if path := filepath.Join(p.UserBinDir, name); exists(path) {
			p.Bins = append(p.Bins, path)
		}
	}
	if configRoot != "" {
		if dir := filepath.Join(configRoot, stateDirName); exists(dir) {
			p.ConfigDir = dir
			if f := filepath.Join(dir, clientsFile); exists(f) {
				p.ConfigFiles = append(p.ConfigFiles, f)
			}
		}
	}
	switch goos {
	case "linux":
		// systemd 用户单元搜索路径含 $XDG_CONFIG_HOME/systemd/user 与 ~/.config/systemd/user，
		// 两者都查；packaging/install.sh 装的是后者。
		p.UserUnit = firstExisting(exists,
			filepath.Join(configRoot, "systemd", "user", UnitName+".service"),
			filepath.Join(home, ".config", "systemd", "user", UnitName+".service"))
		if exists(systemUnitPath) {
			p.SystemUnit = systemUnitPath
		}
		if exists(udevRulePath) {
			p.UdevRule = udevRulePath
		}
		switch {
		case p.UserUnit != "":
			p.Kind = KindLinuxTar
		case p.SystemUnit != "":
			p.Kind = KindDebRPM
		case len(p.Bins) > 0:
			// 单元已丢失但二进制残留：仍按手工安装形态处理（卸载时清理二进制）
			p.Kind = KindLinuxTar
		default:
			p.Kind = KindUnknown
		}
	case "darwin":
		p.Plist = firstExisting(exists, filepath.Join(home, "Library", "LaunchAgents", AgentLabel+".plist"))
		p.LogFile = firstExisting(exists, filepath.Join(home, "Library", "Logs", daemonLogName))
		if p.Plist != "" || len(p.Bins) > 0 {
			p.Kind = KindMacOSAgent
		} else {
			p.Kind = KindUnknown
		}
	default:
		p.Kind = KindUnknown
	}
	return p
}

// firstExisting 返回第一个存在的绝对路径；都不存在（或路径为空/相对）返回空串。
// 相对路径一律忽略：探测结果不应受当前工作目录影响。
func firstExisting(exists func(string) bool, paths ...string) string {
	for _, path := range paths {
		if path == "" || !filepath.IsAbs(path) {
			continue
		}
		if exists(path) {
			return path
		}
	}
	return ""
}
