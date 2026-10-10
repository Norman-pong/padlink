package session

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
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

	// mu 保护跨 goroutine 访问的会话态：boundID/tokCache/txSeq/keysDown/btnsDown
	// （Server.OnlineClients、控制权裁决、NOTICE 下发会从其他 goroutine 读取）。
	// 锁序约束：持 mu 时禁止再取 Server.mu（Server.mu → mu 是允许方向）。
	mu sync.Mutex
	// wmu 串行化 conn 写：会话 goroutine 与 NOTICE 下发方都会写同一连接。
	wmu sync.Mutex

	helloDone bool
	boundID   string // 认证命中的客户端 id（"" 未绑定）
	tokCache  []byte // 绑定后复用 token，避免每包 hex 解码
	txSeq     uint16

	badAuth      int
	dropWinStart time.Time
	dropWinCount int

	// pairWaitUntil 配对进行中的读超时宽限截止（零值=无宽限）。
	// 用户在手机端读码输码合法地超过默认 10s 空闲判失联（macOS E2E 实测踩中），
	// 宽限覆盖码 TTL + 提交余量，轮次结束（成功/过期/超限）即收起。
	pairWaitUntil time.Time

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
	t.conn.SetReadDeadline(time.Now().Add(effectiveReadTimeout(t.srv.cfg.ReadTimeout, t.pairWaitUntil, time.Now())))
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

// effectiveReadTimeout 配对宽限期内的读超时取两者较大者。
func effectiveReadTimeout(base time.Duration, pairWaitUntil time.Time, now time.Time) time.Duration {
	if d := pairWaitUntil.Sub(now); d > base {
		return d
	}
	return base
}

// pairReadMargin 配对宽限在码 TTL 外的余量（覆盖最后一次 NAK 往返与用户提交间隔）。
const pairReadMargin = 10 * time.Second

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
	if t.bound() == "" {
		t.mu.Lock()
		t.boundID, t.tokCache = client.ID, tok
		t.mu.Unlock()
		t.srv.logf("客户端 %q（%s）经 %s 认证上线", client.Name, client.ID, t.conn.RemoteAddr())
	}
	return t.dispatch(pkt)
}

// authenticate：已绑定会话只验绑定 token；未绑定时逐个尝试全部已配对 token。
func (t *tcpSession) authenticate(pkt *proto.Packet) (pairing.Client, []byte, bool) {
	if tok := t.token(); tok != nil {
		if proto.VerifyHMAC(pkt, tok) {
			return pairing.Client{ID: t.bound()}, tok, true
		}
		return pairing.Client{}, nil, false
	}
	return t.srv.matchToken(pkt)
}

// bound 返回认证命中的客户端 id（"" 未绑定）。
func (t *tcpSession) bound() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.boundID
}

// token 返回绑定 token（未绑定为 nil）；仅作只读使用。
func (t *tcpSession) token() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tokCache
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
// payload 空(0B)或 JSON={"name":<设备名>,"dev":<设备指纹>}（≤64B name / ≤32B dev）= 发起配对
// （成功无响应，确认码在主机侧展示）；
// payload 4B ASCII 数字 = 确认码尝试 → PAIR_OK(封签新 token) / PAIR_NAK(1B reason)。
func (t *tcpSession) handlePairReq(pkt proto.Packet) error {
	if len(pkt.Payload) == 0 || pkt.Payload[0] == '{' {
		name, devID := "", ""
		if len(pkt.Payload) > 0 {
			var meta struct {
				Name string `json:"name"`
				Dev  string `json:"dev"`
			}
			if err := json.Unmarshal(pkt.Payload, &meta); err != nil || len(meta.Name) > pairing.MaxNameLen {
				t.sendErr(ErrCodeProto)
				return errClose
			}
			if !pairing.ValidDevID(meta.Dev) {
				// 指纹非法属协议违规：与名字超长同口径拒收（手机端不应发出）
				t.srv.logf("%s PAIR_REQ 设备指纹非法（%dB），拒收", t.conn.RemoteAddr(), len(meta.Dev))
				t.sendErr(ErrCodeProto)
				return errClose
			}
			name, devID = meta.Name, meta.Dev
		}
		if _, err := t.srv.pair.StartPairing(name, devID); err != nil {
			t.sendNak(nakReasonOf(err))
			return nil
		}
		// 读超时宽限：用户读码输码合法地超过 10s 默认空闲判失联
		t.pairWaitUntil = time.Now().Add(t.srv.pair.ActiveCodeTTL() + pairReadMargin)
		return nil // 发起成功不回包；确认码经桌面通知 / padlinkctl pair 展示
	}
	if len(pkt.Payload) == pairing.CodeLen && isDigits(pkt.Payload) {
		client, err := t.srv.pair.Verify(string(pkt.Payload))
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
		proto.Seal(&reply, tok) // 用刚签发的 token 封签，便于手机端到端自证
		return t.writePkt(reply)
	}
	t.sendErr(ErrCodeProto)
	return errClose
}

func (t *tcpSession) dispatch(pkt proto.Packet) error {
	// 多设备控制权：非持权设备在他人操作期间的控制事件一律丢弃（含 NOTICE 提示），
	// 冷静止期判定见 controlArbiter；ECHO/BYE 等非控制事件不受门控。
	if isControlType(pkt.Type) && !t.srv.admitControl(t.bound()) {
		return nil
	}
	switch pkt.Type {
	case proto.TypeMove:
		dx, dy := pkt.Move.DX, pkt.Move.DY
		t.enqueue(t.srv.doInj("MOVE", func() error { return t.srv.inj.Move(dx, dy) }))
	case proto.TypeScroll:
		dy := pkt.Scroll.DyHiRes
		t.enqueue(t.srv.doInj("SCROLL", func() error { return t.srv.inj.Scroll(dy) }))
	case proto.TypeButton:
		t.trackButton(pkt.Button.Btn, pkt.Button.Down)
		btn, down := pkt.Button.Btn, pkt.Button.Down
		t.enqueue(t.srv.doInj(fmt.Sprintf("BUTTON btn=%d", btn), func() error { return t.srv.inj.Button(btn, down) }))
	case proto.TypeKey:
		t.trackKey(pkt.Key.HIDUsage, pkt.Key.Down)
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
	keys, btns := t.takeHeld()
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
	lastOfClient := t.boundID != "" && !t.srv.hasSessionLocked(t.boundID)
	t.srv.mu.Unlock()
	if lastOfClient {
		t.srv.arb.release(t.boundID) // 该客户端已无会话：控制权释放给其他设备
	}
	t.conn.Close()
	t.srv.sessWG.Done()
}

// trackButton/trackKey 记录按住状态，并在"按住数 0↔非0"翻转时同步给控制权裁决器
// （按住的按键/按钮同样算"在操作"，见 controlArbiter.primaryActiveLocked）。
func (t *tcpSession) trackButton(btn uint8, down bool) {
	t.mu.Lock()
	before := len(t.keysDown) + len(t.btnsDown)
	if down {
		t.btnsDown[btn] = struct{}{}
	} else {
		delete(t.btnsDown, btn)
	}
	after := len(t.keysDown) + len(t.btnsDown)
	id := t.boundID
	t.mu.Unlock()
	t.syncHeld(id, before, after)
}

func (t *tcpSession) trackKey(hid uint16, down bool) {
	t.mu.Lock()
	before := len(t.keysDown) + len(t.btnsDown)
	if down {
		t.keysDown[hid] = struct{}{}
	} else {
		delete(t.keysDown, hid)
	}
	after := len(t.keysDown) + len(t.btnsDown)
	id := t.boundID
	t.mu.Unlock()
	t.syncHeld(id, before, after)
}

func (t *tcpSession) syncHeld(id string, before, after int) {
	if (before == 0) != (after == 0) {
		t.srv.arb.setHeld(id, after > 0)
	}
}

// takeHeld 取出并清空按住集合（会话收尾兜底释放 / 被拒设备残留释放用）。
func (t *tcpSession) takeHeld() ([]uint16, []uint8) {
	t.mu.Lock()
	keys := make([]uint16, 0, len(t.keysDown))
	for hid := range t.keysDown {
		keys = append(keys, hid)
	}
	btns := make([]uint8, 0, len(t.btnsDown))
	for b := range t.btnsDown {
		btns = append(btns, b)
	}
	t.keysDown = make(map[uint16]struct{})
	t.btnsDown = make(map[uint8]struct{})
	id := t.boundID
	t.mu.Unlock()
	t.srv.arb.setHeld(id, false)
	return keys, btns
}

// hasSessionLocked 报告是否还有其他会话绑定该客户端（调用方须持 Server.mu）。
func (s *Server) hasSessionLocked(clientID string) bool {
	for sess := range s.sessions {
		if sess.bound() == clientID {
			return true
		}
	}
	return false
}

func (t *tcpSession) nextSeq() uint16 {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.txSeq++
	return t.txSeq
}

func (t *tcpSession) writePkt(pkt proto.Packet) error {
	buf, err := proto.Encode(&pkt)
	if err != nil {
		return err
	}
	t.wmu.Lock()
	defer t.wmu.Unlock()
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
