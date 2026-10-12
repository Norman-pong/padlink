package session

import (
	"testing"
	"time"
)

func TestRateBucketBurstAndRefill(t *testing.T) {
	b := newRateBucket(10, 3) // 每秒 10 个、容量 3
	t0 := time.Unix(1000, 0)
	for i := 0; i < 3; i++ {
		if !b.allow(t0) {
			t.Fatalf("容量内第 %d 个应放行", i+1)
		}
	}
	if b.allow(t0) {
		t.Fatal("超出容量应拒绝")
	}
	if !b.allow(t0.Add(150 * time.Millisecond)) { // 0.15s × 10/s = 1.5 令牌
		t.Fatal("补充后应放行")
	}
	if b.allow(t0.Add(150 * time.Millisecond)) {
		t.Fatal("仅补充 1 枚，第二个应拒绝")
	}
}

func TestRateBucketRefundCappedAtBurst(t *testing.T) {
	b := newRateBucket(1, 2)
	t0 := time.Unix(1000, 0)
	b.allow(t0) // 2→1
	b.refund()  // 1→2
	b.refund()  // 容量上限，不累积
	b.refund()
	if !b.allow(t0) {
		t.Fatal("补回的第一枚应可用")
	}
	if !b.allow(t0) {
		t.Fatal("容量内的第二枚应可用")
	}
	if b.allow(t0) {
		t.Fatal("耗尽后应拒绝")
	}
}
