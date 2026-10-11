package main

import (
	"bufio"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"padlink/daemon/internal/proto"
)

type fuzzOptions struct {
	Host    string
	Port    int
	Token   []byte // 可选；提供则叠加认证路径变异，并以封签 ECHO 做活性探测
	Seconds int
	Rate    int
}

type fuzzStats struct {
	SentTCP  int
	SentUDP  int
	Errs     int
	TCPCuts  int
	Alive    bool
	AliveHow string
	Elapsed  time.Duration
}

// fuzzTCP 维护一条 fuzz 用 TCP 会话：服务端断开或写失败后自动重连。
type fuzzTCP struct {
	host string
	port int
	tok  []byte
	errs *atomic.Int64
	cuts *atomic.Int64

	conn net.Conn
	dead chan struct{}
	wg   sync.WaitGroup
}

func (f *fuzzTCP) ensure() error {
	if f.conn != nil {
		select {
		case <-f.dead:
			f.dropConn()
		default:
			return nil
		}
	}
	conn, _, err := dialHello(f.host, f.port, 2*time.Second)
	if err != nil {
		return err
	}
	f.conn = conn
	f.dead = make(chan struct{})
	dead := f.dead
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		defer close(dead)
		br := bufio.NewReader(conn)
		for {
			frame, err := readFrame(br)
			if err != nil {
				return
			}
			if proto.Type(frame[3]) == proto.TypeErr {
				f.errs.Add(1)
			}
		}
	}()
	return nil
}

func (f *fuzzTCP) dropConn() {
	if f.conn != nil {
		f.cuts.Add(1)
		f.conn.Close()
		f.conn = nil
	}
}

func (f *fuzzTCP) finalClose() {
	if f.conn != nil {
		f.conn.Close()
		f.conn = nil
	}
}

// waitReaders 等读 goroutine 退出（≤1s），不留后台驻留。
func (f *fuzzTCP) waitReaders() {
	ch := make(chan struct{})
	go func() {
		f.wg.Wait()
		close(ch)
	}()
	select {
	case <-ch:
	case <-time.After(time.Second):
	}
}

func (f *fuzzTCP) send(data []byte) bool {
	if err := f.ensure(); err != nil {
		return false
	}
	if _, err := f.conn.Write(data); err != nil {
		f.dropConn()
		return false
	}
	return true
}

func (f *fuzzTCP) sendFragmented(rng *rand.Rand, data []byte) bool {
	if err := f.ensure(); err != nil {
		return false
	}
	parts := 2 + rng.Intn(3)
	for i := 0; i < parts; i++ {
		lo := i * len(data) / parts
		hi := (i + 1) * len(data) / parts
		if _, err := f.conn.Write(data[lo:hi]); err != nil {
			f.dropConn()
			return false
		}
		if i < parts-1 {
			time.Sleep(time.Duration(1+rng.Intn(2)) * time.Millisecond)
		}
	}
	return true
}

func legalFramePool(tok []byte) [][]byte {
	mk := []func(seq uint16) proto.Packet{
		func(seq uint16) proto.Packet { return proto.NewMove(seq, 3, -2) },
		func(seq uint16) proto.Packet { return proto.NewScroll(seq, 120) },
		func(seq uint16) proto.Packet { return proto.NewButton(seq, 1, true) },
		func(seq uint16) proto.Packet { return proto.NewKey(seq, 0x04, true) }, // HID A
		func(seq uint16) proto.Packet { return proto.NewText(seq, "padlink-fuzz") },
		func(seq uint16) proto.Packet { return proto.NewEcho(seq, time.Now().UnixMilli()) },
	}
	pool := make([][]byte, 0, len(mk))
	for i, f := range mk {
		pkt := f(uint16(i + 1))
		if len(tok) > 0 {
			proto.Seal(&pkt, tok)
		}
		b, err := proto.Encode(&pkt)
		if err != nil {
			continue
		}
		pool = append(pool, b)
	}
	return pool
}

// enableBroadcast 尽力开启 SO_BROADCAST；失败只影响广播路径，不算 fuzz 失败。
func enableBroadcast(uc *net.UDPConn) {
	rc, err := uc.SyscallConn()
	if err != nil {
		return
	}
	_ = rc.Control(func(fd uintptr) {
		_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
	})
}

// sendBroadcastHello 尽力向广播地址发 HELLO，失败静默忽略；回环目标须用定向广播 127.255.255.255。
func sendBroadcastHello(uc *net.UDPConn, raddr *net.UDPAddr, hello []byte) {
	ip4 := raddr.IP.To4()
	if ip4 == nil {
		return
	}
	bc := &net.UDPAddr{IP: net.IPv4bcast, Port: raddr.Port}
	if ip4.IsLoopback() {
		bc = &net.UDPAddr{IP: net.IPv4(127, 255, 255, 255), Port: raddr.Port}
	}
	_, _ = uc.WriteToUDP(hello, bc)
}

// runFuzz 以 --rate 上限变异注入 --seconds 秒后判定活性。断言口径：fuzz 不得使
// daemon panic/挂死；daemon 侧丢弃计数无法从外部读取，以活性与响应行为为准。
func runFuzz(o fuzzOptions) (fuzzStats, error) {
	var st fuzzStats
	if o.Host == "" {
		return st, fmt.Errorf("需要 -host")
	}
	if o.Seconds <= 0 {
		return st, fmt.Errorf("--seconds 必须 ≥ 1")
	}
	if o.Rate <= 0 {
		return st, fmt.Errorf("--rate 必须 ≥ 1")
	}
	if o.Port == 0 {
		o.Port = defaultPort
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	start := time.Now()
	deadline := start.Add(time.Duration(o.Seconds) * time.Second)
	pace := newPacer(o.Rate)

	uc, err := net.ListenUDP("udp", nil)
	if err != nil {
		return st, fmt.Errorf("打开 UDP: %w", err)
	}
	defer uc.Close()
	enableBroadcast(uc)
	raddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(o.Host, strconv.Itoa(o.Port)))
	if err != nil {
		return st, err
	}

	var errs, cuts atomic.Int64
	tc := &fuzzTCP{host: o.Host, port: o.Port, tok: o.Token, errs: &errs, cuts: &cuts}

	hello := proto.NewRaw(proto.TypeHello, 1, []byte{byte(proto.Ver >> 8), byte(proto.Ver)})
	helloBytes, err := proto.Encode(&hello)
	if err != nil {
		return st, err
	}
	burst := make([][]byte, 0, 24)
	for i := 0; i < 24; i++ {
		pkt := proto.NewMove(uint16(i+1), 1, -1)
		if len(o.Token) > 0 {
			proto.Seal(&pkt, o.Token)
		}
		b, err := proto.Encode(&pkt)
		if err != nil {
			return st, err
		}
		burst = append(burst, b)
	}
	pool := legalFramePool(o.Token)

	for time.Now().Before(deadline) {
		pace.wait()
		if rng.Intn(2) == 0 {
			data := helloBytes
			if rng.Intn(5) != 0 {
				data = mutate(rng, pool[rng.Intn(len(pool))], mutateKind(rng.Intn(int(mKindCount))))
			}
			if _, err := uc.WriteToUDP(data, raddr); err == nil {
				st.SentUDP++
			}
			if rng.Intn(25) == 0 {
				sendBroadcastHello(uc, raddr, helloBytes)
			}
		} else {
			switch {
			case rng.Intn(50) == 0:
				for _, b := range burst {
					if tc.send(b) {
						st.SentTCP++
					}
				}
			case rng.Intn(20) == 0:
				data := mutate(rng, pool[rng.Intn(len(pool))], mutateKind(rng.Intn(int(mKindCount))))
				if tc.sendFragmented(rng, data) {
					st.SentTCP++
				}
			default:
				data := mutate(rng, pool[rng.Intn(len(pool))], mutateKind(rng.Intn(int(mKindCount))))
				if tc.send(data) {
					st.SentTCP++
				}
			}
			if tc.conn == nil {
				time.Sleep(20 * time.Millisecond)
			}
		}
	}
	tc.finalClose()
	tc.waitReaders()

	st.Errs = int(errs.Load())
	st.TCPCuts = int(cuts.Load())
	st.Elapsed = time.Since(start)

	st.Alive, st.AliveHow = probeLiveness(o.Host, o.Port, o.Token, 800*time.Millisecond)
	return st, nil
}

// probeLiveness 连发 3 个 ECHO（有 token 则封签）判活性：ECHO 回包 = 认证路径存活，ERR 回包 = 协议处理路径存活。
func probeLiveness(host string, port int, tok []byte, timeout time.Duration) (bool, string) {
	for i := 0; i < 3; i++ {
		alive, how := probeOnce(host, port, tok, timeout)
		if alive {
			return true, how
		}
		time.Sleep(150 * time.Millisecond)
	}
	return false, "无响应"
}

func probeOnce(host string, port int, tok []byte, timeout time.Duration) (bool, string) {
	conn, _, err := dialHello(host, port, timeout)
	if err != nil {
		return false, ""
	}
	defer conn.Close()
	pkt := proto.NewEcho(1, time.Now().UnixMilli())
	if len(tok) > 0 {
		proto.Seal(&pkt, tok)
	}
	buf, err := proto.Encode(&pkt)
	if err != nil {
		return false, ""
	}
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return false, ""
	}
	if _, err := conn.Write(buf); err != nil {
		return false, ""
	}
	frame, err := readFrame(bufio.NewReader(conn))
	if err != nil {
		return false, ""
	}
	switch proto.Type(frame[3]) {
	case proto.TypeEcho:
		return true, "ECHO 回包（认证路径完好）"
	case proto.TypeErr:
		return true, "ERR 响应（协议处理路径存活）"
	default:
		return true, fmt.Sprintf("意外回包 %v（服务仍在处理）", proto.Type(frame[3]))
	}
}

func fuzzMain(args []string) error {
	fs := flag.NewFlagSet("padlinktoy fuzz", flag.ExitOnError)
	host := fs.String("host", "", "目标 daemon 主机/IP（必需）")
	port := fs.Int("port", defaultPort, "目标端口")
	tokenHex := fs.String("token", "", "token（hex，可选；缺省读 PADLINK_TOKEN，提供则叠加认证路径变异）")
	seconds := fs.Int("seconds", 10, "持续秒数")
	rate := fs.Int("rate", 500, "每秒发包数上限")
	fs.Parse(args)
	if fs.NArg() > 0 {
		return fmt.Errorf("多余参数 %q", fs.Args())
	}
	tok, err := resolveToken(*tokenHex)
	if err != nil {
		return err
	}

	st, err := runFuzz(fuzzOptions{Host: *host, Port: *port, Token: tok, Seconds: *seconds, Rate: *rate})
	if err != nil {
		return err
	}
	fmt.Printf("fuzz 完成: 发送 TCP=%d UDP=%d，ERR 收到=%d，TCP 被断=%d，耗时 %s\n",
		st.SentTCP, st.SentUDP, st.Errs, st.TCPCuts, st.Elapsed.Round(time.Millisecond))
	if st.Alive {
		fmt.Printf("活性判定: daemon 存活（%s）\n", st.AliveHow)
		return nil
	}
	fmt.Println("活性判定: daemon 无响应 —— 服务可能被打挂，请检查 daemon 进程与日志")
	return fmt.Errorf("fuzz 活性判定失败：daemon 无响应")
}
