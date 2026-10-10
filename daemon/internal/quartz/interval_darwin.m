//go:build darwin

// NSEvent.doubleClickInterval 取值（系统"双击速度"设置，默认 0.5s）。
// 独立 Objective-C 文件承载：cgo 的 .c 文件按 C 编译，调不到 AppKit 类方法。
// 实测非 GUI 进程可直接取值（本机 clang -framework AppKit 复现），
// 取不到时返回 0，由 Go 侧回落 inject.DefaultDoubleClickInterval。
#import <AppKit/AppKit.h>

double padlinkDoubleClickInterval(void) {
	@autoreleasepool {
		return (double)[NSEvent doubleClickInterval];
	}
}
