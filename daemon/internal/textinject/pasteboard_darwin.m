//go:build darwin

// macOS 粘贴板全保真快照/恢复（NSPasteboard 原生 API）。
// 序列化格式（大端）：u32 条目数；每条目 u32 类型数；每类型 u32 名长 + 名(UTF8) + u32 数据长 + 数据。
// 只收录 dataForType 能读出的类型：惰性/承诺型（如文件 promise）读不出，跳过——恢复后其余类型仍原样。
#import <AppKit/AppKit.h>
#import <stdint.h>
#import <stdlib.h>
#import <string.h>

static void appendU32(NSMutableData *buf, uint32_t v) {
	uint8_t b[4] = {(uint8_t)(v >> 24), (uint8_t)(v >> 16), (uint8_t)(v >> 8), (uint8_t)v};
	[buf appendBytes:b length:4];
}

static int readU32(const uint8_t **p, const uint8_t *end, uint32_t *v) {
	if (end - *p < 4) {
		return -1;
	}
	*v = ((uint32_t)(*p)[0] << 24) | ((uint32_t)(*p)[1] << 16) | ((uint32_t)(*p)[2] << 8) | (uint32_t)(*p)[3];
	*p += 4;
	return 0;
}

long plPasteboardSnapshot(void **outBuf) {
	*outBuf = NULL;
	@autoreleasepool {
		NSArray<NSPasteboardItem *> *items = [[NSPasteboard generalPasteboard] pasteboardItems];
		NSMutableData *buf = [NSMutableData data];
		appendU32(buf, (uint32_t)items.count);
		for (NSPasteboardItem *item in items) {
			NSMutableArray<NSString *> *types = [NSMutableArray array];
			NSMutableArray<NSData *> *datas = [NSMutableArray array];
			for (NSPasteboardType t in [item types]) {
				NSData *d = [item dataForType:t];
				if (d == nil) {
					continue;
				}
				[types addObject:t];
				[datas addObject:d];
			}
			appendU32(buf, (uint32_t)types.count);
			for (NSUInteger i = 0; i < types.count; i++) {
				NSData *name = [types[i] dataUsingEncoding:NSUTF8StringEncoding];
				NSData *d = datas[i];
				appendU32(buf, (uint32_t)name.length);
				[buf appendData:name];
				appendU32(buf, (uint32_t)d.length);
				[buf appendData:d];
			}
		}
		void *out = malloc(buf.length > 0 ? buf.length : 1);
		if (out == NULL) {
			return -1;
		}
		memcpy(out, buf.bytes, buf.length);
		*outBuf = out;
		return (long)buf.length;
	}
}

int plPasteboardRestore(const void *buf, long len) {
	@autoreleasepool {
		const uint8_t *p = buf;
		const uint8_t *end = p + len;
		uint32_t nItems = 0;
		if (readU32(&p, end, &nItems) != 0) {
			return -1;
		}
		NSMutableArray<NSPasteboardItem *> *items = [NSMutableArray arrayWithCapacity:nItems];
		for (uint32_t i = 0; i < nItems; i++) {
			uint32_t nTypes = 0;
			if (readU32(&p, end, &nTypes) != 0) {
				return -1;
			}
			NSPasteboardItem *item = [[[NSPasteboardItem alloc] init] autorelease];
			for (uint32_t j = 0; j < nTypes; j++) {
				uint32_t nameLen = 0;
				uint32_t dataLen = 0;
				if (readU32(&p, end, &nameLen) != 0 || end - p < (long)nameLen) {
					return -1;
				}
				NSString *type = [[[NSString alloc] initWithBytes:p length:nameLen
					encoding:NSUTF8StringEncoding] autorelease];
				p += nameLen;
				if (readU32(&p, end, &dataLen) != 0 || end - p < (long)dataLen) {
					return -1;
				}
				NSData *d = [NSData dataWithBytes:p length:dataLen];
				p += dataLen;
				if (type == nil || ![item setData:d forType:type]) {
					return -1;
				}
			}
			[items addObject:item];
		}
		if (p != end) {
			return -1; // 尾部多余字节：格式不符，拒绝恢复以免写脏
		}
		NSPasteboard *pb = [NSPasteboard generalPasteboard];
		[pb clearContents];
		if (nItems == 0) {
			return 0; // 空快照 = 清空收尾（writeObjects 空数组行为不保证）
		}
		return [pb writeObjects:items] ? 0 : -1;
	}
}

int plPasteboardSetText(const char *utf8) {
	@autoreleasepool {
		NSString *s = [NSString stringWithUTF8String:utf8];
		if (s == nil) {
			return -1;
		}
		NSPasteboard *pb = [NSPasteboard generalPasteboard];
		[pb clearContents];
		return [pb setString:s forType:NSPasteboardTypeString] ? 0 : -1;
	}
}

int plPasteboardClear(void) {
	@autoreleasepool {
		[[NSPasteboard generalPasteboard] clearContents];
		return 0;
	}
}

void plPasteboardFree(void *p) {
	free(p);
}
