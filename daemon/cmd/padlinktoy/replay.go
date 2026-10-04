package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"time"

	"padlink/daemon/internal/proto"
)

// typeOrder 固定统计打印顺序。
var typeOrder = []proto.Type{
	proto.TypeMove, proto.TypeScroll, proto.TypeButton, proto.TypeKey,
	proto.TypeText, proto.TypeEcho, proto.TypeHello, proto.TypeBye,
}

type replayOptions struct {
	Path  string
	Host  string
	Port  int
	Token []byte
	Speed float64
}

type replayStats struct {
	Total        int
	Sent         int
	Failed       int
	SentByType   map[proto.Type]int
	ErrsReceived int
	AgreedVer    uint16
	Elapsed      time.Duration
}

// runReplay 将 .plrec 回放到目标 daemon：TCP 建连 + 未认证 HELLO →
// 按时间戳节奏发送（通道 0 走 UDP 单播，通道 1 走已封签 TCP）→ 封签 BYE 优雅退出。
func runReplay(o replayOptions) (replayStats, error) {
	var st replayStats
	if o.Speed <= 0 {
		return st, fmt.Errorf("speed 必须 > 0")
	}
	f, err := os.Open(o.Path)
	if err != nil {
		return st, fmt.Errorf("打开录制: %w", err)
	}
	defer f.Close()
	hdr, frames, err := ReadPLREC(f)
	if err != nil {
		return st, err
	}
	if len(frames) == 0 {
		return st, fmt.Errorf("录制为空")
	}
	if !hdr.Sealed && len(o.Token) == 0 {
		return st, fmt.Errorf("录制为 raw（未封签），回放需要 -token 或 PADLINK_TOKEN")
	}
	st.Total = len(frames)
	st.SentByType = make(map[proto.Type]int, 4)

	conn, agree, err := dialHello(o.Host, o.Port, 3*time.Second)
	if err != nil {
		return st, err
	}
	defer conn.Close()
	st.AgreedVer = agree

	var errsRecv atomic.Int64
	rxExited := make(chan struct{})
	go func() {
		defer close(rxExited)
		br := bufio.NewReader(conn)
		for {
			frame, err := readFrame(br)
			if err != nil {
				return
			}
			if proto.Type(frame[3]) == proto.TypeErr {
				errsRecv.Add(1)
			}
		}
	}()

	var udp *net.UDPConn
	if hasChannel(frames, ChanUDP) {
		udp, err = dialUDP(o.Host, o.Port)
		if err != nil {
			return st, err
		}
		defer udp.Close()
	}

	start := time.Now()
	for _, fr := range frames {
		if d := start.Add(scaleMs(fr.RelMs, o.Speed)).Sub(time.Now()); d > 0 {
			time.Sleep(d)
		}
		data := fr.Data
		if !hdr.Sealed {
			data, err = sealRaw(fr.Data, o.Token)
			if err != nil {
				st.Failed++
				continue
			}
		}
		if fr.Chan == ChanUDP {
			_, err = udp.Write(data)
		} else {
			_, err = conn.Write(data)
		}
		if err != nil {
			st.Failed++
			continue
		}
		st.Sent++
		st.SentByType[proto.Type(data[3])]++
	}

	if len(o.Token) > 0 {
		bye := proto.NewRaw(proto.TypeBye, 1, nil)
		proto.Seal(&bye, o.Token)
		if buf, err := proto.Encode(&bye); err == nil {
			_, _ = conn.Write(buf)
		}
	}
	conn.Close() // BYE 后 daemon 关连接；无 token 时直接收尾
	select {
	case <-rxExited:
	case <-time.After(2 * time.Second):
	}

	st.ErrsReceived = int(errsRecv.Load())
	st.Elapsed = time.Since(start)
	return st, nil
}

// scaleMs 换算回放目标延时（speed > 1 加速）。
func scaleMs(relMs uint64, speed float64) time.Duration {
	return time.Duration(float64(relMs) / speed * float64(time.Millisecond))
}

func hasChannel(frames []Frame, ch Channel) bool {
	for _, f := range frames {
		if f.Chan == ch {
			return true
		}
	}
	return false
}

func replayMain(args []string) error {
	fs := flag.NewFlagSet("padlinktoy replay", flag.ExitOnError)
	file := fs.String("f", "", ".plrec 录制文件（必需）")
	host := fs.String("host", "", "目标 daemon 主机/IP（必需）")
	port := fs.Int("port", defaultPort, "目标端口")
	tokenHex := fs.String("token", "", "token（hex，缺省读 PADLINK_TOKEN；raw 录制必需，sealed 录制可选——用于封签 BYE）")
	speed := fs.Float64("speed", 1.0, "回放速度因子（>1 加速，<1 减速）")
	fs.Parse(args)
	if fs.NArg() > 0 {
		return fmt.Errorf("多余参数 %q", fs.Args())
	}
	tok, err := resolveToken(*tokenHex)
	if err != nil {
		return err
	}
	if *file == "" {
		return fmt.Errorf("需要 -f <file>")
	}
	if *host == "" {
		return fmt.Errorf("需要 -host <host>")
	}

	st, err := runReplay(replayOptions{Path: *file, Host: *host, Port: *port, Token: tok, Speed: *speed})
	if err != nil {
		return err
	}
	fmt.Printf("回放完成（协商版本 v%d）: 共 %d 帧，成功 %d，失败 %d，ERR 收到 %d，耗时 %s\n",
		st.AgreedVer, st.Total, st.Sent, st.Failed, st.ErrsReceived, st.Elapsed.Round(time.Millisecond))
	for _, t := range typeOrder {
		if n := st.SentByType[t]; n > 0 {
			fmt.Printf("  %-13s %d\n", t, n)
		}
	}
	return nil
}
