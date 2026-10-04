package main

import (
	"testing"
	"time"
)

func TestQuantileSorted(t *testing.T) {
	s := []float64{1, 2, 3, 4}
	if got := quantileSorted(s, 0.5); got != 2.5 {
		t.Errorf("q50 = %v, want 2.5", got)
	}
	if got := quantileSorted(s, 0); got != 1 {
		t.Errorf("q0 = %v, want 1", got)
	}
	if got := quantileSorted(s, 1); got != 4 {
		t.Errorf("q1 = %v, want 4", got)
	}
	if got := quantileSorted(s, 0.25); got != 1.75 {
		t.Errorf("q25 = %v, want 1.75", got)
	}
	if got := quantileSorted([]float64{7}, 0.99); got != 7 {
		t.Errorf("单样本 q99 = %v, want 7", got)
	}
	if got := quantileSorted(nil, 0.5); got != 0 {
		t.Errorf("空样本 q50 = %v, want 0", got)
	}
}

func TestSummarizeLatencies(t *testing.T) {
	if _, ok := summarizeLatencies(nil); ok {
		t.Fatal("空样本应 ok=false")
	}
	ds := []time.Duration{
		time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond, 4 * time.Millisecond,
	}
	st, ok := summarizeLatencies(ds)
	if !ok {
		t.Fatal("ok = false")
	}
	if st.Count != 4 {
		t.Errorf("Count = %d, want 4", st.Count)
	}
	if st.Min != 1 || st.Max != 4 {
		t.Errorf("min/max = %v/%v, want 1/4", st.Min, st.Max)
	}
	if st.P50 != 2.5 || st.P90 != 3.7 {
		t.Errorf("分位数 = p50=%v p90=%v", st.P50, st.P90)
	}
	// 线性插值含浮点误差，用容差比较
	if diff := st.P95 - 3.85; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("p95 = %v, want 3.85", st.P95)
	}
	if diff := st.P99 - 3.97; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("p99 = %v, want 3.97", st.P99)
	}
}
