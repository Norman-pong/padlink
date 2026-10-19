package session

import (
	"encoding/binary"
	"errors"
	"time"

	"padlink/daemon/internal/pairing"
	"padlink/daemon/internal/proto"
)

// effectiveReadTimeout 取基准与配对宽限中的较大者。
func effectiveReadTimeout(base time.Duration, pairWaitUntil time.Time, now time.Time) time.Duration {
	if d := pairWaitUntil.Sub(now); d > base {
		return d
	}
	return base
}

// pairReadMargin 配对宽限在码 TTL 外的余量（覆盖最后一次 NAK 往返与提交间隔）。
const pairReadMargin = 10 * time.Second

func (t *tcpSession) handle(pkt proto.Packet) error {
	if !t.helloDone {
		if pkt.Type != proto.TypeHello {
			t.srv.logf("%s 首包非 HELLO（%v），拒绝", t.conn.RemoteAddr(), pkt.Type)
			t.sendErr(ErrCodeAuth)
			return errClose
		}
		return t.handleHello(pkt)
	}
	switch pkt.Type {
	case proto.TypeHello, proto.TypeDiscoverResp:
		// 会话层报文不得出现在握手后的 TCP 流（协议约定其走 UDP）
		t.srv.logf("%s TCP 入站出现会话层报文 %v，拒绝", t.conn.RemoteAddr(), pkt.Type)
		t.sendErr(ErrCodeProto)
		return errClose
	}
	if pkt.Flags&proto.FlagAuth == 0 {
		if pkt.Type == proto.TypePairReq {
			return t.handlePairReq(pkt)
		}
		t.srv.logf("%s 未认证包 %v，拒绝", t.conn.RemoteAddr(), pkt.Type)
		t.sendErr(ErrCodeAuth)
		return errClose
	}
	client, tok, ok := t.authenticate(&pkt)
	if !ok {
		t.badAuth++
		t.srv.stats.HMACFail.Add(1)
		t.srv.vlogf("%s 认证失败（连续 %d/%d），丢弃", t.conn.RemoteAddr(), t.badAuth, MaxBadAuthStreak)
		if t.badAuth >= MaxBadAuthStreak {
			t.srv.logf("%s 连续 %d 次认证失败，断开", t.conn.RemoteAddr(), t.badAuth)
			t.sendErr(ErrCodeAuth)
			return errClose
		}
		return nil
	}
	t.badAuth = 0
	if t.bound() == "" {
		t.mu.Lock()
		t.boundID, t.tokCache = client.ID, tok
		t.mu.Unlock()
		t.srv.logf("客户端 %q（%s）经 %s 认证上线", client.Name, client.ID, t.conn.RemoteAddr())
	}
	return t.dispatch(pkt)
}

// authenticate：已绑定会话只验绑定 token（防客户端间连接复用）；未绑定时尝试全部已配对 token。
func (t *tcpSession) authenticate(pkt *proto.Packet) (pairing.Client, []byte, bool) {
	if tok := t.token(); tok != nil {
		if proto.VerifyHMAC(pkt, tok) {
			return pairing.Client{ID: t.bound()}, tok, true
		}
		return pairing.Client{}, nil, false
	}
	return t.srv.matchToken(pkt)
}

func (t *tcpSession) handleHello(pkt proto.Packet) error {
	if len(pkt.Payload) != 2 {
		t.sendErr(ErrCodeProto)
		return errClose
	}
	peerVer := binary.BigEndian.Uint16(pkt.Payload)
	agree := peerVer
	if proto.Ver < agree {
		agree = proto.Ver
	}
	if agree < proto.Ver {
		t.srv.logf("版本协商降级：对端 ver=%d，按 ver=%d 通信", peerVer, agree)
	}
	t.helloDone = true
	reply := proto.NewRaw(proto.TypeHello, t.nextSeq(), []byte{byte(agree >> 8), byte(agree)})
	return t.writePkt(reply)
}

// handlePairReq 处理配对路径（payload 三形态的文法见 pairing.ParsePairReqPayload）：
// 发起配对成功无响应，确认码仅在主机侧展示；4B 确认码 → PAIR_OK(封签新 token) / PAIR_NAK(1B reason)。
func (t *tcpSession) handlePairReq(pkt proto.Packet) error {
	name, devID, code, err := pairing.ParsePairReqPayload(pkt.Payload)
	if err != nil {
		if !errors.Is(err, pairing.ErrBadPairReq) { // 名字超长/指纹非法属手机端不应发出的违规
			t.srv.logf("%s PAIR_REQ payload 拒收: %v", t.conn.RemoteAddr(), err)
		}
		t.sendErr(ErrCodeProto)
		return errClose
	}
	if code == "" {
		if _, err := t.srv.pair.StartPairing(name, devID); err != nil {
			t.sendNak(nakReasonOf(err))
			return nil
		}
		// 读超时宽限：覆盖用户读码输码窗口
		t.pairWaitUntil = time.Now().Add(t.srv.pair.ActiveCodeTTL() + pairReadMargin)
		return nil // 发起成功不回包；确认码仅在主机侧展示
	}
	client, err := t.srv.pair.Verify(code)
	if err != nil {
		if !errors.Is(err, pairing.ErrWrongCode) {
			t.pairWaitUntil = time.Time{} // 轮次结束（过期/超限/无会话），收起宽限
		}
		t.sendNak(nakReasonOf(err))
		return nil
	}
	t.pairWaitUntil = time.Time{} // 配对成功，恢复正常读超时
	tok, err := client.Token()
	if err != nil {
		t.srv.logf("配对成功但 token 解码失败（%s）: %v", client.ID, err)
		t.sendNak(NakNoSession)
		return nil
	}
	t.srv.logf("客户端 %q（%s）配对成功", client.Name, client.ID)
	reply := proto.NewRaw(proto.TypePairOK, t.nextSeq(), tok)
	proto.Seal(&reply, tok) // PAIR_OK 必须用刚签发的 token 封签；PAIR_NAK 不封签
	return t.writePkt(reply)
}

func nakReasonOf(err error) uint8 {
	switch {
	case errors.Is(err, pairing.ErrWrongCode):
		return NakWrongCode
	case errors.Is(err, pairing.ErrExpired):
		return NakExpired
	case errors.Is(err, pairing.ErrTooManyAttempts):
		return NakTooMany
	case errors.Is(err, pairing.ErrClientsFull):
		return NakFull
	default:
		return NakNoSession
	}
}
