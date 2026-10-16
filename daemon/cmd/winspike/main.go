//go:build windows

// winspike 是 Windows 适配（PRD 附录 B）M0 阶段的技术尖峰程序：把四项平台未知数
// 一次性上机实测出数——① SendInput 绝对定位是否绕开「提高指针精确度」弹道加速；
// ② AF_UNIX 控制通道（路径长度上限、close 后残留）；③ 剪贴板写读往返与 Ctrl+V
// 上屏时序；④ 系统对快速连击是否自动合成 WM_LBUTTONDBLCLK（决定注入端是否要自管
// 点击计数）。独立 main，不进发版构建；结论回填 docs/PLAN-WINDOWS.md。
package main

import (
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procSendInput             = user32.NewProc("SendInput")
	procGetCursorPos          = user32.NewProc("GetCursorPos")
	procGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	procSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
	procGetDoubleClickTime    = user32.NewProc("GetDoubleClickTime")
	procOpenClipboard         = user32.NewProc("OpenClipboard")
	procCloseClipboard        = user32.NewProc("CloseClipboard")
	procEmptyClipboard        = user32.NewProc("EmptyClipboard")
	procGetClipboardData      = user32.NewProc("GetClipboardData")
	procSetClipboardData      = user32.NewProc("SetClipboardData")
	procGetModuleHandleW      = kernel32.NewProc("GetModuleHandleW")
	procGlobalAlloc           = kernel32.NewProc("GlobalAlloc")
	procGlobalLock            = kernel32.NewProc("GlobalLock")
	procGlobalUnlock          = kernel32.NewProc("GlobalUnlock")
	procGlobalSize            = kernel32.NewProc("GlobalSize")
	procRtlMoveMemory         = kernel32.NewProc("RtlMoveMemory")

	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procWindowFromPoint  = user32.NewProc("WindowFromPoint")
	procGetClassNameW    = user32.NewProc("GetClassNameW")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procFindWindowW      = user32.NewProc("FindWindowW")
	procSendMessageW     = user32.NewProc("SendMessageW")
	procEnumChildWindows = user32.NewProc("EnumChildWindows")
)

// INPUT 结构按 x64/arm64 布局（windows 目标全为 64 位）：type(4)+pad(4)+联合体，
// 联合体按最大成员 MOUSEINPUT 取 32 字节，总长 40。两种事件用各自的完整结构定义，
// 尺寸断言在 init 里兜底（布局错了 SendInput 会整批拒绝）。
type mouseEvt struct {
	typ uint32
	_   uint32
	mi  mouseInput
}

type mouseInput struct {
	dx, dy    int32
	mouseData uint32
	dwFlags   uint32
	time      uint32
	_         uint32
	extra     uintptr
}

type keybdEvt struct {
	typ uint32
	_   uint32
	ki  keybdInput
	_   [8]byte
}

type keybdInput struct {
	wVk, wScan uint16
	dwFlags    uint32
	time       uint32
	_          uint32
	extra      uintptr
}

const (
	inputMouse    = 0
	inputKeyboard = 1

	mevMove         = 0x0001
	mevLeftDown     = 0x0002
	mevLeftUp       = 0x0004
	mevWheel        = 0x0800
	mevAbsolute     = 0x8000
	kevKeyUp        = 0x0002
	cfUnicodeText   = 13
	gmemMoveable    = 0x0002
	spiGetMouse     = 0x0003
	spiSetMouse     = 0x0004
	smCxScreen      = 0
	smCyScreen      = 1
	wmLButtonDown   = 0x0201
	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmDestroy       = 0x0002
	wmUser          = 0x0400
	csDblClks       = 0x0008
	wsVisible       = 0x10000000
	wsOverlapped    = 0x00CF0000
	vkControl       = 0x11
	vkV             = 0x56
	wmGetTextLength = 0x000E
	wmGetText       = 0x000D
)

func init() {
	if unsafe.Sizeof(mouseEvt{}) != 40 || unsafe.Sizeof(keybdEvt{}) != 40 {
		panic(fmt.Sprintf("INPUT 结构尺寸异常: mouse=%d keybd=%d（应均为 40）",
			unsafe.Sizeof(mouseEvt{}), unsafe.Sizeof(keybdEvt{})))
	}
}

func sendMouse(dx, dy int32, mouseData, flags uint32) {
	e := mouseEvt{typ: inputMouse, mi: mouseInput{dx: dx, dy: dy, mouseData: mouseData, dwFlags: flags}}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&e)), 40)
}

func sendKey(vk uint16, flags uint32) {
	e := keybdEvt{typ: inputKeyboard, ki: keybdInput{wVk: vk, dwFlags: flags}}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&e)), 40)
}

type point struct{ x, y int32 }

func getCursor() point {
	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p
}

func screenWH() (int32, int32) {
	w, _, _ := procGetSystemMetrics.Call(smCxScreen)
	h, _, _ := procGetSystemMetrics.Call(smCyScreen)
	return int32(w), int32(h)
}

// moveAbs 把光标绝对定位到像素坐标（主屏 0–65535 归一化）。
func moveAbs(x, y int32) {
	w, h := screenWH()
	nx := int32(uint32(x) * 65535 / uint32(w-1))
	ny := int32(uint32(y) * 65535 / uint32(h-1))
	sendMouse(nx, ny, 0, mevMove|mevAbsolute)
}

func setMouseParams(p *[3]uint32) bool {
	r, _, _ := procSystemParametersInfoW.Call(spiSetMouse, 0, uintptr(unsafe.Pointer(p)), 0)
	return r != 0
}

func getMouseParams() ([3]uint32, bool) {
	var p [3]uint32
	r, _, _ := procSystemParametersInfoW.Call(spiGetMouse, 0, uintptr(unsafe.Pointer(&p)), 0)
	return p, r != 0
}

// circleError 在屏中心画 r=220px 的 96 段圆，逐段绝对定位并采样实际落点，
// 返回最大偏差与收尾误差（末点距起点）。
func circleError() (maxDev, closure float64) {
	const r = 220
	seg := 96
	w, h := screenWH()
	cx, cy := w/2, h/2
	moveAbs(cx, cy)
	time.Sleep(30 * time.Millisecond)
	start := getCursor()
	var last point
	for i := 0; i <= seg; i++ {
		ang := 2 * math.Pi * float64(i) / float64(seg)
		ix := cx + int32(r*math.Cos(ang))
		iy := cy + int32(r*math.Sin(ang))
		moveAbs(ix, iy)
		time.Sleep(3 * time.Millisecond)
		act := getCursor()
		dev := math.Hypot(float64(act.x-cx), float64(act.y-cy))
		if d := math.Abs(dev - r); d > maxDev {
			maxDev = d
		}
		last = act
	}
	closure = math.Hypot(float64(last.x-start.x), float64(last.y-start.y))
	return
}

func testPointerAccel() {
	fmt.Println("=== 测试 A：SendInput 绝对定位 vs 指针加速 ===")
	orig, ok := getMouseParams()
	fmt.Printf("当前加速参数 (threshold1,threshold2,speed) = %v（读参数 %v）\n", orig, ok)
	cases := []struct {
		name string
		p    [3]uint32
	}{
		{"当前配置", orig},
		{"加速关闭 {0,0,0}", [3]uint32{0, 0, 0}},
		{"加速开启 {6,10,1}", [3]uint32{6, 10, 1}},
	}
	for _, c := range cases {
		if !setMouseParams(&c.p) {
			fmt.Printf("  %s：SPI_SETMOUSE 失败，跳过\n", c.name)
			continue
		}
		time.Sleep(50 * time.Millisecond)
		maxDev, closure := circleError()
		fmt.Printf("  %s：最大半径偏差 %.1f px，收尾误差 %.1f px\n", c.name, maxDev, closure)
	}
	setMouseParams(&orig)
	fmt.Println("（判定：三种配置偏差同级且都很小 → 绝对定位绕开弹道加速）")
}

func socketRoundtrip(path string) {
	// AF_UNIX bind 不建父目录：实测父目录缺失时报 "dead network"，先补目录再探长度
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Printf("  路径(%d 字符) 建目录失败: %v\n", len(path), err)
		return
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		fmt.Printf("  路径(%d 字符) bind 失败: %v\n", len(path), err)
		return
	}
	type result struct {
		data byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			ch <- result{err: err}
			return
		}
		defer conn.Close()
		var buf [1]byte
		_, err = conn.Read(buf[:])
		ch <- result{data: buf[0], err: err}
	}()
	c, err := net.Dial("unix", path)
	if err != nil {
		fmt.Printf("  路径(%d 字符) dial 失败: %v\n", len(path), err)
		l.Close()
		return
	}
	c.Write([]byte{0xAB})
	r := <-ch
	c.Close()
	l.Close()
	if r.err != nil || r.data != 0xAB {
		fmt.Printf("  路径(%d 字符) 往返失败: data=%#x err=%v\n", len(path), r.data, r.err)
		return
	}
	_, statErr := os.Stat(path)
	leftover := statErr == nil
	if leftover {
		os.Remove(path)
	}
	fmt.Printf("  路径(%d 字符) 往返 OK；close 后文件残留=%v\n", len(path), leftover)
}

func testUnixSocket() {
	fmt.Println("=== 测试 B：AF_UNIX 控制通道 ===")
	socketRoundtrip(filepath.Join(os.TempDir(), "pl-spike.sock"))
	// 逐步加深逼近 AF_UNIX 路径长度上限（base 已含用户名，深度按 60 字符递增）
	base := os.TempDir()
	for n := 1; n <= 6; n++ {
		deep := filepath.Join(base, strings.Repeat("padlink-deep-dir/", n), "pl.sock")
		if len(deep) > 260 {
			break
		}
		socketRoundtrip(deep)
	}
	fmt.Println("（判定：往返回调全通过且 close 无残留 → 可直接用 net \"unix\"；bind 失败的长度即上限）")
}

// 全局内存读写字方向走 RtlMoveMemory：句柄（GlobalLock 返回值）保持 uintptr 直传，
// 只有「Go 指针 → uintptr」方向的转换——vet 的 unsafeptr 检查天然干净。
func setClipboardText(s string) error {
	u16, err := syscall.UTF16FromString(s)
	if err != nil {
		return err
	}
	size := len(u16) * 2
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(size))
	if h == 0 {
		return fmt.Errorf("GlobalAlloc 失败")
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return fmt.Errorf("GlobalLock 失败")
	}
	defer procGlobalUnlock.Call(h)
	buf := make([]byte, size)
	for i, v := range u16 {
		buf[i*2] = byte(v)
		buf[i*2+1] = byte(v >> 8)
	}
	procRtlMoveMemory.Call(p, uintptr(unsafe.Pointer(&buf[0])), uintptr(size))
	r, _, _ := procSetClipboardData.Call(cfUnicodeText, h)
	if r == 0 {
		return fmt.Errorf("SetClipboardData 失败")
	}
	return nil
}

func getClipboardText() (string, bool) {
	r, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if r == 0 {
		return "", false
	}
	sz, _, _ := procGlobalSize.Call(r)
	if sz == 0 || sz > 1<<20 {
		sz = 1 << 20
	}
	p, _, _ := procGlobalLock.Call(r)
	if p == 0 {
		return "", false
	}
	defer procGlobalUnlock.Call(r)
	buf := make([]byte, sz)
	procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&buf[0])), p, sz)
	// UTF-16 读到 NUL 为止
	u16 := make([]uint16, 0, sz/2)
	for i := 0; i+1 < len(buf); i += 2 {
		v := uint16(buf[i]) | uint16(buf[i+1])<<8
		if v == 0 {
			break
		}
		u16 = append(u16, v)
	}
	return syscall.UTF16ToString(u16), true
}

func clipboardRoundtrip() (ok bool, writeUs, readUs int64) {
	t0 := time.Now()
	r1, _, _ := procOpenClipboard.Call(0)
	if r1 == 0 {
		return false, 0, 0
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	if err := setClipboardText("padlink-剪贴板-测试-12345"); err != nil {
		return false, 0, 0
	}
	writeUs = time.Since(t0).Microseconds()

	t1 := time.Now()
	if r2, _, _ := procOpenClipboard.Call(0); r2 == 0 {
		return false, writeUs, 0
	}
	defer procCloseClipboard.Call()
	got, _ := getClipboardText()
	readUs = time.Since(t1).Microseconds()
	return got == "padlink-剪贴板-测试-12345", writeUs, readUs
}

func testClipboardEcho() {
	fmt.Println("=== 测试 C1：剪贴板写读往返（20 轮） ===")
	fail := 0
	var wMax, rMax int64
	for i := 0; i < 20; i++ {
		ok, w, r := clipboardRoundtrip()
		if !ok {
			fail++
			continue
		}
		if w > wMax {
			wMax = w
		}
		if r > rMax {
			rMax = r
		}
	}
	fmt.Printf("  失败 %d/20；单轮写+空剪贴板最大 %d µs，读最大 %d µs\n", fail, wMax, rMax)
	fmt.Println("（判定：0 失败且耗时为百微秒级 → Win32 API 同进程剪贴板无 wl-clipboard 类陷阱）")
}

func testClipboardPaste() {
	fmt.Println("=== 测试 C2：Ctrl+V 上屏（自动拉起记事本） ===")
	delay := *pasteDelay
	// 经计划任务投到交互桌面时无人在旁置焦点，自己拉起记事本（新窗口默认持焦点）
	if err := exec.Command("notepad").Start(); err != nil {
		fmt.Printf("启动记事本失败: %v（请手动开记事本后重跑 -paste）\n", err)
	}
	time.Sleep(2 * time.Second)

	orig, hadOrig := "", false
	if r, _, _ := procOpenClipboard.Call(0); r != 0 {
		if s, ok := getClipboardText(); ok {
			orig, hadOrig = s, true
		}
		procCloseClipboard.Call()
	}

	if r, _, _ := procOpenClipboard.Call(0); r != 0 {
		procEmptyClipboard.Call()
		setClipboardText("padlink粘贴测试-无多余换行")
		procCloseClipboard.Call()
	}

	sendKey(vkControl, 0)
	time.Sleep(20 * time.Millisecond)
	sendKey(vkV, 0)
	time.Sleep(20 * time.Millisecond)
	sendKey(vkV, kevKeyUp)
	sendKey(vkControl, kevKeyUp)

	time.Sleep(time.Duration(delay) * time.Millisecond)
	if r, _, _ := procOpenClipboard.Call(0); r != 0 {
		procEmptyClipboard.Call()
		if hadOrig {
			setClipboardText(orig)
		}
		procCloseClipboard.Call()
	}
	fmt.Printf("已注入 Ctrl+V 并在 %d ms 后恢复原剪贴板。\n", delay)
	// 上屏验证：枚举记事本全部子控件读文本（Win11 新记事本无经典 Edit 控件，
	// 枚举法两种代次都覆盖）
	time.Sleep(500 * time.Millisecond)
	clsNP, _ := syscall.UTF16PtrFromString("Notepad")
	np, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(clsNP)), 0)
	if np == 0 {
		fmt.Println("上屏验证：未找到记事本窗口，请人工核对")
		return
	}
	enumChildCb = syscall.NewCallback(enumChildFn)
	procEnumChildWindows.Call(np, enumChildCb, 0)
	fmt.Printf("上屏验证结论：命中 = %v\n", pasteHit.Load())
}

var (
	downCount atomic.Int32
	dblCount  atomic.Int32
	upCount   atomic.Int32
	userCount atomic.Int32
	wndProcCb uintptr
)

// 签名必须全指针尺寸（syscall.NewCallback 的硬约束）：msg 用 uint32 会错位读参，
// 消息码对不上号——首轮实测 DOWN 恒 0 的根因。
func wndProc(hwnd, msg, wp, lp uintptr) uintptr {
	switch msg {
	case wmLButtonDown:
		downCount.Add(1)
	case wmLButtonUp:
		upCount.Add(1)
	case wmLButtonDblClk:
		dblCount.Add(1)
	case wmUser:
		userCount.Add(1)
	case wmDestroy:
		procPostQuitMessage.Call(0)
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}

// pasteHit 由枚举回调置位：任一子控件文本包含测试串即算上屏成功
var pasteHit atomic.Bool
var enumChildCb uintptr

// enumChildCb 必须全指针尺寸（同 wndProc 的 NewCallback 约束）。
func enumChildFn(hwnd, lp uintptr) uintptr {
	var cls [64]uint16
	procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&cls[0])), 64)
	n, _, _ := procSendMessageW.Call(hwnd, wmGetTextLength, 0, 0)
	if n == 0 {
		return 1
	}
	buf := make([]uint16, n+1)
	procSendMessageW.Call(hwnd, wmGetText, uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
	text := syscall.UTF16ToString(buf)
	if strings.Contains(text, "padlink粘贴测试-无多余换行") {
		pasteHit.Store(true)
		fmt.Printf("  子控件 %q 命中: %q\n", syscall.UTF16ToString(cls[:]), text)
	} else {
		fmt.Printf("  子控件 %q: %q\n", syscall.UTF16ToString(cls[:]), text)
	}
	return 1
}

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   syscall.Handle
	icon       syscall.Handle
	cursor     syscall.Handle
	background uintptr
	menuName   *uint16
	className  *uint16
	iconSm     syscall.Handle
}

type msg struct {
	hwnd     syscall.Handle
	message  uint32
	wp, lp   uintptr
	time     uint32
	pt       point
	lPrivate uintptr
}

func testDoubleClick() {
	fmt.Println("=== 测试 D：系统双击合成 ===")
	dblTime, _, _ := procGetDoubleClickTime.Call()
	fmt.Printf("系统双击间隔阈值 = %d ms\n", dblTime)

	clsName, _ := syscall.UTF16PtrFromString("PLSpike")
	inst, _, _ := procGetModuleHandleW.Call(0)
	wc := wndClassEx{
		size:       uint32(unsafe.Sizeof(wndClassEx{})),
		style:      csDblClks,
		wndProc:    syscall.NewCallback(wndProc),
		instance:   syscall.Handle(inst),
		background: 6, // COLOR_WINDOW+1
		className:  clsName,
	}
	wndProcCb = wc.wndProc // 防回调被 GC
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		fmt.Printf("RegisterClassExW 失败: %v\n", err)
		return
	}
	title, _ := syscall.UTF16PtrFromString("winspike")
	// 窗口必须「创建线程 == 泵消息线程」（Win32 窗口消息投递到创建线程的队列），
	// goroutine 会漂移线程，所以创建+泵都锁在同一个 LockOSThread goroutine 里；
	// 位置放屏幕中央，光标点窗口客户区中心——否则点击落在窗外测不到事件。
	w, h := screenWH()
	wx, wy := (w-260)/2, (h-260)/2
	ready := make(chan syscall.Handle, 1)
	pumped := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		// WS_EX_TOPMOST(8)：计划任务起的进程受前台锁限制抢不到前台，非置顶会被
		// 用户正在用的窗口盖住——点击就落不到测试窗上（首轮实测踩坑）。
		hwnd, _, err := procCreateWindowExW.Call(8, uintptr(unsafe.Pointer(clsName)),
			uintptr(unsafe.Pointer(title)), wsOverlapped|wsVisible,
			uintptr(wx), uintptr(wy), 260, 260, 0, 0, inst, 0)
		if hwnd == 0 {
			fmt.Printf("CreateWindowExW 失败: %v\n", err)
			ready <- 0
			return
		}
		ready <- syscall.Handle(hwnd)
		var m msg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if r == 0 || uintptr(r) == ^uintptr(0) {
				break
			}
			// 取到消息必须 Dispatch，wndProc 才会被调（首轮实测漏了这步，
			// 连 PostMessage 的自检计数都停在 0）
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
		close(pumped)
	}()
	hwnd := <-ready
	if hwnd == 0 {
		return
	}
	time.Sleep(300 * time.Millisecond)

	moveAbs(wx+130, wy+150)
	time.Sleep(100 * time.Millisecond)
	// 诊断：这个点最顶层窗口是谁（POINT 8 字节按值传参，x64/arm64 均占单个寄存器槽）
	ptArg := uintptr(uint32(wx+130)) | uintptr(uint32(wy+150))<<32
	wh, _, _ := procWindowFromPoint.Call(ptArg)
	var cls [64]uint16
	procGetClassNameW.Call(wh, uintptr(unsafe.Pointer(&cls[0])), 64)
	fmt.Printf("点击点顶层窗口类 = %q（hwnd=%#x，应为 PLSpike）\n", syscall.UTF16ToString(cls[:]), wh)

	// 泵自检：PostMessage USER 上来，先证明「窗口+回调+消息泵」链路本身通，
	// 把泵故障与 SendInput 投递故障隔离开
	procPostMessageW.Call(uintptr(hwnd), wmUser, 0x123, 0)
	time.Sleep(200 * time.Millisecond)
	fmt.Printf("泵自检（PostMessage WM_USER）：userCount=%d（应=1）\n", userCount.Load())

	// 双击：4 段事件间隔远小于阈值
	click := func(down bool) {
		f := uint32(mevLeftDown)
		if !down {
			f = mevLeftUp
		}
		sendMouse(0, 0, 0, f)
	}
	downCount.Store(0)
	upCount.Store(0)
	dblCount.Store(0)
	click(true)
	time.Sleep(30 * time.Millisecond)
	click(false)
	time.Sleep(80 * time.Millisecond)
	click(true)
	time.Sleep(30 * time.Millisecond)
	click(false)
	time.Sleep(400 * time.Millisecond)
	fmt.Printf("快速双击（80ms 间隔 < %dms 阈值）：DOWN=%d UP=%d DBLCLK=%d\n",
		dblTime, downCount.Load(), upCount.Load(), dblCount.Load())

	// 两次单击：间隔大于阈值
	downCount.Store(0)
	upCount.Store(0)
	dblCount.Store(0)
	click(true)
	time.Sleep(30 * time.Millisecond)
	click(false)
	time.Sleep(time.Duration(dblTime+300) * time.Millisecond)
	click(true)
	time.Sleep(30 * time.Millisecond)
	click(false)
	time.Sleep(400 * time.Millisecond)
	fmt.Printf("慢速两次单击（间隔 > %dms 阈值）：DOWN=%d UP=%d DBLCLK=%d\n",
		dblTime, downCount.Load(), upCount.Load(), dblCount.Load())

	procDestroyWindow.Call(uintptr(hwnd))
	<-pumped
	fmt.Println("（判定：双击时 DBLCLK 替换第二次 DOWN → 系统已代管点击计数，注入端无需自合成；")
	fmt.Println("        接收窗口未开 CS_DBLCLKS 时只收普通 DOWN，两种应用都能正常工作）")
}

var pasteDelay = flag.Duration("paste-delay", 800, "Ctrl+V 后恢复剪贴板的延迟（ms）")

func main() {
	paste := flag.Bool("paste", false, "只跑剪贴板 Ctrl+V 手动验证（其余测试跳过）")
	out := flag.String("out", "", "输出同时写入该文件（计划任务投递用：schtasks 起的进程有独立控制台，stdout 无人看见）")
	flag.Parse()
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fmt.Printf("打开输出文件失败: %v\n", err)
			os.Exit(1)
		}
		// os.Stdout 是 *os.File 具体类型，不能塞 MultiWriter；改用管道：
		// 换上新写端，goroutine 从读端向「原终端 + 文件」双写（测试函数都走 fmt→os.Stdout）
		r, w, err := os.Pipe()
		if err != nil {
			fmt.Printf("创建管道失败: %v\n", err)
			os.Exit(1)
		}
		orig := os.Stdout
		os.Stdout = w
		go func() {
			io.Copy(io.MultiWriter(orig, f), r)
			r.Close()
		}()
		defer func() {
			w.Close()
			time.Sleep(200 * time.Millisecond) // 排空管道再退出，防尾段丢失
			f.Close()
		}()
	}
	fmt.Printf("winspike  target=windows/%s  tmp=%s\n", runtime.GOARCH, os.TempDir())
	if *paste {
		testClipboardPaste()
		return
	}
	testPointerAccel()
	testUnixSocket()
	testClipboardEcho()
	testDoubleClick()
	fmt.Println("\n=== 全部自动测试完成 ===")
}
