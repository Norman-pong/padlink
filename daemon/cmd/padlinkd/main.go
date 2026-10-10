// padlinkd 是 PadLink 守护进程入口。
// --test：注入链路自测（PRD §7.3 / PROBE-LINUX §4）；
// 常驻模式：hostinfo 自检 → uinput → token 存储 → 网络（pairing/session/discovery）→ 控制通道，
// SIGINT/SIGTERM 优雅退出（关全部会话、控制 socket、恢复按键）。
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
	"syscall"
	"time"

	"padlink/daemon/internal/control"
	"padlink/daemon/internal/hostinfo"
	"padlink/daemon/internal/inject"
	"padlink/daemon/internal/pairing"
	"padlink/daemon/internal/session"
	"padlink/daemon/internal/textinject"
	"padlink/daemon/internal/uinput"
)

// daemonVersion 由发布流程以 ldflags -X 注入（goreleaser）；本地开发构建回落 0.1.0-dev。
var daemonVersion = "0.1.0-dev"

func main() {
	test := flag.Bool("test", false, "运行注入链路自测（画圆/点击/敲键/滚动，无手机）")
	verbose := flag.Bool("v", false, "verbose 输出（逐包调试行）")
	port := flag.Int("port", session.DefaultPort, "TCP/UDP 服务端口")
	stateDir := flag.String("state-dir", "", "覆盖 token 存储目录（默认 $XDG_CONFIG_HOME/padlink 或 ~/.config/padlink）")
	restoreDelayMs := flag.Int("restore-delay-ms", int(textinject.DefaultRestoreDelay/time.Millisecond), "文本注入后恢复剪贴板的延迟（ms，300–500）")
	flag.Parse()

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

	dev, err := uinput.Open()
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

// run 常驻模式主流程；返回错误即启动失败（fatal）。
func run(cfg config) error {
	logf := log.New(os.Stdout, "padlinkd ", log.LstdFlags)

	// ① 主机环境自检（警告不阻断）；hostinfo 检查项仅对 Linux 有意义
	// （macOS 指针加速由 darwin 注入后端的绝对定位语义绕开，docs/PLAN-MACOS.md §2.2）。
	// 非 Linux 平台 rep 保持零值（AccelProfile 空 = 未检测到，控制通道语义不变）。
	var rep hostinfo.Report
	if runtime.GOOS == "linux" {
		rep = hostinfo.Check(hostinfo.Options{})
		for _, f := range rep.Findings {
			logf.Printf("警告[%s] %s", f.Check, f.Message)
		}
	}

	// ② 注入后端（uinput.Open 失败即 fatal，错误文案含处置指引）
	dev, err := uinput.Open()
	if err != nil {
		return err
	}
	inj := inject.NewInjector(dev, inject.Config{})

	// ③ token 存储
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

	// ④ 网络（会话/配对/发现）与文本注入
	ti := textinject.New(textinject.ExecRunner{}, inj.CtrlV, cfg.restoreDelay)
	srv := session.New(session.Config{
		Port:          cfg.port,
		DaemonVersion: daemonVersion,
		Verbose:       cfg.verbose,
	}, store, mgr, inj, ti.Inject)
	if err := srv.Start(); err != nil {
		inj.Close()
		return err
	}

	// ⑤ 控制通道（无 XDG_RUNTIME_DIR 时禁用并告警，不阻断）
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

	// ⑥ SIGINT/SIGTERM 优雅退出
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	logf.Printf("收到退出信号，正在关闭…")
	if ctl != nil {
		ctl.Stop()
	}
	srv.Stop() // 会话收尾兜底补发未释放按键，排空注入队列
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

// runTest 执行 PROBE-LINUX §4 的合成注入序列。
func runTest(w inject.DeviceWriter, verbose bool) error {
	inj := inject.NewInjector(w, inject.Config{})

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

// drawCircles 以圆周相邻点求每段位移，保证各圆按整数计数精确闭合回起点。
func drawCircles(inj *inject.Injector, circles, radius, segments int) error {
	fmt.Printf("drawing %d circles (r=%dpx, %d segments each)…\n", circles, radius, segments)
	for c := 0; c < circles; c++ {
		prevX, prevY := float64(radius), 0.0 // 起点即终点：angle 0
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
