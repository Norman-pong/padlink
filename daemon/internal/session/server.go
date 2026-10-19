// Package session 实现 TCP 会话层与 UDP 数据面（同端口复用）：HELLO 版本协商、
// PAIR_REQ 配对路径、token 逐包认证、心跳超时、速率限制与事件分发。
// 所有注入调用（含 key repeat 连发，经注入队列投递）在单一 goroutine 串行执行，保证事件序。
package session

import (
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

const (
	DefaultPort        = 53021
	DefaultReadTimeout = 10 * time.Second
	DefaultRatePerSec  = 2000
	DefaultRateBurst   = 4000
	DefaultRateDropMax = 2000 // 1s 窗口内超限丢弃数上限，超过即断连
	MaxBadAuthStreak   = 5    // 连续错 HMAC 达此值断连
	MaxInflightInj     = 1024
	writeTimeout       = 5 * time.Second
	rateDropWindow     = time.Second
)

// ERR payload 1B 代码。
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

// Config 可调参数；零值字段取默认值。
// Port=0 必须兑现系统临时端口语义，DefaultPort 由调用方显式传入（不在此归一）。
type Config struct {
	Port          int
	ReadTimeout   time.Duration // 10s 无包判失联
	RatePerSec    int
	RateBurst     int
	RateDropMax   int
	Hostname      string // DISCOVER_RESP 的 name（默认 os.Hostname()）
	HostID        string // DISCOVER_RESP 的 host_id（空 = 省略字段，兼容旧手机）
	DaemonVersion string
	Verbose       bool
	Log           *log.Logger // nil 时用标准 logger

	// PreemptCooldown 非持权设备被拒后的冷静期（默认 15s；测试可压小）。
	PreemptCooldown time.Duration
	// PreemptIdle 持权设备多久无输入算"没有在操作"（默认 1s；测试可压小）。
	PreemptIdle time.Duration
}

type Stats struct {
	HMACFail  atomic.Uint64 // FlagAuth 包无匹配 token 或 HMAC 错
	Dropped   atomic.Uint64 // 解码失败 / 超速丢弃 / UDP 未认证丢弃等
	Preempted atomic.Uint64 // 多设备控制权裁决拒绝的控制事件数
}

type Snapshot struct {
	HMACFail  uint64
	Dropped   uint64
	Preempted uint64
}

// Server 是 TCP+UDP 服务；经 New 创建，Start 启动，Stop 优雅停止。
type Server struct {
	cfg    Config
	store  *pairing.Store
	pair   *pairing.Manager
	inj    *inject.Injector
	onText func(clientID, text string) error

	stats   Stats
	limiter *rateBucket // UDP 全局桶：未认证洪泛防线（已认证包退还后改记客户端桶）
	// udpLimiters 已认证 UDP 的按客户端限速（对齐 TCP 每会话独立桶；条目 ≤ 客户端数，
	// 惰性建立，解配后残留条目只占恒定内存）。
	udpLimiters map[string]*rateBucket
	arb         *controlArbiter // 多设备控制权裁决（TCP/UDP 控制事件统一过闸）

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

// New 构造服务。onText 处理已认证 TEXT（剪贴板注入路径，clientID 为发送方，供失败时经
// NotifyTextFailed 回传提示），可为 nil（丢弃计数）；它在注入 goroutine 内被调用，可安全使用 inj。
func New(cfg Config, store *pairing.Store, pair *pairing.Manager, inj *inject.Injector, onText func(clientID, text string) error) *Server {
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
	srv := &Server{
		cfg:         cfg,
		store:       store,
		pair:        pair,
		inj:         inj,
		onText:      onText,
		limiter:     newRateBucket(cfg.RatePerSec, cfg.RateBurst),
		udpLimiters: make(map[string]*rateBucket),
		arb:         newControlArbiter(cfg.PreemptCooldown, cfg.PreemptIdle),
		injCh:       make(chan func(), MaxInflightInj),
		closed:      make(chan struct{}),
		sessions:    make(map[*tcpSession]struct{}),
	}
	if inj != nil {
		inj.SetRepeatEnqueue(srv.tryEnqueue) // 连发与主事件同队列串行
	}
	return srv
}

// Start 绑定 UDP 与 TCP（同一端口）。
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return errors.New("session: 服务已启动")
	}
	uc, ln, port, err := bindPairWith(s.cfg.Port, listenUDP, listenTCP)
	if err != nil {
		return err
	}
	s.started = true
	s.tcpLn, s.udp, s.port = ln, uc, port

	s.internalWG.Add(3)
	go s.acceptLoop(ln)
	go s.udpLoop(uc)
	go s.injectLoop()
	return nil
}

type udpBinder func(port int) (*net.UDPConn, error)
type tcpBinder func(port int) (net.Listener, error)

func listenUDP(port int) (*net.UDPConn, error) {
	return net.ListenUDP("udp", &net.UDPAddr{Port: port})
}

func listenTCP(port int) (net.Listener, error) {
	return net.Listen("tcp", fmt.Sprintf(":%d", port))
}

// bindPairWith 绑定同端口 UDP+TCP。Port=0 时 UDP 与 TCP 的临时端口空间相互独立：
// UDP 选中的空闲端口在 TCP 空间可能已被占（CI 实测偶发），冲突则换端口重试；
// 显式端口不重试——端口被占属部署错误，直接报错。
func bindPairWith(port int, udp udpBinder, tcp tcpBinder) (*net.UDPConn, net.Listener, int, error) {
	const retries = 10
	for i := 0; ; i++ {
		uc, err := udp(port)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("session: 监听 UDP %d: %w", port, err)
		}
		p := uc.LocalAddr().(*net.UDPAddr).Port
		ln, err := tcp(p)
		if err == nil {
			return uc, ln, p, nil
		}
		uc.Close()
		if port != 0 || i >= retries {
			return nil, nil, 0, fmt.Errorf("session: 监听 TCP %d: %w", p, err)
		}
	}
}

// Port 返回实际监听端口（Start 后有效）。
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// Stop 优雅停止：关监听与全部会话（各会话退出时兜底补发未释放按键），
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

func (s *Server) StatsSnapshot() Snapshot {
	return Snapshot{
		HMACFail:  s.stats.HMACFail.Load(),
		Dropped:   s.stats.Dropped.Load(),
		Preempted: s.stats.Preempted.Load(),
	}
}

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

// notifyControlBusy 向被拒设备下发 NOTICE(控制被占用, 剩余秒数)，手机端据此 toast。
func (s *Server) notifyControlBusy(clientID string, retryAfter time.Duration) {
	secs := uint8(0)
	if secsF := retryAfter.Seconds(); secsF > 0 {
		n := int(secsF + 0.999)
		if n > 255 {
			n = 255
		}
		secs = uint8(n)
	}
	s.notifyClient(clientID, proto.NoticeControlBusy, secs)
}

// NotifyTextFailed 向该客户端下发 NOTICE(TEXT 未上屏, 原因码)：注入失败要让用户可见，
// 否则手机端表现为「发送成功但电脑毫无反应」（详见 PROTOCOL.md §4.7）。
func (s *Server) NotifyTextFailed(clientID string, reason uint8) {
	s.notifyClient(clientID, proto.NoticeTextFailed, reason)
}

// notifyClient 向客户端会话下发 NOTICE(code, arg)；无 TCP 会话（仅 UDP 在发）或未认证时
// 静默丢弃——提示不是注入路径的必要条件。
func (s *Server) notifyClient(clientID string, code uint8, arg uint8) {
	sess := s.sessionOf(clientID)
	if sess == nil {
		return
	}
	tok := sess.token()
	if tok == nil {
		return // 未认证会话无法封签，手机端会丢包
	}
	pkt := proto.NewNotice(sess.nextSeq(), code, arg)
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

func (s *Server) logf(format string, args ...any) {
	s.cfg.Log.Printf(format, args...)
}

func (s *Server) vlogf(format string, args ...any) {
	if s.cfg.Verbose {
		s.cfg.Log.Printf(format, args...)
	}
}

// udpClientAllow 已认证 UDP 的按客户端限速。
func (s *Server) udpClientAllow(id string, now time.Time) bool {
	s.mu.Lock()
	b := s.udpLimiters[id]
	if b == nil {
		b = newRateBucket(s.cfg.RatePerSec, s.cfg.RateBurst)
		s.udpLimiters[id] = b
	}
	s.mu.Unlock()
	return b.allow(now)
}

// matchToken 逐个尝试已配对客户端 token（≤4 个）。
func (s *Server) matchToken(pkt *proto.Packet) (pairing.Client, []byte, bool) {
	for _, e := range s.store.AuthIndex() {
		if proto.VerifyHMAC(pkt, e.Token) {
			return e.Client, e.Token, true
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
	// HostID 未认证通道里的候选提示（PROTOCOL.md §4.10），空时省略字段。
	HostID string `json:"host_id,omitempty"`
}

func (s *Server) discoverBody() []byte {
	b, _ := json.Marshal(discoverResp{
		Name:   s.cfg.Hostname,
		OS:     runtime.GOOS,
		Daemon: s.cfg.DaemonVersion,
		Ver:    proto.Ver,
		Paired: s.store.Count(),
		Port:   s.Port(),
		HostID: s.cfg.HostID,
	})
	return b
}
