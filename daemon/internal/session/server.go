// Package session 实现 TCP 会话层与 UDP 数据面（同端口复用）：
// HELLO 版本协商、PAIR_REQ 配对路径、token 逐包认证、心跳超时、
// 速率限制与事件分发。所有注入调用经单一 goroutine 串行化，
// 保证事件序与 DeviceWriter 无并发写。
package session

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"padlink/daemon/internal/inject"
	"padlink/daemon/internal/pairing"
	"padlink/daemon/internal/proto"
)

// 可调参数默认值（PRD §7：心跳/超时/速率）。
const (
	DefaultPort        = 53021
	DefaultReadTimeout = 10 * time.Second
	DefaultRatePerSec  = 2000
	DefaultRateBurst   = 4000
	DefaultRateDropMax = 2000 // 1s 窗口内超限丢弃数上限，超过即断连
	MaxBadAuthStreak   = 5    // 连续错 HMAC 次数上限
	MaxInflightInj     = 1024 // 注入队列深度
	writeTimeout       = 5 * time.Second
	rateDropWindow     = time.Second
)

// ERR payload 1B 代码（daemon README 协议约定表）。
const (
	ErrCodeAuth  uint8 = 1 // 未认证 / 连续错 HMAC 达上限
	ErrCodeProto uint8 = 2 // 会话层违规或坏 payload
	ErrCodeRate  uint8 = 3 // 速率持续超限
)

// PAIR_NAK payload 1B reason。
const (
	NakWrongCode uint8 = 0
	NakExpired   uint8 = 1
	NakTooMany   uint8 = 2
	NakNoSession uint8 = 3
	NakFull      uint8 = 4
)

// Config 可调参数；零值字段取默认值（Port=0 为系统临时端口，DefaultPort 由调用方显式传入）。
type Config struct {
	Port          int
	ReadTimeout   time.Duration // 10s 无包判失联
	RatePerSec    int
	RateBurst     int
	RateDropMax   int
	Hostname      string // DISCOVER_RESP 的 name（默认 os.Hostname()）
	DaemonVersion string
	Verbose       bool
	Log           *log.Logger // nil 时用标准 logger

	// PreemptCooldown 非持权设备被拒后的冷静期（默认 15s，PROTOCOL.md §4.9；测试可压小）。
	PreemptCooldown time.Duration
	// PreemptIdle 持权设备多久无输入算"没有在操作"（默认 1s；测试可压小）。
	PreemptIdle time.Duration
}

// Stats 计数器（原子）。
type Stats struct {
	HMACFail  atomic.Uint64 // FlagAuth 包无匹配 token 或 HMAC 错
	Dropped   atomic.Uint64 // 解码失败 / 超速丢弃 / UDP 未认证丢弃等
	Preempted atomic.Uint64 // 多设备控制权裁决拒绝的控制事件数
}

// Snapshot 是 Stats 的瞬时快照。
type Snapshot struct {
	HMACFail  uint64
	Dropped   uint64
	Preempted uint64
}

// Server 是 TCP+UDP 服务。经 New 创建，Start 启动，Stop 优雅停止。
type Server struct {
	cfg    Config
	store  *pairing.Store
	pair   *pairing.Manager
	inj    *inject.Injector
	onText func(string) error

	stats   Stats
	limiter *rateBucket     // UDP 数据面全局限速（UDP 无会话概念）
	arb     *controlArbiter // 多设备控制权裁决（TCP/UDP 控制事件统一过闸）

	injCh      chan func()
	closed     chan struct{}
	udpSeq     atomic.Uint32
	internalWG sync.WaitGroup // accept / udp / inject goroutine
	sessWG     sync.WaitGroup // TCP 会话 goroutine

	mu       sync.Mutex
	tcpLn    net.Listener
	udp      *net.UDPConn
	port     int
	sessions map[*tcpSession]struct{}
	started  bool
	stopped  bool
}

// New 构造服务。onText 处理已认证 TEXT 事件（剪贴板注入路径），可为 nil（丢弃计数）；
// 它在注入 goroutine 内被调用，可安全使用 inj（CtrlV）。
func New(cfg Config, store *pairing.Store, pair *pairing.Manager, inj *inject.Injector, onText func(string) error) *Server {
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = DefaultReadTimeout
	}
	if cfg.RatePerSec <= 0 {
		cfg.RatePerSec = DefaultRatePerSec
	}
	if cfg.RateBurst <= 0 {
		cfg.RateBurst = DefaultRateBurst
	}
	if cfg.RateDropMax <= 0 {
		cfg.RateDropMax = DefaultRateDropMax
	}
	if cfg.Hostname == "" {
		if h, err := os.Hostname(); err == nil && h != "" {
			cfg.Hostname = h
		} else {
			cfg.Hostname = "padlink-host"
		}
	}
	if cfg.Log == nil {
		cfg.Log = log.New(os.Stdout, "padlinkd ", log.LstdFlags)
	}
	return &Server{
		cfg:      cfg,
		store:    store,
		pair:     pair,
		inj:      inj,
		onText:   onText,
		limiter:  newRateBucket(cfg.RatePerSec, cfg.RateBurst),
		arb:      newControlArbiter(cfg.PreemptCooldown, cfg.PreemptIdle),
		injCh:    make(chan func(), MaxInflightInj),
		closed:   make(chan struct{}),
		sessions: make(map[*tcpSession]struct{}),
	}
}

// Start 绑定 UDP 与 TCP（同一端口）。Port=0 时取系统临时端口。
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return errors.New("session: 服务已启动")
	}
	uc, err := net.ListenUDP("udp", &net.UDPAddr{Port: s.cfg.Port})
	if err != nil {
		return fmt.Errorf("session: 监听 UDP %d: %w", s.cfg.Port, err)
	}
	port := uc.LocalAddr().(*net.UDPAddr).Port
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		uc.Close()
		return fmt.Errorf("session: 监听 TCP %d: %w", port, err)
	}
	s.started = true
	s.tcpLn, s.udp, s.port = ln, uc, port

	s.internalWG.Add(3)
	go s.acceptLoop(ln)
	go s.udpLoop(uc)
	go s.injectLoop()
	return nil
}

// Port 返回实际监听端口（Start 后有效）。
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// Stop 优雅停止：关监听与全部会话（会话结束兜底补发未释放按键），
// 排空注入队列后收掉注入 goroutine。
func (s *Server) Stop() {
	s.mu.Lock()
	if s.stopped || !s.started {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	ln, uc := s.tcpLn, s.udp
	conns := make([]net.Conn, 0, len(s.sessions))
	for sess := range s.sessions {
		conns = append(conns, sess.conn)
	}
	s.mu.Unlock()

	close(s.closed)
	if ln != nil {
		ln.Close()
	}
	if uc != nil {
		uc.Close()
	}
	for _, c := range conns {
		c.Close() // 各会话 run() 退出并经 finish 释放按键
	}
	s.sessWG.Wait()
	close(s.injCh)
	s.internalWG.Wait()
}

// StatsSnapshot 返回计数快照（ctl status 用）。
func (s *Server) StatsSnapshot() Snapshot {
	return Snapshot{
		HMACFail:  s.stats.HMACFail.Load(),
		Dropped:   s.stats.Dropped.Load(),
		Preempted: s.stats.Preempted.Load(),
	}
}

// ActiveSessions 返回活跃 TCP 会话数。
func (s *Server) ActiveSessions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// OnlineClients 返回当前在线（已认证绑定）的客户端 id 集合。
func (s *Server) OnlineClients() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	online := make(map[string]bool)
	for sess := range s.sessions {
		if id := sess.bound(); id != "" {
			online[id] = true
		}
	}
	return online
}

// KickClient 踢掉绑定指定客户端的全部会话（unpair 路径）。
func (s *Server) KickClient(id string) int {
	s.mu.Lock()
	var targets []*tcpSession
	for sess := range s.sessions {
		if sess.bound() == id {
			targets = append(targets, sess)
		}
	}
	s.mu.Unlock()
	for _, sess := range targets {
		sess.conn.Close() // run() 退出 → finish 补发释放
	}
	s.arb.release(id) // 控制权不留给已解除配对的设备
	return len(targets)
}

// admitControl 裁决一次控制事件（MOVE/SCROLL/BUTTON/KEY/TEXT，TCP 与 UDP 共用）。
// 被拒时按需下发一次 NOTICE 并兜底释放该设备残留的按键（异常时序防御）。
func (s *Server) admitControl(clientID string) bool {
	if clientID == "" {
		return true // 未绑定（理论上不会到达注入路径）：不参与多设备裁决
	}
	res := s.arb.admit(clientID, time.Now())
	if res.Allowed {
		return true
	}
	s.stats.Preempted.Add(1)
	if res.FirstDenied {
		s.vlogf("控制权在设备 %s，已拒绝 %s 的输入并进入冷静期（剩余 %.0fs）",
			s.arb.primaryID(), clientID, res.RetryAfter.Seconds())
		s.notifyControlBusy(clientID, res.RetryAfter)
		if s.arb.isHeld(clientID) {
			s.dropHeld(clientID) // 被拒设备若仍按住按键/按钮（竞态兜底），先补发释放
		}
	}
	return false
}

// notifyControlBusy 向被拒设备下发 NOTICE(控制被占用, 剩余秒数)。
// 手机端据此 toast；无 TCP 会话（仅 UDP 在发）时静默丢弃——提示不是控制路径的必要条件。
func (s *Server) notifyControlBusy(clientID string, retryAfter time.Duration) {
	sess := s.sessionOf(clientID)
	if sess == nil {
		return
	}
	secs := uint8(0)
	if secsF := retryAfter.Seconds(); secsF > 0 {
		n := int(secsF + 0.999)
		if n > 255 {
			n = 255
		}
		secs = uint8(n)
	}
	tok := sess.token()
	if tok == nil {
		return // 未认证会话无法封签，手机端会丢包
	}
	pkt := proto.NewNotice(sess.nextSeq(), proto.NoticeControlBusy, secs)
	proto.Seal(&pkt, tok)
	if err := sess.writePkt(pkt); err != nil {
		s.vlogf("NOTICE 下发失败（%s）: %v", clientID, err)
	}
}

// sessionOf 返回绑定该客户端的一条会话（优先已缓存 token 的会话）。
func (s *Server) sessionOf(clientID string) *tcpSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	var fallback *tcpSession
	for sess := range s.sessions {
		if sess.bound() != clientID {
			continue
		}
		if sess.token() != nil {
			return sess
		}
		fallback = sess
	}
	return fallback
}

// dropHeld 释放某设备残留的按键/按钮（被拒时的防御路径，PRD §11.2 零残留红线）。
func (s *Server) dropHeld(clientID string) {
	s.mu.Lock()
	targets := make([]*tcpSession, 0, 1)
	for sess := range s.sessions {
		if sess.bound() == clientID {
			targets = append(targets, sess)
		}
	}
	s.mu.Unlock()
	for _, sess := range targets {
		if keys, btns := sess.takeHeld(); len(keys) > 0 || len(btns) > 0 {
			s.enqueueBlocking(func() {
				for _, hid := range keys {
					if err := s.inj.Key(hid, false); err != nil {
						s.logf("被拒设备残留按键释放失败 hid=0x%04X: %v", hid, err)
					}
				}
				for _, b := range btns {
					if err := s.inj.Button(b, false); err != nil {
						s.logf("被拒设备残留按钮释放失败 btn=%d: %v", b, err)
					}
				}
			})
			s.logf("设备 %s 被拒控制权时仍按住输入，已补发释放", clientID)
		}
	}
}

func (s *Server) logf(format string, args ...any) {
	s.cfg.Log.Printf(format, args...)
}

func (s *Server) vlogf(format string, args ...any) {
	if s.cfg.Verbose {
		s.cfg.Log.Printf(format, args...)
	}
}

func (s *Server) injectLoop() {
	defer s.internalWG.Done()
	for f := range s.injCh {
		f()
	}
}

// enqueueBlocking 在注入队列入队（TCP 路径：背压保序；服务停止时放行）。
func (s *Server) enqueueBlocking(f func()) {
	select {
	case s.injCh <- f:
	case <-s.closed:
	}
}

// tryEnqueue 非阻塞入队（UDP 路径：满即丢弃计数）。
func (s *Server) tryEnqueue(f func()) bool {
	select {
	case s.injCh <- f:
		return true
	default:
		s.stats.Dropped.Add(1)
		return false
	}
}

// doInj 包装注入调用，失败记录日志（token/文本内容不进日志）。
func (s *Server) doInj(what string, f func() error) func() {
	return func() {
		if err := f(); err != nil {
			s.logf("注入 %s 失败: %v", what, err)
		}
	}
}

// matchToken 对 FlagAuth 包逐个尝试已配对客户端 token（≤4 个）。
func (s *Server) matchToken(pkt *proto.Packet) (pairing.Client, []byte, bool) {
	for _, c := range s.store.Clients() {
		tok, err := hex.DecodeString(c.TokenHex)
		if err != nil {
			continue
		}
		if proto.VerifyHMAC(pkt, tok) {
			return c, tok, true
		}
	}
	return pairing.Client{}, nil, false
}

// discoverResp 组装发现应答 JSON（不含敏感信息）。
type discoverResp struct {
	Name   string `json:"name"`
	OS     string `json:"os"`
	Daemon string `json:"daemon"`
	Ver    int    `json:"ver"`
	Paired int    `json:"paired"`
	Port   int    `json:"port"`
}

func (s *Server) discoverBody() []byte {
	b, _ := json.Marshal(discoverResp{
		Name:   s.cfg.Hostname,
		OS:     runtime.GOOS,
		Daemon: s.cfg.DaemonVersion,
		Ver:    proto.Ver,
		Paired: s.store.Count(),
		Port:   s.Port(),
	})
	return b
}
