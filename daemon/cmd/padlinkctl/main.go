// padlinkctl 是 padlinkd 的控制 CLI：status / pair / clients / unpair 走 Unix socket 控制通道（行分隔 JSON）；
// start / stop / restart / uninstall 由 internal/service 直接操作 systemd --user 或 launchd（daemon 未运行也可用）；
// update 由 internal/update 从 GitHub Releases 取包验签后执行包内安装脚本。
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"padlink/daemon/internal/control"
	"padlink/daemon/internal/service"
	"padlink/daemon/internal/update"
)

// ctlVersion 由发布流程以 ldflags -X 注入（goreleaser）；本地构建回落 0.1.0-dev。
var ctlVersion = "0.1.0-dev"

func usage() {
	fmt.Fprint(os.Stderr, `用法: padlinkctl <命令>

命令:
  status          查看 daemon 状态（版本/uptime/配对/会话/计数/控制权抢占/accel-profile）
  pair            发起配对，终端显示 4 位配对码（60 秒内有效，最多 5 次尝试）
  clients         列出已配对客户端（id/名称/配对时间/设备指纹/在线）
  unpair <id>     解除指定客户端配对并踢下线

  start           启动 padlinkd 服务（Linux systemd --user / macOS launchd）
  stop            停止 padlinkd 服务
  restart         重启 padlinkd 服务
  update          升级到最新 release（下载 → SHA-256 校验 → 执行包内安装脚本）
                  选项:
                    --check    只检查是否有新版本，不下载不安装
                    [vX.Y.Z]   指定目标版本（默认最新；可用于回退）
  uninstall       卸载 padlink：停服务、移除服务定义与用户级二进制
                  选项:
                    --purge    连配置与 token 一起删除（需二次确认）
                    --yes      跳过 --purge 的二次确认
                    --dry-run  只打印将执行的命令与将删除的路径
`)
}

// parseUninstallArgs 的未知选项由调用方按退出码 2 退出（CLI 契约）。
func parseUninstallArgs(args []string) (service.UninstallOptions, error) {
	var opts service.UninstallOptions
	for _, arg := range args {
		switch arg {
		case "--purge":
			opts.Purge = true
		case "--yes":
			opts.Yes = true
		case "--dry-run":
			opts.DryRun = true
		default:
			return opts, fmt.Errorf("未知选项 %q（用法: padlinkctl uninstall [--purge] [--yes] [--dry-run]）", arg)
		}
	}
	return opts, nil
}

// runUpdate 处理 padlinkctl update [--check] [vX.Y.Z]。deb/rpm 与未安装形态不接管；
// Linux tar.gz / macOS / Windows 形态由 update 包取包验签后执行包内安装脚本。
// Linux 安装后需显式重启服务——install.sh 的
// enable --now 对已运行单元是 no-op；macOS 的 install.sh 自带 bootout+bootstrap 无需补手。
func runUpdate(args []string) error {
	check := false
	var target string
	for _, arg := range args {
		switch arg {
		case "--check":
			check = true
		case "-h", "--help":
			fmt.Println("用法: padlinkctl update [--check] [vX.Y.Z]")
			return nil
		default:
			if strings.HasPrefix(arg, "-") || target != "" {
				return fmt.Errorf("未知选项 %q（用法: padlinkctl update [--check] [vX.Y.Z]）", arg)
			}
			target = arg
		}
	}

	p := service.HostProbe()
	switch p.Kind {
	case service.KindDebRPM:
		return fmt.Errorf("deb/rpm 安装由包管理器管理，请执行: sudo apt upgrade padlink（Debian/Ubuntu）或 sudo dnf upgrade padlink（Fedora/RHEL）")
	case service.KindUnknown:
		if runtime.GOOS == "windows" {
			return fmt.Errorf("未检测到已知安装形态，请先在 PowerShell 中用一行安装器安装: irm https://raw.githubusercontent.com/Norman-pong/padlink/main/script/install.ps1 | iex")
		}
		return fmt.Errorf("未检测到已知安装形态，请先用一行安装器安装: curl -fsSL https://raw.githubusercontent.com/Norman-pong/padlink/main/script/install.sh | sh")
	}
	fmt.Fprintf(os.Stdout, "安装形态: %s\n", p.Kind)

	res, err := update.Run(update.Options{Target: target, Current: ctlVersion, CheckOnly: check, Out: os.Stdout})
	if err != nil {
		return err
	}

	if check {
		switch res.Relation {
		case update.RelSame:
			fmt.Printf("已是最新: %s（当前 %s）\n", res.Tag, res.Current)
		case update.RelUpgrade:
			fmt.Printf("可升级: %s → %s（执行 padlinkctl update）\n", res.Current, res.Tag)
		case update.RelDowngrade:
			fmt.Printf("当前版本 %s 比最新 release %s 还新\n", res.Current, res.Tag)
		case update.RelDev:
			fmt.Printf("当前为开发构建（%s），最新 release: %s\n", res.Current, res.Tag)
		}
		return nil
	}
	if res.UpToDate {
		fmt.Printf("已是最新: %s\n", res.Tag)
		return nil
	}

	if p.Kind == service.KindLinuxTar {
		if err := service.Control(service.ActionRestart, os.Stdout); err != nil {
			fmt.Fprintf(os.Stdout, "警告: 安装已完成，但重启服务失败: %v（手动执行: padlinkctl restart）\n", err)
		}
	}
	// 终验：update 替换的是落盘文件，ctl 自身仍是旧进程，只有已安装的 padlinkd 能报出新版本
	if bin := p.DaemonBinPath(); bin != "" {
		if out, err := exec.Command(bin, "-version").Output(); err == nil {
			fmt.Printf("已安装 padlinkd 版本: %s\n", strings.TrimSpace(string(out)))
		}
	}
	fmt.Println("手机端会自动重连；APP 版本独立发布，不受本命令影响。")
	return nil
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "start", "stop", "restart":
		if err := service.Control(service.ControlAction(os.Args[1]), os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "padlinkctl: %v\n", err)
			os.Exit(1)
		}
		return
	case "update":
		if err := runUpdate(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "padlinkctl: %v\n", err)
			os.Exit(1)
		}
		return
	case "uninstall":
		if len(os.Args) > 2 && (os.Args[2] == "-h" || os.Args[2] == "--help") {
			usage()
			return
		}
		opts, err := parseUninstallArgs(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "padlinkctl: %v\n", err)
			os.Exit(2)
		}
		if p, err := control.SocketPath(); err == nil {
			opts.SocketPath = p
		}
		if err := service.Uninstall(opts, os.Stdin, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "padlinkctl: %v\n", err)
			os.Exit(1)
		}
		return
	}

	var req control.Request
	switch os.Args[1] {
	case "status":
		req.Cmd = "status"
	case "pair":
		req.Cmd = "pair"
	case "clients":
		req.Cmd = "clients"
	case "unpair":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "用法: padlinkctl unpair <客户端ID>（id 见 padlinkctl clients）")
			os.Exit(2)
		}
		req.Cmd, req.Arg = "unpair", os.Args[2]
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "未知命令 %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}

	resp, err := roundTrip(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "padlinkctl: %v\n", err)
		os.Exit(1)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "padlinkd: %s\n", resp.Error)
		os.Exit(1)
	}
	printResult(req, resp)
}

func roundTrip(req control.Request) (control.Response, error) {
	path, err := control.SocketPath()
	if err != nil {
		return control.Response{}, fmt.Errorf("%w（无法定位 padlinkd 控制 socket）", err)
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		return control.Response{}, fmt.Errorf("无法连接 %s: %v —— padlinkd 未在运行？", path, err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return control.Response{}, fmt.Errorf("发送请求: %v", err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return control.Response{}, fmt.Errorf("读取响应: %v", err)
	}
	var resp control.Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return control.Response{}, fmt.Errorf("解析响应: %v", err)
	}
	return resp, nil
}

func printResult(req control.Request, resp control.Response) {
	switch req.Cmd {
	case "status":
		var st control.StatusData
		if mustUnmarshal(resp.Data, &st) {
			pairing := "无"
			if st.Pairing.Active {
				pairing = fmt.Sprintf("配对码 %s（%d 秒后过期）", st.Pairing.Code, st.Pairing.ExpiresInSec)
			}
			accel := st.AccelProfile
			if accel == "" {
				accel = "未检测到"
			}
			fmt.Printf("版本          %s\n运行时间      %.0f 秒\n已配对客户端  %d\n活跃会话      %d\n进行中配对    %s\nHMAC 错误     %d\n丢包计数      %d\n控制权抢占    %d\naccel-profile %s\n",
				st.Version, st.UptimeSec, st.PairedClients, st.ActiveSessions,
				pairing, st.Stats.HMACFail, st.Stats.Dropped, st.Stats.Preempted, accel)
		}
	case "pair":
		var d struct {
			Code string `json:"code"`
		}
		if mustUnmarshal(resp.Data, &d) {
			fmt.Printf("配对码: %s（60 秒内有效，最多 5 次尝试；主机弹窗已尝试显示）\n", d.Code)
		}
	case "clients":
		var clients []control.ClientInfo
		if mustUnmarshal(resp.Data, &clients) {
			if len(clients) == 0 {
				fmt.Println("尚无已配对客户端（padlinkctl pair 发起配对）")
				return
			}
			fmt.Println("ID       名称               配对时间             设备指纹   在线")
			for _, c := range clients {
				online := "否"
				if c.Online {
					online = "是"
				}
				name := c.Name
				if name == "" {
					name = "(未命名)"
				}
				dev := "-"
				if len(c.DevID) >= 8 {
					dev = c.DevID[:8]
				}
				fmt.Printf("%-8s %-18s %s  %-10s %s\n", c.ID, name, c.PairedAt.Local().Format("2006-01-02 15:04"), dev, online)
			}
		}
	case "unpair":
		fmt.Printf("已解除配对: %s\n", req.Arg)
	}
}

func mustUnmarshal(data any, v any) bool {
	b, err := json.Marshal(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "padlinkctl: 解析 data: %v\n", err)
		return false
	}
	if err := json.Unmarshal(b, v); err != nil {
		fmt.Fprintf(os.Stderr, "padlinkctl: 解析 data: %v\n", err)
		return false
	}
	return true
}
