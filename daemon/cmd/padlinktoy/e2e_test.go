package main

// 端到端用例：测试内装配真实 session.Server（fake writer + 已知 token，回环随机端口），
// 跑 record→replay 闭环、fuzz 活性、latency 统计。

import (
	"bytes"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"padlink/daemon/internal/inject"
	"padlink/daemon/internal/pairing"
	"padlink/daemon/internal/proto"
	"padlink/daemon/internal/session"
)

type capKind uint8

const (
	capKey capKind = iota
	capRel
	capSync
)

type capEvent struct {
	kind  capKind
	code  uint16
	value int32
}

type capWriter struct {
	mu  sync.Mutex
	evs []capEvent
}

func (w *capWriter) KeyEvent(code uint16, value int32) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.evs = append(w.evs, capEvent{capKey, code, value})
	return nil
}

func (w *capWriter) RelEvent(code uint16, value int32) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.evs = append(w.evs, capEvent{capRel, code, value})
	return nil
}

// SetPointerState 空实现：本 harness 只断言事件序，不消费多击序号。
func (w *capWriter) SetPointerState(st inject.PointerState) {}

func (w *capWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.evs = append(w.evs, capEvent{kind: capSync})
	return nil
}

func (w *capWriter) Close() error { return nil }

func (w *capWriter) sumRel(code uint16) int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	var total int64
	for _, e := range w.evs {
		if e.kind == capRel && e.code == code {
			total += int64(e.value)
		}
	}
	return total
}

func (w *capWriter) count(kind capKind, code uint16, value int32) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, e := range w.evs {
		if e.kind == kind && e.code == code && e.value == value {
			n++
		}
	}
	return n
}

func (w *capWriter) syncCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, e := range w.evs {
		if e.kind == capSync {
			n++
		}
	}
	return n
}

var _ inject.DeviceWriter = (*capWriter)(nil)

type toyServer struct {
	srv   *session.Server
	store *pairing.Store
	cap   *capWriter
	texts chan string
}

// pickFreePort 先向内核要一个空闲 TCP 端口再交还：session.New 会把 Port=0 归一成默认端口
// 53021（不是临时端口），固定端口会与本包及 internal/session 的并行测试二进制互相撞端口。
func pickFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pickFreePort: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func newToyServer(t *testing.T) *toyServer {
	t.Helper()
	store, err := pairing.Load(filepath.Join(t.TempDir(), "clients.json"))
	if err != nil {
		t.Fatalf("pairing.Load: %v", err)
	}
	mgr := pairing.NewManager(store)
	mgr.SetNotify(func(string) {})
	ts := &toyServer{store: store, cap: &capWriter{}, texts: make(chan string, 8)}
	inj := inject.NewInjector(ts.cap, inject.Config{RepeatDelay: time.Hour, RepeatInterval: time.Hour})
	cfg := session.Config{
		Port:          pickFreePort(t),
		DaemonVersion: "toytest",
		Hostname:      "toyhost",
		Log:           log.New(io.Discard, "", 0),
	}
	ts.srv = session.New(cfg, store, mgr, inj, func(clientID string, text string) error {
		select {
		case ts.texts <- text:
		default: // 防异常用例灌满通道阻塞注入 goroutine
		}
		return nil
	})
	if err := ts.srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ts.srv.Stop()
		inj.Close()
	})
	return ts
}

func addToken(t *testing.T, ts *toyServer) []byte {
	t.Helper()
	c, _, err := ts.store.Issue("toy客户端", "")
	if err != nil {
		t.Fatalf("store.Add: %v", err)
	}
	tok, err := c.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	return tok
}

// waitFor 轮询断言异步注入事件到达。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待 %s 超时", what)
}

func TestRecordReplayClosedLoop(t *testing.T) {
	t.Run("raw 录制 + 回放封签", func(t *testing.T) { closedLoop(t, false) })
	t.Run("sealed 录制 + 原样回放", func(t *testing.T) { closedLoop(t, true) })
}

func closedLoop(t *testing.T, sealed bool) {
	const (
		segs    = 36
		notches = 2
	)
	ts := newToyServer(t)
	tok := addToken(t, ts)

	spec := synthSpec{CircleR: 50, CircleSegs: segs, ScrollNotches: notches, Buttons: true, Text: "padlink 闭环", GapMs: 1}
	var buf bytes.Buffer
	recTok := []byte(nil)
	if sealed {
		recTok = tok
	}
	if err := runRecord(recordOptions{Out: &buf, ErrOut: io.Discard, Token: recTok, Spec: spec}); err != nil {
		t.Fatalf("runRecord: %v", err)
	}
	wantMode := "raw"
	if sealed {
		wantMode = "sealed"
	}
	firstLine, _, _ := strings.Cut(buf.String(), "\n")
	if firstLine != "PLREC 1 "+wantMode {
		t.Fatalf("头行 = %q, want %q", firstLine, "PLREC 1 "+wantMode)
	}

	path := filepath.Join(t.TempDir(), "loop.plrec")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	replayTok := []byte(nil)
	if !sealed {
		replayTok = tok
	}
	st, err := runReplay(replayOptions{Path: path, Host: "127.0.0.1", Port: ts.srv.Port(), Token: replayTok, Speed: 1})
	if err != nil {
		t.Fatalf("runReplay: %v", err)
	}
	if st.AgreedVer != proto.Ver {
		t.Errorf("协商版本 = %d, want %d", st.AgreedVer, proto.Ver)
	}

	total := segs + notches + 4 + 1 // 圆周 + 滚动 + 左右键 down/up + TEXT
	if st.Sent != total || st.Failed != 0 {
		t.Errorf("回放统计: sent=%d failed=%d, want %d/0", st.Sent, st.Failed, total)
	}

	// TEXT 不产生注入事件：Sync 数 = MOVE + SCROLL + 按键 4 次
	wantSync := segs + notches + 4
	waitFor(t, "注入完成", func() bool { return ts.cap.syncCount() == wantSync })

	if x, y := ts.cap.sumRel(inject.RelX), ts.cap.sumRel(inject.RelY); x != 0 || y != 0 {
		t.Errorf("圆周净位移 ΣX=%d ΣY=%d, want 0/0", x, y)
	}
	// legacy REL_WHEEL 帧内向零取整补发 +1
	if got := ts.cap.count(capRel, inject.RelWheelHiRes, 120); got != notches {
		t.Errorf("REL_WHEEL_HI_RES +120 = %d 次, want %d", got, notches)
	}
	if got := ts.cap.count(capRel, inject.RelWheel, 1); got != notches {
		t.Errorf("legacy REL_WHEEL +1 = %d 次, want %d", got, notches)
	}
	if got := ts.cap.count(capKey, inject.BtnLeft, 1); got != 1 {
		t.Errorf("BTN_LEFT down = %d 次, want 1", got)
	}
	if got := ts.cap.count(capKey, inject.BtnLeft, 0); got != 1 {
		t.Errorf("BTN_LEFT up = %d 次, want 1", got)
	}
	if got := ts.cap.count(capKey, inject.BtnRight, 1); got != 1 {
		t.Errorf("BTN_RIGHT down = %d 次, want 1", got)
	}
	if got := ts.cap.count(capKey, inject.BtnRight, 0); got != 1 {
		t.Errorf("BTN_RIGHT up = %d 次, want 1", got)
	}
	select {
	case got := <-ts.texts:
		if got != spec.Text {
			t.Errorf("TEXT = %q, want %q", got, spec.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("TEXT 未送达")
	}
}

func TestFuzzKeepsDaemonAlive(t *testing.T) {
	t.Run("带 token（含认证路径变异）", func(t *testing.T) {
		ts := newToyServer(t)
		tok := addToken(t, ts)
		st, err := runFuzz(fuzzOptions{Host: "127.0.0.1", Port: ts.srv.Port(), Token: tok, Seconds: 2, Rate: 300})
		if err != nil {
			t.Fatalf("runFuzz: %v", err)
		}
		if st.SentTCP+st.SentUDP == 0 {
			t.Fatal("未发送任何 fuzz 包")
		}
		if !st.Alive {
			t.Fatalf("daemon 无响应: %s（TCP 被断 %d 次）", st.AliveHow, st.TCPCuts)
		}
	})
	t.Run("无 token", func(t *testing.T) {
		ts := newToyServer(t)
		st, err := runFuzz(fuzzOptions{Host: "127.0.0.1", Port: ts.srv.Port(), Seconds: 1, Rate: 200})
		if err != nil {
			t.Fatalf("runFuzz: %v", err)
		}
		if !st.Alive {
			t.Fatalf("daemon 无响应: %s", st.AliveHow)
		}
	})
}

func TestLatencySmoke(t *testing.T) {
	ts := newToyServer(t)
	tok := addToken(t, ts)
	sum, err := runLatency(latencyOptions{Host: "127.0.0.1", Port: ts.srv.Port(), Token: tok, HZ: 50, Duration: time.Second})
	if err != nil {
		t.Fatalf("runLatency: %v", err)
	}
	if sum.Sent < 20 || sum.Replies < 20 {
		t.Errorf("统计非空校验: sent=%d replies=%d, want ≥20", sum.Sent, sum.Replies)
	}
	if sum.Lost > 2 {
		t.Errorf("丢包 %d, want ≤2（回环）", sum.Lost)
	}
	if sum.P50Ms <= 0 {
		t.Errorf("P50 = %v ms, want > 0", sum.P50Ms)
	}
}

func TestLatencyWithLoadSmoke(t *testing.T) {
	ts := newToyServer(t)
	tok := addToken(t, ts)
	sum, err := runLatency(latencyOptions{
		Host: "127.0.0.1", Port: ts.srv.Port(), Token: tok,
		HZ: 50, Duration: 700 * time.Millisecond, Load: true,
	})
	if err != nil {
		t.Fatalf("runLatency: %v", err)
	}
	if sum.LoadSent < 100 {
		t.Errorf("背景负载发送 %d 帧, want ≥100", sum.LoadSent)
	}
	if sum.Replies == 0 {
		t.Error("无 ECHO 回包")
	}
}
