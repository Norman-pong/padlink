package session

import (
	"errors"
	"net"
	"time"

	"padlink/daemon/internal/proto"
)

// udpLoop 处理 UDP 数据面：未认证 HELLO → DISCOVER_RESP 单播；
// FlagAuth 的 MOVE/SCROLL → 注入；ECHO → 回传；其余未认证丢弃计数。
func (s *Server) udpLoop(uc *net.UDPConn) {
	defer s.internalWG.Done()
	buf := make([]byte, proto.HeaderSize+proto.MaxPayload)
	for {
		n, addr, err := uc.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			select {
			case <-s.closed:
				return
			default:
			}
			s.vlogf("UDP 读取错误: %v", err)
			time.Sleep(10 * time.Millisecond) // 防异常错误空转
			continue
		}
		s.handleUDP(buf[:n], addr)
	}
}

func (s *Server) handleUDP(data []byte, addr *net.UDPAddr) {
	now := time.Now()
	// 全局桶先于 HMAC 拦洪泛（未认证流量的唯一防线）；已认证包在 matchToken
	// 命中后退还全局令牌、改记客户端桶——单设备高频不再饿死其他设备。
	if !s.limiter.allow(now) {
		s.stats.Dropped.Add(1)
		return
	}
	pkt, err := proto.Decode(data)
	if err != nil {
		s.stats.Dropped.Add(1)
		return
	}
	s.vlogf("UDP ← %s %v seq=%d auth=%v plen=%d", addr, pkt.Type, pkt.Seq, pkt.Flags&proto.FlagAuth != 0, len(pkt.Payload))
	if pkt.Type == proto.TypeHello {
		// 发现包不认证，应答不含敏感信息（JSON 作为 DISCOVER_RESP payload）
		reply := proto.NewRaw(proto.TypeDiscoverResp, uint16(s.udpSeq.Add(1)), s.discoverBody())
		body, err := proto.Encode(&reply)
		if err != nil {
			s.logf("DISCOVER_RESP 编码失败: %v", err)
			return
		}
		// 写失败必须可见：发现应答发不出去时手机侧表现为"搜不到这台电脑"，
		// 静默吞掉会把排查方向带偏。
		if _, werr := s.udp.WriteToUDP(body, addr); werr != nil {
			s.logf("DISCOVER_RESP 发送失败（目标 %s）: %v", addr, werr)
		}
		return
	}
	if pkt.Flags&proto.FlagAuth == 0 {
		s.stats.Dropped.Add(1)
		return
	}
	client, tok, ok := s.matchToken(&pkt)
	if !ok {
		s.stats.HMACFail.Add(1)
		return
	}
	s.limiter.refund()
	if !s.udpClientAllow(client.ID, now) {
		s.stats.Dropped.Add(1)
		return
	}
	// 控制权与 TCP 同闸：MOVE/SCROLL 是光标与滚动主通道，
	// 不过闸则非持权设备仍能驱动指针（裁决形同虚设）；ECHO 属心跳，不受门控。
	if isControlType(pkt.Type) && !s.admitControl(client.ID) {
		return
	}
	switch pkt.Type {
	case proto.TypeMove:
		dx, dy := pkt.Move.DX, pkt.Move.DY
		s.tryEnqueue(s.doInj("UDP MOVE", func() error { return s.inj.Move(dx, dy) }))
	case proto.TypeScroll:
		dy := pkt.Scroll.DyHiRes
		s.tryEnqueue(s.doInj("UDP SCROLL", func() error { return s.inj.Scroll(dy) }))
	case proto.TypeEcho:
		reply := proto.NewEcho(uint16(s.udpSeq.Add(1)), pkt.Echo.TsMs)
		proto.Seal(&reply, tok)
		buf, err := proto.Encode(&reply)
		if err != nil {
			s.logf("ECHO 应答编码失败: %v", err) // 定长 payload 不可能超限，防御分支
			return
		}
		_, _ = s.udp.WriteToUDP(buf, addr)
	default:
		// BUTTON/KEY/TEXT 属 TCP 专属；UDP 入站视为异常丢弃
		s.stats.Dropped.Add(1)
	}
}
