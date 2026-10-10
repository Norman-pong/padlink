// Package pairing 实现配对确认码生命周期（60s/5 次）与客户端 token 的
// 签发、存储、校验（PRD §4.1）。同一时刻最多一个进行中的配对会话。
package pairing

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
)

// 配对参数与 token 规格。
const (
	CodeLen            = 4
	TokenLen           = 32
	MaxNameLen         = 64
	DefaultCodeTTL     = 60 * time.Second
	DefaultMaxAttempts = 5
	DefaultMaxClients  = 4
)

// 哨兵错误：会话层据此映射 PAIR_NAK reason（0..4），ctl 据此出文案。
var (
	ErrNoSession       = errors.New("无进行中的配对")
	ErrExpired         = errors.New("确认码已过期")
	ErrTooManyAttempts = errors.New("尝试次数超限，本轮配对已作废")
	ErrWrongCode       = errors.New("确认码错误")
	ErrClientsFull     = errors.New("已配对客户端已达上限")
	ErrNameTooLong     = errors.New("客户端名超过 64 字节")
)

// attempt 是一轮进行中的配对会话。
type attempt struct {
	code    string
	name    string
	expires time.Time
	fails   int
}

// Manager 管理配对会话与已配对客户端。经 NewManager 创建，零值不可用。
type Manager struct {
	store *Store

	// CodeTTL / MaxAttempts 可在测试中覆盖（0 时取默认值）。
	CodeTTL     time.Duration
	MaxAttempts int

	notify func(code string)

	mu     sync.Mutex
	active *attempt
}

// NewManager 基于 token 存储创建配对管理器；确认码生成后经 notify-send 展示。
func NewManager(store *Store) *Manager {
	return &Manager{
		store:       store,
		CodeTTL:     DefaultCodeTTL,
		MaxAttempts: DefaultMaxAttempts,
		notify:      NotifyDesktop,
	}
}

// SetNotify 替换确认码展示函数（测试注入用；nil 恢复默认 notify-send）。
func (m *Manager) SetNotify(f func(code string)) {
	if f == nil {
		f = NotifyDesktop
	}
	m.notify = f
}

// StartPairing 发起一轮配对（无进行中会话时生成新码，否则复用既有码，幂等）。
// 成功后经 notify 展示确认码（notify-send，无桌面环境时静默失败）。
func (m *Manager) StartPairing(name string) (string, error) {
	if len(name) > MaxNameLen {
		return "", ErrNameTooLong
	}
	if m.store.Count() >= m.store.MaxClients {
		return "", ErrClientsFull
	}

	m.mu.Lock()
	var code string
	isNew := false
	if m.active != nil && time.Now().Before(m.active.expires) {
		code = m.active.code
	} else {
		c, err := genCode()
		if err != nil {
			m.mu.Unlock()
			return "", err
		}
		code = c
		m.active = &attempt{code: code, name: name, expires: time.Now().Add(m.ttl())}
		isNew = true
	}
	m.mu.Unlock()

	if isNew {
		m.notify(code)
	}
	return code, nil
}

// Verify 校验确认码；成功即签发并持久化 token（绑定客户端名与时间戳）。
// 错误码错误次数达上限即作废本轮，需重新发起。
func (m *Manager) Verify(code string) (Client, error) {
	m.mu.Lock()
	if m.active == nil {
		m.mu.Unlock()
		return Client{}, ErrNoSession
	}
	now := time.Now()
	if now.After(m.active.expires) {
		m.active = nil
		m.mu.Unlock()
		return Client{}, ErrExpired
	}
	// 常数时间比较，防逐位猜码
	if subtle.ConstantTimeCompare([]byte(code), []byte(m.active.code)) != 1 {
		m.active.fails++
		if m.active.fails >= m.maxAttempts() {
			m.active = nil
			m.mu.Unlock()
			return Client{}, ErrTooManyAttempts
		}
		m.mu.Unlock()
		return Client{}, ErrWrongCode
	}
	name := m.active.name
	m.active = nil
	m.mu.Unlock()

	return m.store.Add(name)
}

// ActiveCode 返回进行中配对的确认码与过期时刻（ctl status/pair 用）。
func (m *Manager) ActiveCode() (code string, expires time.Time, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil || time.Now().After(m.active.expires) {
		return "", time.Time{}, false
	}
	return m.active.code, m.active.expires, true
}

// ActiveCodeTTL 返回当前生效的确认码 TTL（会话层做配对读超时宽限用）。
func (m *Manager) ActiveCodeTTL() time.Duration {
	return m.ttl()
}

func (m *Manager) ttl() time.Duration {
	if m.CodeTTL <= 0 {
		return DefaultCodeTTL
	}
	return m.CodeTTL
}

func (m *Manager) maxAttempts() int {
	if m.MaxAttempts <= 0 {
		return DefaultMaxAttempts
	}
	return m.MaxAttempts
}

// genCode 生成 4 位数字确认码（0000-9999，拒绝采样保证均匀）。
func genCode() (string, error) {
	for {
		var b [2]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", fmt.Errorf("pairing: 生成确认码: %w", err)
		}
		n := binary.BigEndian.Uint16(b[:])
		if n >= 60000 { // 65536 非万整数倍，拒绝高位段避免偏置
			continue
		}
		return fmt.Sprintf("%04d", n%10000), nil
	}
}
