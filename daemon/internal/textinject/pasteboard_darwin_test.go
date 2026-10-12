//go:build darwin

package textinject

import (
	"bytes"
	"os"
	"testing"
)

// TestPasteboardNativeRoundTrip 真机手动验收：显式设置 PADLINK_PB_ROUNDTRIP=1 才运行——
// 它会真实读写系统粘贴板（含用户内容），不得进入常规 go test。
// 覆盖：非文本板快照 → 写文本 → 恢复 → 字节级一致；清空 → 再恢复 → 仍一致。
func TestPasteboardNativeRoundTrip(t *testing.T) {
	if os.Getenv("PADLINK_PB_ROUNDTRIP") != "1" {
		t.Skip("设置 PADLINK_PB_ROUNDTRIP=1 运行真机粘贴板往返验收")
	}
	snap1, err := nativeSnapshot()
	if err != nil {
		t.Fatalf("快照: %v", err)
	}
	if err := nativeSetText("padlink-roundtrip-probe"); err != nil {
		t.Fatalf("写文本: %v", err)
	}
	if err := nativeRestore(snap1); err != nil {
		t.Fatalf("恢复: %v", err)
	}
	snap2, err := nativeSnapshot()
	if err != nil {
		t.Fatalf("二次快照: %v", err)
	}
	if !bytes.Equal(snap1, snap2) {
		t.Fatalf("写文本后恢复不一致：%d vs %d 字节", len(snap1), len(snap2))
	}
	if err := nativeClear(); err != nil {
		t.Fatalf("清空: %v", err)
	}
	if err := nativeRestore(snap1); err != nil {
		t.Fatalf("从空板恢复: %v", err)
	}
	snap3, err := nativeSnapshot()
	if err != nil {
		t.Fatalf("三次快照: %v", err)
	}
	if !bytes.Equal(snap1, snap3) {
		t.Fatalf("空板后恢复不一致：%d vs %d 字节", len(snap1), len(snap3))
	}
	t.Logf("粘贴板往返字节级一致（快照 %d 字节）", len(snap1))
}
