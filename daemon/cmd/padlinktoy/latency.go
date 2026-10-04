package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"padlink/daemon/internal/proto"
)

// loadRate 是 --load 的 MOVE 背景负载速率（PRD §8 退化测量口径：1000/s）。
const loadRate = 1000

type latencyOptions struct {
	Host     string
	Port     int
	Token    []byte
	HZ       int
	Duration time.Duration
	Load     bool
}

// rttSummary 是一次压测的机器可读汇总（毫秒）。
type rttSummary struct {
	HZ        int     `json:"hz"`
	DurationS float64 `json:"duration_s"`
	Sent      int     `json:"sent"`
	Replies   int     `json:"replies"`
	Lost      int     `json:"lost"`
	LoadSent  int64   `json:"load_sent,omitempty"`
	MinMs     float64 `json:"min_ms"`
	P50Ms     float64 `json:"p50_ms"`
	P90Ms     float64 `json:"p90_ms"`
	P95Ms     float64 `json:"p95_ms"`
	P99Ms     float64 `json:"p99_ms"`
	MaxMs     float64 `json:"max_ms"`
}

// runLatency 以 --hz 频率经 TCP 发送封签 ECHO 并按回包计算 RTT；
// --load 叠加 1000/s UDP MOVE 背景负载测 RTT 退化。
func runLatency(o latencyOptions) (rttSummary, error) {
	var sum rttSummary
	if o.Host == "" {
		return sum, fmt.Errorf("需要 -host")
	}
	if len(o.Token) == 0 {
		return sum, fmt.Errorf("latency 需要 token（-token 或 PADLINK_TOKEN）：ECHO 须认证才有回包")
	}
	if o.HZ <= 0 || o.HZ > 1000 {
		return sum, fmt.Errorf("--hz 取值 1..1000")
	}
	if o.Duration <= 0 {
		return sum, fmt.Errorf("--duration 必须 > 0")
	}
	if o.Port == 0 {
		o.Port = defaultPort
	}

	conn, _, err := dialHello(o.Host, o.Port, 3*time.Second)
	if err != nil {
		return sum, err
	}
	defer conn.Close()
	br := bufio.NewReader(conn)
	if err := bindSession(br, conn, o.Token, 2*time.Second); err != nil {
		return sum, err
	}

	var (
		mu          sync.Mutex
		outstanding []time.Time
		samples     []time.Duration
	)
	rxErr := make(chan error, 1)
	go func() {
		for {
			frame, err := readFrame(br)
			if err != nil {
				rxErr <- err
				return
			}
			if proto.Type(frame[3]) != proto.TypeEcho {
				continue
			}
			now := time.Now()
			mu.Lock()
			if len(outstanding) > 0 { // TCP 有序，回包与发送 FIFO 一一对应
				samples = append(samples, now.Sub(outstanding[0]))
				outstanding = outstanding[1:]
			}
			mu.Unlock()
		}
	}()

	var loadSent atomic.Int64
	stopLoad := make(chan struct{})
	loadDone := make(chan struct{})
	if o.Load {
		go func() {
			defer close(loadDone)
			runLoad(o.Host, o.Port, o.Token, stopLoad, &loadSent)
		}()
	}

	sent := 0
	var seq uint16
	ticker := time.NewTicker(time.Second / time.Duration(o.HZ))
	defer ticker.Stop()
	timeout := time.After(o.Duration)
measure:
	for {
		select {
		case <-timeout:
			break measure
		case err := <-rxErr:
			return sum, fmt.Errorf("连接中断: %w", err)
		case <-ticker.C:
			seq++
			pkt := proto.NewEcho(seq, time.Now().UnixMilli())
			proto.Seal(&pkt, o.Token)
			buf, err := proto.Encode(&pkt)
			if err != nil {
				return sum, err
			}
			now := time.Now()
			if _, err := conn.Write(buf); err != nil {
				return sum, fmt.Errorf("发送 ECHO 失败: %w", err)
			}
			sent++
			mu.Lock()
			outstanding = append(outstanding, now)
			mu.Unlock()
		}
	}
	close(stopLoad)
	if o.Load {
		<-loadDone
	}

	// 收尾：等待未决回包（≤500ms）
	drainDeadline := time.Now().Add(500 * time.Millisecond)
	for {
		mu.Lock()
		n := len(outstanding)
		mu.Unlock()
		if n == 0 || time.Now().After(drainDeadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	conn.Close() // 读 goroutine 随 readFrame 错误退出（rxErr 带缓冲，不阻塞）

	mu.Lock()
	lost := len(outstanding)
	got := append([]time.Duration(nil), samples...)
	mu.Unlock()

	rstats, ok := summarizeLatencies(got)
	if !ok {
		return sum, fmt.Errorf("未收到任何 ECHO 回包")
	}
	return rttSummary{
		HZ: o.HZ, DurationS: o.Duration.Seconds(),
		Sent: sent, Replies: len(got), Lost: lost,
		LoadSent: loadSent.Load(),
		MinMs:    rstats.Min, P50Ms: rstats.P50, P90Ms: rstats.P90,
		P95Ms: rstats.P95, P99Ms: rstats.P99, MaxMs: rstats.Max,
	}, nil
}

// bindSession 发送一个封签 ECHO 并等待回包，完成会话↔token 绑定（不计时，
// 避免把首包认证开销计入 RTT 样本）。
func bindSession(br *bufio.Reader, conn net.Conn, tok []byte, timeout time.Duration) error {
	pkt := proto.NewEcho(1, time.Now().UnixMilli())
	proto.Seal(&pkt, tok)
	buf, err := proto.Encode(&pkt)
	if err != nil {
		return err
	}
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	defer conn.SetDeadline(time.Time{})
	if _, err := conn.Write(buf); err != nil {
		return fmt.Errorf("发送绑定 ECHO: %w", err)
	}
	frame, err := readFrame(br)
	if err != nil {
		return fmt.Errorf("等待绑定回包: %w", err)
	}
	rp, err := proto.Decode(frame)
	if err != nil || rp.Type != proto.TypeEcho {
		return fmt.Errorf("绑定回包异常（frame=%dB, err=%v）", len(frame), err)
	}
	return nil
}

// runLoad 以 loadRate/s 经 UDP 发送封签 MOVE（±1 交替，净位移为零）。
func runLoad(host string, port int, tok []byte, stop chan struct{}, sent *atomic.Int64) {
	uc, err := dialUDP(host, port)
	if err != nil {
		return
	}
	defer uc.Close()
	pace := newPacer(loadRate)
	dx := int16(1)
	var seq uint16
	for {
		select {
		case <-stop:
			return
		default:
		}
		pace.wait()
		pkt := proto.NewMove(seq, dx, 0)
		seq++
		dx = -dx
		proto.Seal(&pkt, tok)
		buf, err := proto.Encode(&pkt)
		if err != nil {
			return
		}
		if _, err := uc.Write(buf); err == nil {
			sent.Add(1)
		}
	}
}

func latencyMain(args []string) error {
	fs := flag.NewFlagSet("padlinktoy latency", flag.ExitOnError)
	host := fs.String("host", "", "目标 daemon 主机/IP（必需）")
	port := fs.Int("port", defaultPort, "目标端口")
	tokenHex := fs.String("token", "", "token（hex，缺省读 PADLINK_TOKEN，必需）")
	hz := fs.Int("hz", 50, "ECHO 频率（Hz）")
	duration := fs.String("duration", "10s", "压测时长（Go duration，如 10s / 1m）")
	load := fs.Bool("load", false, "叠加 1000/s UDP MOVE 背景负载（测 RTT 退化）")
	asJSON := fs.Bool("json", false, "以 JSON 输出")
	fs.Parse(args)
	if fs.NArg() > 0 {
		return fmt.Errorf("多余参数 %q", fs.Args())
	}
	tok, err := resolveToken(*tokenHex)
	if err != nil {
		return err
	}
	d, err := time.ParseDuration(*duration)
	if err != nil {
		return fmt.Errorf("--duration: %w", err)
	}

	sum, err := runLatency(latencyOptions{Host: *host, Port: *port, Token: tok, HZ: *hz, Duration: d, Load: *load})
	if err != nil {
		return err
	}
	if *asJSON {
		b, err := json.MarshalIndent(sum, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	loadNote := ""
	if sum.LoadSent > 0 {
		loadNote = fmt.Sprintf("（背景负载 MOVE %d 帧）", sum.LoadSent)
	}
	fmt.Printf("ECHO RTT @%dHz × %s：发送 %d，回包 %d，丢包 %d%s\n",
		sum.HZ, *duration, sum.Sent, sum.Replies, sum.Lost, loadNote)
	fmt.Printf("min=%.3f p50=%.3f p90=%.3f p95=%.3f p99=%.3f max=%.3f (ms)\n",
		sum.MinMs, sum.P50Ms, sum.P90Ms, sum.P95Ms, sum.P99Ms, sum.MaxMs)
	fmt.Println("注：此为传输层 RTT 分量；触摸→光标端到端延迟按 PRD §8 需真机录屏逐帧口径测量。")
	return nil
}
