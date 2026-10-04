package session

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"padlink/daemon/internal/pairing"
	"padlink/daemon/internal/proto"
)

var (
	errClose    = errors.New("session: 关闭连接")
	errGraceful = errors.New("session: 对端 BYE")
)

// tcpSession 是一条已通过 HELLO 的 TCP 会话。
type tcpSession struct {
	srv  *Server
	conn net.Conn

	limiter *rateBucket

	helloDone bool
	boundID   string // 认证命中的客户端 id（"" 未绑定）
	tokCache  []byte // 绑定后复用 token，避免每包 hex 解码
	txSeq     uint16

	badAuth      int
	dropWinStart time.Time
	dropWinCount int

	keysDown map[uint16]struct{} // 已按下未释放的 HID usage
	btnsDown map[uint8]struct{}  // 已按下未释放的按钮号
}

func (s *Server) acceptLoop(ln net.Listener) {
	defer s.internalWG.Done()
	for {
		c, err := ln.Accept()
		if err != nil {
			select {
			case <-s.closed:
			default:
				s.logf("接受 TCP 连接失败: %v", err)
			}
			return
		}
		sess := &tcpSession{
			srv:      s,
			conn:     c,
			limiter:  newRateBucket(s.cfg.RatePerSec, s.cfg.RateBurst),
			keysDown: make(map[uint16]struct{}),
			btnsDown: make(map[uint8]struct{}),
		}
		s.mu.Lock()
		s.sessions[sess] = struct{}{}
		s.mu.Unlock()
		s.sessWG.Add(1)
		go sess.run()
	}
}

func (t *tcpSession) run() {
	defer t.finish()
	for {
		frame, err := t.readFrame()
		if err != nil {
			switch {
			case errors.Is(err, net.ErrClosed):
			case errors.Is(err, os.ErrDeadlineExceeded):
				t.srv.logf("会话 %s 读取超时（%v 无任何包），判失联断开", t.conn.RemoteAddr(), t.srv.cfg.ReadTimeout)
			default:
				t.srv.vlogf("会话 %s 结束: %v", t.conn.RemoteAddr(), err)
			}
			return
		}

		now := time.Now()
		if t.rateLimited(now) {
			t.sendErr(ErrCodeRate)
			return
		}
		pkt, err := proto.Decode(frame)
		if err != nil {
			t.srv.stats.Dropped.Add(1)
			t.srv.vlogf("丢弃坏包（%s）: %v", t.conn.RemoteAddr(), err)
			continue
		}
		t.srv.vlogf("TCP ← %s %v seq=%d auth=%v plen=%d", t.conn.RemoteAddr(), pkt.Type, pkt.Seq, pkt.Flags&proto.FlagAuth != 0, len(pkt.Payload))
		if err := t.handle(pkt); err != nil {
			if errors.Is(err, errGraceful) {
				t.srv.vlogf("会话 %s 对端 BYE，优雅关闭", t.conn.RemoteAddr())
			} else if !errors.Is(err, errClose) {
				t.srv.vlogf("会话 %s 处理异常: %v", t.conn.RemoteAddr(), err)
			}
			return
		}
	}
}

// readFrame 按头声明的 payload_len 精确读取一帧。
func (t *tcpSession) readFrame() ([]byte, error) {
	t.conn.SetReadDeadline(time.Now().Add(t.srv.cfg.ReadTimeout))
	var head [proto.HeaderSize]byte
	if _, err := io.ReadFull(t.conn, head[:]); err != nil {
		return nil, err
	}
	plen := int(binary.BigEndian.Uint16(head[7:9]))
	if plen > proto.MaxPayload {
		return nil, fmt.Errorf("payload_len=%d 超限", plen)
	}
	frame := make([]byte, 0, proto.HeaderSize+plen)
	frame = append(frame, head[:]...)
	if plen > 0 {
		payload := make([]byte, plen)
		if _, err := io.ReadFull(t.conn, payload); err != nil {
			return nil, err
		}
		frame = append(frame, payload...)
	}
	return frame, nil
}

// rateLimited 令牌桶超限时丢弃并计数；1s 窗口内丢弃数超上限返回 true（调用方 ERR+断连）。
func (t *tcpSession) rateLimited(now time.Time) bool {
	if t.limiter.allow(now) {
		return false
	}
	t.srv.stats.Dropped.Add(1)
	if t.dropWinStart.IsZero() || now.Sub(t.dropWinStart) > rateDropWindow {
		t.dropWinStart, t.dropWinCount = now, 0
	}
	t.dropWinCount++
	return t.dropWinCount > t.srv.cfg.RateDropMax
}

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
		// 会话层报文不允许出现在握手之后的 TCP 流（协议约定其走 UDP）
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
	if t.boundID == "" {
		t.boundID, t.tokCache = client.ID, tok
		t.srv.logf("客户端 %q（%s）经 %s 认证上线", client.Name, client.ID, t.conn.RemoteAddr())
	}
	return t.dispatch(pkt)
}

// authenticate：已绑定会话只验绑定 token；未绑定时逐个尝试全部已配对 token。
func (t *tcpSession) authenticate(pkt *proto.Packet) (pairing.Client, []byte, bool) {
	if t.tokCache != nil {
		if proto.VerifyHMAC(pkt, t.tokCache) {
			return pairing.Client{ID: t.boundID}, t.tokCache, true
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

// handlePairReq 处理配对路径（协议约定见 daemon README）：
// payload 空(0B)或 JSON={"name"}（≤64B）= 发起配对（成功无响应，确认码在主机侧展示）；
// payload 4B ASCII 数字 = 确认码尝试 → PAIR_OK(封签新 token) / PAIR_NAK(1B reason)。
func (t *tcpSession) handlePairReq(pkt proto.Packet) error {
	if len(pkt.Payload) == 0 || pkt.Payload[0] == '{' {
		name := ""
		if len(pkt.Payload) > 0 {
			var meta struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(pkt.Payload, &meta); err != nil || len(meta.Name) > pairing.MaxNameLen {
				t.sendErr(ErrCodeProto)
				return errClose
			}
			name = meta.Name
		}
		if _, err := t.srv.pair.StartPairing(name); err != nil {
			t.sendNak(nakReasonOf(err))
			return nil
		}
		return nil // 发起成功不回包；确认码经 notify-send / padlinkctl pair 展示
	}
	if len(pkt.Payload) == pairing.CodeLen && isDigits(pkt.Payload) {
		client, err := t.srv.pair.Verify(string(pkt.Payload))
		if err != nil {
			t.sendNak(nakReasonOf(err))
			return nil
		}
		tok, err := client.Token()
		if err != nil {
			t.srv.logf("配对成功但 token 解码失败（%s）: %v", client.ID, err)
			t.sendNak(NakNoSession)
			return nil
		}
		t.srv.logf("客户端 %q（%s）配对成功", client.Name, client.ID)
		reply := proto.NewRaw(proto.TypePairOK, t.nextSeq(), tok)
		proto.Seal(&reply, tok) // 用刚签发的 token 封签，便于手机端到端自证
		return t.writePkt(reply)
	}
	t.sendErr(ErrCodeProto)
	return errClose
}

func (t *tcpSession) dispatch(pkt proto.Packet) error {
	switch pkt.Type {
	case proto.TypeMove:
		dx, dy := pkt.Move.DX, pkt.Move.DY
		t.enqueue(t.srv.doInj("MOVE", func() error { return t.srv.inj.Move(dx, dy) }))
	case proto.TypeScroll:
		dy := pkt.Scroll.DyHiRes
		t.enqueue(t.srv.doInj("SCROLL", func() error { return t.srv.inj.Scroll(dy) }))
	case proto.TypeButton:
		if pkt.Button.Down {
			t.btnsDown[pkt.Button.Btn] = struct{}{}
		} else {
			delete(t.btnsDown, pkt.Button.Btn)
		}
		btn, down := pkt.Button.Btn, pkt.Button.Down
		t.enqueue(t.srv.doInj(fmt.Sprintf("BUTTON btn=%d", btn), func() error { return t.srv.inj.Button(btn, down) }))
	case proto.TypeKey:
		if pkt.Key.Down {
			t.keysDown[pkt.Key.HIDUsage] = struct{}{}
		} else {
			delete(t.keysDown, pkt.Key.HIDUsage)
		}
		hid, down := pkt.Key.HIDUsage, pkt.Key.Down
		t.enqueue(t.srv.doInj(fmt.Sprintf("KEY hid=0x%04X", hid), func() error { return t.srv.inj.Key(hid, down) }))
	case proto.TypeText:
		fn := t.srv.onText
		if fn == nil {
			t.srv.stats.Dropped.Add(1)
			t.srv.vlogf("TEXT 到达但文本注入未配置，丢弃（长度 %d）", len(pkt.Text))
			return nil
		}
		text := pkt.Text
		t.enqueue(t.srv.doInj("TEXT", func() error { return fn(text) }))
	case proto.TypeEcho:
		reply := proto.NewEcho(t.nextSeq(), pkt.Echo.TsMs)
		proto.Seal(&reply, t.tokCache)
		return t.writePkt(reply)
	case proto.TypeBye:
		return errGraceful
	default:
		// PAIR_OK/PAIR_NAK/ERR 不应入站
		t.srv.stats.Dropped.Add(1)
	}
	return nil
}

// finish 会话收尾：补发本会话期间按下未释放的按键与按钮 up
// （PRD §11.2 零残留的服务端兜底），再注销并关连接。
func (t *tcpSession) finish() {
	keys := make([]uint16, 0, len(t.keysDown))
	for hid := range t.keysDown {
		keys = append(keys, hid)
	}
	btns := make([]uint8, 0, len(t.btnsDown))
	for b := range t.btnsDown {
		btns = append(btns, b)
	}
	if len(keys) > 0 || len(btns) > 0 {
		srv := t.srv
		srv.enqueueBlocking(func() {
			for _, hid := range keys {
				if err := srv.inj.Key(hid, false); err != nil {
					srv.logf("残留按键释放失败 hid=0x%04X: %v", hid, err)
				}
			}
			for _, b := range btns {
				if err := srv.inj.Button(b, false); err != nil {
					srv.logf("残留按钮释放失败 btn=%d: %v", b, err)
				}
			}
			srv.logf("会话结束兜底：补发 %d 个按键 / %d 个按钮 up", len(keys), len(btns))
		})
	}
	t.srv.mu.Lock()
	delete(t.srv.sessions, t)
	t.srv.mu.Unlock()
	t.conn.Close()
	t.srv.sessWG.Done()
}

func (t *tcpSession) nextSeq() uint16 {
	t.txSeq++
	return t.txSeq
}

func (t *tcpSession) writePkt(pkt proto.Packet) error {
	buf, err := proto.Encode(&pkt)
	if err != nil {
		return err
	}
	t.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	_, err = t.conn.Write(buf)
	return err
}

func (t *tcpSession) sendErr(code uint8) {
	_ = t.writePkt(proto.NewRaw(proto.TypeErr, t.nextSeq(), []byte{code}))
}

func (t *tcpSession) sendNak(reason uint8) {
	_ = t.writePkt(proto.NewRaw(proto.TypePairNak, t.nextSeq(), []byte{reason}))
}

func (t *tcpSession) enqueue(f func()) {
	t.srv.enqueueBlocking(f)
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

func isDigits(b []byte) bool {
	for _, c := range b {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
