package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"padlink/daemon/internal/proto"
)

// readFrame 从 TCP 流按 19B 头声明的 payload_len 精确读取一帧。
// 超时由调用方经 conn.SetDeadline 控制。
func readFrame(br *bufio.Reader) ([]byte, error) {
	var head [proto.HeaderSize]byte
	if _, err := io.ReadFull(br, head[:]); err != nil {
		return nil, err
	}
	plen := int(binary.BigEndian.Uint16(head[7:9]))
	if plen > proto.MaxPayload {
		return nil, fmt.Errorf("payload_len %d 超限", plen)
	}
	frame := make([]byte, proto.HeaderSize+plen)
	copy(frame, head[:])
	if _, err := io.ReadFull(br, frame[proto.HeaderSize:]); err != nil {
		return nil, err
	}
	return frame, nil
}

// dialHello 建立 TCP 连接并完成未认证 HELLO 握手，返回连接与协商版本。
func dialHello(host string, port int, timeout time.Duration) (net.Conn, uint16, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, 0, fmt.Errorf("连接 %s: %w", addr, err)
	}
	hello := proto.NewRaw(proto.TypeHello, 1, []byte{byte(proto.Ver >> 8), byte(proto.Ver)})
	buf, err := proto.Encode(&hello)
	if err != nil {
		conn.Close()
		return nil, 0, err
	}
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		conn.Close()
		return nil, 0, err
	}
	if _, err := conn.Write(buf); err != nil {
		conn.Close()
		return nil, 0, fmt.Errorf("发送 HELLO: %w", err)
	}
	frame, err := readFrame(bufio.NewReader(conn))
	if err != nil {
		conn.Close()
		return nil, 0, fmt.Errorf("等待 HELLO 回应: %w", err)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return nil, 0, err
	}
	pkt, err := proto.Decode(frame)
	if err != nil || pkt.Type != proto.TypeHello || len(pkt.Payload) != 2 {
		conn.Close()
		return nil, 0, fmt.Errorf("HELLO 回应异常（frame=%dB, err=%v）", len(frame), err)
	}
	return conn, binary.BigEndian.Uint16(pkt.Payload), nil
}

// dialUDP 建立到 host:port 的已连接 UDP socket（单播）。
func dialUDP(host string, port int) (*net.UDPConn, error) {
	raddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("解析 %s: %w", host, err)
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return nil, fmt.Errorf("打开 UDP: %w", err)
	}
	return conn, nil
}

// sealRaw 对未封签线帧封签（回放 raw 录制时逐帧调用）。
func sealRaw(frame []byte, tok []byte) ([]byte, error) {
	pkt, err := proto.Decode(frame)
	if err != nil {
		return nil, err
	}
	proto.Seal(&pkt, tok)
	return proto.Encode(&pkt)
}

// pacer 以固定速率放行发送（fuzz 与背景负载共用）。
type pacer struct {
	interval time.Duration
	next     time.Time
}

func newPacer(ratePerSec int) *pacer {
	return &pacer{interval: time.Second / time.Duration(ratePerSec)}
}

// wait 阻塞到下一个发送槽；落后过多时重置，避免补偿性突发。
func (p *pacer) wait() {
	now := time.Now()
	if p.next.IsZero() {
		p.next = now
	}
	if d := p.next.Sub(now); d > 0 {
		time.Sleep(d)
	}
	p.next = p.next.Add(p.interval)
	if p.next.Before(time.Now()) {
		p.next = time.Now()
	}
}
