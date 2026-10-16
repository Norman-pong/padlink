//go:build darwin

#import <AppKit/AppKit.h>

// 配对码弹窗：NSTimer 每秒刷新倒计时，归零 abortModal 自行关闭。
// 在 osascript 里用 ASOC 脚本对象做 timer target 会因对象桥接缺陷全部 -1728，
// 故弹窗改由本守护进程的隐藏子命令（_pairing-dialog）以原生 ObjC 呈现。
@interface PLPairingDialog : NSObject
@property (nonatomic, strong) NSTextField *hint;
@property (nonatomic, copy) NSString *pre;
@property (nonatomic, copy) NSString *suf;
@property (nonatomic) long remaining;
@end

@implementation PLPairingDialog
- (void)tick:(NSTimer *)timer {
	self.remaining -= 1;
	if (self.remaining < 1) {
		[timer invalidate];
		[NSApp abortModal];
		return;
	}
	self.hint.stringValue =
		[NSString stringWithFormat:@"%@%ld%@", self.pre, self.remaining, self.suf];
}
@end

static NSTextField *PLMakeLabel(NSRect frame, CGFloat size, NSColor *color, BOOL selectable) {
	NSTextField *tf = [[NSTextField alloc] initWithFrame:frame];
	tf.bezeled = NO;
	tf.drawsBackground = NO;
	tf.editable = NO;
	tf.selectable = selectable;
	tf.font = [NSFont systemFontOfSize:size];
	if (color != nil) {
		tf.textColor = color;
	}
	tf.alignment = NSTextAlignmentCenter;
	return tf;
}

// 返回 0 = 正常展示并关闭（点击或倒计时归零）；非 0 = 启动失败，调用方走回落。
int padlink_show_pairing_dialog(const char *codeC, const char *iconC, const char *preC,
								const char *sufC, const char *okC, int ttlSec) {
	@autoreleasepool {
		if (codeC == NULL || preC == NULL || sufC == NULL || okC == NULL || ttlSec < 1) {
			return 1;
		}
		NSString *code = [NSString stringWithUTF8String:codeC];
		NSString *pre = [NSString stringWithUTF8String:preC];
		NSString *suf = [NSString stringWithUTF8String:sufC];
		NSString *ok = [NSString stringWithUTF8String:okC];
		NSString *iconPath = iconC != NULL ? [NSString stringWithUTF8String:iconC] : nil;

		// 后台进程不激活则弹窗不可见（与 osascript 版本同口径）。
		NSApplication *app = [NSApplication sharedApplication];
		[app setActivationPolicy:NSApplicationActivationPolicyAccessory];
		[app activateIgnoringOtherApps:YES];

		BOOL hasIcon = NO;
		NSImage *logo = nil;
		if (iconPath.length > 0) {
			logo = [[NSImage alloc] initWithContentsOfFile:iconPath];
			hasIcon = logo != nil;
		}

		NSAlert *alert = [[NSAlert alloc] init];
		alert.messageText = @"";
		alert.informativeText = @"";
		alert.icon = [[NSImage alloc] init];

		CGFloat boxH = hasIcon ? 230 : 124;
		NSView *box = [[NSView alloc] initWithFrame:NSMakeRect(0, 0, 300, boxH)];

		if (hasIcon) {
			NSImageView *logoView =
				[[NSImageView alloc] initWithFrame:NSMakeRect(95, 116, 110, 110)];
			logoView.image = logo;
			logoView.imageScaling = NSImageScaleProportionallyUpOrDown;
			[box addSubview:logoView];
		}

		NSTextField *title = PLMakeLabel(NSMakeRect(0, 88, 300, 22), 16,
										 [NSColor labelColor], NO);
		title.font = [NSFont systemFontOfSize:16 weight:NSFontWeightSemibold];
		title.stringValue = @"PadLink";
		[box addSubview:title];

		NSTextField *codeField = PLMakeLabel(NSMakeRect(0, 38, 300, 46), 40,
											 [NSColor labelColor], YES);
		codeField.font = [NSFont monospacedDigitSystemFontOfSize:40
														  weight:NSFontWeightSemibold];
		codeField.stringValue = code;
		[box addSubview:codeField];

		NSTextField *hint = PLMakeLabel(NSMakeRect(0, 16, 300, 16), 12,
										[NSColor secondaryLabelColor], NO);
		hint.stringValue =
			[NSString stringWithFormat:@"%@%d%@", pre, ttlSec, suf];
		[box addSubview:hint];

		alert.accessoryView = box;
		[alert addButtonWithTitle:ok];

		PLPairingDialog *ctl = [[PLPairingDialog alloc] init];
		ctl.hint = hint;
		ctl.pre = pre;
		ctl.suf = suf;
		ctl.remaining = ttlSec;
		NSTimer *timer = [NSTimer timerWithTimeInterval:1.0
												 target:ctl
											   selector:@selector(tick:)
											   userInfo:nil
												repeats:YES];
		// scheduledTimer 挂默认 mode，模态会话只跑 NSModalPanelRunLoopMode，必须显式挂该 mode。
		[[NSRunLoop currentRunLoop] addTimer:timer forMode:NSModalPanelRunLoopMode];

		[alert runModal];
		[timer invalidate];
		return 0;
	}
}
