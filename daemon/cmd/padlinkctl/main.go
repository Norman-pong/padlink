// padlinkctl 是 padlinkd 的控制 CLI：status / pair / clients / unpair，
// 经 daemon 的 Unix socket 控制通道（行分隔 JSON）通信；
// start / stop / restart / uninstall 由 internal/service 直接操作
// systemd --user 或 launchd，不经控制 socket（padlinkd 未运行时也可用）。
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	"padlink/daemon/internal/control"
	"padlink/daemon/internal/service"
)

func usage() {
	fmt.Fprint(os.Stderr, `用法: padlinkctl <命令>

命令:
  status          查看 daemon 状态（版本/uptime/配对/会话/计数/控制权抢占/accel-profile）
  pair            发起配对，终端显示 4 位确认码（60 秒内有效，最多 5 次尝试）
  clients         列出已配对客户端（id/名称/配对时间/设备指纹/在线）
  unpair <id>     解除指定客户端配对并踢下线

  start           启动 padlinkd 服务（Linux systemd --user / macOS launchd）
  stop            停止 padlinkd 服务
  restart         重启 padlinkd 服务
  uninstall       卸载 padlink：停服务、移除服务定义与用户级二进制
                  选项:
                    --purge    连配置与 token 一起删除（需二次确认）
                    --yes      跳过 --purge 的二次确认
                    --dry-run  只打印将执行的命令与将删除的路径
`)
}

// parseUninstallArgs 解析 uninstall 的选项；未知选项报错（退出码 2）。
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

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	// 服务管理与卸载不走控制 socket：padlinkd 未运行时也必须可用
	switch os.Args[1] {
	case "start", "stop", "restart":
		if err := service.Control(service.ControlAction(os.Args[1]), os.Stdout); err != nil {
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
				pairing = fmt.Sprintf("确认码 %s（%d 秒后过期）", st.Pairing.Code, st.Pairing.ExpiresInSec)
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
			fmt.Printf("配对确认码: %s（60 秒内有效，最多 5 次尝试；主机桌面通知已尝试发送）\n", d.Code)
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
				// 设备指纹前缀：同一台手机重配会命中同一指纹（只轮换 token，不新增记录），
				// 旧版手机未上报时显示 "-"。
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
