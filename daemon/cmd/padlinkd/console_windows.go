//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

var kernel32Con = syscall.NewLazyDLL("kernel32.dll")

func init() { fixConsoleHook = attachParentConsole }

// attachParentConsole 把 windowsgui 子系统的进程接回父控制台（交互调试场景）：
// 由 Run 键/计划任务/隐藏启动拉起时无父控制台，保持无窗口静默（日志文件为准）；
// 从终端手动运行（padlinkd -v / --test）时 Attach 后重开标准句柄，输出可见。
// 必须在 main 最早处调用——os.Stdout 在 runtime 初始化时已定型，晚了对已缓存句柄无效。
func attachParentConsole() {
	// 已有有效标准输出（cmd 重定向/句柄继承）时绝不 Attach——否则会把指向文件的
	// stdout 换成 CONOUT$，`--test > file` 的重定向失效（实测踩坑）
	const stdOutputHandle = ^uintptr(10) // uintptr(-11) = STD_OUTPUT_HANDLE
	h, _, _ := kernel32Con.NewProc("GetStdHandle").Call(stdOutputHandle)
	if h != 0 && h != ^uintptr(0) {
		return
	}
	const attachParentProcess = ^uintptr(0) // ATTACH_PARENT_PROCESS
	r, _, _ := kernel32Con.NewProc("AttachConsole").Call(attachParentProcess)
	if r == 0 {
		return
	}
	if f := openCon("CONOUT$"); f != nil {
		os.Stdout = f
		os.Stderr = f
	}
	if f := openCon("CONIN$"); f != nil {
		os.Stdin = f
	}
}

func openCon(name string) *os.File {
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil
	}
	h, _, _ := kernel32Con.NewProc("CreateFileW").Call(
		uintptr(unsafe.Pointer(p)),
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ|syscall.GENERIC_WRITE,
		0, syscall.OPEN_EXISTING, 0, 0)
	if h == ^uintptr(0) || h == 0 {
		return nil
	}
	return os.NewFile(h, name)
}
