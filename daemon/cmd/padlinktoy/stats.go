package main

import (
	"math"
	"sort"
	"time"
)

// quantileSorted 返回升序样本的线性插值分位数（q∈[0,1]）；空样本返回 0。
func quantileSorted(sorted []float64, q float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	pos := q * float64(n-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	frac := pos - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

// rttStats 是一组 RTT 样本的汇总（毫秒）。
type rttStats struct {
	Count int
	Min   float64
	P50   float64
	P90   float64
	P95   float64
	P99   float64
	Max   float64
}

// summarizeLatencies 汇总 RTT 样本；空样本返回 ok=false。
func summarizeLatencies(ds []time.Duration) (rttStats, bool) {
	if len(ds) == 0 {
		return rttStats{}, false
	}
	ms := make([]float64, len(ds))
	for i, d := range ds {
		ms[i] = float64(d) / float64(time.Millisecond)
	}
	sort.Float64s(ms)
	return rttStats{
		Count: len(ms),
		Min:   ms[0],
		P50:   quantileSorted(ms, 0.50),
		P90:   quantileSorted(ms, 0.90),
		P95:   quantileSorted(ms, 0.95),
		P99:   quantileSorted(ms, 0.99),
		Max:   ms[len(ms)-1],
	}, true
}
