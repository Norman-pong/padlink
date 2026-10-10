//go:build darwin

package quartz

import (
	"testing"
	"time"
)

// 双击间隔必须落在合理区间，或返回 0 表示不可用（调用方回落 inject 默认 500ms）。
// 断言不绑定具体系统设置值（用户可改"双击速度"），只防单位换算错误。
func TestDoubleClickIntervalContract(t *testing.T) {
	d := DoubleClickInterval()
	if d != 0 && (d < 100*time.Millisecond || d > 2*time.Second) {
		t.Fatalf("DoubleClickInterval = %v 越界（应 ∈[100ms,2s] 或 0）", d)
	}
	t.Logf("系统双击间隔 = %v（0 = 不可用，回落 inject.DefaultDoubleClickInterval）", d)
}
