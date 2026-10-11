// PadLink macOS 应用图标生成器：把鸿蒙端的全幅方形图标处理成 macOS 规范形态。
//
// 输入是 apps/harmony/AppScope/resources/base/media/background.png（1024×1024 全幅方形）
// ——鸿蒙的分层图标里 foreground.png 是空白层，实际构图全在 background 上。
// macOS 的图标不是一个满幅方块，而是「内容区 + 透明边距 + 圆角」：
// 1024 画布里内容占 824（约 80.5%），圆角半径 824×0.225 ≈ 185.4（Apple 的 macOS 图标模板几何）。
//
// 用法：clang -framework AppKit -framework CoreGraphics -o /tmp/make-appicon make-appicon.m
//       /tmp/make-appicon <源png> <输出png> [画布边长=1024] [内容占比=0.8047]
// 产物提交到 packaging/macos/PadLink.app.icon.png；安装脚本再用系统自带的 sips + iconutil
// 从它生成 .icns（终端用户无需 clang）。
#import <AppKit/AppKit.h>
#import <CoreGraphics/CoreGraphics.h>
#import <ImageIO/ImageIO.h>
#include <stdio.h>
#include <stdlib.h>

int main(int argc, const char *argv[]) {
    @autoreleasepool {
        if (argc < 3) {
            fprintf(stderr, "用法: %s <源png> <输出png> [画布边长=1024] [内容占比=0.8047]\n", argv[0]);
            return 2;
        }
        NSString *srcPath = [NSString stringWithUTF8String:argv[1]];
        NSString *outPath = [NSString stringWithUTF8String:argv[2]];
        size_t canvas = argc > 3 ? (size_t)atoi(argv[3]) : 1024;
        double ratio = argc > 4 ? atof(argv[4]) : 0.8047;

        CGImageSourceRef src = CGImageSourceCreateWithURL(
            (__bridge CFURLRef)[NSURL fileURLWithPath:srcPath], NULL);
        if (!src) {
            fprintf(stderr, "读不到源图: %s\n", argv[1]);
            return 1;
        }
        CGImageRef img = CGImageSourceCreateImageAtIndex(src, 0, NULL);
        if (!img) {
            fprintf(stderr, "解码失败: %s\n", argv[1]);
            CFRelease(src);
            return 1;
        }

        CGColorSpaceRef cs = CGColorSpaceCreateDeviceRGB();
        CGBitmapInfo info = (CGBitmapInfo)kCGImageAlphaPremultipliedLast;
        CGContextRef ctx = CGBitmapContextCreate(NULL, canvas, canvas, 8, 0, cs, info);
        if (!ctx) {
            fprintf(stderr, "创建位图上下文失败\n");
            return 1;
        }
        double inset = (double)canvas * (1.0 - ratio) / 2.0;
        double side = (double)canvas - 2.0 * inset;
        double radius = side * 0.225; // 824 → 185.4，对齐 macOS 图标模板
        CGRect r = CGRectMake(inset, inset, side, side);
        CGPathRef path = CGPathCreateWithRoundedRect(r, radius, radius, NULL);
        CGContextAddPath(ctx, path);
        CGContextClip(ctx);
        CGContextSetInterpolationQuality(ctx, kCGInterpolationHigh);
        CGContextDrawImage(ctx, r, img); // CG 原点在左下，图像方向天然正确
        CGImageRef out = CGBitmapContextCreateImage(ctx);

        CGImageDestinationRef dst = CGImageDestinationCreateWithURL(
            (__bridge CFURLRef)[NSURL fileURLWithPath:outPath], CFSTR("public.png"), 1, NULL);
        if (!dst) {
            fprintf(stderr, "创建 PNG 目标失败: %s\n", argv[2]);
            return 1;
        }
        // CGImageDestinationAddImage 返回 void，不能参与布尔判断
        CGImageDestinationAddImage(dst, out, NULL);
        if (!CGImageDestinationFinalize(dst)) {
            fprintf(stderr, "写出失败: %s\n", argv[2]);
            return 1;
        }
        printf("已生成 %s（%zux%zu，内容 %ldpx，圆角 %.1f）\n", argv[2], canvas, canvas,
               (long)side, radius);
        CGImageRelease(out);
        CGContextRelease(ctx);
        CGColorSpaceRelease(cs);
        CGPathRelease(path);
        CGImageRelease(img);
        CFRelease(src);
        CFRelease(dst);
        return 0;
    }
}
