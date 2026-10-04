package session

import (
	"sync"
	"time"
)

// rateBucket 是每秒 rate 个令牌、容量 burst 的令牌桶。
type rateBucket struct {
	rate   float64
	burst  float64
	mu     sync.Mutex
	tokens float64
	last   time.Time
}

func newRateBucket(rate, burst int) *rateBucket {
	return &rateBucket{rate: float64(rate), burst: float64(burst), tokens: float64(burst)}
}

func (b *rateBucket) allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.last.IsZero() {
		b.tokens += now.Sub(b.last).Seconds() * b.rate
		if b.tokens > b.burst {
			b.tokens = b.burst
		}
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
