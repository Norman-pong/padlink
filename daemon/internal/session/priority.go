package session

import (
	"sync"
	"time"

	"padlink/daemon/internal/proto"
)

// 多设备控制权参数默认值（PROTOCOL.md §4.9）。
const (
	// DefaultPreemptCooldown 非持权设备被拒后的冷静期：期间其控制事件一律丢弃（只提示一次）。
	DefaultPreemptCooldown = 15 * time.Second
	// DefaultPreemptIdle 持权设备多久无输入算"没有在操作"（冷静期满后的让位判据）。
	DefaultPreemptIdle = time.Second
)

// admitResult 是一次控制事件的裁决结果。
type admitResult struct {
	Allowed     bool
	RetryAfter  time.Duration // 被拒时距冷静期结束的剩余时间（NOTICE arg）
	FirstDenied bool          // 本轮冷静期首次被拒（据此只下发一次提示，不刷屏）
}

// controlArbiter 裁决多设备控制权：同一时刻只有一台设备（"第一设备"）的控制事件被放行。
// 规则（PROTOCOL.md §4.9）：
//   - 无持权设备时首个控制事件取得控制权；
//   - 持权设备有操作（idle 内有事件，或仍按住按键/按钮）时，其他设备的事件被拒并进入
//     cooldown 冷静期，冷静期内继续被拒（首次被拒时提示一次）；
//   - 冷静期满后，仅当持权设备当前无操作才放行接管（接管者成为新的第一设备），
//     否则重新进入冷静期；
//   - 持权设备断开即释放控制权。
//
// 并发：UDP 数据面与各 TCP 会话 goroutine 都会调用，内部互斥。
type controlArbiter struct {
	cooldown time.Duration
	idle     time.Duration

	mu      sync.Mutex
	primary string               // 当前持权客户端 id（"" = 无）
	lastAct time.Time            // 持权设备最后一次控制事件时刻
	blocked map[string]time.Time // 非持权设备 → 冷静期截止时刻
	held    map[string]bool      // 各设备是否按住按键/按钮（会话镜像，避免跨 goroutine 读会话态）
}

func newControlArbiter(cooldown, idle time.Duration) *controlArbiter {
	if cooldown <= 0 {
		cooldown = DefaultPreemptCooldown
	}
	if idle <= 0 {
		idle = DefaultPreemptIdle
	}
	return &controlArbiter{
		cooldown: cooldown,
		idle:     idle,
		blocked:  make(map[string]time.Time),
		held:     make(map[string]bool),
	}
}

// admit 裁决一台设备的一次控制事件。
func (a *controlArbiter) admit(id string, now time.Time) admitResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.primary == "" {
		a.setPrimaryLocked(id, now)
		return admitResult{Allowed: true}
	}
	if a.primary == id {
		a.lastAct = now
		return admitResult{Allowed: true}
	}
	if until, ok := a.blocked[id]; ok && now.Before(until) {
		return admitResult{RetryAfter: until.Sub(now)}
	}
	if a.primaryActiveLocked(now) {
		a.blocked[id] = now.Add(a.cooldown)
		return admitResult{RetryAfter: a.cooldown, FirstDenied: true}
	}
	// 冷静期满且持权设备无操作：接管
	delete(a.blocked, id)
	a.setPrimaryLocked(id, now)
	return admitResult{Allowed: true}
}

// release 释放某设备的控制权与冷静期（断开/解除配对时调用）。
func (a *controlArbiter) release(id string) {
	if id == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.primary == id {
		a.primary = ""
	}
	delete(a.blocked, id)
	delete(a.held, id)
}

// setHeld 由会话在"本设备按住数 0↔非 0"翻转时调用：按住的按键/按钮也是"在操作"。
func (a *controlArbiter) setHeld(id string, held bool) {
	if id == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if held {
		a.held[id] = true
	} else {
		delete(a.held, id)
	}
}

// isHeld 报告该设备当前是否按住按键/按钮（被拒时兜底释放残留用）。
func (a *controlArbiter) isHeld(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.held[id]
}

// primaryID 返回当前持权设备 id（空 = 无）；调试与单测用。
func (a *controlArbiter) primaryID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.primary
}

func (a *controlArbiter) setPrimaryLocked(id string, now time.Time) {
	a.primary, a.lastAct = id, now
}

// primaryActiveLocked 持权设备是否仍在操作：idle 内有事件，或仍按住按键/按钮。
func (a *controlArbiter) primaryActiveLocked(now time.Time) bool {
	if a.held[a.primary] {
		return true
	}
	return now.Sub(a.lastAct) < a.idle
}

// isControlType 报告事件类型是否属"控制"（受多设备控制权裁决、需真实注入）。
// ECHO/BYE/ERR/HELLO/DISCOVER_RESP/PAIR_* 均非控制（心跳与信令不得被控制权影响）。
func isControlType(t proto.Type) bool {
	switch t {
	case proto.TypeMove, proto.TypeScroll, proto.TypeButton, proto.TypeKey, proto.TypeText:
		return true
	default:
		return false
	}
}
