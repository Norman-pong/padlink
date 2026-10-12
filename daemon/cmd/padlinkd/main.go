// padlinkd 是 PadLink 守护进程入口：常驻模式起注入后端、配对存储、网络与控制通道，
// --test 只跑合成注入自测（无手机）。
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"padlink/daemon/internal/control"
	"padlink/daemon/internal/hostinfo"
	"padlink/daemon/internal/inject"
	"padlink/daemon/internal/pairing"
	"padlink/daemon/internal/proto"
	"padlink/daemon/internal/session"
	"padlink/daemon/internal/textinject"
	"padlink/daemon/internal/uinput"
)

// daemonVersion 由发布流程以 ldflags -X 注入（goreleaser）；本地构建回落 0.1.0-dev。
var daemonVersion = "0.1.0-dev"

func main() {
	ensureUTF8Locale()
	test := flag.Bool("test", false, "运行注入链路自测（画圆/点击/敲键/滚动，无手机）")
	verbose := flag.Bool("v", false, "verbose 输出（逐包调试行）")
	port := flag.Int("port", session.DefaultPort, "TCP/UDP 服务端口")
	stateDir := flag.String("state-dir", "", "覆盖 token 存储目录（默认 $XDG_CONFIG_HOME/padlink 或 ~/.config/padlink）")
	restoreDelayMs := flag.Int("restore-delay-ms", defaultRestoreDelayMs(), "文本注入后恢复剪贴板的延迟（ms，300–500）")
	showVersion := flag.Bool("version", false, "打印版本号后退出（安装脚本据此写 app bundle 的 Info.plist）")
	flag.Parse()

	if *showVersion {
		fmt.Println(daemonVersion)
		return
	}

	if !*test {
		if err := run(config{
			port:         *port,
			verbose:      *verbose,
			stateDir:     *stateDir,
			restoreDelay: time.Duration(*restoreDelayMs) * time.Millisecond,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "padlinkd: %v\n", err)
			os.Exit(1)
		}
		return
	}

	dev, err := uinput.Open(log.New(os.Stderr, "", 0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "padlinkd --test FAILED: %v\n", err)
		os.Exit(1)
	}
	if err := runTest(dev, *verbose); err != nil {
		dev.Close()
		fmt.Fprintf(os.Stderr, "padlinkd --test FAILED: %v\n", err)
		os.Exit(1)
	}
	if err := dev.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "padlinkd --test FAILED: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("padlinkd --test PASSED")
}

type config struct {
	port         int
	verbose      bool
	stateDir     string
	restoreDelay time.Duration
}

func run(cfg config) error {
	logf := log.New(os.Stdout, "padlinkd ", log.LstdFlags)

	// 主机自检仅 Linux 有意义：非 Linux 保持 rep 零值（AccelProfile 空 = 未检测到）。
	var rep hostinfo.Report
	if runtime.GOOS == "linux" {
		rep = hostinfo.Check(hostinfo.Options{})
		for _, f := range rep.Findings {
			logf.Printf("警告[%s] %s", f.Check, f.Message)
		}
	}

	dev, err := uinput.Open(logf)
	if err != nil {
		return err
	}
	// 多击间隔必须取平台系统设置：darwin 注入要显式写 CGEvent 点击序号，阈值不一致会让双击失效。
	inj := inject.NewInjector(dev, inject.Config{
		DoubleClickInterval: uinput.DoubleClickInterval(),
		CapsLockActive:      uinput.CapsLockActive,
	})

	storePath, err := storePath(cfg.stateDir)
	if err != nil {
		inj.Close()
		return err
	}
	store, err := pairing.Load(storePath)
	if err != nil {
		inj.Close()
		return err
	}
	for _, w := range store.Warnings() {
		logf.Printf("警告[token] %s", w)
	}
	mgr := pairing.NewManager(store)
	mgr.Log = logf.Printf

	// 粘贴组合键按平台选：Linux Ctrl+V，macOS Cmd+V。
	pasteKey := inj.CtrlV
	if runtime.GOOS == "darwin" {
		pasteKey = inj.SuperV
	}
	ti := textinject.New(textinject.ExecRunner{}, pasteKey, cfg.restoreDelay)
	// srv 前向引用：onText 需要经 srv 回传注入失败提示，而 srv 构造又要收 onText（闭包在
	// Start 后才可能在会话 goroutine 里执行，赋值先于一切调用）。
	var srv *session.Server
	onText := func(clientID string, text string) error {
		err := ti.Inject(text)
		if err != nil {
			srv.NotifyTextFailed(clientID, textFailReason(err))
		}
		return err
	}
	srv = session.New(session.Config{
		Port:          cfg.port,
		DaemonVersion: daemonVersion,
		Verbose:       cfg.verbose,
	}, store, mgr, inj, onText)
	if err := srv.Start(); err != nil {
		inj.Close()
		return err
	}

	// 控制通道：无 XDG_RUNTIME_DIR 时禁用并告警，不阻断启动。
	ctl, err := control.Start(&ctlBackend{
		start: time.Now(),
		store: store,
		mgr:   mgr,
		srv:   srv,
		accel: rep.AccelProfile,
	})
	if errors.Is(err, control.ErrDisabled) {
		logf.Printf("警告[control] %v", err)
	} else if err != nil {
		srv.Stop()
		inj.Close()
		return err
	}
	logf.Printf("padlinkd %s 已启动（端口 %d，已配对客户端 %d，文本注入 %s）",
		daemonVersion, srv.Port(), store.Count(), onOff(ti.Available()))

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	logf.Printf("收到退出信号，正在关闭…")
	if ctl != nil {
		ctl.Stop()
	}
	srv.Stop() // 先收会话：兜底补发未释放按键，排空注入队列
	inj.Close()
	logf.Printf("已退出")
	return nil
}

func storePath(stateDir string) (string, error) {
	if stateDir != "" {
		return filepath.Join(stateDir, "clients.json"), nil
	}
	return pairing.DefaultPath()
}

func onOff(b bool) string {
	if b {
		return "可用"
	}
	return "降级"
}

// textFailReason 把文本注入错误映射为 NOTICE(TEXT 未上屏) 的 arg 原因码（与 PROTOCOL.md §4.7 同步）。
func textFailReason(err error) uint8 {
	switch {
	case errors.Is(err, textinject.ErrNonText):
		return proto.TextFailNonText
	case errors.Is(err, textinject.ErrUnavailable):
		return proto.TextFailUnavailable
	default:
		return proto.TextFailUnknown
	}
}

// ensureUTF8Locale 保证本进程及子进程拿到 UTF-8 locale（仅 macOS）：launchd 环境无 locale 时
// pbcopy 写 CJK 会得到空内容，备份/还原非 ASCII 剪贴板等于清空用户剪贴板；wl-clipboard 不受影响。
func ensureUTF8Locale() {
	if runtime.GOOS != "darwin" {
		return
	}
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := strings.ToUpper(os.Getenv(k)); strings.Contains(v, "UTF-8") || strings.Contains(v, "UTF8") {
			return // 已有 UTF-8 locale，不得覆盖
		}
	}
	_ = os.Setenv("LC_CTYPE", "UTF-8")
	if os.Getenv("LANG") == "" {
		_ = os.Setenv("LANG", "en_US.UTF-8")
	}
}

// defaultRestoreDelayMs 返回还原剪贴板的默认延迟：必须够目标应用读完粘贴板，
// macOS 800ms / Linux 300ms，改小前须真机复测。
func defaultRestoreDelayMs() int {
	if runtime.GOOS == "darwin" {
		return 800
	}
	return int(textinject.DefaultRestoreDelay / time.Millisecond)
}

func runTest(w inject.DeviceWriter, verbose bool) error {
	inj := inject.NewInjector(w, inject.Config{CapsLockActive: uinput.CapsLockActive})

	if err := drawCircles(inj, 2, 200, 120); err != nil {
		return err
	}

	fmt.Println("left click…")
	if err := inj.Button(1, true); err != nil {
		return err
	}
	if err := inj.Button(1, false); err != nil {
		return err
	}

	fmt.Println("typing 'pl' (HID usage map)…")
	for _, hid := range []uint16{inject.HIDP, inject.HIDL} {
		if err := inj.Key(hid, true); err != nil {
			return err
		}
		if err := inj.Key(hid, false); err != nil {
			return err
		}
	}

	fmt.Println("scrolling hi-res +2 notches / -1 notch…")
	if err := inj.Scroll(240); err != nil {
		return err
	}
	if err := inj.Scroll(-120); err != nil {
		return err
	}

	if verbose {
		fmt.Printf("[verbose] device %q created, sequence complete\n", uinput.DeviceName)
	}
	return nil
}

// drawCircles 以相邻点求每段位移，保证每圆位移代数和为零（精确闭合）。
func drawCircles(inj *inject.Injector, circles, radius, segments int) error {
	fmt.Printf("drawing %d circles (r=%dpx, %d segments each)…\n", circles, radius, segments)
	for c := 0; c < circles; c++ {
		prevX, prevY := float64(radius), 0.0
		for i := 1; i <= segments; i++ {
			a := 2 * math.Pi * float64(i) / float64(segments)
			x := math.Round(float64(radius) * math.Cos(a))
			y := math.Round(float64(radius) * math.Sin(a))
			if err := inj.Move(int16(x-prevX), int16(y-prevY)); err != nil {
				return err
			}
			prevX, prevY = x, y
		}
	}
	return nil
}
