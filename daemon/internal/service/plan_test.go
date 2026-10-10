package service

import (
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// setExists 返回按集合判定存在性的探测函数（注入 BuildInstallProbe，保持纯逻辑可测）。
func setExists(paths ...string) func(string) bool {
	m := make(map[string]bool, len(paths))
	for _, p := range paths {
		m[p] = true
	}
	return func(p string) bool { return m[p] }
}

// commandsOf 返回计划中的命令行序列（保持计划顺序）。
func commandsOf(plan Plan) []string {
	var out []string
	for _, st := range plan.Steps {
		if st.Kind == StepRunCommand {
			out = append(out, st.CommandLine())
		}
	}
	return out
}

// removePathsOf 返回计划中的删除目标（保持计划顺序）。
func removePathsOf(plan Plan) []string {
	var out []string
	for _, st := range plan.Steps {
		if st.Kind == StepRemoveFile || st.Kind == StepRemoveDir {
			out = append(out, st.Path)
		}
	}
	return out
}

// noticeOf 返回第一条包含 substr 的提示的归类与文本。
func noticeOf(plan Plan, substr string) (NoticeKind, string, bool) {
	for _, st := range plan.Steps {
		if st.Kind == StepNotice && strings.Contains(st.Text, substr) {
			return st.Notice, st.Text, true
		}
	}
	return NoticeInfo, "", false
}

// dumpPlan 把计划逐条写进测试日志（go test -v 时可见，便于核对命令序列与文件清单）。
func dumpPlan(t *testing.T, plan Plan) {
	t.Helper()
	for i, st := range plan.Steps {
		switch st.Kind {
		case StepRunCommand:
			t.Logf("step %d: 命令 %s（optional=%v）", i+1, st.CommandLine(), st.Optional)
		case StepRemoveFile:
			t.Logf("step %d: 删除文件 %s（守卫前缀 %s）", i+1, st.Path, st.Guard)
		case StepRemoveDir:
			t.Logf("step %d: 删除目录 %s（守卫前缀 %s）", i+1, st.Path, st.Guard)
		case StepNotice:
			t.Logf("step %d: 提示[%d] %s", i+1, st.Notice, st.Text)
		}
	}
}

func assertStrings(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %q，期望 %q", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s[%d] = %q，期望 %q（完整: %q）", what, i, got[i], want[i], got)
		}
	}
}

func mustContain(t *testing.T, what, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("%s = %q，应包含 %q", what, haystack, needle)
	}
}

const (
	testHome    = "/home/u"
	testUserCfg = "/home/u/.config"
	testUserBin = "/home/u/.local/bin"
	testUnit    = "/home/u/.config/systemd/user/padlink.service"
	testSysUnit = "/usr/lib/systemd/user/padlink.service"
	testUdev    = "/etc/udev/rules.d/69-padlink-uinput.rules"
	testCfgDir  = "/home/u/.config/padlink"
	testClients = "/home/u/.config/padlink/clients.json"
	testHomeBin = "/home/u/.local/bin/padlinkd"
	testCtlBin  = "/home/u/.local/bin/padlinkctl"
)

func TestBuildInstallProbe(t *testing.T) {
	tests := []struct {
		name       string
		goos       string
		home       string
		xdg        string
		paths      []string
		wantKind   InstallKind
		wantUnit   string
		wantSys    string
		wantBins   int
		wantCfgDir string
		wantCl     int
		wantUdev   bool
		wantPlist  string
		wantLog    string
	}{
		{
			name: "linux tar.gz 手工安装",
			goos: "linux", home: testHome,
			paths:    []string{testUnit, testHomeBin, testCtlBin, testCfgDir, testClients, testUdev},
			wantKind: KindLinuxTar, wantUnit: testUnit, wantBins: 2,
			wantCfgDir: testCfgDir, wantCl: 1, wantUdev: true,
		},
		{
			name: "linux deb/rpm 包安装",
			goos: "linux", home: testHome,
			paths:    []string{testSysUnit},
			wantKind: KindDebRPM, wantSys: testSysUnit,
		},
		{
			name: "linux 单元丢失仅剩二进制",
			goos: "linux", home: testHome,
			paths:    []string{testHomeBin},
			wantKind: KindLinuxTar, wantBins: 1,
		},
		{
			name: "linux 未安装",
			goos: "linux", home: testHome,
			wantKind: KindUnknown,
		},
		{
			name: "linux XDG_CONFIG_HOME 自定义",
			goos: "linux", home: testHome, xdg: "/xdg/cfg",
			paths:      []string{"/xdg/cfg/systemd/user/padlink.service", "/xdg/cfg/padlink", "/xdg/cfg/padlink/clients.json"},
			wantKind:   KindLinuxTar,
			wantUnit:   "/xdg/cfg/systemd/user/padlink.service",
			wantCfgDir: "/xdg/cfg/padlink", wantCl: 1,
		},
		{
			name: "macOS LaunchAgent 安装",
			goos: "darwin", home: "/Users/u",
			paths:     []string{"/Users/u/Library/LaunchAgents/com.zhimingcool.padlink.plist", "/Users/u/.local/bin/padlinkd", "/Users/u/Library/Logs/padlinkd.log"},
			wantKind:  KindMacOSAgent,
			wantBins:  1,
			wantPlist: "/Users/u/Library/LaunchAgents/com.zhimingcool.padlink.plist",
			wantLog:   "/Users/u/Library/Logs/padlinkd.log",
		},
		{
			name: "macOS 未安装",
			goos: "darwin", home: "/Users/u",
			wantKind: KindUnknown,
		},
		{
			name: "其他平台",
			goos: "windows", home: "C:/Users/u",
			paths:    []string{"C:/Users/u/.local/bin/padlinkd"},
			wantKind: KindUnknown, wantBins: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := BuildInstallProbe(tc.goos, tc.home, tc.xdg, 1000, setExists(tc.paths...))
			if p.Kind != tc.wantKind {
				t.Fatalf("Kind = %q，期望 %q", p.Kind, tc.wantKind)
			}
			if p.UserUnit != tc.wantUnit {
				t.Errorf("UserUnit = %q，期望 %q", p.UserUnit, tc.wantUnit)
			}
			if p.SystemUnit != tc.wantSys {
				t.Errorf("SystemUnit = %q，期望 %q", p.SystemUnit, tc.wantSys)
			}
			if len(p.Bins) != tc.wantBins {
				t.Errorf("Bins = %q，期望 %d 个", p.Bins, tc.wantBins)
			}
			if p.ConfigDir != tc.wantCfgDir {
				t.Errorf("ConfigDir = %q，期望 %q", p.ConfigDir, tc.wantCfgDir)
			}
			if len(p.ConfigFiles) != tc.wantCl {
				t.Errorf("ConfigFiles = %q，期望 %d 个", p.ConfigFiles, tc.wantCl)
			}
			if (p.UdevRule != "") != tc.wantUdev {
				t.Errorf("UdevRule = %q，期望存在=%v", p.UdevRule, tc.wantUdev)
			}
			if p.Plist != tc.wantPlist {
				t.Errorf("Plist = %q，期望 %q", p.Plist, tc.wantPlist)
			}
			if p.LogFile != tc.wantLog {
				t.Errorf("LogFile = %q，期望 %q", p.LogFile, tc.wantLog)
			}
		})
	}
}

func TestBuildControlLinux(t *testing.T) {
	p := BuildInstallProbe("linux", testHome, "", 1000, setExists(testUnit))
	tests := []struct {
		action ControlAction
		want   string
	}{
		{ActionStart, "systemctl --user start padlink"},
		{ActionStop, "systemctl --user stop padlink"},
		{ActionRestart, "systemctl --user restart padlink"},
	}
	for _, tc := range tests {
		t.Run(string(tc.action), func(t *testing.T) {
			plan, err := BuildControl(p, tc.action)
			if err != nil {
				t.Fatalf("BuildControl: %v", err)
			}
			assertStrings(t, "命令", commandsOf(plan), []string{tc.want})
			dumpPlan(t, plan)
			if len(removePathsOf(plan)) != 0 {
				t.Errorf("启停计划不应有删除步骤: %q", removePathsOf(plan))
			}
		})
	}
	// deb/rpm 形态同样走 systemctl --user
	t.Run("deb/rpm", func(t *testing.T) {
		plan, err := BuildControl(BuildInstallProbe("linux", testHome, "", 1000, setExists(testSysUnit)), ActionStart)
		if err != nil {
			t.Fatalf("BuildControl: %v", err)
		}
		assertStrings(t, "命令", commandsOf(plan), []string{"systemctl --user start padlink"})
	})
}

func TestBuildControlMacOS(t *testing.T) {
	plist := "/Users/u/Library/LaunchAgents/com.zhimingcool.padlink.plist"
	p := BuildInstallProbe("darwin", "/Users/u", "", 501, setExists(plist))
	target := "gui/501/com.zhimingcool.padlink"
	tests := []struct {
		action ControlAction
		want   []string
	}{
		// 起服务先 bootstrap 保证 job 已注册（已注册时失败无害），再 kickstart 拉起
		{ActionStart, []string{
			"launchctl bootstrap gui/501 " + plist,
			"launchctl kickstart " + target,
		}},
		{ActionRestart, []string{
			"launchctl bootstrap gui/501 " + plist,
			"launchctl kickstart -k " + target,
		}},
		// plist KeepAlive=true：只有 bootout 卸载 job 才算停住（kill 会被 launchd 立刻拉起）
		{ActionStop, []string{"launchctl bootout " + target}},
	}
	for _, tc := range tests {
		t.Run(string(tc.action), func(t *testing.T) {
			plan, err := BuildControl(p, tc.action)
			if err != nil {
				t.Fatalf("BuildControl: %v", err)
			}
			assertStrings(t, "命令", commandsOf(plan), tc.want)
		})
	}
}

func TestBuildControlErrors(t *testing.T) {
	tests := []struct {
		name    string
		probe   Probe
		action  ControlAction
		wantSub []string
	}{
		{
			name: "linux 无单元", probe: BuildInstallProbe("linux", testHome, "", 1000, setExists()), action: ActionStart,
			wantSub: []string{"未找到 padlink 的 systemd user 单元", "./install.sh", "apt install padlink", "dnf install padlink"},
		},
		{
			name: "macOS 无 plist", probe: BuildInstallProbe("darwin", "/Users/u", "", 501, setExists()), action: ActionStart,
			wantSub: []string{"未找到 LaunchAgent", "./install.sh"},
		},
		{
			name: "不支持的平台", probe: BuildInstallProbe("windows", "C:/Users/u", "", 1, setExists()), action: ActionStart,
			wantSub: []string{"不支持的平台"},
		},
		{
			name: "未知动作", probe: BuildInstallProbe("linux", testHome, "", 1000, setExists(testUnit)), action: "enable",
			wantSub: []string{"未知的启停动作"},
		},
		{
			name: "macOS uid 不可用", probe: BuildInstallProbe("darwin", "/Users/u", "", 0,
				setExists("/Users/u/Library/LaunchAgents/com.zhimingcool.padlink.plist")), action: ActionStop,
			wantSub: []string{"uid"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildControl(tc.probe, tc.action)
			if err == nil {
				t.Fatal("期望报错，实际成功")
			}
			for _, sub := range tc.wantSub {
				mustContain(t, "错误消息", err.Error(), sub)
			}
		})
	}
}

func TestBuildUninstallLinuxTar(t *testing.T) {
	p := BuildInstallProbe("linux", testHome, "", 1000, setExists(testUnit, testHomeBin, testCtlBin, testCfgDir, testClients, testUdev))
	plan, err := BuildUninstall(p, false)
	if err != nil {
		t.Fatalf("BuildUninstall: %v", err)
	}
	assertStrings(t, "命令",
		commandsOf(plan),
		[]string{"systemctl --user stop padlink", "systemctl --user disable padlink", "systemctl --user daemon-reload"})
	dumpPlan(t, plan)
	assertStrings(t, "删除目标", removePathsOf(plan), []string{testUnit, testHomeBin, testCtlBin})
	for _, st := range plan.Steps {
		if st.Kind == StepRunCommand && !st.Optional {
			t.Errorf("卸载过程中的命令应为尽力执行（Optional）: %s", st.CommandLine())
		}
	}
	kind, text, ok := noticeOf(plan, "udev 规则需 root 删除")
	if !ok || kind != NoticeManual {
		t.Fatalf("udev 提示缺失或归类错误: ok=%v kind=%v text=%q", ok, kind, text)
	}
	mustContain(t, "udev 提示", text, "sudo rm -f "+testUdev)
	mustContain(t, "udev 提示", text, "sudo udevadm control --reload")
	if kind, _, ok := noticeOf(plan, "配置与 token 保留"); !ok || kind != NoticeKeep {
		t.Fatalf("未加 --purge 时应提示配置保留: ok=%v kind=%v", ok, kind)
	}
	if plan.Purge {
		t.Error("未加 --purge 时 plan.Purge 应为 false")
	}
}

func TestBuildUninstallDebRPM(t *testing.T) {
	// deb/rpm 形态 + 残留的手工安装二进制与配置：整体 purge，仍不得出现 /usr 下的删除步骤
	p := BuildInstallProbe("linux", testHome, "", 1000, setExists(testSysUnit, testHomeBin, testCfgDir, testClients, testUdev))
	plan, err := BuildUninstall(p, true)
	if err != nil {
		t.Fatalf("BuildUninstall: %v", err)
	}
	for _, path := range removePathsOf(plan) {
		if strings.HasPrefix(path, "/usr/") {
			t.Fatalf("deb/rpm 形态不得删除 /usr 下的文件，却出现: %q", path)
		}
		for _, bin := range []string{"/usr/bin/padlinkd", "/usr/bin/padlinkctl"} {
			if path == bin {
				t.Fatalf("deb/rpm 形态不得删除 %q", bin)
			}
		}
	}
	assertStrings(t, "命令", commandsOf(plan), []string{"systemctl --user stop padlink"})
	assertStrings(t, "删除目标", removePathsOf(plan), []string{testHomeBin, testClients, testCfgDir})
	dumpPlan(t, plan)
	kind, text, ok := noticeOf(plan, "sudo apt remove padlink")
	if !ok || kind != NoticeManual {
		t.Fatalf("缺少包管理器卸载提示: ok=%v kind=%v", ok, kind)
	}
	mustContain(t, "包管理器提示", text, "sudo dnf remove padlink")
	if kind, text, ok := noticeOf(plan, testSysUnit); !ok || kind != NoticeKeep {
		t.Fatalf("应提示 /usr 下文件保留: ok=%v kind=%v text=%q", ok, kind, text)
	}
}

func TestBuildUninstallLinuxBothFormsRemovesUserUnitOnly(t *testing.T) {
	// 两种单元同时存在（deb 包 + 手工安装残留）：删用户单元，/usr 单元只提示
	p := BuildInstallProbe("linux", testHome, "", 1000, setExists(testUnit, testSysUnit))
	if p.Kind != KindLinuxTar {
		t.Fatalf("Kind = %q，期望 %q（systemd 优先用户单元）", p.Kind, KindLinuxTar)
	}
	plan, err := BuildUninstall(p, false)
	if err != nil {
		t.Fatalf("BuildUninstall: %v", err)
	}
	assertStrings(t, "删除目标", removePathsOf(plan), []string{testUnit})
	if _, _, ok := noticeOf(plan, "sudo apt remove padlink"); !ok {
		t.Error("系统单元存在时应提示用包管理器卸载")
	}
}

func TestBuildUninstallMacOS(t *testing.T) {
	const (
		home  = "/Users/u"
		plist = "/Users/u/Library/LaunchAgents/com.zhimingcool.padlink.plist"
		log   = "/Users/u/Library/Logs/padlinkd.log"
		bin   = "/Users/u/.local/bin/padlinkd"
		cfg   = "/Users/u/.config/padlink"
		cl    = "/Users/u/.config/padlink/clients.json"
	)
	p := BuildInstallProbe("darwin", home, "", 501, setExists(plist, log, bin, cfg, cl))
	target := "gui/501/com.zhimingcool.padlink"

	t.Run("不含 purge", func(t *testing.T) {
		plan, err := BuildUninstall(p, false)
		if err != nil {
			t.Fatalf("BuildUninstall: %v", err)
		}
		assertStrings(t, "命令",
			commandsOf(plan),
			[]string{"launchctl bootout " + target})
		assertStrings(t, "删除目标", removePathsOf(plan), []string{plist, bin})
		if _, _, ok := noticeOf(plan, "日志保留："+log); !ok {
			t.Error("未加 --purge 时应提示日志保留")
		}
	})
	t.Run("含 purge", func(t *testing.T) {
		plan, err := BuildUninstall(p, true)
		if err != nil {
			t.Fatalf("BuildUninstall: %v", err)
		}
		assertStrings(t, "删除目标", removePathsOf(plan), []string{plist, bin, log, cl, cfg})
		dumpPlan(t, plan)
		if _, _, ok := noticeOf(plan, "保留"); ok {
			t.Error("--purge 时不应有保留提示")
		}
	})
}

func TestBuildUninstallGuardRejectsBadPaths(t *testing.T) {
	// HOME 缺失 → 二进制路径为相对路径 → 守卫拒绝（宁可不删）
	_, err := BuildUninstall(BuildInstallProbe("linux", "", "", 1000, setExists(".local/bin/padlinkd")), false)
	if err == nil {
		t.Fatal("期望路径守卫报错，实际成功")
	}
	mustContain(t, "错误消息", err.Error(), "绝对路径")

	// 不支持的平台
	if _, err := BuildUninstall(BuildInstallProbe("windows", "C:/Users/u", "", 1, setExists()), false); err == nil {
		t.Fatal("期望不支持的平台报错")
	}
}

func TestCheckRemovable(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		prefix  string
		wantErr bool
	}{
		{"合法文件", "/home/u/.local/bin/padlinkd", "/home/u/.local/bin", false},
		{"合法目录", "/home/u/.config/padlink", "/home/u/.config", false},
		{"配置目录本身可删", "/home/u/.config/padlink", "/home/u/.config/padlink", false},
		{"相对路径", ".local/bin/padlinkd", "/home/u/.local/bin", true},
		{"含上跳", "/home/u/.local/bin/../../etc/padlink", "/home/u/.local/bin", true},
		{"未规范化", "/home/u/.local/bin//padlinkd", "/home/u/.local/bin", true},
		{"越出前缀", "/home/u/.local/bin/padlinkd", "/home/u/other", true},
		{"前缀相似但非父目录", "/home/u/.local/bin-evil/padlinkd", "/home/u/.local/bin", true},
		{"不含 padlink 关键字", "/home/u/.local/bin/other", "/home/u/.local/bin", true},
		{"空路径", "", "/home/u", true},
		{"空前缀", "/home/u/padlink", "", true},
		{"根前缀", "/etc/padlink", "/", true},
		{"前缀非绝对", "/home/u/padlink", "home/u", true},
		{"运行目录下的 padlink 文件", "/tmp/padlink-test/config", "/tmp/padlink-test", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckRemovable(tc.path, tc.prefix)
			if (err != nil) != tc.wantErr {
				t.Fatalf("CheckRemovable(%q, %q) = %v，期望报错=%v", tc.path, tc.prefix, err, tc.wantErr)
			}
		})
	}
}

// failReader 断言确认流程未读取 stdin。
type failReader struct{ t *testing.T }

func (r failReader) Read([]byte) (int, error) {
	r.t.Error("--yes 时不应读取 stdin")
	return 0, errors.New("不应读取")
}

func TestConfirmPurge(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		assumeYes bool
		want      bool
	}{
		{"y 确认", "y\n", false, true},
		{"Y 确认", "Y\n", false, true},
		{"yes 确认", "yes\n", false, true},
		{"带空格 yes", "  Yes  \n", false, true},
		{"n 拒绝", "n\n", false, false},
		{"回车默认拒绝", "\n", false, false},
		{"其他输入拒绝", "no\n", false, false},
		{"EOF 视为拒绝", "", false, false},
		{"无换行的 y（管道）", "y", false, true},
		{"--yes 跳过确认", "", true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ConfirmPurge(strings.NewReader(tc.input), io.Discard, tc.assumeYes)
			if err != nil {
				t.Fatalf("ConfirmPurge: %v", err)
			}
			if got != tc.want {
				t.Fatalf("ConfirmPurge = %v，期望 %v", got, tc.want)
			}
		})
	}
	t.Run("--yes 不读取 stdin", func(t *testing.T) {
		got, err := ConfirmPurge(failReader{t: t}, io.Discard, true)
		if err != nil || !got {
			t.Fatalf("ConfirmPurge(assumeYes) = %v, %v", got, err)
		}
	})
	t.Run("读取失败上报", func(t *testing.T) {
		if _, err := ConfirmPurge(errReader{}, io.Discard, false); err == nil {
			t.Fatal("期望读取错误上报")
		}
	})
}

// errReader 始终返回非 EOF 错误。
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("读取失败") }

func TestInstallKindString(t *testing.T) {
	for kind, want := range map[InstallKind]string{
		KindLinuxTar:   "Linux tar.gz 手工安装",
		KindDebRPM:     "Linux deb/rpm 包安装",
		KindMacOSAgent: "macOS LaunchAgent 安装",
		KindUnknown:    "未检测到已知安装形态",
	} {
		if got := kind.String(); got != want {
			t.Errorf("%q.String() = %q，期望 %q", kind, got, want)
		}
	}
}

func TestBuildUninstallPlansOnlyGuardCheckedPaths(t *testing.T) {
	// 计划中每个删除步骤都必须带守卫前缀，且守卫本身有效
	p := BuildInstallProbe("linux", testHome, "", 1000, setExists(testUnit, testHomeBin, testCfgDir, testClients))
	plan, err := BuildUninstall(p, true)
	if err != nil {
		t.Fatalf("BuildUninstall: %v", err)
	}
	for _, st := range plan.Steps {
		if st.Kind != StepRemoveFile && st.Kind != StepRemoveDir {
			continue
		}
		if st.Guard == "" {
			t.Errorf("删除步骤缺少守卫前缀: %q", st.Path)
		}
		if err := CheckRemovable(st.Path, st.Guard); err != nil {
			t.Errorf("计划中的删除步骤未通过守卫: %v", err)
		}
		if !filepath.IsAbs(st.Path) {
			t.Errorf("删除目标应为绝对路径: %q", st.Path)
		}
	}
}
