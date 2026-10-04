// padlinkd 是 PadLink 守护进程入口。
// 本阶段仅实现 --test 注入自测（PRD §7.3 / PROBE-LINUX §4）；
// 网络会话层由后续里程碑接入。
package main

import (
	"flag"
	"fmt"
	"math"
	"os"

	"padlink/daemon/internal/inject"
	"padlink/daemon/internal/uinput"
)

func main() {
	test := flag.Bool("test", false, "运行注入链路自测（画圆/点击/敲键/滚动，无手机）")
	verbose := flag.Bool("v", false, "verbose 输出")
	flag.Parse()

	if !*test {
		fmt.Println("网络会话层后续里程碑接入")
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
