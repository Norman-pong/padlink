package session

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"padlink/daemon/internal/inject"
	"padlink/daemon/internal/pairing"
	"padlink/daemon/internal/proto"
)

// ---- fake DeviceWriter ----

type evKind uint8

const (
	evKey evKind = iota
	evRel
	evSync
)

type ev struct {
	kind  evKind
	code  uint16
	value int32
}

type capture struct {
	mu     sync.Mutex
	events []ev
}

func (c *capture) KeyEvent(code uint16, value int32) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev{evKey, code, value})
	return nil
}

func (c *capture) RelEvent(code uint16, value int32) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev{evRel, code, value})
	return nil
}

func (c *capture) SetPointerState(st inject.PointerState) {}

func (c *capture) Sync() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev{kind: evSync})
	return nil
}

func (c *capture) Close() error { return nil }

func (c *capture) count(kind evKind, code uint16, value int32) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, e := range c.events {
		if e.kind == kind && e.code == code && e.value == value {
			n++
		}
	}
	return n
}

// waitFor 轮询断言注入事件到达（异步管道，最终一致）。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待 %s 超时", what)
}

var _ inject.DeviceWriter = (*capture)(nil)

// ---- 测试服务器装具 ----

type testSrv struct {
	srv   *Server
	store *pairing.Store
	pair  *pairing.Manager
	cap   *capture
}

func newTestServer(t *testing.T, mutate func(*Config)) *testSrv {
	t.Helper()
	store, err := pairing.Load(filepath.Join(t.TempDir(), "clients.json"))
	if err != nil {
		t.Fatalf("pairing.Load: %v", err)
	}
	mgr := pairing.NewManager(store)
	mgr.SetNotify(func(string) {})
	cap := &capture{}
	inj := inject.NewInjector(cap, inject.Config{RepeatDelay: time.Hour, RepeatInterval: time.Hour})
	cfg := Config{
		Port:          0,
		DaemonVersion: "test",
		Hostname:      "testhost",
		Verbose:       false,
		Log:           log.New(io.Discard, "", 0),
	}
	if mutate != nil {
		mutate(&cfg)
	}
	srv := New(cfg, store, mgr, inj, nil)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		srv.Stop()
		inj.Close()
	})
	return &testSrv{srv: srv, store: store, pair: mgr, cap: cap}
}

// ---- TCP 测试客户端 ----

// TestPortZeroEphemeral 钉住 Start 的承诺：Config.Port=0 绑定系统临时端口
// （此前 New 误归一成 DefaultPort，文档与行为矛盾且并行测试互抢 53021）。
func TestPortZeroEphemeral(t *testing.T) {
	ts := newTestServer(t, nil)
	if p := ts.srv.Port(); p <= 0 || p == DefaultPort {
		t.Fatalf("Port=0 应绑定临时端口，实得 %d", p)
	}
}

type tcpClient struct {
	t    *testing.T
	conn net.Conn
	rd   *bufio.Reader
}

func dialTCP(t *testing.T, port int) *tcpClient {
	t.Helper()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return &tcpClient{t: t, conn: conn, rd: bufio.NewReader(conn)}
}

func (c *tcpClient) send(pkt proto.Packet) {
	c.t.Helper()
	buf, err := proto.Encode(&pkt)
	if err != nil {
		c.t.Fatalf("Encode: %v", err)
	}
	if _, err := c.conn.Write(buf); err != nil {
		c.t.Fatalf("send %v: %v", pkt.Type, err)
	}
}

func (c *tcpClient) sendRaw(b []byte) {
	c.t.Helper()
	if _, err := c.conn.Write(b); err != nil {
		c.t.Fatalf("sendRaw: %v", err)
	}
}

func (c *tcpClient) recv(timeout time.Duration) (proto.Packet, error) {
	c.t.Helper()
	c.conn.SetReadDeadline(time.Now().Add(timeout))
	var head [proto.HeaderSize]byte
	if _, err := io.ReadFull(c.rd, head[:]); err != nil {
		return proto.Packet{}, err
	}
	plen := int(binary.BigEndian.Uint16(head[7:9]))
	frame := append([]byte{}, head[:]...)
	if plen > 0 {
		payload := make([]byte, plen)
		if _, err := io.ReadFull(c.rd, payload); err != nil {
			return proto.Packet{}, err
		}
		frame = append(frame, payload...)
	}
	return proto.Decode(frame)
}

func (c *tcpClient) mustRecv(timeout time.Duration) proto.Packet {
	c.t.Helper()
	pkt, err := c.recv(timeout)
	if err != nil {
		c.t.Fatalf("recv: %v", err)
	}
	return pkt
}

// hello 完成 HELLO 握手并断言双向版本。
func (c *tcpClient) hello(peerVer uint16, wantAgree uint16) {
	c.t.Helper()
	pl := make([]byte, 2)
	binary.BigEndian.PutUint16(pl, peerVer)
	c.send(proto.NewRaw(proto.TypeHello, 1, pl))
	reply := c.mustRecv(time.Second)
	if reply.Type != proto.TypeHello {
		c.t.Fatalf("HELLO 回复类型 = %v, want HELLO", reply.Type)
	}
	if got := binary.BigEndian.Uint16(reply.Payload); got != wantAgree {
		c.t.Fatalf("协商版本 = %d, want %d", got, wantAgree)
	}
}

func (c *tcpClient) expectEOF(timeout time.Duration) {
	c.t.Helper()
	c.conn.SetReadDeadline(time.Now().Add(timeout))
	_, err := c.rd.ReadByte()
	if errors.Is(err, io.EOF) || errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, net.ErrClosed) || errors.Is(err, syscall.ECONNRESET) {
		return
	}
	c.t.Fatalf("期望连接关闭，实得 %v", err)
}

// ---- 用例 ----

func TestFullPairFlow(t *testing.T) {
	ts := newTestServer(t, nil)
	c := dialTCP(t, ts.srv.Port())
	c.hello(1, 1)

	// 发起配对：JSON 名字
	c.send(proto.NewRaw(proto.TypePairReq, 2, []byte(`{"name":"测试手机"}`)))
	// 发起成功不回包
	if _, err := c.recv(250 * time.Millisecond); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("发起配对不应回包，实得 %v", err)
	}
	code, _, ok := ts.pair.ActiveCode()
	if !ok {
		t.Fatal("配对会话未建立")
	}

	// 确认码尝试 → PAIR_OK（封签 32B token），字节级对拍
	c.send(proto.NewRaw(proto.TypePairReq, 3, []byte(code)))
	okPkt := c.mustRecv(time.Second)
	if okPkt.Type != proto.TypePairOK {
		t.Fatalf("类型 = %v, want PAIR_OK", okPkt.Type)
	}
	if okPkt.Flags&proto.FlagAuth == 0 {
		t.Fatal("PAIR_OK 未封签 FlagAuth")
	}
	if len(okPkt.Payload) != pairing.TokenLen {
		t.Fatalf("PAIR_OK payload = %dB, want 32B", len(okPkt.Payload))
	}
	if !proto.VerifyHMAC(&okPkt, okPkt.Payload) {
		t.Fatal("PAIR_OK HMAC 未通过（应以 payload 即 token 校验）")
	}
	clients := ts.store.Clients()
	if len(clients) != 1 || clients[0].Name != "测试手机" {
		t.Fatalf("token 未持久化/名字不符: %+v", clients)
	}

	// 用新 token 认证 ECHO 往返（同 payload、封签、seq 自增）
	token := okPkt.Payload
	echo := proto.NewEcho(9, 1234567890)
	proto.Seal(&echo, token)
	c.send(echo)
	reply := c.mustRecv(time.Second)
	if reply.Type != proto.TypeEcho {
		t.Fatalf("类型 = %v, want ECHO", reply.Type)
	}
	if reply.Echo.TsMs != 1234567890 {
		t.Errorf("ECHO payload 往返不一致: %d", reply.Echo.TsMs)
	}
	if reply.Seq != okPkt.Seq+1 { // 回包 seq 自增（HELLO/PAIR_OK 之后）
		t.Errorf("ECHO 回包 seq = %d, want %d", reply.Seq, okPkt.Seq+1)
	}
	if !proto.VerifyHMAC(&reply, token) {
		t.Error("ECHO 回包 HMAC 校验失败")
	}
	if ts.srv.ActiveSessions() != 1 {
		t.Errorf("活跃会话 = %d, want 1", ts.srv.ActiveSessions())
	}
	if online := ts.srv.OnlineClients(); len(online) != 1 || !online[clients[0].ID] {
		t.Errorf("在线客户端 = %v", online)
	}
}

func TestWrongCodeFiveAttemptsLock(t *testing.T) {
	ts := newTestServer(t, nil)
	if _, err := ts.pair.StartPairing("p", ""); err != nil {
		t.Fatalf("StartPairing: %v", err)
	}
	code, _, _ := ts.pair.ActiveCode()
	c := dialTCP(t, ts.srv.Port())
	c.hello(1, 1)

	for i := 0; i < 4; i++ {
		c.send(proto.NewRaw(proto.TypePairReq, uint16(i+2), []byte(wrongCode(code, i+1))))
		nak := c.mustRecv(time.Second)
		if nak.Type != proto.TypePairNak || len(nak.Payload) != 1 || nak.Payload[0] != NakWrongCode {
			t.Fatalf("第 %d 次: got %v payload=%v, want NAK(0)", i+1, nak.Type, nak.Payload)
		}
		if nak.Flags&proto.FlagAuth != 0 {
			t.Fatalf("NAK 不应封签（无 token）")
		}
	}
	c.send(proto.NewRaw(proto.TypePairReq, 9, []byte(wrongCode(code, 5))))
	nak := c.mustRecv(time.Second)
	if nak.Payload[0] != NakTooMany {
		t.Fatalf("第 5 次错码 reason = %d, want 2（超次）", nak.Payload[0])
	}
	// 锁定后即使正确码也无会话
	c.send(proto.NewRaw(proto.TypePairReq, 10, []byte(code)))
	nak = c.mustRecv(time.Second)
	if nak.Payload[0] != NakNoSession {
		t.Fatalf("锁定后 reason = %d, want 3（无进行中配对）", nak.Payload[0])
	}
	if ts.store.Count() != 0 {
		t.Errorf("锁定轮不应签发 token")
	}
}

func TestUnauthPacketRejected(t *testing.T) {
	ts := newTestServer(t, nil)
	c := dialTCP(t, ts.srv.Port())
	c.hello(1, 1)
	c.send(proto.NewMove(5, 1, 2)) // 未认证 MOVE
	errPkt := c.mustRecv(time.Second)
	if errPkt.Type != proto.TypeErr || len(errPkt.Payload) != 1 || errPkt.Payload[0] != ErrCodeAuth {
		t.Fatalf("got %v payload=%v, want ERR(1)", errPkt.Type, errPkt.Payload)
	}
	c.expectEOF(time.Second)
}

func TestFirstPacketMustBeHello(t *testing.T) {
	ts := newTestServer(t, nil)
	c := dialTCP(t, ts.srv.Port())
	c.send(proto.NewRaw(proto.TypePairReq, 1, nil)) // 未握手先配对
	errPkt := c.mustRecv(time.Second)
	if errPkt.Type != proto.TypeErr || errPkt.Payload[0] != ErrCodeAuth {
		t.Fatalf("got %v, want ERR(1)", errPkt.Type)
	}
	c.expectEOF(time.Second)
}

func TestDuplicateHelloAndDiscoverOnTCP(t *testing.T) {
	ts := newTestServer(t, nil)
	token := prepair(t, ts)
	c := dialTCP(t, ts.srv.Port())
	c.hello(1, 1)
	// 认证绑定
	echo := proto.NewEcho(1, 1)
	proto.Seal(&echo, token)
	c.send(echo)
	_ = c.mustRecv(time.Second)

	// 已认证后重复 HELLO → ERR(2)+关
	c.send(proto.NewRaw(proto.TypeHello, 2, []byte{0, 1}))
	errPkt := c.mustRecv(time.Second)
	if errPkt.Type != proto.TypeErr || errPkt.Payload[0] != ErrCodeProto {
		t.Fatalf("重复 HELLO: got %v, want ERR(2)", errPkt.Type)
	}
	c.expectEOF(time.Second)

	// DISCOVER_RESP 出现在 TCP 入站（未认证）→ ERR(2)+关
	c2 := dialTCP(t, ts.srv.Port())
	c2.hello(1, 1)
	c2.send(proto.NewRaw(proto.TypeDiscoverResp, 1, []byte("{}")))
	errPkt = c2.mustRecv(time.Second)
	if errPkt.Type != proto.TypeErr || errPkt.Payload[0] != ErrCodeProto {
		t.Fatalf("TCP 入站 DISCOVER_RESP: got %v, want ERR(2)", errPkt.Type)
	}
	c2.expectEOF(time.Second)
}

func TestBadHMACDropAndDisconnect(t *testing.T) {
	ts := newTestServer(t, nil)
	prepair(t, ts) // 库里有 token，但下面用错误 token 发包
	c := dialTCP(t, ts.srv.Port())
	c.hello(1, 1)
	bad := []byte("wrong-token-wrong-token-0000")

	for i := 0; i < 4; i++ {
		pkt := proto.NewMove(uint16(i+2), 3, 4)
		proto.Seal(&pkt, bad)
		c.send(pkt)
	}
	if _, err := c.recv(300 * time.Millisecond); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("前 4 次错 HMAC 应静默丢弃，实得 %v", err)
	}
	pkt := proto.NewMove(9, 3, 4)
	proto.Seal(&pkt, bad)
	c.send(pkt)
	errPkt := c.mustRecv(time.Second)
	if errPkt.Type != proto.TypeErr || errPkt.Payload[0] != ErrCodeAuth {
		t.Fatalf("第 5 次: got %v, want ERR(1)", errPkt.Type)
	}
	c.expectEOF(time.Second)
	if got := ts.srv.StatsSnapshot().HMACFail; got != 5 {
		t.Errorf("HMACFail = %d, want 5", got)
	}
}

func TestRateLimitDisconnect(t *testing.T) {
	ts := newTestServer(t, func(cfg *Config) {
		cfg.RateBurst = 5
		cfg.RatePerSec = 1
		cfg.RateDropMax = 20
	})
	token := prepair(t, ts)
	c := dialTCP(t, ts.srv.Port())
	c.hello(1, 1)

	// 连发 40 包触发限速断连；服务端先行关闭后余包写入会 EPIPE，属预期
	for i := 0; i < 40; i++ {
		pkt := proto.NewMove(uint16(i+2), 1, 1)
		proto.Seal(&pkt, token)
		buf, err := proto.Encode(&pkt)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if _, err := c.conn.Write(buf); err != nil {
			break
		}
	}
	errPkt := c.mustRecv(2 * time.Second)
	if errPkt.Type != proto.TypeErr || errPkt.Payload[0] != ErrCodeRate {
		t.Fatalf("got %v payload=%v, want ERR(3)", errPkt.Type, errPkt.Payload)
	}
	c.expectEOF(time.Second)
}

func TestSessionEndReleasesPressedKeys(t *testing.T) {
	t.Run("断开", func(t *testing.T) {
		ts := newTestServer(t, nil)
		token := prepair(t, ts)
		c := dialTCP(t, ts.srv.Port())
		c.hello(1, 1)
		authEcho(t, c, token)

		down := proto.NewKey(2, inject.HIDA, true)
		proto.Seal(&down, token)
		c.send(down)
		btn := proto.NewButton(3, 1, true)
		proto.Seal(&btn, token)
		c.send(btn)
		waitFor(t, "KEY_A down + BTN_LEFT down", func() bool {
			return ts.cap.count(evKey, 30, 1) == 1 && ts.cap.count(evKey, 272, 1) == 1
		})

		c.conn.Close() // 异常断开
		waitFor(t, "兜底补发 up", func() bool {
			return ts.cap.count(evKey, 30, 0) == 1 && ts.cap.count(evKey, 272, 0) == 1
		})
	})
	t.Run("BYE 优雅关闭", func(t *testing.T) {
		ts := newTestServer(t, nil)
		token := prepair(t, ts)
		c := dialTCP(t, ts.srv.Port())
		c.hello(1, 1)
		authEcho(t, c, token)

		down := proto.NewKey(2, inject.HIDA, true)
		proto.Seal(&down, token)
		c.send(down)
		waitFor(t, "KEY_A down", func() bool { return ts.cap.count(evKey, 30, 1) == 1 })

		bye := proto.NewRaw(proto.TypeBye, 5, nil)
		proto.Seal(&bye, token)
		c.send(bye)
		waitFor(t, "BYE 后兜底补发 up", func() bool { return ts.cap.count(evKey, 30, 0) == 1 })
		c.expectEOF(time.Second)
	})
}

func TestTextDispatch(t *testing.T) {
	store, err := pairing.Load(filepath.Join(t.TempDir(), "clients.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	mgr := pairing.NewManager(store)
	mgr.SetNotify(func(string) {})
	cap := &capture{}
	inj := inject.NewInjector(cap, inject.Config{})
	got := make(chan string, 1)
	srv := New(Config{Port: 0, DaemonVersion: "t", Log: log.New(io.Discard, "", 0)},
		store, mgr, inj, func(text string) error { got <- text; return nil })
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { srv.Stop(); inj.Close() })

	token := addClient(t, store, "t")
	c := dialTCP(t, srv.Port())
	c.hello(1, 1)
	authEcho(t, c, token)

	txt := proto.NewText(7, "你好 padlink")
	proto.Seal(&txt, token)
	c.send(txt)
	select {
	case g := <-got:
		if g != "你好 padlink" {
			t.Errorf("onText = %q", g)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("TEXT 未送达 onText")
	}
}

func TestUDPDiscoverResp(t *testing.T) {
	ts := newTestServer(t, nil)
	uc, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: ts.srv.Port()})
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	t.Cleanup(func() { uc.Close() })

	// 未认证 HELLO（模拟广播/单播发现）
	pl := make([]byte, 2)
	binary.BigEndian.PutUint16(pl, proto.Ver)
	hello := proto.NewRaw(proto.TypeHello, 1, pl)
	helloBytes, err := proto.Encode(&hello)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if _, err := uc.Write(helloBytes); err != nil {
		t.Fatalf("Write: %v", err)
	}
	buf := make([]byte, 2048)
	uc.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := uc.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("ReadFromUDP: %v", err)
	}
	pkt, err := proto.Decode(buf[:n])
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if pkt.Type != proto.TypeDiscoverResp {
		t.Fatalf("类型 = %v, want DISCOVER_RESP", pkt.Type)
	}
	var resp struct {
		Name   string `json:"name"`
		OS     string `json:"os"`
		Daemon string `json:"daemon"`
		Ver    int    `json:"ver"`
		Paired int    `json:"paired"`
		Port   int    `json:"port"`
	}
	if err := json.Unmarshal(pkt.Payload, &resp); err != nil {
		t.Fatalf("JSON 解析: %v", err)
	}
	if resp.Name != "testhost" || resp.OS != runtime.GOOS || resp.Daemon != "test" {
		t.Errorf("发现应答字段不符: %+v", resp)
	}
	if resp.Ver != proto.Ver || resp.Paired != 0 || resp.Port != ts.srv.Port() {
		t.Errorf("发现应答数值字段不符: %+v (port want %d)", resp, ts.srv.Port())
	}
}

func TestUDPMoveAndEcho(t *testing.T) {
	ts := newTestServer(t, nil)
	token := addClient(t, ts.store, "udp手机")
	uc, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: ts.srv.Port()})
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	t.Cleanup(func() { uc.Close() })

	sendUDP := func(pkt proto.Packet) {
		t.Helper()
		proto.Seal(&pkt, token)
		buf, err := proto.Encode(&pkt)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if _, err := uc.Write(buf); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	sendUDP(proto.NewMove(1, 12, -34))
	sendUDP(proto.NewScroll(2, 240))
	waitFor(t, "UDP MOVE/SCROLL 注入", func() bool {
		return ts.cap.count(evRel, 0, 12) == 1 && ts.cap.count(evRel, 1, -34) == 1 && ts.cap.count(evRel, 11, 240) == 1
	})

	// UDP ECHO 回传
	uc.SetReadDeadline(time.Now().Add(2 * time.Second))
	sendUDP(proto.NewEcho(3, 777))
	buf := make([]byte, 2048)
	n, _, err := uc.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("ECHO 回读: %v", err)
	}
	reply, err := proto.Decode(buf[:n])
	if err != nil || reply.Type != proto.TypeEcho {
		t.Fatalf("got (%v, %v), want ECHO", reply.Type, err)
	}
	if reply.Echo.TsMs != 777 || !proto.VerifyHMAC(&reply, token) {
		t.Errorf("UDP ECHO 回传不符: ts=%d hmac=%v", reply.Echo.TsMs, proto.VerifyHMAC(&reply, token))
	}

	// 错认证丢弃：无响应、无注入
	bad := proto.NewMove(4, 99, 99)
	proto.Seal(&bad, []byte("wrong-token-wrong-token-0000"))
	raw, _ := proto.Encode(&bad)
	uc.Write(raw)
	before := ts.srv.StatsSnapshot()
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		uc.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		if n, _, err := uc.ReadFromUDP(buf); err == nil {
			t.Fatalf("错认证包不应有响应，实得 %dB", n)
		}
		if ts.srv.StatsSnapshot().HMACFail > before.HMACFail {
			break
		}
	}
	if got := ts.srv.StatsSnapshot().HMACFail; got != before.HMACFail+1 {
		t.Errorf("UDP 错认证未计数: %d → %d", before.HMACFail, got)
	}
	waitFor(t, "错包不注入", func() bool { return ts.cap.count(evRel, 0, 99) == 0 })
}

// ---- 辅助 ----

// prepair 直接经存储预置一个已配对客户端，返回其 token。
func prepair(t *testing.T, ts *testSrv) []byte {
	t.Helper()
	return addClient(t, ts.store, "预配对")
}

func addClient(t *testing.T, store *pairing.Store, name string) []byte {
	t.Helper()
	c, _, err := store.Issue(name, "")
	if err != nil {
		t.Fatalf("store.Issue: %v", err)
	}
	tok, err := c.Token()
	if err != nil {
		t.Fatalf("Token(): %v", err)
	}
	return tok
}

func authEcho(t *testing.T, c *tcpClient, token []byte) {
	t.Helper()
	echo := proto.NewEcho(1, 1)
	proto.Seal(&echo, token)
	c.send(echo)
	_ = c.mustRecv(time.Second)
}

func wrongCode(code string, delta int) string {
	d := int(code[3]-'0') + delta
	d %= 10
	return code[:3] + string(rune('0'+d))
}

// 配对宽限读超时：宽限截止前取宽限值，过期后回落基准。
func TestEffectiveReadTimeout(t *testing.T) {
	base := 10 * time.Second
	now := time.Now()
	if got := effectiveReadTimeout(base, now.Add(70*time.Second), now); got != 70*time.Second {
		t.Errorf("宽限内 = %v, want 70s", got)
	}
	if got := effectiveReadTimeout(base, now.Add(5*time.Second), now); got != base {
		t.Errorf("宽限短于基准 = %v, want %v", got, base)
	}
	if got := effectiveReadTimeout(base, time.Time{}, now); got != base {
		t.Errorf("无宽限 = %v, want %v", got, base)
	}
	if got := effectiveReadTimeout(base, now.Add(-time.Second), now); got != base {
		t.Errorf("宽限已过期 = %v, want %v", got, base)
	}
}
