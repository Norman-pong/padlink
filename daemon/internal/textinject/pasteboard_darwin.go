//go:build darwin

package textinject

/*
#cgo LDFLAGS: -framework AppKit -framework Foundation
#include <stdlib.h>

// 实现见 pasteboard_darwin.m（cgo 的 .c 文件按 C 编译，调不到 AppKit 类方法，故独立 ObjC 文件）。
long plPasteboardSnapshot(void **outBuf);
int plPasteboardRestore(const void *buf, long len);
int plPasteboardSetText(const char *utf8);
int plPasteboardClear(void);
void plPasteboardFree(void *p);
*/
import "C"

import (
	"errors"
	"unsafe"
)

// 可替换以便测试：单元测试替换这些变量，绝不触碰真实粘贴板（替换方负责 t.Cleanup 还原）。
var (
	pbSnapshot = nativeSnapshot
	pbRestore  = nativeRestore
	pbSetText  = nativeSetText
	pbClear    = nativeClear
)

func nativeSnapshot() ([]byte, error) {
	var buf unsafe.Pointer
	n := C.plPasteboardSnapshot(&buf)
	if n < 0 {
		return nil, errors.New("NSPasteboard 快照失败")
	}
	defer C.plPasteboardFree(buf)
	if n == 0 {
		return []byte{}, nil
	}
	return C.GoBytes(buf, C.int(n)), nil
}

func nativeRestore(blob []byte) error {
	c := C.CBytes(blob)
	defer C.free(c)
	if C.plPasteboardRestore(c, C.long(len(blob))) != 0 {
		return errors.New("NSPasteboard 恢复失败")
	}
	return nil
}

func nativeSetText(text string) error {
	cs := C.CString(text)
	defer C.free(unsafe.Pointer(cs))
	if C.plPasteboardSetText(cs) != 0 {
		return errors.New("NSPasteboard 写入失败")
	}
	return nil
}

func nativeClear() error {
	if C.plPasteboardClear() != 0 {
		return errors.New("NSPasteboard 清空失败")
	}
	return nil
}
