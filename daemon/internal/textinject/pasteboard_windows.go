//go:build windows

package textinject

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	user32Clip           = syscall.NewLazyDLL("user32.dll")
	procOpenClipboard    = user32Clip.NewProc("OpenClipboard")
	procCloseClipboard   = user32Clip.NewProc("CloseClipboard")
	procEmptyClipboard   = user32Clip.NewProc("EmptyClipboard")
	procGetClipboardData = user32Clip.NewProc("GetClipboardData")
	procSetClipboardData = user32Clip.NewProc("SetClipboardData")
	procEnumClipboardFmt = user32Clip.NewProc("EnumClipboardFormats")
	kernel32Clip         = syscall.NewLazyDLL("kernel32.dll")
	procGlobalAllocClip  = kernel32Clip.NewProc("GlobalAlloc")
	procGlobalLockClip   = kernel32Clip.NewProc("GlobalLock")
	procGlobalUnlockClip = kernel32Clip.NewProc("GlobalUnlock")
	procGlobalSizeClip   = kernel32Clip.NewProc("GlobalSize")
	procRtlMoveMemoryC   = kernel32Clip.NewProc("RtlMoveMemory")
)

const (
	cfText        = 1
	cfOemText     = 7
	cfUnicodeText = 13
	cfLocale      = 16
	gmemMoveableC = 0x0002
)

// 可替换以便测试：单元测试替换这些变量，绝不触碰真实剪贴板（替换方负责 t.Cleanup 还原）。
var (
	winSnapshot = nativeSnapshot
	winRestore  = nativeRestore
	winSetText  = nativeSetText
	winClear    = nativeClear
)

// winClipboard：Windows 原生剪贴板后端。备份为文本级快照——枚举全部格式，出现任何
// 非文本格式（图片/文件等）即按两端共同契约取消注入（同 darwin 的 ErrNonText 语义，
// 绝不用空备份覆盖原内容）；注入与恢复走 CF_UNICODETEXT。
var winClipboard = clipboard{
	name: "Win32 剪贴板",
	hint: "Windows 系统自带，失败请查日志",
	readMIME: func(Runner) (string, error) {
		return pbSnapshotType, nil
	},
	read: func(_ Runner, mime string) ([]byte, error) {
		if mime != pbSnapshotType {
			return nil, fmt.Errorf("备份类型标记异常: %q", mime)
		}
		return winSnapshot()
	},
	write: func(_ Runner, data []byte, mime string) error {
		if mime == pbSnapshotType {
			return winRestore(string(data))
		}
		return winSetText(string(data))
	},
	clear: func(Runner) error {
		return winClear()
	},
}

// openClip 打开剪贴板并带重试：剪贴板被其他程序短暂占有时 OpenClipboard 立即失败是常态。
func openClip() error {
	for i := 0; i < 5; i++ {
		if r, _, _ := procOpenClipboard.Call(0); r != 0 {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("OpenClipboard 失败（连续重试被占用）")
}

// 全局内存读写字方向走 RtlMoveMemory：句柄保持 uintptr 直传，只有「Go 指针 → uintptr」
// 方向的转换（vet unsafeptr 干净，同 winspike 模式）。
func readGlobal(h uintptr) []byte {
	sz, _, _ := procGlobalSizeClip.Call(h)
	if sz == 0 || sz > 1<<22 {
		sz = 1 << 22
	}
	p, _, _ := procGlobalLockClip.Call(h)
	if p == 0 {
		return nil
	}
	defer procGlobalUnlockClip.Call(h)
	buf := make([]byte, sz)
	procRtlMoveMemoryC.Call(uintptr(unsafe.Pointer(&buf[0])), p, sz)
	return buf
}

func utf16ToString(b []byte) string {
	u16 := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		v := uint16(b[i]) | uint16(b[i+1])<<8
		if v == 0 {
			break
		}
		u16 = append(u16, v)
	}
	return syscall.UTF16ToString(u16)
}

// nativeSnapshot 备份当前剪贴板：仅文本格式（CF_TEXT/OEMTEXT/UNICODETEXT/LOCALE）可备份，
// 出现任何其他格式返回 ErrNonText（注入在调用方被取消）；空剪贴板返回空快照（可精确恢复为空）。
func nativeSnapshot() ([]byte, error) {
	if err := openClip(); err != nil {
		return nil, err
	}
	defer procCloseClipboard.Call()
	for f, _, _ := procEnumClipboardFmt.Call(0); f != 0; f, _, _ = procEnumClipboardFmt.Call(f) {
		switch f {
		case cfText, cfOemText, cfUnicodeText, cfLocale:
		default:
			return nil, fmt.Errorf("%w：剪贴板含非文本格式 %d", ErrNonText, f)
		}
	}
	if h, _, _ := procGetClipboardData.Call(cfUnicodeText); h != 0 {
		return []byte(utf16ToString(readGlobal(h))), nil
	}
	if h, _, _ := procGetClipboardData.Call(cfText); h != 0 {
		// ANSI 文本（罕见遗留）：按字节近似读取，M2 已知边界
		return readGlobal(h), nil
	}
	return []byte{}, nil
}

// writeClipboardText 打开剪贴板、清空并写入 UTF-8 文本（转 UTF-16LE + NUL）。
func writeClipboardText(s string) error {
	if err := openClip(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()
	if r, _, _ := procEmptyClipboard.Call(); r == 0 {
		return errors.New("EmptyClipboard 失败")
	}
	if s == "" {
		return nil
	}
	u16 := utf16.Encode([]rune(s))
	u16 = append(u16, 0)
	size := len(u16) * 2
	h, _, _ := procGlobalAllocClip.Call(gmemMoveableC, uintptr(size))
	if h == 0 {
		return errors.New("GlobalAlloc 失败")
	}
	p, _, _ := procGlobalLockClip.Call(h)
	if p == 0 {
		return errors.New("GlobalLock 失败")
	}
	buf := make([]byte, size)
	for i, v := range u16 {
		buf[i*2] = byte(v)
		buf[i*2+1] = byte(v >> 8)
	}
	procRtlMoveMemoryC.Call(p, uintptr(unsafe.Pointer(&buf[0])), uintptr(size))
	procGlobalUnlockClip.Call(h)
	if r, _, _ := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		return errors.New("SetClipboardData 失败")
	}
	return nil
}

// nativeRestore 恢复备份：空快照恢复为空剪贴板，否则按文本写回。
func nativeRestore(blob string) error {
	return writeClipboardText(blob)
}

func nativeSetText(text string) error {
	return writeClipboardText(text)
}

func nativeClear() error {
	if err := openClip(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()
	if r, _, _ := procEmptyClipboard.Call(); r == 0 {
		return errors.New("EmptyClipboard 失败")
	}
	return nil
}

// muSerial 串行化剪贴板打开（Windows 剪贴板是单打开者资源，同进程并发打开必失败）。
var muSerial sync.Mutex

func init() {
	// Inject 的备份/写入/恢复序列可能被并发语音文本触发，进程内先串行化
	// （跨进程竞争由 openClip 重试兜底）。
	origSnapshot, origRestore, origSetText, origClear := winSnapshot, winRestore, winSetText, winClear
	winSnapshot = func() ([]byte, error) {
		muSerial.Lock()
		defer muSerial.Unlock()
		return origSnapshot()
	}
	winRestore = func(s string) error {
		muSerial.Lock()
		defer muSerial.Unlock()
		return origRestore(s)
	}
	winSetText = func(s string) error {
		muSerial.Lock()
		defer muSerial.Unlock()
		return origSetText(s)
	}
	winClear = func() error {
		muSerial.Lock()
		defer muSerial.Unlock()
		return origClear()
	}
}
